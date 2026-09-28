// Package ike implements the subset of IKEv1 (RFC 2409) Main Mode with
// pre-shared-key authentication, plus NAT-T (RFC 3947/3948), needed to
// establish a Phase 1 SA and a Phase 2 (Quick Mode) IPsec SA against an
// L2TP/IPsec server — matching the compatibility reference at
// ~/l2tp-proxy/entrypoint.sh (keyexchange=ikev1, PSK auth).
package ike

import (
	"encoding/binary"
	"fmt"
)

// Payload type octets, RFC 2408 §3.1.
const (
	PayloadNone      = 0
	PayloadSA        = 1
	PayloadProposal  = 2
	PayloadTransform = 3
	PayloadKE        = 4
	PayloadID        = 5
	PayloadCert      = 6
	PayloadCertReq   = 7
	PayloadHash      = 8
	PayloadSig       = 9
	PayloadNonce     = 10
	PayloadNotify    = 11
	PayloadDelete    = 12
	PayloadVendorID  = 13
)

// Exchange types, RFC 2408 §3.1.
const (
	ExchangeBase          = 1
	ExchangeIdentityProt  = 2 // "Main Mode"
	ExchangeAuthOnly      = 3
	ExchangeAggressive    = 4
	ExchangeInformational = 5
	ExchangeQuickMode     = 32
)

// Domain of Interpretation / doi.
const DOIIPsec = 1

// Flags, RFC 2408 §3.1.
const (
	FlagEncryption = 1 << 0
	FlagCommit     = 1 << 1
	FlagAuthOnly   = 1 << 2
)

const headerLen = 28

// Header is the fixed ISAKMP header prepended to every message.
type Header struct {
	InitiatorSPI [8]byte
	ResponderSPI [8]byte
	NextPayload  uint8
	Version      uint8 // major<<4 | minor, always 0x10 for IKEv1
	ExchangeType uint8
	Flags        uint8
	MessageID    uint32
	Length       uint32
}

func (h Header) Marshal() []byte {
	b := make([]byte, headerLen)
	copy(b[0:8], h.InitiatorSPI[:])
	copy(b[8:16], h.ResponderSPI[:])
	b[16] = h.NextPayload
	b[17] = h.Version
	b[18] = h.ExchangeType
	b[19] = h.Flags
	binary.BigEndian.PutUint32(b[20:24], h.MessageID)
	binary.BigEndian.PutUint32(b[24:28], h.Length)
	return b
}

func ParseHeader(b []byte) (Header, error) {
	if len(b) < headerLen {
		return Header{}, fmt.Errorf("ISAKMP header truncated: got %d bytes, want %d", len(b), headerLen)
	}
	var h Header
	copy(h.InitiatorSPI[:], b[0:8])
	copy(h.ResponderSPI[:], b[8:16])
	h.NextPayload = b[16]
	h.Version = b[17]
	h.ExchangeType = b[18]
	h.Flags = b[19]
	h.MessageID = binary.BigEndian.Uint32(b[20:24])
	h.Length = binary.BigEndian.Uint32(b[24:28])
	return h, nil
}

// GenericPayloadHeader is the 4-byte header preceding every payload body,
// RFC 2408 §3.2.
type GenericPayloadHeader struct {
	NextPayload uint8
	Length      uint16 // includes this 4-byte header
}

func marshalPayload(nextPayload uint8, body []byte) []byte {
	out := make([]byte, 4+len(body))
	out[0] = nextPayload
	out[1] = 0 // reserved
	binary.BigEndian.PutUint16(out[2:4], uint16(4+len(body)))
	copy(out[4:], body)
	return out
}

// RawPayload is one decoded payload: its type (from the chain, not stored in
// the payload itself), and its body (excluding the 4-byte generic header).
type RawPayload struct {
	Type uint8
	Body []byte
}

const maxISAKMPPayloads = 64

// SplitPayloads walks the next-payload chain starting at firstType,
// returning each payload's type and body. This mirrors how ISAKMP payloads
// are actually framed: the body of message N tells you the type of the
// payload that follows it, not a fixed layout.
func SplitPayloads(firstType uint8, data []byte) ([]RawPayload, error) {
	var out []RawPayload
	next := firstType
	for next != PayloadNone {
		if len(out) >= maxISAKMPPayloads {
			return nil, fmt.Errorf("ISAKMP payload count exceeds safety limit (%d)", maxISAKMPPayloads)
		}
		if len(data) < 4 {
			return nil, fmt.Errorf("payload chain truncated (next=%d, %d bytes left)", next, len(data))
		}
		length := binary.BigEndian.Uint16(data[2:4])
		if int(length) < 4 || int(length) > len(data) {
			return nil, fmt.Errorf("payload %d has invalid length %d (%d bytes available)", next, length, len(data))
		}
		body := data[4:length]
		out = append(out, RawPayload{Type: next, Body: body})
		next = data[0]
		data = data[length:]
	}
	return out, nil
}
