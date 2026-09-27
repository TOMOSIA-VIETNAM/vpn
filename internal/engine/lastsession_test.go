package engine

import (
	"os"
	"testing"
	"time"
)

func TestLastSessionSurvivesAndIsScopedToServerAndAge(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if loadLastSession("vpn.example", time.Now()) != nil {
		t.Fatal("found a session before anything was saved")
	}
	saveLastSession("vpn.example", 1694, 58187, 50000)

	got := loadLastSession("vpn.example", time.Now())
	if got == nil || got.tunnel != 1694 || got.session != 58187 {
		t.Fatalf("saved IDs not restored: %+v", got)
	}
	if loadLastSession("other.example", time.Now()) != nil {
		t.Fatal("a record for another server must not be used")
	}
	if loadLastSession("vpn.example", time.Now().Add(lastSessionMaxAge+time.Minute)) != nil {
		t.Fatal("a record last seen alive long ago must not be used: the server has dropped it and may have reused the IDs")
	}

	p, _ := lastSessionPath()
	if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("record must be private (0600): %v %v", fi, err)
	}

	clearLastSession()
	if loadLastSession("vpn.example", time.Now()) != nil {
		t.Fatal("a cleanly closed session must leave no record")
	}
}

func TestLastSessionIgnoresGarbageAndZeroIDs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	saveLastSession("s", 0, 5, 50000)
	if loadLastSession("s", time.Now()) != nil {
		t.Fatal("zero IDs mean 'never established' and must not be evicted")
	}
	p, _ := lastSessionPath()
	os.WriteFile(p, []byte("not json"), 0o600)
	if loadLastSession("s", time.Now()) != nil {
		t.Fatal("a corrupt record must be ignored")
	}
}

func TestEvictStaleRunsOncePerRecord(t *testing.T) {
	st, sg := staleTerminateGap, staleSettle
	staleTerminateGap, staleSettle = time.Millisecond, time.Millisecond
	defer func() { staleTerminateGap, staleSettle = st, sg }()

	ct := &captureTransport{}
	s := &staleSession{tunnel: 7, session: 9}
	evictStale(ct, s)
	evictStale(ct, s) // e.g. the next reconnect attempt
	if len(ct.sent) != staleTerminateAttempts {
		t.Fatalf("sent %d, want one round of %d", len(ct.sent), staleTerminateAttempts)
	}
}
