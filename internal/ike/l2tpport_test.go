package ike

import (
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"
)

func idPort(id []byte) uint16 { return binary.BigEndian.Uint16(id[2:4]) }

func TestQuickModeIDsUseOurL2TPPort(t *testing.T) {
	local, remote := net.ParseIP("10.0.0.2"), net.ParseIP("10.0.0.1")
	for _, tc := range []struct {
		name string
		port uint16
		want uint16
	}{
		{"default", 0, 1701},
		{"own port", 51234, 51234},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Session{L2TPPort: tc.port}
			idci, idcr := s.quickModeIDs(local, remote)
			if idci[1] != 17 || idcr[1] != 17 {
				t.Fatalf("protocol = %d/%d, want UDP (17)", idci[1], idcr[1])
			}
			if got := idPort(idci); got != tc.want {
				t.Fatalf("IDci port = %d, want %d", got, tc.want)
			}
			if got := idPort(idcr); got != L2TPPort {
				t.Fatalf("IDcr port = %d, want the server's 1701", got)
			}
		})
	}
}

// Config.L2TPPort reaches the Session, and a Quick Mode with it completes.
func TestQuickModeWithOwnL2TPPortAgainstFakeServer(t *testing.T) {
	f := newFakeServer(t, "sekrit", []string{"aes128-sha1"})
	useFakePorts(t, f, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cfg := fakeClientConfig("sekrit")
	cfg.L2TPPort = 51234
	sess, err := establishPhase1To(ctx, cfg, f.mainPort())
	if err != nil {
		t.Fatalf("Phase 1: %v", err)
	}
	defer sess.Close()
	if sess.L2TPPort != 51234 {
		t.Fatalf("Session.L2TPPort = %d, want 51234", sess.L2TPPort)
	}
	lo := net.ParseIP("127.0.0.1")
	if _, err := sess.EstablishQuickMode([]string{"aes128-sha1"}, lo, lo); err != nil {
		t.Fatalf("Quick Mode: %v", err)
	}
}
