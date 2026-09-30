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
	if entries, _ := os.ReadDir(Dir); len(entries) != 1 {
		t.Fatalf("expected only state.json in %s, got %d entries (a .tmp file was left behind)", Dir, len(entries))
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
