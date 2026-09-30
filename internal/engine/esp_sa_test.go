package engine

import (
	"bytes"
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vpn/internal/bufpool"
	"vpn/internal/ike"
	"vpn/internal/ipsec"
)

func TestNewESPSAUsesNegotiatedTransform(t *testing.T) {
	cases := []struct {
		tr        ike.Transform
		enc, auth int
		cipher    ipsec.Cipher
		integrity ipsec.Integrity
	}{
		{ike.Transform{Encryption: ike.Enc3DES, Hash: ike.HashSHA1}, 24, 20, ipsec.Cipher3DESCBC, ipsec.IntegHMACSHA1_96},
		{ike.Transform{Encryption: ike.EncAES, KeyBits: 128, Hash: ike.HashSHA1}, 16, 20, ipsec.CipherAESCBC, ipsec.IntegHMACSHA1_96},
		{ike.Transform{Encryption: ike.EncAES, KeyBits: 256, Hash: ike.HashSHA256}, 32, 32, ipsec.CipherAESCBC, ipsec.IntegHMACSHA256_128},
		{ike.Transform{Encryption: ike.EncAES, KeyBits: 128, Hash: ike.HashMD5}, 16, 16, ipsec.CipherAESCBC, ipsec.IntegHMACMD5_96},
		{ike.Transform{Encryption: ike.EncAES, KeyBits: 256, Hash: ike.HashSHA384}, 32, 48, ipsec.CipherAESCBC, ipsec.IntegHMACSHA384_192},
		{ike.Transform{Encryption: ike.EncAES, KeyBits: 256, Hash: ike.HashSHA512}, 32, 64, ipsec.CipherAESCBC, ipsec.IntegHMACSHA512_256},
	}

	for _, c := range cases {
		sa, err := newESPSA(ike.ChildSA{SPI: 1, EncKey: make([]byte, c.enc), AuthKey: make([]byte, c.auth), Transform: c.tr})
		if err != nil {
			t.Fatalf("%+v: %v", c.tr, err)
		}
		if sa.Cipher != c.cipher || sa.Integrity != c.integrity {
			t.Fatalf("%+v: got cipher %d integrity %d", c.tr, sa.Cipher, sa.Integrity)
		}
	}
	if _, err := newESPSA(ike.ChildSA{SPI: 1, EncKey: make([]byte, 8), AuthKey: make([]byte, 20), Transform: ike.Transform{Encryption: ike.EncDES, Hash: ike.HashSHA1}}); err == nil {
		t.Fatal("single DES accepted")
	}
}

// The watchdog declares the peer dead when no valid packet has been seen for
// deadAfter, so an authenticated inbound packet MUST refresh liveness — a
// missing touch here once meant every healthy tunnel was "dead" after a minute.
func TestInboundAuthenticatedPacketRefreshesLiveness(t *testing.T) {
	qm := testQM(0x11, 0x22, time.Hour)
	sas, err := newSASet(qm)
	if err != nil {
		t.Fatal(err)
	}
	live := newLiveness()
	live.lastRx.Store(time.Now().Add(-10 * time.Minute).UnixNano())
	tr := &espTransport{sas: sas, live: live}

	peerOut, err := newESPSA(qm.Inbound) // what the server encrypts with: same SPI and keys as our inbound SA
	if err != nil {
		t.Fatal(err)
	}
	udp := append([]byte{0x06, 0xa5, 0x06, 0xa5, 0x00, 0x0a, 0, 0}, 0xAA, 0xBB)
	pkt, err := peerOut.Encrypt(udp, protoUDP)
	if err != nil {
		t.Fatal(err)
	}
	msg, ok := tr.process(pkt)
	if !ok || len(msg) != 2 {
		t.Fatalf("valid packet not delivered: ok=%v msg=%x", ok, msg)
	}
	if live.idle() > time.Second {
		t.Fatalf("an authenticated packet left the peer looking silent for %s", live.idle())
	}

	// Junk and 1-byte NAT keepalives are not proof of life.
	live.lastRx.Store(time.Now().Add(-10 * time.Minute).UnixNano())
	for _, junk := range [][]byte{{0xFF}, make([]byte, 40), append([]byte{0, 0, 0, 0x99}, make([]byte, 40)...)} {
		if _, ok := tr.process(junk); ok {
			t.Fatal("garbage delivered")
		}
	}
	if live.idle() < 9*time.Minute {
		t.Fatal("unauthenticated packets must not refresh liveness")
	}
}

