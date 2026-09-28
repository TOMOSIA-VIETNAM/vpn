package ike

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"time"

	"vpn/internal/vpnlog"
)

// IPsec DOI (RFC 2407) protocol identifiers and ESP transform IDs.
const (
	protoIPsecESP = 3

	espDES  = 2
	esp3DES = 3
	espAES  = 12
)

// IPsec SA attribute classes (RFC 2407 §4.5).
const (
	ipsecAttrLifeType        = 1
	ipsecAttrLifeDuration    = 2
	ipsecAttrEncapsulateMode = 4
	ipsecAttrAuthAlgorithm   = 5
	ipsecAttrKeyLength       = 6
)

// Encapsulation Mode values — Tunnel/Transport from RFC 2407, the
// UDP-encapsulated variants from the NAT-T drafts (RFC 3947/3948 lineage).
const (
	encapTransport    = 2
	encapUDPTransport = 4
)

// Authentication Algorithm values (RFC 2407 §4.5, RFC 4868 §2.4):
//   - HMAC-MD5-96: IANA value 1 (RFC 2403), 16-byte key, 12-byte ICV
//   - HMAC-SHA1-96: IANA value 2 (RFC 2404), 20-byte key, 12-byte ICV
//   - HMAC-SHA2-256: IANA value 5 (RFC 4868 §2.4), 32-byte key, 16-byte ICV
//   - HMAC-SHA2-384: IANA value 6 (RFC 4868 §2.4), 48-byte key, 24-byte ICV
//   - HMAC-SHA2-512: IANA value 7 (RFC 4868 §2.4), 64-byte key, 32-byte ICV
const (
	authHMACMD5    = 1
	authHMACSHA1   = 2
	authHMACSHA256 = 5
	authHMACSHA384 = 6
	authHMACSHA512 = 7
)

// espAuthAlgorithm maps a proposal's hash to the ESP authentication
// algorithm it names.
func espAuthAlgorithm(hash int) (uint16, error) {
	switch hash {
	case HashMD5:
		return authHMACMD5, nil
	case HashSHA1:
		return authHMACSHA1, nil
	case HashSHA256:
		return authHMACSHA256, nil
	case HashSHA384:
		return authHMACSHA384, nil
	case HashSHA512:
		return authHMACSHA512, nil
	default:
		return 0, fmt.Errorf("unsupported ESP integrity hash %d", hash)
	}
}

const payloadNATOA = 21 // RFC 3947 §5.1, same wire shape as an ID payload.

// ChildSA is one direction's worth of established IPsec keying material —
// Quick Mode always produces two of these (inbound and outbound), sharing
// the same cipher/auth choice but each with its own SPI and derived keys.
type ChildSA struct {
	SPI       uint32
	EncKey    []byte
	AuthKey   []byte
	Transform Transform // Encryption/Hash carry the negotiated ESP cipher/HMAC; Group/AuthMethod unused here
}

// QuickModeResult is what a completed Quick Mode exchange hands back to the
// ESP layer.
type QuickModeResult struct {
	Inbound  ChildSA // decrypts packets we receive (our SPI, sent to the peer so *they* use it as ESP's SPI when sending to us)
	Outbound ChildSA // encrypts packets we send (peer's SPI)
	// Lifetime is the shortest of what we proposed and what the responder
	// answered (chosen SA attributes or a RESPONDER-LIFETIME notify): the
	// SA pair must be replaced before it elapses.
	Lifetime time.Duration
}

// espLifetime is the life duration we propose for every ESP SA.
const espLifetime = 3600 * time.Second

// notifyResponderLifetime is RFC 2407 §4.6.3.1's RESPONDER-LIFETIME: the
// responder accepted our proposal but will expire the SA sooner.
const notifyResponderLifetime = 24576

// espProposalFor mirrors ParseProposal's cipher-hash vocabulary (entrypoint.sh's
// esp= line uses the same "aes256-sha256" style, just without a DH group).
func espProposalFor(s string) (Transform, error) {
	full, err := ParseProposal(s + "-modp1024") // group is irrelevant for ESP (no PFS here); reuse the parser by padding a dummy group
	if err != nil {
		return Transform{}, fmt.Errorf("ESP proposal %q: %w", s, err)
	}
	full.Group = 0
	// ParseProposal accepts md5/sha1/sha256 — all are now valid for ESP.
	if _, err := espAuthAlgorithm(full.Hash); err != nil {
		return Transform{}, fmt.Errorf("ESP proposal %q: %w (use md5, sha1, or sha256)", s, err)
	}
	return full, nil
}

