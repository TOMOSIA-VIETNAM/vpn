package l2tp

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
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

// One of our 10-second Hellos is lost on the way to the server. Every later
// control message we send — Hellos AND the ZLB that acknowledges the server's
// own Hello — would carry an Ns the server considers "ahead" and drops, unless
// the lost Hello is resent first. If the server's Hello is never
// acknowledged, it tears the tunnel down 31 s later (StopCCN result 1
// "Timeout").
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
	if tun.unacked() {
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
	if !tun.unacked() {
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

// A message still pending is never silently replaced by a newer one.
func TestQueueNeverReplacesPending(t *testing.T) {
	tr := &scriptedTransport{in: make(chan []byte, 1)}
	tun := &Tunnel{t: tr, localTunnelID: 0x1111, peerTunnelID: 0x2222}
	tun.ctrlMu.Lock()
	defer tun.ctrlMu.Unlock()
	if err := tun.queueLocked(concatAVPs(MessageTypeAVP(MsgHello))); err != nil {
		t.Fatal(err)
	}
	if err := tun.queueLocked(concatAVPs(MessageTypeAVP(MsgCDN))); !errors.Is(err, errUnacked) {
		t.Fatalf("second queue: err %v, want errUnacked", err)
	}
	if tun.pendingNs != 0 || tun.ns != 1 || len(tr.sent) != 1 {
		t.Fatalf("pending Ns %d, next Ns %d, sent %d: the first message was disturbed", tun.pendingNs, tun.ns, len(tr.sent))
	}
}

// Close with a Hello still pending: that Hello goes out first, then CDN and
// StopCCN numbered right after it — an LNS that gets all three accepts them
// in order.
func TestCloseSendsPendingThenCDNThenStopCCN(t *testing.T) {
	tr := &scriptedTransport{in: make(chan []byte, 1)}
	tun := &Tunnel{t: tr, localTunnelID: 0x1111, peerTunnelID: 0x2222, peerSessionID: 9}
	_ = tun.SendHello() // lost
	lost := len(tr.sent)
	tun.Close()
	srv := &xl2tpdReceiver{}
	want := []uint16{MsgHello, MsgCDN, MsgStopCCN}
	got := tr.sent[lost:]
	if len(got) != len(want) {
		t.Fatalf("Close sent %d messages, want %d", len(got), len(want))
	}
	for i, raw := range got {
		if !srv.receive(t, raw) {
			t.Fatalf("message %d dropped as out of order", i)
		}
		m, _ := Parse(raw)
		if typ, _ := MessageType(m.AVPs); typ != want[i] {
			t.Fatalf("message %d is type %d, want %d", i, typ, want[i])
		}
	}
}

// brokenTransport fails every Recv at once, as a closed socket would.
type brokenTransport struct {
	scriptedTransport
	recvs int
}

func (b *brokenTransport) Recv(context.Context) ([]byte, error) {
	b.recvs++
	return nil, errors.New("use of closed network connection")
}

// A transport that fails outright ends the wait instead of spinning through
// resends for the whole of ackWait.
func TestAwaitAckStopsOnTransportError(t *testing.T) {
	tr := &brokenTransport{}
	tun := &Tunnel{t: tr, localTunnelID: 0x1111, peerTunnelID: 0x2222}
	tun.ctrlMu.Lock()
	defer tun.ctrlMu.Unlock()
	_ = tun.queueLocked(concatAVPs(MessageTypeAVP(MsgICCN)))
	tun.awaitAckLocked(t.Context())
	if tr.recvs != 1 || len(tr.sent) != 1 {
		t.Fatalf("%d receives and %d sends after a transport error, want 1 and 1 (no resend loop)", tr.recvs, len(tr.sent))
	}
}

// The LNS's first LCP frame arrives before any ack of our ICCN: the wait ends
// at once and RecvData delivers that frame instead of it being dropped.
func TestAwaitAckKeepsEarlyDataForRecvData(t *testing.T) {
	tr := &scriptedTransport{in: make(chan []byte, 2)}
	tun := &Tunnel{t: tr, localTunnelID: 0x1111, peerTunnelID: 0x2222, localSessionID: 1, peerSessionID: 9}
	lcp := []byte{0xff, 0x03, 0xc0, 0x21, 0x01}
	frame := MarshalData(0x1111, 1, lcp)
	tr.in <- frame
	tun.ctrlMu.Lock()
	_ = tun.queueLocked(concatAVPs(MessageTypeAVP(MsgICCN)))
	start := time.Now()
	tun.awaitAckLocked(t.Context())
	tun.ctrlMu.Unlock()
	if d := time.Since(start); d > retransmitInterval {
		t.Fatalf("kept waiting %s after the session's first data frame", d)
	}
	for i := range frame {
		frame[i] = 0 // the transport reuses its buffer
	}
	got, err := tun.RecvData(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, lcp) {
		t.Fatalf("RecvData returned %x, want the early LCP frame %x", got, lcp)
	}
}

// Every lost message gets its first resend logged, not only the first one of
// the session: the counter starts over once a message is acknowledged.
func TestResendCounterIsPerMessage(t *testing.T) {
	tr := &scriptedTransport{in: make(chan []byte, 1)}
	tun := &Tunnel{t: tr, localTunnelID: 0x1111, peerTunnelID: 0x2222}
	_ = tun.SendHello()
	_ = tun.SendHello() // resend
	tun.ctrlMu.Lock()
	tun.ackedLocked(1)
	resends := tun.resends
	tun.ctrlMu.Unlock()
	if resends != 0 {
		t.Fatalf("resends = %d after the ack, want 0", resends)
	}
}
