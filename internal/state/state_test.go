package state

import (
	"os"
	"path/filepath"
	"testing"
)

func useTempDirs(t *testing.T) string {
	t.Helper()
	oldDir, oldOwner := Dir, ownerFile
	dir := filepath.Join(t.TempDir(), "run")
	Dir, ownerFile = dir, filepath.Join(t.TempDir(), "no-owner-file")
	t.Cleanup(func() { Dir, ownerFile = oldDir, oldOwner })
	return dir
}

func TestLoadWithoutFileIsDisconnected(t *testing.T) {
	useTempDirs(t)
	s, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if s.Phase != PhaseDisconnected {
		t.Fatalf("Phase = %q, want DISCONNECTED", s.Phase)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	useTempDirs(t)
	in := &State{Phase: PhaseConnected, Profile: "office", Server: "vpn.example.com", Warnings: []string{"w"}, Reconnects: 2}
	if err := in.Save(); err != nil {
		t.Fatal(err)
	}
	out, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if out.Phase != PhaseConnected || out.Profile != "office" || out.Reconnects != 2 || len(out.Warnings) != 1 {
		t.Fatalf("round trip lost data: %+v", out)
	}
	if out.UpdatedAt.IsZero() {
		t.Fatal("Save must stamp UpdatedAt")
	}
	if entries, _ := os.ReadDir(Dir); len(entries) != 2 {
		t.Fatalf("expected only state.json and state.root.json in %s, got %d entries (a .tmp file was left behind)", Dir, len(entries))
	}
}

// trustOwnFiles makes LoadTrusted accept files this test process wrote.
func trustOwnFiles(t *testing.T) {
	t.Helper()
	old := trustedOwner
	trustedOwner = uint32(os.Getuid())
	t.Cleanup(func() { trustedOwner = old })
}

func TestLoadTrustedReadsTheRootOnlyCopy(t *testing.T) {
	useTempDirs(t)
	trustOwnFiles(t)
	if err := (&State{Phase: PhaseConnected, PID: 4242, Server: "192.0.2.1"}).Save(); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(trustedPath())
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("root-only copy mode = %o, want 600", fi.Mode().Perm())
	}
	// What the installing user can do to their own state.json.
	forged := []byte(`{"phase":"CONNECTED","pid":1,"server":"198.51.100.7"}`)
	if err := os.WriteFile(path(), forged, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := LoadTrusted()
	if err != nil {
		t.Fatal(err)
	}
	if s.PID != 4242 || s.Server != "192.0.2.1" {
		t.Fatalf("LoadTrusted followed the user-writable state.json: %+v", s)
	}
}

func TestLoadTrustedIgnoresFilesRootDoesNotOwn(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root: every file written here is root-owned")
	}
	useTempDirs(t)
	if err := (&State{Phase: PhaseConnected, PID: 4242}).Save(); err != nil {
		t.Fatal(err)
	}
	s, err := LoadTrusted()
	if err != nil {
		t.Fatal(err)
	}
	if s.Phase != PhaseDisconnected || s.PID != 0 {
		t.Fatalf("LoadTrusted believed a file root does not own: %+v", s)
	}
}

func TestLoadTrustedFallsBackToARootOwnedStateFile(t *testing.T) {
	useTempDirs(t)
	trustOwnFiles(t)
	if err := os.MkdirAll(Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := []byte(`{"phase":"CONNECTED","pid":4242}`)
	if err := os.WriteFile(path(), legacy, 0o644); err != nil {
		t.Fatal(err)
	}
	if s, err := LoadTrusted(); err != nil || s.PID != 4242 {
		t.Fatalf("LoadTrusted = %+v, %v; want the owner-only-writable state.json", s, err)
	}
	if err := os.Chmod(path(), 0o666); err != nil {
		t.Fatal(err)
	}
	if s, err := LoadTrusted(); err != nil || s.Phase != PhaseDisconnected {
		t.Fatalf("LoadTrusted = %+v, %v; want DISCONNECTED for a file others can write", s, err)
	}
}

func TestSaveWithoutOwnerFileIsReadableByTheApp(t *testing.T) {
	useTempDirs(t)
	if err := (&State{Phase: PhaseConnecting}).Save(); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path())
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o644 {
		t.Fatalf("mode = %o, want 644 when no owner is known", fi.Mode().Perm())
	}
}

func TestOwnerUID(t *testing.T) {
	useTempDirs(t)
	if _, ok := ownerUID(); ok {
		t.Fatal("ownerUID reported an owner without an owner file")
	}
	for _, bad := range []string{"abc", "-5", ""} {
		if err := os.WriteFile(ownerFile, []byte(bad), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, ok := ownerUID(); ok {
			t.Fatalf("ownerUID accepted %q", bad)
		}
	}
	if err := os.WriteFile(ownerFile, []byte("501\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if uid, ok := ownerUID(); !ok || uid != 501 {
		t.Fatalf("ownerUID = %d, %v; want 501, true", uid, ok)
	}
}

func TestClearResetsToDisconnected(t *testing.T) {
	useTempDirs(t)
	_ = (&State{Phase: PhaseFailed, FailStage: "IKE"}).Save()
	if err := Clear(); err != nil {
		t.Fatal(err)
	}
	s, _ := Load()
	if s.Phase != PhaseDisconnected || s.FailStage != "" {
		t.Fatalf("Clear left %+v", s)
	}
}
