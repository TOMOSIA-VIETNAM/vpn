package ppp

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
	"time"
)

const (
	testOurMagic  = 0x11223344
	testPeerMagic = 0xaabbccdd
)

func magicBytes(m uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, m)
	return b
}

// recordingTransport keeps every frame the client sends.
type recordingTransport struct{ sent []fakeFrame }

func (r *recordingTransport) SendFrame(protocol uint16, payload []byte) error {
	r.sent = append(r.sent, fakeFrame{protocol, append([]byte(nil), payload...)})
	return nil
}

func (r *recordingTransport) RecvFrame(ctx context.Context) (uint16, []byte, error) {
	<-ctx.Done()
	return 0, nil, ctx.Err()
}

// A reply that echoes the requester's magic back is discarded by pppd as
// looped back, so the LNS ends the session after lcp-echo-failure misses —
// reproduced live as every tunnel dying ~2 minutes after connect.
func TestEchoReplyCarriesOwnMagic(t *testing.T) {
	req := ControlPacket{Code: CodeEchoRequest, Identifier: 42, Data: append(magicBytes(testPeerMagic), "data"...)}
	got := EchoReply(req, testOurMagic)
	if got.Code != CodeEchoReply || got.Identifier != 42 {
		t.Fatalf("code/id = %d/%d, want %d/42", got.Code, got.Identifier, CodeEchoReply)
	}
	if want := append(magicBytes(testOurMagic), "data"...); !bytes.Equal(got.Data, want) {
		t.Fatalf("data = %x, want %x (our magic, then the request's data)", got.Data, want)
	}
	if !bytes.Equal(req.Data[:4], magicBytes(testPeerMagic)) {
		t.Fatal("EchoReply modified the request")
	}
}

func TestEchoReplyToRequestWithoutMagic(t *testing.T) {
	got := EchoReply(ControlPacket{Code: CodeEchoRequest, Identifier: 1}, testOurMagic)
	if !bytes.Equal(got.Data, magicBytes(testOurMagic)) {
		t.Fatalf("data = %x, want just our magic", got.Data)
	}
}

func TestHandleOpenedLCP(t *testing.T) {
	echo := ControlPacket{Code: CodeEchoRequest, Identifier: 7, Data: magicBytes(testPeerMagic)}
	confReq := ControlPacket{Code: CodeConfigureRequest, Identifier: 3, Data: []byte{1, 4, 5, 0x78}}
	for _, tc := range []struct {
		name string
		in   []byte
		want []byte // nil: nothing sent
	}{
		{"echo request", echo.Marshal(), EchoReply(echo, testOurMagic).Marshal()},
		{"retransmitted configure-request", confReq.Marshal(), ControlPacket{Code: CodeConfigureAck, Identifier: 3, Data: confReq.Data}.Marshal()},
		{"discard request", ControlPacket{Code: CodeDiscardRequest, Identifier: 1, Data: magicBytes(testPeerMagic)}.Marshal(), nil},
		{"garbage", []byte{9}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt := &recordingTransport{}
			HandleOpenedLCP(rt, tc.in, testOurMagic)
			if tc.want == nil {
				if len(rt.sent) != 0 {
					t.Fatalf("sent %d frames, want none", len(rt.sent))
				}
				return
			}
			if len(rt.sent) != 1 || rt.sent[0].protocol != ProtoLCP || !bytes.Equal(rt.sent[0].payload, tc.want) {
				t.Fatalf("sent %+v, want one LCP frame %x", rt.sent, tc.want)
			}
		})
	}
}

// fakeLCPPeer sends its own Configure-Request, then acks ours — or first
// rejects our Magic-Number when rejectMagic is set.
type fakeLCPPeer struct {
	rejectMagic bool
	inbox       chan fakeFrame
}

func newFakeLCPPeer(rejectMagic bool) *fakeLCPPeer {
	p := &fakeLCPPeer{rejectMagic: rejectMagic, inbox: make(chan fakeFrame, 8)}
	req := ControlPacket{Code: CodeConfigureRequest, Identifier: 9, Data: MarshalOptions([]Option{{Type: OptMagicNumber, Data: magicBytes(testPeerMagic)}})}
	p.inbox <- fakeFrame{ProtoLCP, req.Marshal()}
	return p
}

func (p *fakeLCPPeer) SendFrame(protocol uint16, payload []byte) error {
	pkt, err := ParseControlPacket(payload)
	if err != nil || protocol != ProtoLCP || pkt.Code != CodeConfigureRequest {
		return err
	}
	opts, err := ParseOptions(pkt.Data)
	if err != nil {
		return err
	}
	if p.rejectMagic && magicOf(opts) != 0 {
		rej := ControlPacket{Code: CodeConfigureReject, Identifier: pkt.Identifier, Data: MarshalOptions([]Option{{Type: OptMagicNumber, Data: magicBytes(magicOf(opts))}})}
		p.inbox <- fakeFrame{ProtoLCP, rej.Marshal()}
		return nil
	}
	ack := ControlPacket{Code: CodeConfigureAck, Identifier: pkt.Identifier, Data: pkt.Data}
	p.inbox <- fakeFrame{ProtoLCP, ack.Marshal()}
	return nil
}

func (p *fakeLCPPeer) RecvFrame(ctx context.Context) (uint16, []byte, error) {
	select {
	case fr := <-p.inbox:
		return fr.protocol, fr.payload, nil
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	}
}

func TestRunLCPReturnsNegotiatedMagic(t *testing.T) {
	for _, tc := range []struct {
		name        string
		rejectMagic bool
		want        uint32
	}{
		{"acked", false, testOurMagic},
		{"rejected by the peer", true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			got, err := runLCP(ctx, newFakeLCPPeer(tc.rejectMagic), LCPConfig{MRU: 1400, MagicNumber: testOurMagic})
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("magic = %08x, want %08x", got, tc.want)
			}
		})
	}
}

// brokenTransport's link is gone: every receive fails at once.
type brokenTransport struct{ sends int }

func (b *brokenTransport) SendFrame(uint16, []byte) error { b.sends++; return nil }
func (b *brokenTransport) RecvFrame(context.Context) (uint16, []byte, error) {
	return 0, nil, errors.New("the server closed the L2TP session")
}

// A failed link ends LCP at once, with its reason, instead of retransmitting
// Configure-Requests into it until the phase deadline.
func TestRunLCPStopsOnFailedLink(t *testing.T) {
	bt := &brokenTransport{}
	start := time.Now()
	_, err := runLCP(context.Background(), bt, LCPConfig{MRU: 1400, MagicNumber: testOurMagic})
	if err == nil || !strings.Contains(err.Error(), "closed the L2TP session") {
		t.Fatalf("err = %v, want the link's own error", err)
	}
	if time.Since(start) > time.Second || bt.sends > 1 {
		t.Fatalf("kept going on a dead link: %v, %d sends", time.Since(start), bt.sends)
	}
}
