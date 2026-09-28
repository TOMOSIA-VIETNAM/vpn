package l2tp

import (
	"encoding/binary"
	"fmt"
)

// AVP attribute types, RFC 2661 §4.4-4.6 (the IETF vendor ID 0 subset this
// client needs — no vendor-specific AVPs).
const (
	AVPMessageType         = 0
	AVPResultCode          = 1
	AVPProtocolVersion     = 2
	AVPFramingCapabilities = 3
	AVPBearerCapabilities  = 4
	AVPFirmwareRevision    = 6
	AVPHostName            = 7
	AVPVendorName          = 8
	AVPAssignedTunnelID    = 9
	AVPReceiveWindowSize   = 10
	AVPChallenge           = 11
	AVPCauseCode           = 12
	AVPChallengeResponse   = 13
	AVPAssignedSessionID   = 14
	AVPCallSerialNumber    = 15
	AVPBearerType          = 18
	AVPFramingType         = 19
	AVPCalledNumber        = 21
	AVPCallingNumber       = 22
	AVPTxConnectSpeed      = 24
	AVPRxConnectSpeed      = 38
)

// Message Type AVP values, RFC 2661 §6.
const (
	MsgSCCRQ   = 1
	MsgSCCRP   = 2
	MsgSCCCN   = 3
	MsgStopCCN = 4
	MsgHello   = 6
	MsgICRQ    = 10
	MsgICRP    = 11
	MsgICCN    = 12
	MsgCDN     = 14
	MsgWEN     = 15 // WAN-Error-Notify
	MsgSLI     = 16 // Set-Link-Info: an LNS may send it once PPP is up
)

// AVP is one decoded Attribute-Value Pair (RFC 2661 §4.3).
type AVP struct {
	Mandatory bool
	Hidden    bool // never used by this client — we don't implement AVP hiding, only relevant with tunnel authentication, which the reference config doesn't use
	VendorID  uint16
	Type      uint16
	Value     []byte
}

// MarshalAVP encodes one AVP. Mandatory defaults to true for every AVP this
// client sends, matching how a real LAC marks its own control AVPs — an LNS
// is required to reject a message with an unrecognized *mandatory* AVP, but
// every AVP type used here is a well-known IETF one any real LNS supports.
func MarshalAVP(mandatory bool, avpType uint16, value []byte) []byte {
	flags := uint16(len(value)+6) & 0x03FF // length in low 10 bits
	if mandatory {
		flags |= 1 << 15
	}
	b := make([]byte, 6+len(value))
	binary.BigEndian.PutUint16(b[0:2], flags)
	binary.BigEndian.PutUint16(b[2:4], 0) // Vendor ID 0 = IETF
	binary.BigEndian.PutUint16(b[4:6], avpType)
	copy(b[6:], value)
	return b
}

func avpUint16(mandatory bool, avpType uint16, v uint16) []byte {
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, v)
	return MarshalAVP(mandatory, avpType, b)
}

func avpString(mandatory bool, avpType uint16, s string) []byte {
	return MarshalAVP(mandatory, avpType, []byte(s))
}

// MessageTypeAVP builds the mandatory Message Type AVP every control
// message must start with (RFC 2661 §6).
func MessageTypeAVP(msgType uint16) []byte {
	return avpUint16(true, AVPMessageType, msgType)
}

// ProtocolVersionAVP is fixed at version 1.0 (RFC 2661 §4.4.2's "1" high
// byte / "0" low byte), the only version this client implements.
func ProtocolVersionAVP() []byte {
	return MarshalAVP(true, AVPProtocolVersion, []byte{1, 0})
}

func HostNameAVP(name string) []byte { return avpString(true, AVPHostName, name) }

// FramingCapAVP/BearerCapAVP are 4-byte bitmask AVPs (RFC 2661 §4.4.3/4.4.4).
// Bit 0 (0x1) = analog framing / bearer, bit 1 (0x2) = digital — this client
// advertises both since it has no real modem, matching what strongSwan's
// xl2tpd sends and every LNS already tolerates.
func FramingCapAVP() []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, 0x3)
	return MarshalAVP(true, AVPFramingCapabilities, b)
}

