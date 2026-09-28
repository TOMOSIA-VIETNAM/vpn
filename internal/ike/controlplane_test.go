package ike

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"
)

// testPeer is the server side of a floated IKE SA on localhost.
type testPeer struct {
	t    *testing.T
	conn *net.UDPConn
	sess *Session // the client session under test
	keys *Phase1Keys
}

func newTestPair(t *testing.T) (*testPeer, *Session) {
	t.Helper()
	lo := net.ParseIP("127.0.0.1")
	server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: lo})
	if err != nil {
		t.Fatal(err)
	}
	client, err := net.ListenUDP("udp4", &net.UDPAddr{IP: lo})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close(); client.Close() })

	peerKeys := &Phase1Keys{
		SKEYIDa: bytes.Repeat([]byte{0xA1}, 20),
		SKEYIDd: bytes.Repeat([]byte{0xD1}, 20),
		EncKey:  bytes.Repeat([]byte{0xE1}, 24),
	}
	clientKeys := &Phase1Keys{
		SKEYIDa: bytes.Repeat([]byte{0xA1}, 20),
		SKEYIDd: bytes.Repeat([]byte{0xD1}, 20),
		EncKey:  bytes.Repeat([]byte{0xE1}, 24),
	}
	s := &Session{
		conn:      client,
		serverIP:  lo,
		destAddr:  server.LocalAddr().(*net.UDPAddr),
		floated:   true,
		Transform: Transform{Encryption: Enc3DES, Hash: HashSHA1},
		Keys:      clientKeys,
		phase1IV:  bytes.Repeat([]byte{0x11}, 8),
	}
	copy(s.InitiatorSPI[:], "initcook")
	copy(s.ResponderSPI[:], "respcook")
	return &testPeer{t: t, conn: server, sess: s, keys: peerKeys}, s
}

func (p *testPeer) clientAddr() *net.UDPAddr { return p.sess.conn.LocalAddr().(*net.UDPAddr) }

func (p *testPeer) sendIKE(msg []byte) {
	p.t.Helper()
	if _, err := p.conn.WriteToUDP(append(append([]byte{}, nonESPMarker...), msg...), p.clientAddr()); err != nil {
		p.t.Fatal(err)
	}
}

func (p *testPeer) recvIKE() []byte {
	p.t.Helper()
	buf := make([]byte, 65535)
	p.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		n, _, err := p.conn.ReadFromUDP(buf)
		if err != nil {
			p.t.Fatalf("peer read: %v", err)
		}
		if n >= 4 && bytes.Equal(buf[:4], nonESPMarker) {
			return append([]byte{}, buf[4:n]...)
		}
	}
}

// encryptFirst builds a peer-initiated exchange message: HASH(1) over
// M-ID | rest, encrypted with the IV derived from the Phase 1 IV.
func (p *testPeer) encryptFirst(exchange uint8, msgID uint32, rest []byte, forgeHash bool, firstType ...uint8) []byte {
	p.t.Helper()
	s := p.sess
	hash, _ := prf(s.Transform.Hash, p.keys.SKEYIDa, append(beUint32(msgID), rest...))
	if forgeHash {
		hash[0] ^= 0xFF
	}
	ft := firstPayloadType(rest)
	if len(firstType) > 0 {
		ft = firstType[0]
	}
	plain := padToBlock(append(marshalPayload(ft, hash), rest...), 8)
	iv, _ := informationalIV(s.Transform.Hash, s.phase1IV, msgID, 8)
	ct, _ := cbcEncrypt(s.Transform, p.keys.EncKey, iv, plain)
	h := Header{InitiatorSPI: s.InitiatorSPI, ResponderSPI: s.ResponderSPI, NextPayload: PayloadHash, Version: 0x10, ExchangeType: exchange, Flags: FlagEncryption, MessageID: msgID}
	h.Length = uint32(headerLen + len(ct))
	return append(h.Marshal(), ct...)
}

// firstPayloadType is the type the HASH payload's next-payload field must
// name; tests only chain Delete payloads after HASH.
func firstPayloadType([]byte) uint8 { return PayloadDelete }

func deletePayload(spis ...uint32) []byte {
	body := make([]byte, 8+4*len(spis))
	binary.BigEndian.PutUint32(body[0:4], DOIIPsec)
	body[4] = protoIPsecESP
	body[5] = 4
	binary.BigEndian.PutUint16(body[6:8], uint16(len(spis)))
	for i, v := range spis {
		binary.BigEndian.PutUint32(body[8+4*i:], v)
	}
	return marshalPayload(PayloadNone, body)
}