// ValidateESPProposals checks a profile's ESP proposals up front, so a bad
// one fails before any packet is sent rather than after Phase 1.
func ValidateESPProposals(proposals []string) error {
	for _, p := range proposals {
		if _, err := espProposalFor(p); err != nil {
			return err
		}
	}
	return nil
}

func espTransformID(t Transform) (int, error) {
	switch t.Encryption {
	case Enc3DES:
		return esp3DES, nil
	case EncDES:
		return espDES, nil
	case EncAES:
		return espAES, nil
	default:
		return 0, fmt.Errorf("unsupported ESP encryption algorithm %d", t.Encryption)
	}
}

func encodeESPLifeDuration(secs uint32) []byte {
	val := make([]byte, 4)
	binary.BigEndian.PutUint32(val, secs)
	return encodeAttrTLV(ipsecAttrLifeDuration, val)
}

func marshalESPTransform(number uint8, t Transform, nextPayload uint8) ([]byte, error) {
	return marshalESPTransformMode(number, t, encapUDPTransport, nextPayload)
}

// marshalESPTransformMode is marshalESPTransform with an explicit
// encapsulation mode: a responder must echo the mode the initiator offered.
func marshalESPTransformMode(number uint8, t Transform, encapMode uint16, nextPayload uint8) ([]byte, error) {
	txID, err := espTransformID(t)
	if err != nil {
		return nil, err
	}
	auth, err := espAuthAlgorithm(t.Hash)
	if err != nil {
		return nil, err
	}
	var attrs []byte
	attrs = append(attrs, encodeAttrTV(ipsecAttrAuthAlgorithm, auth)...)
	attrs = append(attrs, encodeAttrTV(ipsecAttrEncapsulateMode, encapMode)...)
	attrs = append(attrs, encodeAttrTV(ipsecAttrLifeType, lifeTypeSeconds)...)
	// ESP LifeDuration is attribute type 2 (ipsecAttrLifeDuration, RFC 2407 §4.5),
	// encoded as 4-byte TLV.
	attrs = append(attrs, encodeESPLifeDuration(t.LifeSecs)...)
	if t.Encryption == EncAES {
		attrs = append(attrs, encodeAttrTV(ipsecAttrKeyLength, uint16(t.KeyBits))...)
	}

	body := make([]byte, 4+len(attrs))
	body[0] = number
	body[1] = byte(txID)
	copy(body[4:], attrs)
	return marshalPayload(nextPayload, body), nil
}

// marshalESPSA builds a Quick Mode SA payload offering one or more ESP
// transforms under a single proposal, our locally generated SPI.
func marshalESPSA(transforms []Transform, spi uint32) ([]byte, error) {
	var txBody []byte
	for i, t := range transforms {
		next := uint8(PayloadTransform)
		if i == len(transforms)-1 {
			next = PayloadNone
		}
		tx, err := marshalESPTransform(uint8(i+1), t, next)
		if err != nil {
			return nil, err
		}
		txBody = append(txBody, tx...)
	}

	spiBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(spiBytes, spi)

	proposal := make([]byte, 4+4+len(txBody))
	proposal[0] = 1             // proposal #
	proposal[1] = protoIPsecESP // protocol-id
	proposal[2] = 4             // SPI size
	proposal[3] = byte(len(transforms))
	copy(proposal[4:8], spiBytes)
	copy(proposal[8:], txBody)

	proposalPayload := marshalPayload(PayloadNone, proposal)

	sa := make([]byte, 8+len(proposalPayload))
	binary.BigEndian.PutUint32(sa[0:4], DOIIPsec)
	binary.BigEndian.PutUint32(sa[4:8], 1) // situation = SIT_IDENTITY_ONLY
	copy(sa[8:], proposalPayload)
	return sa, nil
}

// chosenESP is what parseChosenESPSA extracts from the responder's Quick
// Mode SA payload.
type chosenESP struct {
	SPI       uint32
	Transform Transform
}

