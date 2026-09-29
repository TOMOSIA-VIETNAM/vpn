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
