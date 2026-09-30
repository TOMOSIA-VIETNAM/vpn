package engine

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"
	"time"

	"vpn/internal/bufpool"
	"vpn/internal/ipsec"
)

// pooled copies pkt into a pool buffer, as the socket reader does.
func pooled(pkt []byte) []byte {
	b := bufpool.Get(len(pkt))
	copy(b, pkt)
	return b
}

func parallelFixture(t testing.TB) (*espTransport, *ikeSessions, *ipsec.SA, context.CancelFunc) {
	t.Helper()
	qm := testQM(0x51, 0x52, time.Hour) // 3DES, the cipher the pipeline is for
	sas, err := newSASet(qm)
	if err != nil {
		t.Fatal(err)
	}
	peerOut, err := newESPSA(qm.Inbound)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	mux := newIKESessions(ctx)
	return &espTransport{mux: mux, sas: sas, live: newLiveness()}, mux, peerOut, cancel
}

// numbered is the ESP datagram for message number i: an inner UDP header, then
// the number and some filler as the "L2TP message".
func numbered(t testing.TB, peerOut *ipsec.SA, i int) []byte {
	body := make([]byte, 8+4+40+i%900)
	binary.BigEndian.PutUint32(body[8:], uint32(i))
	for k := 12; k < len(body); k++ {
		body[k] = byte(i)
	}
	pkt, err := peerOut.Encrypt(body, protoUDP)
	if err != nil {
		t.Fatal(err)
	}
	return pkt
}

// The pipeline decrypts on several goroutines but must hand packets to the
// tunnel in exactly the order they arrived, deliver each once despite replays,
// skip junk, and leave Recv working on the plain path once stopped.
func TestParallelRecvKeepsOrderAndDropsReplays(t *testing.T) {
	if parallelRxWorkers() == 0 {
		t.Skip("needs more than one CPU")
	}
	tr, mux, peerOut, cancel := parallelFixture(t)
	defer cancel()
	ctx, stopCtx := context.WithCancel(context.Background())
	stop := tr.startParallel(ctx)
	if stop == nil {
		t.Fatal("a 3DES session on a multi-core machine must use the parallel pipeline")
	}

	const n = 1500
	go func() {
		for i := 0; i < n; i++ {
			pkt := numbered(t, peerOut, i)
			mux.in <- pooled(pkt)
			if i%7 == 0 {
				mux.in <- pooled(pkt) // a replay right behind the original
			}
			if i%50 == 0 {
				mux.in <- pooled([]byte{0xFF})                                       // NAT keepalive
				mux.in <- pooled(append([]byte{0, 0, 0, 0x51}, make([]byte, 60)...)) // our SPI, garbage
				mux.in <- pooled(append([]byte{9, 9, 9, 9}, make([]byte, 60)...))    // someone else's SPI
			}
			bufpool.Put(pkt)
		}
	}()
	deadline, cancelDeadline := context.WithTimeout(ctx, 60*time.Second)
	defer cancelDeadline()
	for i := 0; i < n; i++ {
		msg, err := tr.Recv(deadline)
		if err != nil {
			t.Fatalf("message %d: %v", i, err)
		}
		if got := int(binary.BigEndian.Uint32(msg)); got != i {
			t.Fatalf("message %d arrived where %d was expected: order lost, or a replay delivered", got, i)
		}
		if len(msg) != 4+40+i%900 || !bytes.Equal(msg[4:], bytes.Repeat([]byte{byte(i)}, len(msg)-4)) {
			t.Fatalf("message %d corrupted", i)
		}
		tr.ReleaseLast()
	}

	stopCtx()
	stop()
	if tr.par != nil {
		t.Fatal("stop must switch Recv back to the plain path")
	}
	mux.in <- pooled(numbered(t, peerOut, n))
	fresh, cancelFresh := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelFresh()
	if msg, err := tr.Recv(fresh); err != nil || int(binary.BigEndian.Uint32(msg)) != n {
		t.Fatalf("plain path after stop: msg=%x err=%v", msg, err)
	}
}

// An AES session stays on the plain path: one core already does gigabits.
func TestParallelRecvOnlyFor3DES(t *testing.T) {
	qm := testQM(0x61, 0x62, time.Hour)
	for _, sa := range []*[]byte{&qm.Inbound.EncKey, &qm.Outbound.EncKey} {
		*sa = make([]byte, 32)
	}
	qm.Inbound.Transform.Encryption, qm.Outbound.Transform.Encryption = 7, 7 // ike.EncAES
	qm.Inbound.Transform.KeyBits, qm.Outbound.Transform.KeyBits = 256, 256
	sas, err := newSASet(qm)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tr := &espTransport{mux: newIKESessions(ctx), sas: sas}
	if stop := tr.startParallel(ctx); stop != nil {
		t.Fatal("an AES session started the parallel pipeline")
	}
}

func benchRecv(b *testing.B, parallel bool) {
	tr, mux, peerOut, cancel := parallelFixture(b)
	defer cancel()
	if parallel {
		if stop := tr.startParallel(context.Background()); stop == nil {
			b.Skip("needs more than one CPU")
		}
	}
	pkts := make([][]byte, b.N)
	for i := range pkts {
		body := make([]byte, 8+1300)
		p, err := peerOut.Encrypt(body, protoUDP)
		if err != nil {
			b.Fatal(err)
		}
		pkts[i] = p
	}
	b.SetBytes(1300)
	b.ReportAllocs()
	b.ResetTimer()
	go func() {
		for _, p := range pkts {
			mux.in <- p
		}
	}()
	ctx := context.Background()
	for i := 0; i < b.N; i++ {
		if _, err := tr.Recv(ctx); err != nil {
			b.Fatal(err)
		}
		tr.ReleaseLast()
	}
}

func BenchmarkRecv3DESOneCore(b *testing.B)  { benchRecv(b, false) }
func BenchmarkRecv3DESParallel(b *testing.B) { benchRecv(b, true) }
