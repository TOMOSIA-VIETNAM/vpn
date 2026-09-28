package engine

import (
	"encoding/binary"
	"os"
	"testing"
	"time"

	"vpn/internal/ike"
)

func TestL2TPUDPHeader(t *testing.T) {
	for _, tc := range []struct{ src, wantSrc uint16 }{{0, 1701}, {51234, 51234}} {
		h := l2tpUDPHeader(tc.src, 10)
		if got := binary.BigEndian.Uint16(h[0:2]); got != tc.wantSrc {
			t.Fatalf("src port = %d, want %d", got, tc.wantSrc)
		}
		if got := binary.BigEndian.Uint16(h[2:4]); got != ike.L2TPPort {
			t.Fatalf("dst port = %d, want 1701", got)
		}
		if got := binary.BigEndian.Uint16(h[4:6]); got != 18 {
			t.Fatalf("length = %d, want 18", got)
		}
	}
}

func TestRandomL2TPPortIsDynamic(t *testing.T) {
	seen := map[uint16]bool{}
	for i := 0; i < 200; i++ {
		p := randomL2TPPort()
		if p < 49152 {
			t.Fatalf("port %d outside the dynamic range", p)
		}
		seen[p] = true
	}
	if len(seen) < 150 {
		t.Fatalf("only %d distinct ports in 200 picks", len(seen))
	}
}

// A session still to be ended on the server must be reached from its own
// port; otherwise every attempt gets a fresh one.
func TestL2TPPortFor(t *testing.T) {
	if p := l2tpPortFor(&staleSession{tunnel: 1, session: 2, l2tpPort: 50001}); p != 50001 {
		t.Fatalf("pending stale session: port %d, want 50001", p)
	}
	if p := l2tpPortFor(&staleSession{tunnel: 1, session: 2, l2tpPort: 50001, done: true}); p == 50001 || p < 49152 {
		t.Fatalf("stale session already ended: port %d, want a fresh one", p)
	}
	if p := l2tpPortFor(nil); p < 49152 {
		t.Fatalf("no stale session: port %d, want a fresh one", p)
	}
}

func TestLastSessionKeepsL2TPPort(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	saveLastSession("vpn.example", 7, 9, 50001)
	s := loadLastSession("vpn.example", time.Now())
	if s == nil || s.l2tpPort != 50001 {
		t.Fatalf("got %+v, want l2tpPort 50001", s)
	}
}

// Records written before per-connection ports carry none: that session ran on 1701.
func TestLegacyLastSessionMeans1701(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	saveLastSession("vpn.example", 7, 9, 50001) // creates the directory
	p, err := lastSessionPath()
	if err != nil {
		t.Fatal(err)
	}
	legacy := `{"server":"vpn.example","peer_tunnel":7,"peer_session":9,"alive_at":"` + time.Now().Format(time.RFC3339Nano) + `"}`
	if err := os.WriteFile(p, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	s := loadLastSession("vpn.example", time.Now())
	if s == nil || s.l2tpPort != ike.L2TPPort {
		t.Fatalf("got %+v, want l2tpPort 1701", s)
	}
}
