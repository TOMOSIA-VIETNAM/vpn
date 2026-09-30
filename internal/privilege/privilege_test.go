package privilege

import (
	"errors"
	"os"
	"testing"
)

func TestOwnerCheckSkippedWithoutSetuidPrivilege(t *testing.T) {
	// A release candidate is executed as this ordinary process for `vpn
	// version` before install. It must not need access to a root-only owner
	// file that an existing install may have created.
	if err := CheckOwner(); err != nil {
		t.Fatalf("CheckOwner() = %v, want nil for ordinary process", err)
	}
}

func TestElevateWithoutRootReportsErrNotPrivileged(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("already root: Elevate always succeeds")
	}
	err := Elevate(func() error {
		t.Fatal("fn must not run when the process cannot become root")
		return nil
	})
	if !errors.Is(err, ErrNotPrivileged) {
		t.Fatalf("Elevate() = %v, want an error wrapping ErrNotPrivileged", err)
	}
}

func TestSanitizeEnvKeepsOnlyAllowlist(t *testing.T) {
	got := sanitizeEnv([]string{
		"HOME=/Users/a", "PATH=/tmp/evil", "DYLD_INSERT_LIBRARIES=/tmp/x.dylib",
		"LC_ALL=en_US.UTF-8", "HTTPS_PROXY=http://proxy:3128", "VPN_DAEMON_CHILD=1", "MallocStackLogging=1",
	})
	want := map[string]bool{
		"HOME=/Users/a": true, "LC_ALL=en_US.UTF-8": true, "VPN_DAEMON_CHILD=1": true,
		"HTTPS_PROXY=http://proxy:3128": true, "PATH=" + safePath: true,
	}
	if len(got) != len(want) {
		t.Fatalf("sanitizeEnv() = %v, want exactly %d entries", got, len(want))
	}
	for _, kv := range got {
		if !want[kv] {
			t.Errorf("unexpected variable kept: %q", kv)
		}
	}
}

func TestSanitizeEnvNoopWithoutSetuid(t *testing.T) {
	t.Setenv("VPN_TEST_SENTINEL", "1")
	SanitizeEnv()
	if os.Getenv("VPN_TEST_SENTINEL") != "1" {
		t.Fatal("SanitizeEnv cleared the environment of a process that is not setuid-root")
	}
}