func parseChosenESPSA(saBody []byte) (chosenESP, error) {
	if len(saBody) < 8 {
		return chosenESP{}, fmt.Errorf("ESP SA payload too short")
	}
	payloads, err := SplitPayloads(PayloadProposal, saBody[8:])
	if err != nil {
		return chosenESP{}, err
	}
	if len(payloads) != 1 {
		return chosenESP{}, fmt.Errorf("expected exactly one ESP proposal, got %d", len(payloads))
	}
	prop := payloads[0].Body
	if len(prop) < 8 {
		return chosenESP{}, fmt.Errorf("ESP proposal body too short")
	}
	spiSize := int(prop[2])
	numTx := int(prop[3])
	if numTx != 1 {
		return chosenESP{}, fmt.Errorf("expected one ESP transform, got %d", numTx)
	}
	if spiSize != 4 { // RFC 2407 §4.4.1: an ESP SPI is 4 bytes; len(prop) >= 8 then covers it
		return chosenESP{}, fmt.Errorf("ESP proposal has a %d-byte SPI, want 4", spiSize)
	}
	spi := prop[4 : 4+spiSize]
	var spiVal uint32
	for _, b := range spi {
		spiVal = spiVal<<8 | uint32(b)
	}
	tx, err := chosenTransform(prop[4+spiSize:])
	if err != nil {
		return chosenESP{}, fmt.Errorf("ESP proposal: %w", err)
	}
	t, _, err := parseESPTransformBody(tx)
	if err != nil {
		return chosenESP{}, err
	}
	return chosenESP{SPI: spiVal, Transform: t}, nil
}

// parseESPTransformBody decodes one ESP Transform payload body: the cipher,
// its attributes (integrity algorithm, key length, seconds lifetime) and the
// encapsulation mode it asks for.
func parseESPTransformBody(body []byte) (t Transform, encapMode uint32, err error) {
	if len(body) < 4 {
		return Transform{}, 0, fmt.Errorf("ESP transform body too short")
	}
	switch int(body[1]) {
	case esp3DES:
		t.Encryption = Enc3DES
	case espDES:
		t.Encryption = EncDES
	case espAES:
		t.Encryption = EncAES
	default:
		return Transform{}, 0, fmt.Errorf("unsupported ESP transform-id %d", body[1])
	}
	data := body[4:]
	// RFC 2407 §4.5: a Life Duration applies to the Life Type preceding it;
	// only a seconds-based duration bounds the SA in time.
	var lifeType uint32
	for len(data) > 0 {
		if len(data) < 4 {
			break
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
			l := int(binary.BigEndian.Uint16(data[2:4]))
			if len(data) < 4+l {
				break
			}
			for _, b := range data[4 : 4+l] {
				val = val<<8 | uint32(b)
			}
			consumed = 4 + l
		}
		switch attrType {
		case ipsecAttrAuthAlgorithm:
			switch val {
			case authHMACMD5:
				t.Hash = HashMD5
			case authHMACSHA1:
				t.Hash = HashSHA1
			case authHMACSHA256:
				t.Hash = HashSHA256
			case authHMACSHA384:
				t.Hash = HashSHA384
			case authHMACSHA512:
				t.Hash = HashSHA512
			default:
				return Transform{}, 0, fmt.Errorf("unsupported ESP authentication algorithm %d", val)
			}

		case ipsecAttrKeyLength:
			t.KeyBits = int(val)
		case ipsecAttrEncapsulateMode:
			encapMode = val
		case ipsecAttrLifeType:
			lifeType = val
		case ipsecAttrLifeDuration:
			if lifeType == lifeTypeSeconds {
				t.LifeSecs = val
			}
		}
		data = data[consumed:]
	}
	if t.Hash == 0 {
		return Transform{}, 0, fmt.Errorf("ESP transform carries no authentication algorithm")
	}
	if t.Encryption == EncAES && t.KeyBits == 0 {
		t.KeyBits = 128
	}
	return t, encapMode, nil
}

