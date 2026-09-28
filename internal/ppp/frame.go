// Package ppp implements the subset of PPP (RFC 1661) needed to bring up an
// IP link over L2TP: LCP negotiation, MS-CHAPv2 authentication (matching
// the reference config's `require-mschap-v2`, ~/l2tp-proxy's
// options.l2tpd.client), and IPCP.
//
// Framing note: PPP's HDLC byte-stuffing/escaping (RFC 1662) exists for
// byte-oriented serial links where frame boundaries aren't otherwise
// delimited. Carried inside L2TP data messages, frame boundaries are
// already exact (one L2TP data message = one PPP frame), so no flag bytes,
// escaping, or FCS are used here — this matches how every L2TP
// implementation (including the reference xl2tpd/pppd pairing) carries PPP.
package ppp

import (
	"encoding/binary"
	"fmt"
)

// Protocol field values, RFC 1661 §2 / RFC 3818.
const (
	ProtoIP   = 0x0021
	ProtoIPCP = 0x8021
	ProtoLCP  = 0xC021
	ProtoPAP  = 0xC023
	ProtoCHAP = 0xC223
)

// Frame is one decoded PPP frame (Address/Control fields already stripped).
type Frame struct {
	Protocol uint16
	Payload  []byte
}

// Marshal encodes a PPP frame with the standard (uncompressed) Address
// (0xFF) and Control (0x03) fields — this client never negotiates ACFC/PFC,
// so every frame it sends uses the full, unambiguous framing every PPP
// peer accepts regardless of what it would have preferred.
func (f Frame) Marshal() []byte {
	b := make([]byte, 4+len(f.Payload))
	b[0] = 0xFF // Address
	b[1] = 0x03 // Control
	binary.BigEndian.PutUint16(b[2:4], f.Protocol)
	copy(b[4:], f.Payload)
	return b
}

// Parse decodes a PPP frame, tolerating peers that use Address/Control Field
// Compression (i.e. omit the 0xFF 0x03 bytes) even though this client never
// requests it itself — some LNS implementations apply ACFC as soon as it's
// enabled in either direction rather than strictly per-direction.
func Parse(b []byte) (Frame, error) {
	if len(b) >= 2 && b[0] == 0xFF && b[1] == 0x03 {
		b = b[2:]
	}
	if len(b) < 2 {
		return Frame{}, fmt.Errorf("PPP frame too short for protocol field: %d bytes", len(b))
	}
	return Frame{Protocol: binary.BigEndian.Uint16(b[0:2]), Payload: b[2:]}, nil
}

// Control packet codes shared by LCP/IPCP (RFC 1661 §5).
const (
	CodeConfigureRequest = 1
	CodeConfigureAck     = 2
	CodeConfigureNak     = 3
	CodeConfigureReject  = 4
	CodeTerminateRequest = 5
	CodeTerminateAck     = 6
	CodeCodeReject       = 7
	CodeProtocolReject   = 8
	CodeEchoRequest      = 9
	CodeEchoReply        = 10
	CodeDiscardRequest   = 11
)

// ControlPacket is the common LCP/IPCP packet layout (RFC 1661 §5.1).
type ControlPacket struct {
	Code       uint8
	Identifier uint8
	Data       []byte // for Configure-*: encoded options; for Terminate-*: optional data; for Echo/Discard: magic number + data
}

func (p ControlPacket) Marshal() []byte {
	b := make([]byte, 4+len(p.Data))
	b[0] = p.Code
	b[1] = p.Identifier
	binary.BigEndian.PutUint16(b[2:4], uint16(4+len(p.Data)))
	copy(b[4:], p.Data)
	return b
}

func ParseControlPacket(b []byte) (ControlPacket, error) {
	if len(b) < 4 {
		return ControlPacket{}, fmt.Errorf("control packet too short: %d bytes", len(b))
	}
	length := int(binary.BigEndian.Uint16(b[2:4]))
	if length < 4 || length > len(b) {
		return ControlPacket{}, fmt.Errorf("control packet length %d invalid (%d bytes available)", length, len(b))
	}
	return ControlPacket{Code: b[0], Identifier: b[1], Data: b[4:length]}, nil
}

// Option is one LCP/IPCP configuration option (RFC 1661 §6 / RFC 1332 §3).
type Option struct {
	Type uint8
	Data []byte
}

func (o Option) Marshal() []byte {
	b := make([]byte, 2+len(o.Data))
	b[0] = o.Type
	b[1] = uint8(2 + len(o.Data))
	copy(b[2:], o.Data)
	return b
}

func MarshalOptions(opts []Option) []byte {
	var b []byte
	for _, o := range opts {
		b = append(b, o.Marshal()...)
	}
	return b
}

const maxOptions = 64

func ParseOptions(b []byte) ([]Option, error) {
	var out []Option
	for len(b) > 0 {
		if len(out) >= maxOptions {
			return nil, fmt.Errorf("PPP option count exceeds safety limit (%d)", maxOptions)
		}
		if len(b) < 2 {
			return nil, fmt.Errorf("truncated option header")
		}
		length := int(b[1])
		if length < 2 || length > len(b) {
			return nil, fmt.Errorf("option length %d invalid (%d bytes available)", length, len(b))
		}
		out = append(out, Option{Type: b[0], Data: append([]byte{}, b[2:length]...)})
		b = b[length:]
	}
	return out, nil
}

func FindOption(opts []Option, t uint8) (Option, bool) {
	for _, o := range opts {
		if o.Type == t {
			return o, true
		}
	}
	return Option{}, false
}
