package ike

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
)

// IKE Phase 1 attribute classes, RFC 2409 Appendix A.
const (
	attrEncryptionAlgorithm = 1
	attrHashAlgorithm       = 2
	attrAuthMethod          = 3
	attrGroupDescription    = 4
	attrLifeType            = 11
	attrLifeDuration        = 12
	attrKeyLength           = 14
)

// Encryption algorithm values, RFC 2409 Appendix A.
const (
	EncDES  = 1
	Enc3DES = 5
	EncAES  = 7
)

// Hash algorithm values, RFC 2409 Appendix A.
const (
	HashMD5    = 1
	HashSHA1   = 2
	HashSHA256 = 4
)

// AuthMethodPSK is the only authentication method this client implements —
// matches entrypoint.sh's PSK-based `authby=secret`.
const AuthMethodPSK = 1

const lifeTypeSeconds = 1

// Transform is one decoded/encodable Phase 1 (IKE SA) transform.
type Transform struct {
	Number     uint8
	Encryption int
	KeyBits    int // 0 for fixed-length ciphers (3DES); 128/192/256 for AES
	Hash       int
	Group      int
	AuthMethod int
	LifeSecs   uint32
}

// ParseProposal turns a compatibility-reference string like
// "aes256-sha256-modp2048" (entrypoint.sh's `ike=`/`esp=` syntax) into a
// Transform. This is the only place cipher/hash/group names are decoded, so
// the accepted vocabulary is easy to audit against what the real server
// requires.
func ParseProposal(s string) (Transform, error) {
	parts := strings.Split(s, "-")
	if len(parts) != 3 {
		return Transform{}, fmt.Errorf("proposal %q: want cipher-hash-group (e.g. aes256-sha256-modp2048)", s)
	}
	t := Transform{AuthMethod: AuthMethodPSK, LifeSecs: 28800}

	switch {
	case parts[0] == "3des":
		t.Encryption = Enc3DES
	case parts[0] == "des":
		t.Encryption = EncDES
	case strings.HasPrefix(parts[0], "aes"):
		t.Encryption = EncAES
		bits := strings.TrimPrefix(parts[0], "aes")
		if bits == "" {
			t.KeyBits = 128
		} else {
			n, err := strconv.Atoi(bits)
			if err != nil {
				return Transform{}, fmt.Errorf("proposal %q: bad AES key size %q", s, bits)
			}
			t.KeyBits = n
		}
	default:
		return Transform{}, fmt.Errorf("proposal %q: unknown cipher %q", s, parts[0])
	}

	switch parts[1] {
	case "md5":
		t.Hash = HashMD5
	case "sha1", "sha":
		t.Hash = HashSHA1
	case "sha256", "sha2":
		t.Hash = HashSHA256
	default:
		return Transform{}, fmt.Errorf("proposal %q: unknown hash %q", s, parts[1])
	}

	if !strings.HasPrefix(parts[2], "modp") {
		return Transform{}, fmt.Errorf("proposal %q: unknown group %q", s, parts[2])
	}
	switch strings.TrimPrefix(parts[2], "modp") {
	case "768":
		t.Group = 1
	case "1024":
		t.Group = 2
	case "1536":
		t.Group = 5
	case "2048":
		t.Group = 14
	default:
		return Transform{}, fmt.Errorf("proposal %q: unsupported DH group %q", s, parts[2])
	}

	return t, nil
}

func encodeAttrTV(typ uint16, val uint16) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint16(b[0:2], typ|0x8000)
	binary.BigEndian.PutUint16(b[2:4], val)
	return b
}

// MarshalTransform encodes one Phase 1 transform payload (transform-id =
// KEY_IKE = 1, RFC 2409 §5).
func (t Transform) MarshalTransform(number uint8, nextPayload uint8) []byte {
	var attrs []byte
	attrs = append(attrs, encodeAttrTV(attrEncryptionAlgorithm, uint16(t.Encryption))...)
	attrs = append(attrs, encodeAttrTV(attrHashAlgorithm, uint16(t.Hash))...)
	attrs = append(attrs, encodeAttrTV(attrAuthMethod, uint16(t.AuthMethod))...)
	attrs = append(attrs, encodeAttrTV(attrGroupDescription, uint16(t.Group))...)
	attrs = append(attrs, encodeAttrTV(attrLifeType, lifeTypeSeconds)...)
	attrs = append(attrs, encodeAttrTV(attrLifeDuration, uint16(t.LifeSecs))...)
	if t.Encryption == EncAES {
		attrs = append(attrs, encodeAttrTV(attrKeyLength, uint16(t.KeyBits))...)
	}

	body := make([]byte, 4+len(attrs))
	body[0] = number
	body[1] = 1 // transform-id = KEY_IKE
	// bytes 2-3 reserved
	copy(body[4:], attrs)

	return marshalPayload(nextPayload, body)
}

