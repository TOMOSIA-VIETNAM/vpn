package ppp

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestNegotiatedIPCPPointToPointPeer(t *testing.T) {
	local := net.ParseIP("192.168.100.205")
	peer := net.ParseIP("192.168.100.1")
	zero := net.ParseIP("0.0.0.0")

	tests := []struct {
		name     string
		ipcp     NegotiatedIPCP
		expected net.IP
	}{
		{
			name: "PeerIP provided",
			ipcp: NegotiatedIPCP{
				LocalIP: local,
				PeerIP:  peer,
			},
			expected: peer,
		},
		{
			name: "PeerIP is nil",
			ipcp: NegotiatedIPCP{
				LocalIP: local,
				PeerIP:  nil,
			},
			expected: defaultPeerIP,
		},
		{
			name: "PeerIP is 0.0.0.0",
			ipcp: NegotiatedIPCP{
				LocalIP: local,
				PeerIP:  zero,
			},
			expected: defaultPeerIP,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.ipcp.PointToPointPeer()
			if !got.Equal(tc.expected) {
				t.Fatalf("got %v, want %v", got, tc.expected)
			}
		})
	}
}

func TestApplyPeerOptionIgnoresZeroIP(t *testing.T) {
	n := &NegotiatedIPCP{
		LocalIP: net.ParseIP("192.168.100.205"),
	}

	// LNS sends IP-Address = 0.0.0.0 in its Configure-Request
	n.ApplyPeerOption(IPv4Option(IPCPOptIPAddress, net.IPv4zero))
	if n.PeerIP != nil {
		t.Fatalf("PeerIP should remain nil when 0.0.0.0 is received, got %v", n.PeerIP)
	}

	// LNS sends valid inside IP
	validPeer := net.ParseIP("192.168.100.1")
	n.ApplyPeerOption(IPv4Option(IPCPOptIPAddress, validPeer))
	if !n.PeerIP.Equal(validPeer) {
		t.Fatalf("PeerIP = %v, want %v", n.PeerIP, validPeer)
	}
}

// fakeIPCPPeer simulates an LNS negotiating IPCP.
type fakeIPCPPeer struct {
	assignedIP   net.IP
	peerInsideIP net.IP
	inbox        chan fakeFrame
}

func newFakeIPCPPeer(assignedIP, peerInsideIP net.IP) *fakeIPCPPeer {
	p := &fakeIPCPPeer{
		assignedIP:   assignedIP,
		peerInsideIP: peerInsideIP,
		inbox:        make(chan fakeFrame, 8),
	}
	var peerOpts []Option
	if peerInsideIP != nil {
		peerOpts = append(peerOpts, IPv4Option(IPCPOptIPAddress, peerInsideIP))
	}
	req := ControlPacket{
		Code:       CodeConfigureRequest,
		Identifier: 1,
		Data:       MarshalOptions(peerOpts),
	}
	p.inbox <- fakeFrame{ProtoIPCP, req.Marshal()}
	return p
}

func (p *fakeIPCPPeer) SendFrame(protocol uint16, payload []byte) error {
	if protocol != ProtoIPCP {
		return nil
	}
	pkt, err := ParseControlPacket(payload)
	if err != nil {
		return err
	}
	if pkt.Code == CodeConfigureAck {
		return nil
	}
	if pkt.Code != CodeConfigureRequest {
		return nil
	}

	opts, err := ParseOptions(pkt.Data)
	if err != nil {
		return err
	}

	// Check if client requested 0.0.0.0 or already our assignedIP
	var needsNak bool
	var nakOpts []Option
	for _, o := range opts {
		if o.Type == IPCPOptIPAddress {
			ip, _ := ParseIPv4Option(o)
			if !ip.Equal(p.assignedIP) {
				needsNak = true
				nakOpts = append(nakOpts, IPv4Option(IPCPOptIPAddress, p.assignedIP))
			}
		}
	}

	if needsNak {
		nak := ControlPacket{
			Code:       CodeConfigureNak,
			Identifier: pkt.Identifier,
			Data:       MarshalOptions(nakOpts),
		}
		p.inbox <- fakeFrame{ProtoIPCP, nak.Marshal()}
		return nil
	}

	ack := ControlPacket{
		Code:       CodeConfigureAck,
		Identifier: pkt.Identifier,
		Data:       pkt.Data,
	}
	p.inbox <- fakeFrame{ProtoIPCP, ack.Marshal()}
	return nil
}

func (p *fakeIPCPPeer) RecvFrame(ctx context.Context) (uint16, []byte, error) {
	select {
	case fr := <-p.inbox:
		return fr.protocol, fr.payload, nil
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	}
}

func TestRunIPCPWithPeerZeroIP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	assigned := net.ParseIP("192.168.100.205")
	// LNS sends 0.0.0.0 as its own IP
	peer := newFakeIPCPPeer(assigned, net.IPv4zero)

	res, err := runIPCP(ctx, peer, nil)
	if err != nil {
		t.Fatalf("runIPCP failed: %v", err)
	}

	if !res.LocalIP.Equal(assigned) {
		t.Fatalf("LocalIP = %v, want %v", res.LocalIP, assigned)
	}
	if res.PeerIP != nil {
		t.Fatalf("PeerIP should be nil for 0.0.0.0, got %v", res.PeerIP)
	}
	if !res.PointToPointPeer().Equal(defaultPeerIP) {
		t.Fatalf("PointToPointPeer = %v, want %v", res.PointToPointPeer(), defaultPeerIP)
	}
}
