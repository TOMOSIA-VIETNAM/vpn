package engine

import (
	"bytes"
	"testing"
	"time"

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