// MarshalProposal wraps one or more Phase 1 transforms (offered in
// preference order, most-preferred first) into a single SA payload,
// protocol-id = PROTO_ISAKMP, matching how a real IKE initiator offers
// several acceptable cipher suites in one SA payload for the responder to
// pick from — not one proposal per suite, which would change semantics.
func MarshalSA(transforms []Transform) []byte {
	var txBody []byte
	for i, t := range transforms {
		next := uint8(PayloadTransform)
		if i == len(transforms)-1 {
			next = PayloadNone
		}
		txBody = append(txBody, t.MarshalTransform(uint8(i+1), next)...)
	}

	proposal := make([]byte, 4+len(txBody))
	proposal[0] = 1                     // proposal #
	proposal[1] = 1                     // protocol-id = PROTO_ISAKMP
	proposal[2] = 0                     // SPI size = 0 (ISAKMP SPI is the header SPIs)
	proposal[3] = byte(len(transforms)) // # of transforms
	copy(proposal[4:], txBody)

	proposalPayload := marshalPayload(PayloadNone, proposal)

	sa := make([]byte, 8+len(proposalPayload))
	binary.BigEndian.PutUint32(sa[0:4], DOIIPsec)
	binary.BigEndian.PutUint32(sa[4:8], 1) // situation = SIT_IDENTITY_ONLY
	copy(sa[8:], proposalPayload)

	return sa
}

// ParseChosenTransform extracts the single transform a responder chose from
// inside an SA payload body (the responder's MM2 SA payload contains exactly
// one proposal with exactly one transform: RFC 2409 §5, "the responder MUST
// choose... a single transform").
func ParseChosenTransform(saBody []byte) (Transform, error) {
	if len(saBody) < 8 {
		return Transform{}, fmt.Errorf("SA payload too short")
	}
	rest := saBody[8:]
	payloads, err := SplitPayloads(PayloadProposal, rest)
	if err != nil {
		return Transform{}, fmt.Errorf("parse SA proposal chain: %w", err)
	}
	if len(payloads) != 1 || payloads[0].Type != PayloadProposal {
		return Transform{}, fmt.Errorf("expected exactly one proposal in responder SA, got %d", len(payloads))
	}
	prop := payloads[0].Body
	if len(prop) < 4 {
		return Transform{}, fmt.Errorf("proposal body too short")
	}
	spiSize := int(prop[2])
	numTx := int(prop[3])
	if numTx != 1 {
		return Transform{}, fmt.Errorf("expected exactly one transform in responder proposal, got %d", numTx)
	}
	// MM2 is neither encrypted nor authenticated: a size field must never be
	// trusted to fit the payload it describes.
	if len(prop) < 4+spiSize {
		return Transform{}, fmt.Errorf("proposal claims a %d-byte SPI but is %d bytes long", spiSize, len(prop))
	}
	txData := prop[4+spiSize:]
	txPayloads, err := SplitPayloads(PayloadTransform, txData)
	if err != nil {
		return Transform{}, fmt.Errorf("parse transform chain: %w", err)
	}
	if len(txPayloads) != 1 {
		return Transform{}, fmt.Errorf("expected one transform payload, got %d", len(txPayloads))
	}
	return parseTransformBody(txPayloads[0].Body)
}

func parseTransformBody(body []byte) (Transform, error) {
	if len(body) < 4 {
		return Transform{}, fmt.Errorf("transform body too short")
	}
	t := Transform{Number: body[0]}
	data := body[4:]
	for len(data) > 0 {
		if len(data) < 4 {
			return Transform{}, fmt.Errorf("truncated attribute")
		}
		typ := binary.BigEndian.Uint16(data[0:2])
		isTV := typ&0x8000 != 0
		attrType := typ &^ 0x8000
		var val uint32
		var consumed int
		if isTV {
			val = uint32(binary.BigEndian.Uint16(data[2:4]))
			consumed = 4
		} else {
			length := int(binary.BigEndian.Uint16(data[2:4]))
			if len(data) < 4+length {
				return Transform{}, fmt.Errorf("truncated TLV attribute")
			}
			vb := data[4 : 4+length]
			for _, b := range vb {
				val = val<<8 | uint32(b)
			}
			consumed = 4 + length
		}
		switch attrType {
		case attrEncryptionAlgorithm:
			t.Encryption = int(val)
		case attrHashAlgorithm:
			t.Hash = int(val)
		case attrAuthMethod:
			t.AuthMethod = int(val)
		case attrGroupDescription:
			t.Group = int(val)
		case attrLifeDuration:
			t.LifeSecs = val
		case attrKeyLength:
			t.KeyBits = int(val)
		}
		data = data[consumed:]
	}
	if t.Encryption == EncAES && t.KeyBits == 0 {
		t.KeyBits = 128
	}
	return t, nil
}

func (t Transform) String() string {
	var enc string
	switch t.Encryption {
	case Enc3DES, esp3DES:
		enc = "3des"
	case EncDES, espDES:
		enc = "des"
	case EncAES, espAES:
		if t.KeyBits > 0 {
			enc = fmt.Sprintf("aes%d", t.KeyBits)
		} else {
			enc = "aes"
		}
	default:
		enc = fmt.Sprintf("enc%d", t.Encryption)
	}

	var h string
	switch t.Hash {
	case HashMD5:
		h = "md5"
	case HashSHA1:
		h = "sha1"
	case HashSHA256:
		h = "sha256"
	default:
		h = fmt.Sprintf("hash%d", t.Hash)
	}

	if t.Group == 0 {
		return fmt.Sprintf("%s-%s", enc, h)
	}

	var g string
	switch t.Group {
	case 1:
		g = "modp768"
	case 2:
		g = "modp1024"
	case 5:
		g = "modp1536"
	case 14:
		g = "modp2048"
	default:
		g = fmt.Sprintf("modp%d", t.Group)
	}

	return fmt.Sprintf("%s-%s-%s", enc, h, g)
}
