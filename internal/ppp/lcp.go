package ppp

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"

	"vpn/internal/vpnlog"
)

// LCP option types, RFC 1661 §6.
const (
	OptMRU          = 1
	OptAuthProtocol = 3
	OptMagicNumber  = 5
	OptPFC          = 7 // Protocol Field Compression — never requested by this client
	OptACFC         = 8 // Address/Control Field Compression — never requested by this client
)

// LCPConfig is what this client proposes/accepts during LCP negotiation,
// matching the reference PPP options file (~/l2tp-proxy's
// options.l2tpd.client: refuse-eap, require-mschap-v2, noccp).
type LCPConfig struct {
	MRU         uint16 // matches the tunnel MTU budget, computed by the engine (see internal/engine mtu calculation)
	MagicNumber uint32
}

// NewLCPConfig picks a random magic number (RFC 1661 §6.4 — used for
// loopback detection; a real per-run random value, not a fixed constant).
func NewLCPConfig(mru uint16) (LCPConfig, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return LCPConfig{}, fmt.Errorf("generate LCP magic number: %w", err)
	}
	return LCPConfig{MRU: mru, MagicNumber: binary.BigEndian.Uint32(b[:])}, nil
}

// ConfigureRequestOptions builds this client's outgoing LCP Configure-Request
// options: MRU and Magic-Number. Deliberately does not request
// Auth-Protocol (this client authenticates in the CHAP *server* role is
// never true — it is the one being challenged, matching pppd's `noauth`
// meaning "don't require the peer to authenticate to us", so requesting our
// own Auth-Protocol option would be requesting the LNS authenticate to us,
// which the reference config never does either) and does not request
// PFC/ACFC, keeping framing fully explicit.
func (c LCPConfig) ConfigureRequestOptions() []Option {
	mru := make([]byte, 2)
	binary.BigEndian.PutUint16(mru, c.MRU)
	magic := make([]byte, 4)
	binary.BigEndian.PutUint32(magic, c.MagicNumber)
	return []Option{
		{Type: OptMRU, Data: mru},
		{Type: OptMagicNumber, Data: magic},
	}
}

// AuthProtocol identifies which authentication protocol the peer's
// Configure-Request asked for (RFC 1661 §6.6). This client only implements
// CHAP/MS-CHAPv2 (matching require-mschap-v2) — PAP and EAP requests are
// rejected via Configure-Reject.
type AuthProtocol struct {
	Protocol  uint16 // ProtoCHAP (0xC223), or 0 if peer requested something else / didn't request auth
	Algorithm uint8  // CHAP algorithm byte: 0x05 = MD5, 0x81 = MS-CHAP-v2 (RFC 2759 §1)
}

const (
	CHAPAlgoMD5      = 5
	CHAPAlgoMSCHAPv2 = 0x81
)

// ParseAuthProtocolOption decodes an Auth-Protocol option's data (protocol
// number, plus one algorithm byte for CHAP).
func ParseAuthProtocolOption(data []byte) (AuthProtocol, error) {
	if len(data) < 2 {
		return AuthProtocol{}, fmt.Errorf("Auth-Protocol option too short")
	}
	proto := binary.BigEndian.Uint16(data[0:2])
	ap := AuthProtocol{Protocol: proto}
	if proto == ProtoCHAP {
		if len(data) < 3 {
			return AuthProtocol{}, fmt.Errorf("CHAP Auth-Protocol option missing algorithm byte")
		}
		ap.Algorithm = data[2]
	}
	return ap, nil
}

// CHAPAuthProtocolOption builds the Auth-Protocol option value for
// "CHAP with MS-CHAP-v2", used only if this client ever needs to assert its
// own auth requirement (it doesn't, by default — see ConfigureRequestOptions
// — but the reference LNS may Configure-Nak our bare request and specify
// this, which we then must accept).
func CHAPAuthProtocolOption(algorithm uint8) Option {
	data := make([]byte, 3)
	binary.BigEndian.PutUint16(data[0:2], ProtoCHAP)
	data[2] = algorithm
	return Option{Type: OptAuthProtocol, Data: data}
}

// magicOf returns the Magic-Number among opts, or 0 when there is none —
// the value RFC 1661 §6.4 has a side use when it has not negotiated one
// (e.g. the peer Configure-Rejected ours).
func magicOf(opts []Option) uint32 {
	for _, o := range opts {
		if o.Type == OptMagicNumber && len(o.Data) == 4 {
			return binary.BigEndian.Uint32(o.Data)
		}
	}
	return 0
}

// mruOf returns the negotiated MRU from opts, or 0 if not present.
func mruOf(opts []Option) uint16 {
	for _, o := range opts {
		if o.Type == OptMRU && len(o.Data) == 2 {
			return binary.BigEndian.Uint16(o.Data)
		}
	}
	return 0
}

// EchoReply answers an LCP Echo-Request (RFC 1661 §5.8). The reply's
// Magic-Number is the replier's own, never the requester's: pppd discards
// a reply carrying its own magic as a looped-back packet ("appear to have
// received our own echo-reply"), so a reply that just echoed the request
// back counts as no reply at all, and after lcp-echo-failure misses in a
// row (4 x 30s in the common xl2tpd/pppd setup) the LNS tears the link
// down.
func EchoReply(req ControlPacket, magic uint32) ControlPacket {
	data := make([]byte, 4, 4+len(req.Data))
	binary.BigEndian.PutUint32(data, magic)
	if len(req.Data) > 4 {
		data = append(data, req.Data[4:]...)
	}
	return ControlPacket{Code: CodeEchoReply, Identifier: req.Identifier, Data: data}
}

// HandleOpenedLCP answers the LCP packets the peer can still send once
// LCP is Opened — during authentication and for the rest of the session:
// Echo-Requests (its keepalive) and a retransmitted Configure-Request
// (our earlier Ack to it was lost; left unacked, the peer's LCP state
// machine stays stuck — reproduced live as a CHAP Challenge that never
// came). magic is ours from Result.Magic. Anything else, including
// unparsable packets, is ignored.
func HandleOpenedLCP(t Transport, payload []byte, magic uint32) {
	pkt, err := ParseControlPacket(payload)
	if err != nil {
		return
	}
	switch pkt.Code {
	case CodeEchoRequest:
		err := t.SendFrame(ProtoLCP, EchoReply(pkt, magic).Marshal())
		if err != nil {
			vpnlog.Error(stage, "failed to answer LCP Echo-Request", vpnlog.Fields{"id": pkt.Identifier, "err": err})
		} else {
			vpnlog.Debug(stage, "answered LCP Echo-Request (keepalive OK)", vpnlog.Fields{"id": pkt.Identifier})
		}
	case CodeConfigureRequest:
		reply := ControlPacket{Code: CodeConfigureAck, Identifier: pkt.Identifier, Data: pkt.Data}
		_ = t.SendFrame(ProtoLCP, reply.Marshal())
	}
}
