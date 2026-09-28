package l2tp

import (
	"context"
	"encoding/binary"
	"testing"
)

func TestTxConnectSpeedAVP(t *testing.T) {
	raw := TxConnectSpeedAVP(1_000_000_000)
	avps, err := ParseAVPs(raw)
	if err != nil {
		t.Fatalf("ParseAVPs failed: %v", err)
	}
	if len(avps) != 1 {
		t.Fatalf("expected 1 AVP, got %d", len(avps))
	}
	avp := avps[0]
	if avp.Type != AVPTxConnectSpeed {
		t.Errorf("expected AVP type %d, got %d", AVPTxConnectSpeed, avp.Type)
	}
	if len(avp.Value) != 4 {
		t.Fatalf("expected 4-byte value, got %d", len(avp.Value))
	}
	speed := binary.BigEndian.Uint32(avp.Value)
	if speed != 1_000_000_000 {
		t.Errorf("expected speed 1000000000, got %d", speed)
	}
}

func TestRxConnectSpeedAVP(t *testing.T) {
	raw := RxConnectSpeedAVP(1_000_000_000)
	avps, err := ParseAVPs(raw)
	if err != nil {
		t.Fatalf("ParseAVPs failed: %v", err)
	}
	if len(avps) != 1 {
		t.Fatalf("expected 1 AVP, got %d", len(avps))
	}
	avp := avps[0]
	if avp.Type != AVPRxConnectSpeed {
		t.Errorf("expected AVP type %d, got %d", AVPRxConnectSpeed, avp.Type)
	}
	if len(avp.Value) != 4 {
		t.Fatalf("expected 4-byte value, got %d", len(avp.Value))
	}
	speed := binary.BigEndian.Uint32(avp.Value)
	if speed != 1_000_000_000 {
		t.Errorf("expected speed 1000000000, got %d", speed)
	}
}

type fakeControlTransport struct {
	sent [][]byte
}

func (f *fakeControlTransport) Send(msg []byte) error {
	f.sent = append(f.sent, append([]byte{}, msg...))
	return nil
}

func (f *fakeControlTransport) Recv(ctx context.Context) ([]byte, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestSendHello(t *testing.T) {
	ft := &fakeControlTransport{}
	tun := &Tunnel{t: ft, peerTunnelID: 10, peerSessionID: 20}
	if err := tun.SendHello(); err != nil {
		t.Fatalf("SendHello failed: %v", err)
	}
	if len(ft.sent) != 1 {
		t.Fatalf("expected 1 sent message, got %d", len(ft.sent))
	}
	parsed, err := Parse(ft.sent[0])
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if !parsed.Header.IsControl {
		t.Fatalf("expected control message")
	}
	msgType, err := MessageType(parsed.AVPs)
	if err != nil {
		t.Fatalf("MessageType failed: %v", err)
	}
	if msgType != MsgHello {
		t.Fatalf("expected MsgHello (%d), got %d", MsgHello, msgType)
	}
}
