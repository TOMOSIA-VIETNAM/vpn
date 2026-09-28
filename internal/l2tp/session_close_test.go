package l2tp

import (
	"context"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
	"time"
)

func controlTo(tunnelID, sessionID, ns, nr uint16, avps ...[]byte) []byte {
	return MarshalControl(tunnelID, sessionID, ns, nr, concatAVPs(avps...))
}

func hasAVP(avps []AVP, typ uint16) bool {
	_, ok := Find(avps, typ)
	return ok
}

// RFC 2661 §6.12: ICCN carries (Tx) Connect Speed and Framing Type and no
// Bearer Type, which belongs in ICRQ. A strict LNS drops an ICCN without
// them and never starts PPP for the session.
func TestIncomingCallSendsRFCMandatoryAVPs(t *testing.T) {
	tr := &scriptedTransport{in: make(chan []byte, 4)}
	tun := &Tunnel{t: tr, localTunnelID: 0x1234, peerTunnelID: 0x4321}
	tr.in <- controlTo(0x1234, 0, 0, 1, MessageTypeAVP(MsgICRP), AssignedSessionIDAVP(77))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := tun.doIncomingCall(ctx); err != nil {
		t.Fatal(err)
	}
	if len(tr.sent) != 2 {
		t.Fatalf("sent %d messages, want ICRQ and ICCN", len(tr.sent))
	}
	icrq, err := Parse(tr.sent[0])
	if err != nil {
		t.Fatal(err)
	}
	iccn, err := Parse(tr.sent[1])
	if err != nil {
		t.Fatal(err)
	}
	if mt, _ := MessageType(iccn.AVPs); mt != MsgICCN {
		t.Fatalf("second message is type %d, want ICCN", mt)
	}
	speed, ok := Find(iccn.AVPs, AVPTxConnectSpeed)
	if !ok || !speed.Mandatory || len(speed.Value) != 4 || binary.BigEndian.Uint32(speed.Value) == 0 {
		t.Fatalf("ICCN (Tx) Connect Speed missing or invalid: %+v", speed)
	}
	if !hasAVP(iccn.AVPs, AVPFramingType) {
		t.Fatal("ICCN has no Framing Type")
	}
	if hasAVP(iccn.AVPs, AVPBearerType) {
		t.Fatal("ICCN carries Bearer Type, which RFC 2661 §6.12 does not allow")
	}
	if !hasAVP(icrq.AVPs, AVPBearerType) {
		t.Fatal("ICRQ lost its Bearer Type")
	}
	if tun.peerSessionID != 77 {
		t.Fatalf("peer session = %d, want 77", tun.peerSessionID)
	}
}

func resultCodeAVP(result, code uint16, msg string) []byte {
	v := make([]byte, 4, 4+len(msg))
	binary.BigEndian.PutUint16(v[0:2], result)
	binary.BigEndian.PutUint16(v[2:4], code)
	return MarshalAVP(true, AVPResultCode, append(v, msg...))
}

// The LNS ending our session must end the wait at once, with its reason —
// silently discarding the CDN left PPP waiting out its whole timeout.
func TestRecvDataReturnsServerClose(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  []byte
		want string
	}{
		{"CDN", controlTo(0x1234, 1, 0, 0, MessageTypeAVP(MsgCDN), resultCodeAVP(2, 6, "missing AVP"), AssignedSessionIDAVP(77)), `CDN, result 2, error 6: "missing AVP"`},
		{"StopCCN", controlTo(0x1234, 0, 0, 0, MessageTypeAVP(MsgStopCCN), resultCodeAVP(1, 0, ""), AssignedTunnelIDAVP(0x4321)), "StopCCN, result 1, error 0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := &scriptedTransport{in: make(chan []byte, 4)}
			tun := &Tunnel{t: tr, localTunnelID: 0x1234, localSessionID: 1}
			tr.in <- tc.msg
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			for i := 0; i < 2; i++ { // and it stays closed
				_, err := tun.RecvData(ctx)
				if !errors.Is(err, ErrPeerClosed) || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("call %d: err = %v, want ErrPeerClosed mentioning %q", i+1, err, tc.want)
				}
			}
		})
	}
}

// A CDN for another session of the tunnel is not ours.
func TestRecvDataIgnoresOtherSessionsCDN(t *testing.T) {
	tr := &scriptedTransport{in: make(chan []byte, 4)}
	tun := &Tunnel{t: tr, localTunnelID: 0x1234, localSessionID: 1}
	tr.in <- controlTo(0x1234, 9, 0, 0, MessageTypeAVP(MsgCDN), resultCodeAVP(3, 0, ""))
	tr.in <- MarshalData(0x1234, 1, []byte{0xff, 0x03, 0xc0, 0x21, 1})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := tun.RecvData(ctx); err != nil {
		t.Fatalf("another session's CDN closed ours: %v", err)
	}
}
