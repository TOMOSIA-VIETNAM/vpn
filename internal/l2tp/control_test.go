package l2tp

import (
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