func BearerCapAVP() []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, 0x3)
	return MarshalAVP(true, AVPBearerCapabilities, b)
}

func AssignedTunnelIDAVP(id uint16) []byte  { return avpUint16(true, AVPAssignedTunnelID, id) }
func AssignedSessionIDAVP(id uint16) []byte { return avpUint16(true, AVPAssignedSessionID, id) }
func ReceiveWindowSizeAVP(n uint16) []byte  { return avpUint16(true, AVPReceiveWindowSize, n) }
func CallSerialNumberAVP(n uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, n)
	return MarshalAVP(true, AVPCallSerialNumber, b)
}
func BearerTypeAVP(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return MarshalAVP(true, AVPBearerType, b)
}
func FramingTypeAVP(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return MarshalAVP(true, AVPFramingType, b)
}
func TxConnectSpeedAVP(bps uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, bps)
	return MarshalAVP(true, AVPTxConnectSpeed, b)
}
func RxConnectSpeedAVP(bps uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, bps)
	return MarshalAVP(true, AVPRxConnectSpeed, b)
}

// describeResult renders a CDN/StopCCN's Result Code AVP (RFC 2661 §4.4.2):
// result code, then optionally an error code and a human-readable message.
func describeResult(avps []AVP) string {
	a, ok := Find(avps, AVPResultCode)
	if !ok || len(a.Value) < 2 {
		return "no result code"
	}
	s := fmt.Sprintf("result %d", binary.BigEndian.Uint16(a.Value[0:2]))
	if len(a.Value) >= 4 {
		s += fmt.Sprintf(", error %d", binary.BigEndian.Uint16(a.Value[2:4]))
	}
	if len(a.Value) > 4 {
		s += fmt.Sprintf(": %q", a.Value[4:])
	}
	return s
}

const maxAVPs = 64

// ParseAVPs decodes a sequence of AVPs from a control message body,
// following each AVP's own length field (RFC 2661 §4.3) rather than
// assuming fixed sizes.
func ParseAVPs(b []byte) ([]AVP, error) {
	var out []AVP
	for len(b) > 0 {
		if len(out) >= maxAVPs {
			return nil, fmt.Errorf("L2TP AVP count exceeds safety limit (%d)", maxAVPs)
		}
		if len(b) < 6 {
			return nil, fmt.Errorf("truncated AVP header (%d bytes left)", len(b))
		}
		flags := binary.BigEndian.Uint16(b[0:2])
		length := int(flags & 0x03FF)
		if length < 6 || length > len(b) {
			return nil, fmt.Errorf("AVP has invalid length %d (%d bytes available)", length, len(b))
		}
		avp := AVP{
			Mandatory: flags&(1<<15) != 0,
			Hidden:    flags&(1<<14) != 0,
			VendorID:  binary.BigEndian.Uint16(b[2:4]),
			Type:      binary.BigEndian.Uint16(b[4:6]),
			Value:     append([]byte{}, b[6:length]...),
		}
		out = append(out, avp)
		b = b[length:]
	}
	return out, nil
}

// MessageType extracts the Message Type AVP's value from a decoded control
// message's AVP list, which RFC 2661 §5.1 requires to be the first AVP.
func MessageType(avps []AVP) (uint16, error) {
	for _, a := range avps {
		if a.VendorID == 0 && a.Type == AVPMessageType {
			if len(a.Value) < 2 {
				return 0, fmt.Errorf("Message Type AVP too short")
			}
			return binary.BigEndian.Uint16(a.Value), nil
		}
	}
	return 0, fmt.Errorf("no Message Type AVP present")
}

// Find returns the first AVP of the given IETF (vendor 0) type, if present.
func Find(avps []AVP, avpType uint16) (AVP, bool) {
	for _, a := range avps {
		if a.VendorID == 0 && a.Type == avpType {
			return a, true
		}
	}
	return AVP{}, false
}
