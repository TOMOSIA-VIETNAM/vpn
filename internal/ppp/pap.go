// Package ppp implements Password Authentication Protocol (PAP, RFC 1334).
package ppp

// PAP packet codes, RFC 1334 §2.

const (
	PAPCodeAuthenticateRequest = 1
	PAPCodeAuthenticateAck     = 2
	PAPCodeAuthenticateNak     = 3
)

// PAPPacket is the RFC 1334 §2 packet layout: Code, Identifier, Length, then
// type-specific data.
type PAPPacket struct {
	Code       uint8
	Identifier uint8
	Data       []byte
}

// GeneratePAPAuthenticateRequest builds a PAP Authenticate-Request packet
// body: Peer-ID Length(1) | Peer-ID | Passwd Length(1) | Passwd (RFC 1334 §2.1).
func GeneratePAPAuthenticateRequest(id uint8, username, password string) ControlPacket {
	uBytes := []byte(username)
	pBytes := []byte(password)
	data := make([]byte, 1+len(uBytes)+1+len(pBytes))
	data[0] = uint8(len(uBytes))
	copy(data[1:], uBytes)
	data[1+len(uBytes)] = uint8(len(pBytes))
	copy(data[2+len(uBytes):], pBytes)

	return ControlPacket{
		Code:       PAPCodeAuthenticateRequest,
		Identifier: id,
		Data:       data,
	}
}

// ParsePAPPacket decodes a PAP packet.
func ParsePAPPacket(b []byte) (PAPPacket, error) {
	cp, err := ParseControlPacket(b)
	if err != nil {
		return PAPPacket{}, err
	}
	return PAPPacket{
		Code:       cp.Code,
		Identifier: cp.Identifier,
		Data:       cp.Data,
	}, nil
}

// ParsePAPMessage extracts the human-readable message from a PAP Ack/Nak.
func ParsePAPMessage(data []byte) string {
	if len(data) < 1 {
		return ""
	}
	msgLen := int(data[0])
	if msgLen+1 > len(data) {
		msgLen = len(data) - 1
	}
	return string(data[1 : 1+msgLen])
}