func TestDataPhaseRoutesESPAndAuthenticatedDelete(t *testing.T) {
	peer, s := newTestPair(t)
	deleted := make(chan []uint32, 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.StartDataPhase(ctx, Events{DeleteESP: func(spis []uint32) { deleted <- spis }})

	// A forged Delete (bad HASH) must be ignored, a genuine one delivered.
	peer.sendIKE(peer.encryptFirst(ExchangeInformational, 0x1111, deletePayload(0xDEADBEEF), true))
	peer.sendIKE(peer.encryptFirst(ExchangeInformational, 0x2222, deletePayload(0xCAFEF00D), false))
	select {
	case spis := <-deleted:
		if len(spis) != 1 || spis[0] != 0xCAFEF00D {
			t.Fatalf("DeleteESP got %08x, want only the authenticated cafef00d", spis)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("authenticated Delete never delivered")
	}
	select {
	case spis := <-deleted:
		t.Fatalf("forged Delete delivered: %08x", spis)
	default:
	}

	// ESP (no non-ESP marker) goes to RecvESP untouched.
	esp := []byte{0x12, 0x34, 0x56, 0x78, 0, 0, 0, 1, 0xAA}
	if _, err := peer.conn.WriteToUDP(esp, peer.clientAddr()); err != nil {
		t.Fatal(err)
	}
	rctx, rcancel := context.WithTimeout(ctx, 3*time.Second)
	defer rcancel()
	got, err := s.RecvESP(rctx)
	if err != nil || !bytes.Equal(got, esp) {
		t.Fatalf("RecvESP = %x, %v; want %x", got, err, esp)
	}
}

// The peer answers the client's rekey QM1 as a responder would, and both
// sides must end with the same keys — proving the demultiplexed round trip,
// the per-exchange IV chain and HASH(2)/HASH(3) all line up.
func TestRekeyQuickModeThroughControlPlane(t *testing.T) {
	peer, s := newTestPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.StartDataPhase(ctx, Events{})

	type result struct {
		qm  *QuickModeResult
		err error
	}
	done := make(chan result, 1)
	go func() {
		qm, err := s.RekeyQuickMode([]string{"aes256-sha256", "3des-sha1"}, net.ParseIP("10.0.0.2"), net.ParseIP("10.0.0.1"))
		done <- result{qm, err}
	}()

	// --- responder side ---
	qm1 := peer.recvIKE()
	h1, _ := ParseHeader(qm1)
	iv, _ := informationalIV(HashSHA1, s.phase1IV, h1.MessageID, 8)
	plain1, err := cbcDecrypt(s.Transform, s.Keys.EncKey, iv, qm1[headerLen:])
	if err != nil {
		t.Fatal(err)
	}
	chain := qm1[len(qm1)-8:]
	pl1, err := SplitPayloads(h1.NextPayload, plain1)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyHash1(HashSHA1, s.Keys.SKEYIDa, h1.MessageID, h1.NextPayload, pl1, plain1); err != nil {
		t.Fatalf("client QM1 HASH(1): %v", err)
	}
	var ni []byte
	var initiatorSPI uint32
	for _, p := range pl1 {
		switch p.Type {
		case PayloadNonce:
			ni = p.Body
		case PayloadSA:
			initiatorSPI = binary.BigEndian.Uint32(p.Body[16:20]) // DOI, situation, proposal header, then SPI
		}
	}

	chosen, _ := espProposalFor("aes256-sha256")
	chosen.LifeSecs = 1200 // shorter than proposed: the result must honor it
	const responderSPI = 0x0BADCAFE
	saBody, _ := marshalESPSA([]Transform{chosen}, responderSPI)
	nr := bytes.Repeat([]byte{0x77}, 16)
	rest := append(marshalPayload(PayloadNonce, saBody), marshalPayload(PayloadNone, nr)...)
	hash2, _ := prf(HashSHA1, s.Keys.SKEYIDa, append(append(beUint32(h1.MessageID), ni...), rest...))
	plain2 := padToBlock(append(marshalPayload(PayloadSA, hash2), rest...), 8)
	ct2, _ := cbcEncrypt(s.Transform, s.Keys.EncKey, chain, plain2)
	chain = ct2[len(ct2)-8:]
	h2 := h1
	h2.Length = uint32(headerLen + len(ct2))
	peer.sendIKE(append(h2.Marshal(), ct2...))

	qm3 := peer.recvIKE()
	plain3, err := cbcDecrypt(s.Transform, s.Keys.EncKey, chain, qm3[headerLen:])
	if err != nil {
		t.Fatal(err)
	}
	want3, _ := quickModeHash3(HashSHA1, s.Keys.SKEYIDa, h1.MessageID, ni, nr)
	if !bytes.Equal(plain3[4:4+len(want3)], want3) {
		t.Fatal("client QM3 HASH(3) wrong")
	}

	r := <-done
	if r.err != nil {
		t.Fatalf("RekeyQuickMode: %v", r.err)
	}
	encLen, authLen := espKeyLens(chosen)
	wantOut, _ := quickModeKeymat(HashSHA1, s.Keys.SKEYIDd, protoIPsecESP, responderSPI, ni, nr, encLen+authLen)
	wantIn, _ := quickModeKeymat(HashSHA1, s.Keys.SKEYIDd, protoIPsecESP, initiatorSPI, ni, nr, encLen+authLen)
	if r.qm.Outbound.SPI != responderSPI || !bytes.Equal(r.qm.Outbound.EncKey, wantOut[:encLen]) {
		t.Fatal("outbound SA does not match the responder's keys")
	}
	if r.qm.Inbound.SPI != initiatorSPI || !bytes.Equal(r.qm.Inbound.AuthKey, wantIn[encLen:]) {
		t.Fatal("inbound SA does not match the responder's keys")
	}
	if r.qm.Lifetime != 1200*time.Second {
		t.Fatalf("lifetime %s, want the responder's shorter 20m", r.qm.Lifetime)
	}
}

func TestResponderLifetimeNotify(t *testing.T) {
	body := make([]byte, 8+4)
	binary.BigEndian.PutUint32(body[0:4], DOIIPsec)
	body[4], body[5] = protoIPsecESP, 4
	binary.BigEndian.PutUint16(body[6:8], notifyResponderLifetime)
	body = append(body, encodeAttrTV(ipsecAttrLifeType, lifeTypeSeconds)...)
	body = append(body, encodeAttrTV(ipsecAttrLifeDuration, 900)...)
	if got := responderLifetimeSecs(body); got != 900 {
		t.Fatalf("got %d, want 900", got)
	}
	if got := effectiveLifetime(espLifetime, 0, 900, 1800); got != 900*time.Second {
		t.Fatalf("effectiveLifetime = %s, want 15m", got)
	}
}

// decryptFromClient decrypts and authenticates a client-initiated
// Informational message the way a server would.
func (p *testPeer) decryptFromClient(msg []byte) []RawPayload {
	p.t.Helper()
	s := p.sess
	h, err := ParseHeader(msg)
	if err != nil || h.ExchangeType != ExchangeInformational {
		p.t.Fatalf("not an Informational message: %v", err)
	}
	bs := blockSize(s.Transform)
	iv, err := informationalIV(s.Transform.Hash, s.phase1IV, h.MessageID, bs)
	if err != nil {
		p.t.Fatalf("IV derivation: %v", err)
	}
	plain, err := cbcDecrypt(s.Transform, p.keys.EncKey, iv, msg[headerLen:])
	if err != nil {
		p.t.Fatalf("decrypt: %v", err)
	}
	payloads, err := SplitPayloads(h.NextPayload, plain)
	if err != nil {
		p.t.Fatalf("split payloads: %v", err)
	}
	if err := verifyHash1(s.Transform.Hash, p.keys.SKEYIDa, h.MessageID, h.NextPayload, payloads, plain); err != nil {
		p.t.Fatalf("client HASH(1) does not verify: %v", err)
	}
	return payloads[1:]
}

func TestAnswersServerDPD(t *testing.T) {
	peer, s := newTestPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.StartDataPhase(ctx, Events{})

	seq := []byte{0, 0, 0x12, 0x34}
	spi := append(append([]byte{}, s.InitiatorSPI[:]...), s.ResponderSPI[:]...)
	rut := marshalPayload(PayloadNone, notifyBody(protoISAKMP, notifyRUThere, spi, seq))
	peer.sendIKE(peer.encryptFirst(ExchangeInformational, 0x3333, rut, false, PayloadNotify))

	got := peer.decryptFromClient(peer.recvIKE())
	if len(got) != 1 || got[0].Type != PayloadNotify {
		t.Fatalf("reply payloads = %+v, want one Notify", got)
	}
	body := got[0].Body
	if typ := binary.BigEndian.Uint16(body[6:8]); typ != notifyRUThereAck {
		t.Fatalf("notify type = %d, want R-U-THERE-ACK %d", typ, notifyRUThereAck)
	}
	if !bytes.Equal(body[8:8+16], spi) || !bytes.Equal(body[8+16:], seq) {
		t.Fatalf("ACK must echo SPI and sequence, got %x", body[8:])
	}
}

func TestCloseSendsIKEDelete(t *testing.T) {
	peer, s := newTestPair(t)
	s.Close()
	s.Close() // idempotent

	got := peer.decryptFromClient(peer.recvIKE())
	if len(got) != 1 || got[0].Type != PayloadDelete {
		t.Fatalf("payloads = %+v, want one Delete", got)
	}
	proto, _, err := parseDelete(got[0].Body)
	if err != nil || proto != protoISAKMP {
		t.Fatalf("Delete proto = %d, %v; want ISAKMP", proto, err)
	}
	want := append(append([]byte{}, s.InitiatorSPI[:]...), s.ResponderSPI[:]...)
	if !bytes.Equal(got[0].Body[8:], want) {
		t.Fatalf("Delete names %x, want this SA's cookies %x", got[0].Body[8:], want)
	}
}