// responderLifetimeSecs extracts the seconds lifetime from a
// RESPONDER-LIFETIME notify body (RFC 2408 §3.14 layout: DOI, protocol,
// SPI size, notify type, SPI, then SA attributes). 0 means none present.
func responderLifetimeSecs(body []byte) uint32 {
	if len(body) < 8 || uint16(body[6])<<8|uint16(body[7]) != notifyResponderLifetime {
		return 0
	}
	spiSize := int(body[5])
	if len(body) < 8+spiSize {
		return 0
	}
	attrs := body[8+spiSize:]
	var lifeType, secs uint32
	for len(attrs) >= 4 {
		typ := binary.BigEndian.Uint16(attrs[0:2])
		var val uint32
		consumed := 4
		if typ&0x8000 != 0 {
			val = uint32(binary.BigEndian.Uint16(attrs[2:4]))
		} else {
			l := int(binary.BigEndian.Uint16(attrs[2:4]))
			if len(attrs) < 4+l {
				break
			}
			for _, b := range attrs[4 : 4+l] {
				val = val<<8 | uint32(b)
			}
			consumed += l
		}
		switch typ &^ 0x8000 {
		case ipsecAttrLifeType:
			lifeType = val
		case ipsecAttrLifeDuration:
			if lifeType == lifeTypeSeconds {
				secs = val
			}
		}
		attrs = attrs[consumed:]
	}
	return secs
}

// effectiveLifetime is the shortest non-zero of the proposed, chosen and
// responder-notified lifetimes.
func effectiveLifetime(proposed time.Duration, secs ...uint32) time.Duration {
	life := proposed
	for _, v := range secs {
		if d := time.Duration(v) * time.Second; v > 0 && d < life {
			life = d
		}
	}
	return life
}

// offeredESP reports whether the responder's choice is one of the
// transforms we proposed — it may only pick, never invent (RFC 2408
// §4.2): accepting anything else would let it select e.g. single DES,
// which the parser recognizes but this client never offers.
func offeredESP(chosen Transform, offered []Transform) bool {
	for _, o := range offered {
		if o.Encryption == chosen.Encryption && o.Hash == chosen.Hash && cipherKeyLen(o) == cipherKeyLen(chosen) {
			return true
		}
	}
	return false
}

// verifyQuickModeHash2 checks QM2's HASH(2), RFC 2409 §5.5:
//
//	HASH(2) = prf(SKEYID_a, M-ID | Ni_b | <QM2 after the HASH payload>)
//
// where the trailing part is every payload after HASH, headers included,
// excluding the encryption padding — the mirror of HASH(1). Without it
// nothing authenticates QM2: IKEv1 CBC encryption carries no integrity of
// its own, so the responder's SA choice, nonce and SPI would be accepted
// as received, bit flips and all.
func verifyQuickModeHash2(hashAlg int, skeyidA []byte, msgID uint32, ni []byte, firstType uint8, payloads []RawPayload, plain []byte) error {
	prefix := append(beUint32(msgID), ni...)
	if err := verifyLeadingHash(hashAlg, skeyidA, prefix, firstType, payloads, plain); err != nil {
		return fmt.Errorf("QM2 HASH(2): %w", err)
	}
	return nil
}

func quickModeHash3(hashAlg int, skeyidA []byte, msgID uint32, niB, nrB []byte) ([]byte, error) {
	midBytes := beUint32(msgID)
	data := append([]byte{0}, midBytes...)
	data = append(data, niB...)
	data = append(data, nrB...)
	return prf(hashAlg, skeyidA, data)
}

func beUint32(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}

// quickModeKeymat derives one direction's ESP keying material, RFC 2409 §5.5
// (no-PFS case): KEYMAT = K1 | K2 | ... where
//
//	K1 = prf(SKEYID_d, protocol | SPI | Ni_b | Nr_b)
//	K2 = prf(SKEYID_d, K1 | protocol | SPI | Ni_b | Nr_b)
//	...
//
// truncated to neededBytes (encryption key length + authentication key
// length for the negotiated ESP transform).
func quickModeKeymat(hashAlg int, skeyidD []byte, protocol uint8, spi uint32, niB, nrB []byte, neededBytes int) ([]byte, error) {
	base := append([]byte{protocol}, beUint32(spi)...)
	base = append(base, niB...)
	base = append(base, nrB...)

	var out []byte
	var prev []byte
	for len(out) < neededBytes {
		input := append(append([]byte{}, prev...), base...)
		k, err := prf(hashAlg, skeyidD, input)
		if err != nil {
			return nil, err
		}
		out = append(out, k...)
		prev = k
	}
	return out[:neededBytes], nil
}

