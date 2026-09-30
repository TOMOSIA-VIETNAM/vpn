package cli

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"vpn/internal/privilege"
	"vpn/internal/release"
)

// releaseAssetBaseURL is where CI publishes prebuilt binaries plus the
// signed manifest — see .github/workflows/release.yml and internal/release.
const releaseAssetBaseURL = "https://github.com/TOMOSIA-VIETNAM/vpn/releases/latest/download/"

const (
	installPath = "/usr/local/bin/vpn"
	// stagingDir is root-owned and not writable by anyone else, so the
	// candidate binary run for its `version` sanity check below cannot be
	// swapped by an unprivileged process between that check and install.
	stagingDir = "/var/run/vpn/update"
	// maxDownload caps every fetched asset; a release binary is ~5 MB.
	maxDownload = 64 << 20
)

func cmdUpdate(args []string) error {
	fs := newFlagSet("update")
	force := fs.Bool("force", false, "install even if the release is not newer than this binary")
	if err := fs.Parse(args); err != nil {
		return err
	}

	arch := runtime.GOARCH
	if arch != "arm64" && arch != "amd64" {
		return fmt.Errorf("no prebuilt release for GOARCH=%s", arch)
	}
	asset := "vpn-darwin-" + arch

	bin, version, err := downloadVerified(releaseAssetBaseURL, asset, *force)
	if errors.Is(err, errUpToDate) {
		fmt.Printf("Already up to date (%s).\n", Version)
		return nil
	}
	if err != nil {
		return err
	}
	fmt.Printf("Downloaded and verified %s (%s)...\n", asset, version)

	staged := filepath.Join(stagingDir, asset)
	defer func() { _ = privilege.Elevate(func() error { return os.RemoveAll(stagingDir) }) }()
	if err := privilege.Elevate(func() error {
		if err := os.MkdirAll(stagingDir, 0o755); err != nil {
			return err
		}
		return writeRootFile(staged, bin, 0o755)
	}); err != nil {
		return fmt.Errorf("stage downloaded binary: %w", err)
	}
	// Run unprivileged: it only has to prove the binary starts on this Mac.
	if out, err := exec.Command(staged, "version").Output(); err != nil {
		return fmt.Errorf("downloaded binary doesn't run: %w", err)
	} else if got := strings.TrimSpace(string(out)); got != "vpn "+version {
		return fmt.Errorf("downloaded binary reports %q, but the signed manifest says %s", got, version)
	}

	if err := privilege.Elevate(func() error {
		tmp := installPath + ".new"
		if err := writeRootFile(tmp, bin, 0o4755); err != nil { // setuid + rwxr-xr-x
			return err
		}
		return os.Rename(tmp, installPath)
	}); err != nil {
		return fmt.Errorf("installing new build to %s failed: %w", installPath, err)
	}

	fmt.Printf("Updated to %s.\n", version)
	if _, ok := installedApp(); ok {
		fmt.Println("This updates the CLI only — to update the TOMOSIA VPN menu bar app too, re-run install.sh.")
	}
	return nil
}

// menuBarApps are where install.sh puts the menu bar app, current name first, then the
// names used by earlier releases (still present on Macs not reinstalled since).
var menuBarApps = []string{"/Applications/TOMOSIA VPN.app", "/Applications/TOMOSIA-VPN.app", "/Applications/TMS VPN.app"}

// installedApp returns the path of the installed menu bar app, if any.
func installedApp() (string, bool) {
	for _, app := range menuBarApps {
		if _, err := os.Stat(app); err == nil {
			return app, true
		}
	}
	return "", false
}

// downloadVerified fetches the signed manifest, its signature and the asset
// from baseURL, returning the asset only once the signature, version order
// and SHA-256 all check out. Everything stays in memory: nothing an
// unprivileged process could modify is ever read back by the privileged
// install step.
func downloadVerified(baseURL, asset string, force bool) (bin []byte, version string, err error) {
	manifestBytes, err := fetch(baseURL + release.ManifestName)
	if err != nil {
		return nil, "", err
	}
	sig, err := fetch(baseURL + release.SignatureName)
	if err != nil {
		return nil, "", err
	}
	manifest, err := verifyManifest(manifestBytes, sig)
	if err != nil {
		return nil, "", err
	}
	if err := checkNewer(manifest.Version, Version, force); err != nil {
		return nil, "", err
	}
	bin, err = fetch(baseURL + asset)
	if err != nil {
		return nil, "", err
	}
	if err := manifest.CheckAsset(asset, bin); err != nil {
		return nil, "", err
	}
	return bin, manifest.Version, nil
}

// errUpToDate is checkNewer's "nothing to do" outcome — not a failure, so
// cmdUpdate reports it and exits 0.
var errUpToDate = errors.New("already up to date")

// checkNewer refuses a downgrade or reinstall unless forced: without it, a
// party able to serve release assets could replay an older, genuinely
// signed release that has a known vulnerability. A current build that is
// not a release tag (a dev build) cannot be ordered, so it always updates.
func checkNewer(candidate, current string, force bool) error {
	next, ok := release.ParseSemver(candidate)
	if !ok {
		return fmt.Errorf("signed release has non-semver version %q", candidate)
	}
	cur, ok := release.ParseSemver(current)
	if !ok || force {
		return nil
	}
	switch next.Compare(cur) {
	case 0:
		return errUpToDate
	case -1:
		return fmt.Errorf("latest release %s is older than this binary (%s) — refusing to downgrade (use --force to override)", candidate, current)
	}
	return nil
}

var httpClient = &http.Client{Timeout: 2 * time.Minute}

// verifyManifest is release.Verify, swappable only so tests can sign with a
// throwaway key instead of the real release key.
var verifyManifest = release.Verify

func fetch(url string) ([]byte, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDownload+1))
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	if len(data) > maxDownload {
		return nil, fmt.Errorf("download %s: larger than %d bytes", url, maxDownload)
	}
	return data, nil
}

// writeRootFile creates path fresh (never following or reusing whatever is
// already there — /usr/local/bin is user-writable on Intel Homebrew Macs)
// and sets owner root:wheel and mode on the open descriptor, so the bits
// land on exactly the file this call wrote. Must run under
// privilege.Elevate.
//
// mode uses raw chmod(2) bits: os.FileMode's setuid is a distinct high bit,
// so os.Chmod(path, 0o4755) would silently NOT set setuid.
func writeRootFile(path string, data []byte, mode uint32) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	fd, err := syscall.Open(path, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0o700)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	f := os.NewFile(uintptr(fd), path)
	fail := func(err error) error {
		f.Close()
		os.Remove(path)
		return err
	}
	if _, err := f.Write(data); err != nil {
		return fail(err)
	}
	if err := syscall.Fchown(fd, 0, 0); err != nil {
		return fail(fmt.Errorf("chown root:wheel: %w", err))
	}
	// After Fchown: chown(2) clears setuid, so the mode must come last.
	if err := syscall.Fchmod(fd, mode); err != nil {
		return fail(fmt.Errorf("chmod %o: %w", mode, err))
	}
	if err := f.Sync(); err != nil {
		return fail(err)
	}
	return f.Close()
}
