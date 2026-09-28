package ike

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"testing"
	"time"
)

// saWithProposal wraps a proposal body in an SA payload body (DOI, situation).
func saWithProposal(prop []byte) []byte {
	sa := make([]byte, 8)
	binary.BigEndian.PutUint32(sa[0:4], DOIIPsec)
	binary.BigEndian.PutUint32(sa[4:8], 1)
	return append(sa, marshalPayload(PayloadNone, prop)...)
}

// MM2 is unauthenticated: a proposal whose SPI size overruns it must be an
// error, not a panic in a process running as root.
func TestParseChosenTransformRejectsOversizedSPI(t *testing.T) {
	sa := saWithProposal([]byte{1, 1, 200, 1})
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	if _, err := ParseChosenTransform(sa); err == nil {
		t.Fatal("accepted a proposal whose SPI overruns it")
	}
}

func TestParseChosenESPSARejectsBadSPISize(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	for _, size := range []byte{0, 3, 200} {
		if _, err := parseChosenESPSA(saWithProposal([]byte{1, protoIPsecESP, size, 1, 0, 0, 0, 0})); err == nil {
			t.Fatalf("accepted an ESP proposal with a %d-byte SPI", size)
		}
	}
}

func mmHeader(ckyI, ckyR [8]byte, next, flags uint8) Header {
	return Header{InitiatorSPI: ckyI, ResponderSPI: ckyR, NextPayload: next, Version: 0x10, ExchangeType: ExchangeIdentityProt, Flags: flags}
}

func TestIsReplyMatchesOnlyTheExpectedMessage(t *testing.T) {
	ours, theirs, other := [8]byte{1}, [8]byte{2}, [8]byte{9}
	s := &Session{InitiatorSPI: ours}
	mm2 := mmReply{firstPayloads: []uint8{PayloadSA, PayloadNotify}}
	if !s.isReply(mmHeader(ours, theirs, PayloadSA, 0), mm2) {
		t.Fatal("MM2 rejected")
	}
	if s.isReply(mmHeader(other, theirs, PayloadSA, 0), mm2) {
		t.Fatal("another IKE SA's MM2 accepted")
	}
	s.ResponderSPI = theirs
	mm4 := mmReply{firstPayloads: []uint8{PayloadKE}}
	if s.isReply(mmHeader(ours, theirs, PayloadSA, 0), mm4) {
		t.Fatal("a duplicate MM2 accepted as MM4")
	}
	if !s.isReply(mmHeader(ours, theirs, PayloadKE, 0), mm4) {
		t.Fatal("MM4 rejected")
	}
	if s.isReply(mmHeader(ours, other, PayloadKE, 0), mm4) {
		t.Fatal("MM4 with a different responder cookie accepted")
	}
	mm6 := mmReply{encrypted: true}
	if s.isReply(mmHeader(ours, theirs, PayloadKE, 0), mm6) {
		t.Fatal("a duplicate MM4 accepted as MM6")
	}
	if !s.isReply(mmHeader(ours, theirs, PayloadID, FlagEncryption), mm6) {
		t.Fatal("MM6 rejected")
	}
}

// fakeReplier answers every message it receives with the next canned replies.
func fakeReplier(t *testing.T, replies ...[]byte) (*net.UDPConn, func()) {
	t.Helper()
	srv, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		buf := make([]byte, 2048)
		_, from, err := srv.ReadFromUDP(buf)
		if err != nil {
			return
		}
		for _, r := range replies {
			_, _ = srv.WriteToUDP(r, from)
		}
	}()
	return srv, func() { srv.Close() }
}

func testSession(t *testing.T, srv *net.UDPConn) *Session {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	addr := srv.LocalAddr().(*net.UDPAddr)
	return &Session{conn: conn, serverIP: addr.IP, destAddr: addr, InitiatorSPI: [8]byte{1}, ResponderSPI: [8]byte{2}}
}

func rawMsg(h Header, body []byte) []byte {
	h.Length = uint32(headerLen + len(body))
	return append(h.Marshal(), body...)
}

// The server answering a retransmitted MM1 twice leaves a duplicate MM2
// queued; waiting for MM4 must skip it and return the real MM4.
func TestExchangeSkipsDuplicateEarlierReply(t *testing.T) {
	ours, theirs := [8]byte{1}, [8]byte{2}
	dupMM2 := rawMsg(mmHeader(ours, theirs, PayloadSA, 0), marshalPayload(PayloadNone, make([]byte, 8)))
	mm4 := rawMsg(mmHeader(ours, theirs, PayloadKE, 0), marshalPayload(PayloadNone, make([]byte, 8)))
	srv, stop := fakeReplier(t, dupMM2, mm4)
	defer stop()
	s := testSession(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	got, err := s.exchangeN(ctx, []byte("MM3"), mmReply{firstPayloads: []uint8{PayloadKE}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if h, _ := ParseHeader(got); h.NextPayload != PayloadKE {
		t.Fatalf("returned a message opening with payload %d, want KE", h.NextPayload)
	}
}

// NO-PROPOSAL-CHOSEN (14) in an unencrypted Informational ends the wait at
// once instead of running out the retransmit budget.
func TestExchangeReturnsServerRefusal(t *testing.T) {
	ours := [8]byte{1}
	notify := make([]byte, 8)
	binary.BigEndian.PutUint32(notify[0:4], DOIIPsec)
	notify[4] = 1 // ISAKMP
	binary.BigEndian.PutUint16(notify[6:8], 14)
	refusal := rawMsg(Header{InitiatorSPI: ours, NextPayload: PayloadNotify, Version: 0x10, ExchangeType: ExchangeInformational}, marshalPayload(PayloadNone, notify))
	srv, stop := fakeReplier(t, refusal)
	defer stop()
	s := testSession(t, srv)
	s.ResponderSPI = [8]byte{}
	start := time.Now()
	_, err := s.exchangeN(context.Background(), []byte("MM1"), mmReply{firstPayloads: []uint8{PayloadSA}}, maxRetransmits)
	if !errors.Is(err, ErrServerRefused) {
		t.Fatalf("err = %v, want ErrServerRefused", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("waited out retransmits instead of returning the refusal")
	}
}

// The same refusal under someone else's cookie is not ours to act on.
func TestPlainErrorNotifyNeedsOurCookie(t *testing.T) {
	s := &Session{InitiatorSPI: [8]byte{1}}
	notify := make([]byte, 8)
	binary.BigEndian.PutUint16(notify[6:8], 14)
	body := marshalPayload(PayloadNone, notify)
	h := Header{InitiatorSPI: [8]byte{9}, NextPayload: PayloadNotify, ExchangeType: ExchangeInformational}
	if _, ok := s.plainErrorNotify(h, body); ok {
		t.Fatal("acted on a refusal for another IKE SA")
	}
	h.InitiatorSPI = s.InitiatorSPI
	if nt, ok := s.plainErrorNotify(h, body); !ok || nt != 14 {
		t.Fatalf("got %d/%v, want 14", nt, ok)
	}
	h.Flags = FlagEncryption
	if _, ok := s.plainErrorNotify(h, body); ok {
		t.Fatal("parsed an encrypted body as plaintext")
	}
}