func espKeyLens(t Transform) (encLen, authLen int) {
	switch t.Encryption {
	case Enc3DES:
		encLen = 24
	case EncDES:
		encLen = 8
	case EncAES:
		if t.KeyBits == 0 {
			encLen = 16
		} else {
			encLen = t.KeyBits / 8
		}
	}
	// HMAC key length is the hash's full output size, independent of the
	// truncated ICV (RFC 2403 §3, RFC 2404 §3, RFC 4868 §2.1.1).
	switch t.Hash {
	case HashMD5:
		authLen = 16 // RFC 2403: HMAC-MD5-96 uses 16-byte key
	case HashSHA256:
		authLen = 32 // RFC 4868: HMAC-SHA256 uses 32-byte key
	case HashSHA384:
		authLen = 48 // RFC 4868: HMAC-SHA384 uses 48-byte key
	case HashSHA512:
		authLen = 64 // RFC 4868: HMAC-SHA512 uses 64-byte key
	default:
		authLen = 20 // SHA-1: 20-byte key
	}
	return

}

// EstablishQuickMode negotiates one Phase 2 (IPsec) SA pair over an already
// established Phase 1 session, using entrypoint.sh's transport-mode,
// UDP-encapsulated ESP parameters (leftprotoport/rightprotoport = 17/1701).
func (s *Session) EstablishQuickMode(espProposals []string, localIP, remoteIP net.IP) (*QuickModeResult, error) {
	return s.quickMode(espProposals, localIP, remoteIP, s.exchangeQuickMode, s.sendRaw)
}

