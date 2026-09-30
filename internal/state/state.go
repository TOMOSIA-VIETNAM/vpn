// Package state persists the running tunnel's status to disk so `status`,
// `disconnect`, and `repair` can inspect or act on a connection started by a
// different process invocation (connect runs in the foreground/daemonized;
// these commands are separate CLI invocations).
package state

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"vpn/internal/privilege"
)

// Phase mirrors the engine's coarse connection lifecycle, independent of the
// fine-grained IKE/PPP sub-state-machines.
type Phase string

const (
	PhaseDisconnected Phase = "DISCONNECTED"
	PhaseConnecting   Phase = "CONNECTING"
	PhaseConnected    Phase = "CONNECTED"
	PhaseFailed       Phase = "FAILED"
)

// State is the on-disk snapshot of the running (or last) connection.
type State struct {
	Phase       Phase     `json:"phase"`
	Profile     string    `json:"profile,omitempty"`
	Account     string    `json:"account,omitempty"`
	Server      string    `json:"server,omitempty"`
	PID         int       `json:"pid,omitempty"`
	TunDevice   string    `json:"tun_device,omitempty"`
	LocalIP     string    `json:"local_ip,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
	FailStage   string    `json:"fail_stage,omitempty"`
	FailDetail  string    `json:"fail_detail,omitempty"`
	SavedRoutes bool      `json:"saved_routes"` // true once original routing/DNS captured for repair/restore

	// Reconnecting is set (with Phase CONNECTING) while the daemon is
	// re-establishing a tunnel it lost, on its own — the UI must not treat it
	// as a failure or start a competing connect. Reconnects counts how many
	// times this daemon has had to do so; FailDetail then holds the reason
	// for the most recent drop or failed attempt.
	Reconnecting bool `json:"reconnecting,omitempty"`
	Reconnects   int  `json:"reconnects,omitempty"`

	// DNS snapshot, captured before Apply so disconnect/repair can restore
	// it even if that's a different process invocation than the one that
	// connected (e.g. after a crash — see dnsmgr.Snapshot). DNSApplied
	// distinguishes "no DNS servers were pushed" (nothing to restore) from
	// "the original config was itself empty/DHCP" (restore to Empty).
	DNSService string   `json:"dns_service,omitempty"`
	DNSServers []string `json:"dns_servers,omitempty"`
	DNSApplied bool     `json:"dns_applied,omitempty"`

	// Warnings are privacy caveats about the live connection (e.g. the LNS
	// pushed no DNS servers) that connect and status surface to the user.
	Warnings []string `json:"warnings,omitempty"`
}

// Dir is where the state file lives — exported so `uninstall` can remove it
// without needing its own copy of the path.
//
// A variable only so tests can point it at a temporary directory.
var Dir = "/var/run/vpn"

// ownerFile is where ownerUID reads the installing user's uid; a variable for the same reason.
var ownerFile = privilege.OwnerFile

// path is where the state file lives, without creating anything — used by
// Load, which must work read-only and unprivileged (e.g. plain `vpn
// status`, before this user has ever connected and root has never had a
// reason to create Dir yet). The installing user owns this file, so what it
// says is for display and for that user's own commands only: see LoadTrusted.
func path() string {
	return filepath.Join(Dir, "state.json")
}

// Load reads the current state, returning a DISCONNECTED state if no file
// (or not even Dir itself) exists yet — nothing has ever connected, and
// this must not try to create Dir itself: Dir lives under /var/run, so
// only Save (always called while privilege.Elevate has this process at
// root — see connect/disconnect/repair/uninstall) is allowed to create it.
func Load() (*State, error) {
	data, err := os.ReadFile(path())
	if os.IsNotExist(err) {
		return &State{Phase: PhaseDisconnected}, nil
	}
	if err != nil {
		return nil, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// trustedPath is the root-only copy of the state file: the one LoadTrusted
// reads, and the only one privileged code may act on.
func trustedPath() string {
	return filepath.Join(Dir, "state.root.json")
}

// trustedOwner is the uid a file must belong to for LoadTrusted to believe
// it: root. A variable only so tests, which do not run as root, can write one.
var trustedOwner uint32

// LoadTrusted reads the state that privileged code acts on: the PID that
// disconnect signals as root, and the server and DNS service whose routes and
// resolvers disconnect and repair restore. state.json belongs to the
// installing user, so any process of theirs can rewrite it; this reads the
// root-only copy instead. Call it as root (inside privilege.Elevate).
//
// A daemon from a release that writes no root-only copy keeps running across
// `vpn update`, and its state.json is root-owned: that file is accepted too,
// but only while root owns it and nobody else can write it. With neither file
// trustworthy the state is DISCONNECTED.
func LoadTrusted() (*State, error) {
	for _, p := range []string{trustedPath(), path()} {
		data, ok, err := readRootOwned(p)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		var s State
		if err := json.Unmarshal(data, &s); err != nil {
			return nil, err
		}
		return &s, nil
	}
	return &State{Phase: PhaseDisconnected}, nil
}

// readRootOwned returns p's contents, or ok=false when p does not exist or is
// not a regular file owned by trustedOwner that only its owner can write. The
// check is on the open descriptor, so the file read is the file checked.
func readRootOwned(p string) (data []byte, ok bool, err error) {
	f, err := os.Open(p)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, false, err
	}
	st, isStat := fi.Sys().(*syscall.Stat_t)
	if !isStat || !fi.Mode().IsRegular() || st.Uid != trustedOwner || fi.Mode().Perm()&0o022 != 0 {
		return nil, false, nil
	}
	data, err = io.ReadAll(f)
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

// Save persists s atomically. Callers must already be root (see
// privilege.Elevate) — /var/run/vpn is root-owned, and this is what
// creates it on first use.
func (s *State) Save() error {
	if err := os.MkdirAll(Dir, 0o755); err != nil {
		return err
	}
	_ = os.Chmod(Dir, 0o755)
	s.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	// The root-only copy goes first, so a PID visible in state.json (see
	// engine.ClaimNewConnect) is always one a disconnect can already find.
	if err := writeAtomic(trustedPath(), data, 0o600, -1); err != nil {
		return err
	}
	// Readable by the menu bar app, which polls this file as the unprivileged
	// installing user, and by nobody else: the profile, account and server
	// names are nobody else's business on a shared Mac. Without an owner file
	// (a plain `sudo vpn` run) fall back to world-readable so the app still works.
	mode, owner := os.FileMode(0o644), -1
	if uid, ok := ownerUID(); ok {
		mode, owner = 0o600, uid
	}
	return writeAtomic(path(), data, mode, owner)
}

// writeAtomic replaces p with data through a temporary file, with exactly
// mode and, when owner >= 0, that owner.
func writeAtomic(p string, data []byte, mode os.FileMode, owner int) error {
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil { // WriteFile's mode is masked by umask and ignored for an existing file
		return err
	}
	if owner >= 0 {
		if err := os.Chown(tmp, owner, -1); err != nil {
			_ = os.Remove(tmp)
			return err
		}
	}
	return os.Rename(tmp, p)
}

// ownerUID is the uid of whoever installed vpn (see privilege.OwnerFile).
func ownerUID() (int, bool) {
	data, err := os.ReadFile(ownerFile)
	if err != nil {
		return 0, false
	}
	uid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || uid < 0 {
		return 0, false
	}
	return uid, true
}

// Clear resets to DISCONNECTED, used once disconnect/repair has actually
// torn everything down.
func Clear() error {
	s := &State{Phase: PhaseDisconnected}
	return s.Save()
}
