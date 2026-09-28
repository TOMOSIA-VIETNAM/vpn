package l2tp

import (
	"context"
	"testing"
	"time"
)

// zlbsIn returns the Nr of every ZLB among the sent messages.
func zlbsIn(t *testing.T, sent [][]byte) (nrs []uint16, others int) {
	t.Helper()
	for _, m := range sent {
		p, err := Parse(m)
		if err != nil {
			t.Fatal(err)
		}
		if p.Header.IsControl && len(p.AVPs) == 0 {
			nrs = append(nrs, p.Header.Nr)
		} else {
			others++
		}
	}
	return nrs, others
}

func runRecv(t *testing.T, tun *Tunnel, tr *scriptedTransport, msgs ...[]byte) {
	t.Helper()
	for _, m := range msgs {
		tr.in <- m
	}
	tr.in <- MarshalData(tun.localTunnelID, tun.localSessionID, []byte{0xff, 0x03, 0xc0, 0x21, 1}) // ends RecvData
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := tun.RecvData(ctx); err != nil {
		t.Fatal(err)
	}
}

// Any control message the LNS sends during the session must be acked — an
// unacked Set-Link-Info was retransmitted and then answered with StopCCN
// "result 7" about 40 s after connecting (seen live).
func TestInterleavedControlIsAcknowledged(t *testing.T) {
	tr := &scriptedTransport{in: make(chan []byte, 8)}
	tun := &Tunnel{t: tr, localTunnelID: 0x1234, peerTunnelID: 0x4321, localSessionID: 1, ns: 4, nr: 2}
	sli := controlTo(0x1234, 1, 2, 4, MessageTypeAVP(MsgSLI))
	runRecv(t, tun, tr, sli)
	nrs, others := zlbsIn(t, tr.sent)
	if len(nrs) != 1 || nrs[0] != 3 || others != 0 {
		t.Fatalf("sent ZLBs with Nr %v and %d other messages, want one ZLB acking Ns 2 (Nr 3)", nrs, others)
	}
	if tun.nr != 3 || tun.ns != 4 {
		t.Fatalf("ns/nr = %d/%d, want 4/3 (a ZLB takes no sequence number)", tun.ns, tun.nr)
	}
}

// Hello is answered with a ZLB, not a Hello of our own.
func TestHelloAnsweredWithZLB(t *testing.T) {
	tr := &scriptedTransport{in: make(chan []byte, 8)}
	tun := &Tunnel{t: tr, localTunnelID: 0x1234, peerTunnelID: 0x4321, localSessionID: 1, ns: 4, nr: 2}
	runRecv(t, tun, tr, controlTo(0x1234, 0, 2, 4, MessageTypeAVP(MsgHello)))
	nrs, others := zlbsIn(t, tr.sent)
	if len(nrs) != 1 || nrs[0] != 3 || others != 0 {
		t.Fatalf("sent ZLBs %v and %d other messages, want one ZLB with Nr 3", nrs, others)
	}
}

// A retransmit (our ack was lost) is acked again but not acted on twice; a
// ZLB from the server moves nothing.
func TestRetransmitReackedZLBIgnored(t *testing.T) {
	tr := &scriptedTransport{in: make(chan []byte, 8)}
	tun := &Tunnel{t: tr, localTunnelID: 0x1234, peerTunnelID: 0x4321, localSessionID: 1, ns: 4, nr: 3}
	dup := controlTo(0x1234, 1, 2, 4, MessageTypeAVP(MsgSLI))
	zlb := controlTo(0x1234, 0, 3, 5)
	runRecv(t, tun, tr, dup, zlb)
	nrs, _ := zlbsIn(t, tr.sent)
	if len(nrs) != 1 || nrs[0] != 3 {
		t.Fatalf("sent ZLBs %v, want exactly one re-ack with Nr 3", nrs)
	}
	if tun.nr != 3 {
		t.Fatalf("nr = %d, want 3: neither a duplicate nor a ZLB advances it", tun.nr)
	}
}

func TestSeqBefore(t *testing.T) {
	for _, c := range []struct {
		a, b uint16
		want bool
	}{{1, 2, true}, {2, 2, false}, {3, 2, false}, {65535, 0, true}, {0, 65535, false}} {
		if got := seqBefore(c.a, c.b); got != c.want {
			t.Errorf("seqBefore(%d,%d) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