// quickMode runs one initiator Quick Mode exchange. roundTrip sends QM1
// (retransmitting as needed) and returns the responder's QM2; send
// delivers the one-shot QM3. Initial setup reads the socket directly
// (exchangeQuickMode); a rekey during the data phase goes through the
// control plane's demultiplexer instead (see RekeyQuickMode).
func (s *Session) quickMode(espProposals []string, localIP, remoteIP net.IP, roundTrip func(msg []byte, msgID uint32) ([]byte, error), send func([]byte) error) (*QuickModeResult, error) {
	transforms := make([]Transform, 0, len(espProposals))
	for _, p := range espProposals {
		t, err := espProposalFor(p)
		if err != nil {
			return nil, err
		}
		t.LifeSecs = uint32(espLifetime / time.Second)
		transforms = append(transforms, t)
	}

	var spiBytes [4]byte
	if _, err := rand.Read(spiBytes[:]); err != nil {
		return nil, err
	}
	mySPI := binary.BigEndian.Uint32(spiBytes[:])

	msgID := randomMessageID()
	saBody, err := marshalESPSA(transforms, mySPI)
	if err != nil {
		return nil, err
	}
	ni := make([]byte, 16)
	if _, err := rand.Read(ni); err != nil {
		return nil, err
	}

	bs := blockSize(s.Transform)

	// IDci/IDcr: transport-mode selectors restricted to UDP — our L2TP port
	// (see Session.L2TPPort) and the server's 1701.
	idci, idcr := s.quickModeIDs(localIP, remoteIP)

	var natOA1, natOA2 []byte
	if s.floated {
		natOA1 = marshalPayload(payloadNATOA, MarshalIPv4ID(localIP))
		natOA2 = marshalPayload(PayloadNone, MarshalIPv4ID(remoteIP))
	}

	saPayload := marshalPayload(PayloadNonce, saBody)
	noncePayload := marshalPayload(PayloadID, ni)
	idciNext := uint8(PayloadID)
	idcrNext := uint8(PayloadNone)
	if s.floated {
		idcrNext = payloadNATOA
	}
	idciPayload := marshalPayload(idciNext, idci)
	idcrPayload := marshalPayload(idcrNext, idcr)

	rest := append(append([]byte{}, saPayload...), noncePayload...)
	rest = append(rest, idciPayload...)
	rest = append(rest, idcrPayload...)
	rest = append(rest, natOA1...)
	rest = append(rest, natOA2...)

	// RFC 2409 §5.5: "HASH(1) is the prf over the message id (M-ID) from
	// the ISAKMP header concatenated with the entire message that follows
	// the hash [payload], including all payload headers, but excluding any
	// padding added for encryption." So this hashes M-ID followed by the
	// fully-serialized SA/Nonce/IDci/IDcr(/NAT-OA) payloads — headers and
	// all — not just their bodies (unlike Phase 1's HASH_I/HASH_R, which do
	// use bodies only).
	hash1, err := prf(s.Transform.Hash, s.Keys.SKEYIDa, append(beUint32(msgID), rest...))
	if err != nil {
		return nil, err
	}
	hashPayload := marshalPayload(PayloadSA, hash1)

	plain := append(append([]byte{}, hashPayload...), rest...)
	padded := padToBlock(plain, bs)
	// QM1 is the first encrypted message of this new exchange, so its IV is
	// reseeded from the last Phase 1 IV — RFC 2409 §5.5. Every subsequent
	// message of *this same* exchange (QM2, QM3), regardless of which side
	// sends it, chains normally from the previous message's ciphertext tail
	// instead, exactly like MM6 chained from MM5 within Phase 1.
	iv, err := informationalIV(s.Transform.Hash, s.phase1IV, msgID, bs)
	if err != nil {
		return nil, err
	}
	cipher, err := cbcEncrypt(s.Transform, s.Keys.EncKey, iv, padded)
	if err != nil {
		return nil, err
	}
	// This exchange's own CBC chain — local, so concurrent exchanges on the
	// same IKE SA (a rekey alongside an Informational) never share state.
	ivChain := cipher[len(cipher)-bs:]

	hdr := Header{InitiatorSPI: s.InitiatorSPI, ResponderSPI: s.ResponderSPI, NextPayload: PayloadHash, Version: 0x10, ExchangeType: ExchangeQuickMode, Flags: FlagEncryption, MessageID: msgID}
	hdr.Length = uint32(headerLen + len(cipher))
	qm1 := append(hdr.Marshal(), cipher...)

	vpnlog.Info(stage, "QM1 sent", vpnlog.Fields{"msg_id": msgID, "bytes": len(qm1)})
	resp, err := roundTrip(qm1, msgID)
	if err != nil {
		return nil, fmt.Errorf("QM1/QM2: %w", err)
	}
	respHdr, err := ParseHeader(resp)
	if err != nil {
		return nil, err
	}
	respBody := resp[headerLen:]
	plainResp, err := cbcDecrypt(s.Transform, s.Keys.EncKey, ivChain, respBody)
	if err != nil {
		return nil, fmt.Errorf("decrypt QM2: %w", err)
	}
	if len(respBody) >= bs {
		ivChain = respBody[len(respBody)-bs:]
	}
	payloads, err := SplitPayloads(respHdr.NextPayload, plainResp)
	if err != nil {
		return nil, fmt.Errorf("QM2 payload chain: %w", err)
	}
	var peerSABody, peerNonce []byte
	var notifiedLife uint32
	for _, p := range payloads {
		switch p.Type {
		case PayloadSA:
			peerSABody = p.Body
		case PayloadNonce:
			peerNonce = p.Body
		case PayloadNotify:
			if v := responderLifetimeSecs(p.Body); v > 0 {
				notifiedLife = v
			}
		}
	}
	if peerSABody == nil || peerNonce == nil {
		return nil, fmt.Errorf("IPSEC_FAILURE: QM2 missing SA or Nonce")
	}
	if err := verifyQuickModeHash2(s.Transform.Hash, s.Keys.SKEYIDa, msgID, ni, respHdr.NextPayload, payloads, plainResp); err != nil {
		return nil, fmt.Errorf("IPSEC_FAILURE: %w", err)
	}
	chosen, err := parseChosenESPSA(peerSABody)
	if err != nil {
		return nil, fmt.Errorf("IPSEC_FAILURE: %w", err)
	}
	if !offeredESP(chosen.Transform, transforms) {
		return nil, fmt.Errorf("IPSEC_FAILURE: responder chose an ESP transform we never offered (encryption %d, %d-bit key, hash %d)", chosen.Transform.Encryption, cipherKeyLen(chosen.Transform)*8, chosen.Transform.Hash)
	}
	vpnlog.Info(stage, "QM2 received (ESP transform agreed)", vpnlog.Fields{
		"transform": chosen.Transform.String(),
	})

	// QM3: HDR*, HASH(3) — acknowledges completion, RFC 2409 §5.5.
	hash3, err := quickModeHash3(s.Transform.Hash, s.Keys.SKEYIDa, msgID, ni, peerNonce)
	if err != nil {
		return nil, err
	}
	hash3Payload := marshalPayload(PayloadNone, hash3)
	padded3 := padToBlock(hash3Payload, bs)
	cipher3, err := cbcEncrypt(s.Transform, s.Keys.EncKey, ivChain, padded3)
	if err != nil {
		return nil, err
	}
	hdr3 := Header{InitiatorSPI: s.InitiatorSPI, ResponderSPI: s.ResponderSPI, NextPayload: PayloadHash, Version: 0x10, ExchangeType: ExchangeQuickMode, Flags: FlagEncryption, MessageID: msgID}
	hdr3.Length = uint32(headerLen + len(cipher3))
	qm3 := append(hdr3.Marshal(), cipher3...)
	if err := send(qm3); err != nil {
		return nil, fmt.Errorf("send QM3: %w", err)
	}
	vpnlog.Info(stage, "QM3 sent (exchange complete)", nil)

	encLen, authLen := espKeyLens(chosen.Transform)
	outKeymat, err := quickModeKeymat(s.Transform.Hash, s.Keys.SKEYIDd, protoIPsecESP, chosen.SPI, ni, peerNonce, encLen+authLen)
	if err != nil {
		return nil, err
	}
	inKeymat, err := quickModeKeymat(s.Transform.Hash, s.Keys.SKEYIDd, protoIPsecESP, mySPI, ni, peerNonce, encLen+authLen)
	if err != nil {
		return nil, err
	}

	result := &QuickModeResult{
		Outbound: ChildSA{SPI: chosen.SPI, EncKey: outKeymat[:encLen], AuthKey: outKeymat[encLen:], Transform: chosen.Transform},
		Inbound:  ChildSA{SPI: mySPI, EncKey: inKeymat[:encLen], AuthKey: inKeymat[encLen:], Transform: chosen.Transform},
		Lifetime: effectiveLifetime(espLifetime, chosen.Transform.LifeSecs, notifiedLife),
	}
	vpnlog.Info(stage, "Quick Mode ESTABLISHED", vpnlog.Fields{
		"in_spi": fmt.Sprintf("%08x", mySPI), "out_spi": fmt.Sprintf("%08x", chosen.SPI), "lifetime_s": int(result.Lifetime / time.Second),
	})
	return result, nil
}

