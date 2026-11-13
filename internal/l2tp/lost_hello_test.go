package l2tp

import (
	"errors"
	"testing"
)

// xl2tpdReceiver models how xl2tpd's check_control() (control.c) accepts
// control messages from us: only Ns == expected is accepted. An Ns that is
// ahead (an earlier message of ours was lost) is dropped with -EINVAL BEFORE
// its Nr is read, and ZLBs get no exemption. Only an accepted message updates
// cLr, which is what stops xl2tpd retransmitting its own Hello; after 5
// retries (1+2+4+8+16 = 31 s) it closes the tunnel with StopCCN "Timeout".
type xl2tpdReceiver struct {
	expectNs uint16 // control_rec_seq_num
	ackedNr  uint16 // cLr: what xl2tpd believes we have received from it
}

func (x *xl2tpdReceiver) receive(t *testing.T, raw []byte) (accepted bool) {
	t.Helper()
	m, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if m.Header.Ns != x.expectNs {
		return false // out of order: dropped, Nr ignored
	}
	if len(m.AVPs) > 0 {
		x.expectNs++ // a ZLB takes no sequence number
	}
	x.ackedNr = m.Header.Nr
	return true
}

// Reproduces the "StopCCN result 1 Timeout" disconnects seen under heavy
// traffic: one of our 10-second Hellos is lost on the way to the server.
// SendHello never retransmits it, so from then on every control message we
// send — later Hellos AND the ZLB that acknowledges the server's own Hello —
// carries an Ns the server considers "ahead" and is dropped. The server's
// Hello is never acknowledged, and it tears the tunnel down 31 s later.
//
// What must hold: after the server's Hello reaches us, the server must
// eventually accept an acknowledgment of it.
func TestLostHelloStillLetsServerSeeOurAck(t *testing.T) {
	tr := &scriptedTransport{in: make(chan []byte, 8)}
	tun := &Tunnel{t: tr, localTunnelID: 0x1111, peerTunnelID: 0x2222, localSessionID: 1, peerSessionID: 9}
	srv := &xl2tpdReceiver{}

	// Hello #1 is sent but lost on the network (or by a full socket buffer).
	if err := tun.SendHello(); err != nil {
		t.Fatal(err)
	}
	lost := len(tr.sent)

	// Ten seconds later Hello #2 goes out and arrives.
	if err := tun.SendHello(); err != nil {
		t.Fatal(err)
	}
	for _, m := range tr.sent[lost:] {
		srv.receive(t, m)
	}

	// The server's periodic Hello (its Ns 0) arrives; we answer with a ZLB.
	before := len(tr.sent)
	tr.in <- MarshalControl(0x1111, 0, 0, srv.expectNs, concatAVPs(MessageTypeAVP(MsgHello))) // acks what it accepted
	tr.in <- MarshalData(0x1111, 1, []byte{0xff, 0x03, 0x00, 0x21})                           // let RecvData return
	if _, err := tun.RecvData(t.Context()); err != nil {
		t.Fatal(err)
	}

	// Further Hellos of ours (the server retransmits its Hello for 31 s).
	for range 3 {
		_ = tun.SendHello()
	}
	for _, m := range tr.sent[before:] {
		srv.receive(t, m)
	}
	if srv.ackedNr < 1 {
		t.Fatalf("server never accepted an ack of its Hello (expects our Ns %d, we are at %d): "+
			"it retransmits for 31 s and closes the tunnel with StopCCN \"Timeout\"", srv.expectNs, tun.ns)
	}
}

// The server's Hello arrives while our lost Hello is still unacknowledged
// (its Nr says it never got it). The ack we send must be that Hello again —
// in order, carrying our new Nr — not a ZLB numbered past it.
func TestServerHelloDuringGapIsAckedByResendingPending(t *testing.T) {
	tr := &scriptedTransport{in: make(chan []byte, 8)}
	tun := &Tunnel{t: tr, localTunnelID: 0x1111, peerTunnelID: 0x2222, localSessionID: 1, peerSessionID: 9}
	srv := &xl2tpdReceiver{}

	_ = tun.SendHello() // lost
	before := len(tr.sent)
	tr.in <- MarshalControl(0x1111, 0, 0, srv.expectNs, concatAVPs(MessageTypeAVP(MsgHello)))
	tr.in <- MarshalData(0x1111, 1, []byte{0xff, 0x03, 0x00, 0x21})
	if _, err := tun.RecvData(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(tr.sent) != before+1 {
		t.Fatalf("sent %d messages in answer to the server's Hello, want 1", len(tr.sent)-before)
	}
	if !srv.receive(t, tr.sent[before]) || srv.ackedNr != 1 {
		t.Fatalf("server did not accept our answer as an in-order ack of its Hello (accepted Nr %d)", srv.ackedNr)
	}
	// Once the server's Nr acknowledges it, nothing stays pending.
	tr.in <- MarshalControl(0x1111, 0, 1, srv.expectNs, nil) // its ZLB
	tr.in <- MarshalData(0x1111, 1, []byte{0xff, 0x03, 0x00, 0x21})
	if _, err := tun.RecvData(t.Context()); err != nil {
		t.Fatal(err)
	}
	if tun.Unacked() {
		t.Fatal("Hello still pending after the server acknowledged it")
	}
}

// failingOnce fails its first Send, as a full socket buffer (ENOBUFS) does
// under heavy traffic, then behaves like scriptedTransport.
type failingOnce struct {
	scriptedTransport
	failed bool
}

func (f *failingOnce) Send(m []byte) error {
	if !f.failed {
		f.failed = true
		return errors.New("sendto: no buffer space available")
	}
	return f.scriptedTransport.Send(m)
}

// A Hello whose send fails is not lost for good: the engine logs the error
// ("it stays pending and is resent") and the next SendHello sends that same
// Hello — same Ns — instead of a new one numbered past it.
func TestHelloSendFailureStaysPending(t *testing.T) {
	tr := &failingOnce{scriptedTransport: scriptedTransport{in: make(chan []byte, 1)}}
	tun := &Tunnel{t: tr, localTunnelID: 0x1111, peerTunnelID: 0x2222, localSessionID: 1, peerSessionID: 9}
	if err := tun.SendHello(); err == nil {
		t.Fatal("first send should report its error to the caller (the engine logs it)")
	}
	if !tun.Unacked() {
		t.Fatal("failed Hello was dropped instead of kept pending")
	}
	if err := tun.SendHello(); err != nil {
		t.Fatal(err)
	}
	srv := &xl2tpdReceiver{}
	if len(tr.sent) != 1 || !srv.receive(t, tr.sent[0]) {
		t.Fatal("the resent Hello was not accepted in order by the server")
	}
}
