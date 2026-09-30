// Package privilege implements the standard setuid-root safety pattern:
// drop to the real (invoking) user by default, and only regain root for
// the specific, minimal operations that actually need it (opening a utun
// device, binding UDP/500, running route/ifconfig/networksetup) — never
// leaving the process privileged longer than one such operation needs.
//
// This is what makes it safe to install this binary setuid-root so `vpn
// connect` etc. don't need `sudo` on every invocation: without dropping by
// default, every subcommand (including ones that just read local config,
// like `profile add`) would run fully as root for its whole lifetime,
// which is unnecessary privilege the process doesn't need and shouldn't
// hold. macOS credentials are process-wide (unlike Linux, where a raw
// setuid(2) call only affects the calling OS thread — see
// https://github.com/golang/go/issues/1435 — that bug does not apply
// here), so a plain syscall.Seteuid from any goroutine correctly changes
// privilege for the whole process on Darwin.
package privilege

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

// realUID is the actual invoking user, captured once at process start
// before Drop ever runs — this is what Elevate restores to afterward.
var realUID = os.Getuid()

// OwnerFile holds the uid of whoever ran install.sh, root-owned 0600 —
// CheckOwner reads it (not a baked-in build-time constant) so the exact
// same binary can be shared/downloaded across machines and users: a
// prebuilt release binary can't have any one person's uid compiled into
// it, since the CI building it doesn't know who'll install it. install.sh
// writes this file as the very last step of installing; vpn uninstall
// removes it.
const OwnerFile = "/etc/vpn-owner-uid"

// CheckOwner enforces that only the user who installed this binary (i.e.
// ran install.sh) can run it at all. A setuid-root binary with no OwnerFile
// is refused rather than let through: that state means an incomplete or
// tampered install, and treating it as "no restriction" would hand root
// capabilities to every local account. This exists because installing setuid-root makes the binary
// executable (and, without this check, root-capable) for *every* local
// account on the machine, not just the person who ran install.sh — call
// this as the very first thing in main(), before Drop or any subcommand
// dispatch, so a different user can't invoke this binary at all. Enforce it
// only when this executable is actually setuid-root (euid 0, real uid
// non-root). A downloaded candidate binary is deliberately run as an
// ordinary user for `vpn version` before installation; it cannot read the
// root-only OwnerFile and has no privilege to protect, so rejecting it would
// break installer/update validation on a machine with vpn already installed.
func CheckOwner() error {
	if os.Geteuid() != 0 || realUID == 0 {
		return nil
	}
	data, err := os.ReadFile(OwnerFile)
	if os.IsNotExist(err) {
		return fmt.Errorf("%s is missing, so this setuid-root vpn binary cannot tell who installed it — reinstall with install.sh", OwnerFile)
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", OwnerFile, err)
	}
	want, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || want < 0 {
		if err == nil {
			err = fmt.Errorf("UID must not be negative")
		}
		return fmt.Errorf("invalid contents of %s: %w", OwnerFile, err)
	}
	if realUID != want {
		return fmt.Errorf("this vpn binary was installed by a different user (uid %d) — only that user can run it; ask them to run it, or reinstall it yourself with ./install.sh", want)
	}
	return nil
}

// ErrNotPrivileged is what Elevate wraps when this process simply cannot become
// root (not setuid-root, not run with sudo) — as opposed to an I/O failure in
// whatever it was going to do as root. Callers that can do without root test for
// it with errors.Is.
var ErrNotPrivileged = errors.New("cannot become root")

// mu serializes Elevate calls: this process is single-purpose (one
// connect/disconnect/repair per invocation), so there's no legitimate case
// for two goroutines needing root at once, and serializing avoids a window
// where one goroutine's Elevate...defer-drop races another's.
var mu sync.Mutex

// Drop lowers the effective UID to the real invoking user. Call this once,
// as the very first thing in main(), before any other code runs. If the
// binary isn't setuid-root (a plain `sudo vpn ...` invocation, or a
// non-root build run without install.sh's setuid step), this is a no-op:
// realUID is already 0 or already equals the effective UID.
func Drop() {
	if os.Geteuid() == 0 && realUID != 0 {
		_ = syscall.Seteuid(realUID)
	}
}

// Elevate runs fn with the effective UID raised to root, then always drops
// back to the real user before returning — even if fn panics or errors.
// Callers must keep fn's body to exactly the operations that need root;
// nothing else should run inside it.
func Elevate(fn func() error) error {
	mu.Lock()
	defer mu.Unlock()

	if os.Getuid() == 0 {
		// Invoked directly as root (e.g. a root login shell, not via
		// setuid) — already privileged, nothing to raise or restore.
		return fn()
	}
	if err := syscall.Seteuid(0); err != nil {
		return fmt.Errorf("this operation needs root — install with the setuid step (see install.sh) or run with sudo: %w: %w", ErrNotPrivileged, err)
	}
	defer func() { _ = syscall.Seteuid(realUID) }()
	return fn()
}

// safePath is the only PATH a setuid-root run keeps. Every tool this client
// runs is invoked by absolute path (see internal/sysbin), so nothing should
// consult it; it is set anyway so a tool that spawns another does not search
// directories the invoking user controls.
const safePath = "/usr/bin:/bin:/usr/sbin:/sbin"

// keepEnv are the only variables a setuid-root run passes on. HOME locates the
// invoking user's config, the rest only affect message language and terminal
// output, the proxy variables keep `vpn update` working behind a proxy, and VPN_DAEMON_CHILD marks the re-exec'd background connect (see
// cli.startDaemon).
var keepEnv = map[string]bool{
	"HOME": true, "USER": true, "LOGNAME": true, "TERM": true, "TMPDIR": true,
	"LANG": true, "VPN_DAEMON_CHILD": true,
	// Set by macOS for every GUI-launched process; the `security` tool reads the
	// user's text encoding from it.
	"__CF_USER_TEXT_ENCODING": true,
	// `vpn update` and the app's downloads honour the user's proxy settings.
	"HTTP_PROXY": true, "HTTPS_PROXY": true, "ALL_PROXY": true, "NO_PROXY": true,
	"http_proxy": true, "https_proxy": true, "all_proxy": true, "no_proxy": true,
}

// sanitizeEnv returns env reduced to keepEnv (plus every LC_* locale
// variable) and a fixed PATH.
func sanitizeEnv(env []string) []string {
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if keepEnv[name] || strings.HasPrefix(name, "LC_") {
			out = append(out, kv)
		}
	}
	return append(out, "PATH="+safePath)
}

// SanitizeEnv strips the environment of a setuid-root process to the small
// allowlist above. The invoking user controls the whole environment, and this
// process runs `route`, `scutil`, `networksetup` and friends with effective
// root: without this, whatever the user exported (DYLD_*, malloc and locale
// debugging switches, a hostile PATH) reaches
// them. It is a no-op unless this executable really is setuid-root, so
// `sudo vpn`, plain user runs and tests keep their environment. Call it right
// after CheckOwner, before anything spawns a child.
func SanitizeEnv() {
	if os.Geteuid() != 0 || realUID == 0 {
		return
	}
	env := sanitizeEnv(os.Environ())
	os.Clearenv()
	for _, kv := range env {
		if name, val, ok := strings.Cut(kv, "="); ok {
			_ = os.Setenv(name, val)
		}
	}
}