// The data plane recycles each inbound buffer (ReleaseLast) once it has written
// the packet to the tunnel device. A recycled buffer is handed out again for
// the very next packet, so every message must still read back exactly as sent —
// and a message that is never released must stay intact while later ones pass.
func TestInboundBuffersSurviveRecycling(t *testing.T) {
	qm := testQM(0x31, 0x32, time.Hour)
	sas, err := newSASet(qm)
	if err != nil {
		t.Fatal(err)
	}
	tr := &espTransport{sas: sas, live: newLiveness()}
	peerOut, err := newESPSA(qm.Inbound)
	if err != nil {
		t.Fatal(err)
	}
	send := func(i int) []byte {
		body := bytes.Repeat([]byte{byte(i)}, 20+i*13%1200)
		pkt, err := peerOut.Encrypt(append(make([]byte, 8), body...), protoUDP)
		if err != nil {
			t.Fatal(err)
		}
		msg, ok := tr.process(pkt)
		if !ok || !bytes.Equal(msg, body) {
			t.Fatalf("packet %d: delivered=%v, %d bytes, want %d bytes of %#x", i, ok, len(msg), len(body), byte(i))
		}
		return msg
	}
	kept := append([]byte(nil), send(1)...)
	held := send(1) // like an LCP frame: never released
	for i := 2; i < 400; i++ {
		send(i)
		tr.ReleaseLast()
		tr.ReleaseLast() // a second call is a no-op
	}
	if !bytes.Equal(held, kept) {
		t.Fatal("a message that was never released was overwritten by later packets")
	}
}

// loopbackIKE stands in for the IKE session's socket: whatever the transport
// sends is decrypted at once with the peer's SA and checked, the way the
// server would see it.
type loopbackIKE struct {
	fakeIKE
	peerIn *ipsec.SA
	t      *testing.T
	seen   atomic.Int64
}

func (l *loopbackIKE) SendESP(pkt []byte) error {
	plain, nh, err := l.peerIn.Decrypt(pkt)
	if err != nil || nh != protoUDP || len(plain) < 16+20 {
		l.t.Errorf("server could not read a sent packet: len=%d nh=%d err=%v", len(plain), nh, err)
		return nil
	}
	ip := plain[16:] // inner UDP (8) + L2TP (6) + PPP (2), then the IP packet
	if !bytes.Equal(ip, bytes.Repeat([]byte{ip[0]}, len(ip))) {
		l.t.Errorf("outbound packet %#x arrived corrupted", ip[0])
	}
	l.seen.Add(1)
	return nil
}

// Both directions at once, the way the data plane runs them: one goroutine
// takes packets "from utun" and sends them, another receives and "writes to
// utun", and every buffer goes back to the shared pool as soon as it is done
// with. A buffer recycled while something still reads it would show up here as
// a corrupted packet (or as a data race under -race).
func TestDataPathBothDirectionsShareThePoolSafely(t *testing.T) {
	qm := testQM(0x71, 0x72, time.Hour)
	sas, err := newSASet(qm)
	if err != nil {
		t.Fatal(err)
	}
	peerOut, err := newESPSA(qm.Inbound) // the server's sending SA
	if err != nil {
		t.Fatal(err)
	}
	peerIn, err := newESPSA(qm.Outbound) // the server's receiving SA
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mux := newIKESessions(ctx)
	srv := &loopbackIKE{peerIn: peerIn, t: t}
	mux.list = append(mux.list, srv)
	tr := &espTransport{mux: mux, sas: sas, live: newLiveness()}

	const n = 3000
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { // utun -> server
		defer wg.Done()
		for i := 0; i < n; i++ {
			pkt := bufpool.Get(40 + i%1200)
			for k := range pkt {
				pkt[k] = byte(0x40 + i%16) // an IPv4 first nibble, the rest a fill
			}
			if err := tr.SendIPFast(7, 9, pkt); err != nil {
				t.Errorf("send %d: %v", i, err)
			}
			bufpool.Put(pkt)
		}
	}()
	go func() { // the socket reader: server -> pooled datagrams
		defer wg.Done()
		for i := 0; i < n; i++ {
			body := append(make([]byte, 8), bytes.Repeat([]byte{byte(i)}, 30+i%1100)...)
			enc, err := peerOut.Encrypt(body, protoUDP)
			if err != nil {
				t.Errorf("server encrypt %d: %v", i, err)
				return
			}
			mux.in <- enc // pooled by Encrypt, exactly like the reader's buffers
		}
	}()
	go func() { // server -> utun
		defer wg.Done()
		for i := 0; i < n; i++ {
			msg, err := tr.Recv(ctx)
			if err != nil {
				t.Errorf("recv %d: %v", i, err)
				return
			}
			if len(msg) != 30+i%1100 || !bytes.Equal(msg, bytes.Repeat([]byte{byte(i)}, len(msg))) {
				t.Errorf("inbound packet %d arrived corrupted or out of order", i)
				return
			}
			tr.ReleaseLast()
		}
	}()
	wg.Wait()
	if got := srv.seen.Load(); got != n {
		t.Fatalf("server received %d packets, want %d", got, n)
	}
}