func marshalUDPPortID(ip net.IP, port uint16) []byte {
	body := make([]byte, 8)
	body[0] = IDTypeIPv4Addr
	body[1] = 17 // UDP
	binary.BigEndian.PutUint16(body[2:4], port)
	copy(body[4:8], ip.To4())
	return body
}

func randomMessageID() uint32 {
	var b [4]byte
	_, _ = rand.Read(b[:])
	v := binary.BigEndian.Uint32(b[:])
	if v == 0 {
		v = 1 // Message-ID 0 is reserved for Phase 1
	}
	return v
}

// L2TPPort is L2TP's well-known UDP port (RFC 2661 §8.1): always the server's
// port, and ours unless Session.L2TPPort picks another.
const L2TPPort = 1701

// ErrQuickModeRejected is wrapped when the server answers a Quick Mode with
// an error Notify (e.g. INVALID-ID-INFORMATION) instead of QM2.
var ErrQuickModeRejected = errors.New("Quick Mode rejected by the server")

// notifyErrorLimit: Notify types below it are errors (RFC 2408 §3.14.1);
// from 16384 up they are status messages.
const notifyErrorLimit = 16384

// quickModeIDs are IDci/IDcr: our L2TP port on our address, 1701 on the server's.
func (s *Session) quickModeIDs(localIP, remoteIP net.IP) (idci, idcr []byte) {
	return marshalUDPPortID(localIP, s.l2tpPort()), marshalUDPPortID(remoteIP, L2TPPort)
}

func (s *Session) l2tpPort() uint16 {
	if s.L2TPPort == 0 {
		return L2TPPort
	}
	return s.L2TPPort
}
