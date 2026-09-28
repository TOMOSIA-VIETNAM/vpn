package ipsec

import (
	"math/rand"
	"testing"
)

// A reference window: every sequence number ever accepted, and the highest.
type naiveWindow struct {
	seen map[uint32]bool
	top  uint32
}

func (n *naiveWindow) ok(seq uint32) bool {
	if seq == 0 {
		return false
	}
	if len(n.seen) == 0 || seq > n.top {
		return true
	}
	return n.top-seq < ReplayWindowSize && !n.seen[seq]
}

func (n *naiveWindow) add(seq uint32) {
	n.seen[seq] = true
	if seq > n.top {
		n.top = seq
	}
}

// The ring bitmap must decide exactly like the reference, across jumps that
// cross word boundaries and wrap the ring several times over.
func TestReplayWindowMatchesReference(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	var w replayWindow
	ref := naiveWindow{seen: map[uint32]bool{}}
	next := uint32(1)
	for i := 0; i < 300000; i++ {
		var seq uint32
		switch r := rng.Intn(100); {
		case r < 70: // in order
			seq = next
			next++
		case r < 85: // reordered or replayed, possibly beyond the window
			back := uint32(rng.Intn(ReplayWindowSize + 2000))
			if back >= next {
				back = next - 1
			}
			seq = next - back
		case r < 95: // small jump ahead
			next += uint32(rng.Intn(200))
			seq = next
			next++
		default: // big jump: clears the whole ring
			next += uint32(rng.Intn(3 * ReplayWindowSize))
			seq = next
			next++
		}
		got, want := w.check(seq) == nil, ref.ok(seq)
		if got != want {
			t.Fatalf("step %d seq %d (top %d): window accepts=%v, reference=%v", i, seq, ref.top, got, want)
		}
		if got {
			w.accept(seq)
			ref.add(seq)
		}
	}
}

func TestReplayWindowEdges(t *testing.T) {
	var w replayWindow
	if w.check(0) == nil {
		t.Fatal("sequence 0 accepted")
	}
	w.accept(ReplayWindowSize + 100)
	top := uint32(ReplayWindowSize + 100)
	if w.check(top-ReplayWindowSize+1) != nil {
		t.Fatal("oldest in-window sequence rejected")
	}
	if w.check(top-ReplayWindowSize) == nil {
		t.Fatal("sequence just outside the window accepted")
	}
	if w.check(top) == nil {
		t.Fatal("replay of the highest sequence accepted")
	}
}

// A forged packet (right SPI, wrong ICV) must leave the window untouched —
// it used to set the window base, after which every genuine packet was
// "too old" and the SA's inbound side was dead.
func TestForgedPacketDoesNotMoveReplayWindow(t *testing.T) {
	for _, s := range suites {
		t.Run(s.String(), func(t *testing.T) {
			out, in := s.pair(t)
			out.seq = 0xFFFFFFF0
			forged, err := out.Encrypt([]byte("forged"), 17)
			if err != nil {
				t.Fatal(err)
			}
			forged[len(forged)-1] ^= 0xff // break the ICV
			if _, _, err := in.Decrypt(forged); err == nil {
				t.Fatal("forged packet decrypted")
			}
			out.seq = 0
			genuine, err := out.Encrypt([]byte("genuine"), 17)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := in.Decrypt(genuine); err != nil {
				t.Fatalf("genuine packet after a forged one rejected: %v", err)
			}
		})
	}
}

// Reordering well beyond the old 64-packet window is accepted; each packet once.
func TestESPReorderedWithinWindow(t *testing.T) {
	out, in := suites[1].pair(t)
	pkts := make([][]byte, 5000)
	for i := range pkts {
		p, err := out.Encrypt([]byte("x"), 17)
		if err != nil {
			t.Fatal(err)
		}
		pkts[i] = p
	}
	for i := len(pkts) - 1; i >= 0; i-- { // fully reversed
		if _, _, err := in.Decrypt(pkts[i]); err != nil {
			t.Fatalf("packet %d: %v", i+1, err)
		}
	}
	if _, _, err := in.Decrypt(pkts[2500]); err == nil {
		t.Fatal("replayed packet accepted")
	}
}
