// Package ipsec implements ESP (RFC 4303) in transport mode with UDP
// encapsulation (RFC 3948) — the data-plane counterpart to internal/ike's
// Quick Mode SA negotiation. This client only ever protects UDP/1701
// (L2TP) traffic, matching entrypoint.sh's `type=transport` config. The
// cipher and integrity transform are whatever Quick Mode negotiated.
package ipsec

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/des"
	"crypto/hmac"
	"crypto/md5" //nolint:gosec // required: HMAC-MD5-96 (RFC 2403) is used by legacy VPN servers
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"fmt"
	"hash"
	"math"
	"sync"
)

// Cipher is the ESP encryption transform of an SA.
type Cipher int

const (
	Cipher3DESCBC Cipher = iota + 1 // RFC 2451, 24-byte key
	CipherAESCBC                    // RFC 3602, 16/24/32-byte key
)

// Integrity is the ESP authentication transform of an SA.
type Integrity int

const (
	IntegHMACMD5_96     Integrity = iota + 1 // RFC 2403, 16-byte key, 12-byte ICV
	IntegHMACSHA1_96                         // RFC 2404, 20-byte key, 12-byte ICV
	IntegHMACSHA256_128                      // RFC 4868, 32-byte key, 16-byte ICV
	IntegHMACSHA384_192                      // RFC 4868, 48-byte key, 24-byte ICV
	IntegHMACSHA512_256                      // RFC 4868, 64-byte key, 32-byte ICV
)

// SA is one direction's ESP security association — mirrors ike.ChildSA but
// lives in this package so ipsec doesn't import ike (keeps the dependency
// direction one-way: engine wires ike's output into ipsec, not the reverse).
// Build it with NewSA, which validates the keys against the transforms.
type SA struct {
	SPI       uint32
	Cipher    Cipher
	Integrity Integrity
	EncKey    []byte
	AuthKey   []byte

	block   cipher.Block
	newHash func() hash.Hash
	icvLen  int
	mac     hash.Hash
	macBuf  [64]byte

	ivPool [64 * 1024]byte
	ivPos  int

	encPlainBuf []byte
	decPlainBuf []byte
	ivBuf       [16]byte

	// mu serializes Encrypt/Decrypt: several goroutines send on the same SA
	// (the utun pump, PPP/L2TP control replies), and an unsynchronized
	// seq++ could put two packets on the wire with one sequence number —
	// the second then dropped by the peer as a replay.
	mu     sync.Mutex
	seq    uint32       // outbound: last sequence number sent
	replay replayWindow // inbound only
}

// NewSA builds an SA for the negotiated transforms, rejecting keys whose
// length doesn't fit them — a mismatch here means the IKE layer and this
// one disagree about what was negotiated, which must never be papered over
// by running the wrong cipher.
func NewSA(spi uint32, c Cipher, i Integrity, encKey, authKey []byte) (*SA, error) {
	sa := &SA{
		SPI:       spi,
		Cipher:    c,
		Integrity: i,
		EncKey:    encKey,
		AuthKey:   authKey,
	}
	var err error
	switch c {
	case Cipher3DESCBC:
		sa.block, err = des.NewTripleDESCipher(encKey)
	case CipherAESCBC:
		sa.block, err = aes.NewCipher(encKey)
	default:
		return nil, fmt.Errorf("unsupported ESP cipher %d", c)
	}
	if err != nil {
		return nil, fmt.Errorf("ESP cipher init: %w", err)
	}
	var keyLen int
	switch i {
	case IntegHMACMD5_96:
		// RFC 2403: HMAC-MD5-96 uses 16-byte key and 96-bit (12-byte) ICV.
		// Required by many legacy VPN servers (Windows RRAS, old Cisco, MikroTik).
		sa.newHash, keyLen, sa.icvLen = md5.New, md5.Size, 12 //nolint:gosec
	case IntegHMACSHA1_96:
		sa.newHash, keyLen, sa.icvLen = sha1.New, sha1.Size, 12
	case IntegHMACSHA256_128:
		sa.newHash, keyLen, sa.icvLen = sha256.New, sha256.Size, 16
	case IntegHMACSHA384_192:
		sa.newHash, keyLen, sa.icvLen = sha512.New384, sha512.Size384, 24
	case IntegHMACSHA512_256:
		sa.newHash, keyLen, sa.icvLen = sha512.New, sha512.Size, 32
	default:
		return nil, fmt.Errorf("unsupported ESP integrity algorithm %d", i)
	}

	if len(authKey) != keyLen {
		return nil, fmt.Errorf("ESP integrity key is %d bytes, want %d", len(authKey), keyLen)
	}
	sa.mac = hmac.New(sa.newHash, sa.AuthKey)
	sa.ivPos = len(sa.ivPool) // force initial fill
	return sa, nil
}

func (sa *SA) getIV(iv []byte) error {
	blockLen := len(iv)
	if sa.ivPos+blockLen > len(sa.ivPool) {
		if _, err := rand.Read(sa.ivPool[:]); err != nil {
			return err
		}
		sa.ivPos = 0
	}
	copy(iv, sa.ivPool[sa.ivPos:sa.ivPos+blockLen])
	sa.ivPos += blockLen
	return nil
}

// Encrypt wraps one IP payload (the UDP/1701 L2TP datagram, without its own
// IP header — transport mode replaces only what ESP replaces) into an ESP
// packet, RFC 4303 format: SPI | Seq | IV | ciphertext(payload | pad | pad-len | next-header) | ICV.
// nextHeader is the IP protocol number of payload (17 for UDP).
func (sa *SA) Encrypt(payload []byte, nextHeader byte) ([]byte, error) {
	sa.mu.Lock()
	defer sa.mu.Unlock()
	// RFC 4303 §3.3.3: the sequence number must never cycle within one SA.
	if sa.seq == math.MaxUint32 {
		return nil, fmt.Errorf("ESP sequence number exhausted — SA must be rekeyed")
	}
	sa.seq++
	seq := sa.seq

	blockLen := sa.block.BlockSize()
	iv := sa.ivBuf[:blockLen]
	if err := sa.getIV(iv); err != nil {
		return nil, err
	}

	// Pad so payload+pad+padLen+nextHeader is a multiple of the block size,
	// RFC 4303 §2.4. Pad bytes are 1,2,3,... (a standard, verifiable filler
	// — not security-relevant, just alignment).
	total := len(payload) + 2 // + pad-length byte + next-header byte
	padNeeded := (blockLen - (total % blockLen)) % blockLen
	plainLen := len(payload) + padNeeded + 2
	if cap(sa.encPlainBuf) < plainLen {
		sa.encPlainBuf = make([]byte, plainLen+2048)
	}
	plain := sa.encPlainBuf[:plainLen]
	copy(plain, payload)
	for i := 0; i < padNeeded; i++ {
		plain[len(payload)+i] = byte(i + 1)
	}
	plain[len(payload)+padNeeded] = byte(padNeeded)
	plain[len(payload)+padNeeded+1] = nextHeader

	out := make([]byte, 8+blockLen+plainLen+sa.icvLen)
	binary.BigEndian.PutUint32(out[0:4], sa.SPI)
	binary.BigEndian.PutUint32(out[4:8], seq)
	copy(out[8:8+blockLen], iv)
	body := out[8+blockLen : 8+blockLen+plainLen]
	cipher.NewCBCEncrypter(sa.block, iv).CryptBlocks(body, plain)

	icvOffset := 8 + blockLen + plainLen
	sa.mac.Reset()
	sa.mac.Write(out[:icvOffset]) // ICV covers SPI|Seq|IV|ciphertext, RFC 4303
	sum := sa.mac.Sum(sa.macBuf[:0])
	copy(out[icvOffset:], sum[:sa.icvLen])
	return out, nil
}

// EncryptIPPacket embeds the inner UDP (8B) + L2TP (6B) + PPP (2B) header directly into the
// cipher buffer, encrypting in one single pass without heap allocations or slice churn.
// srcPort/dstPort are the inner UDP ports: the caller's L2TP port and the server's.
func (sa *SA) EncryptIPPacket(srcPort, dstPort, tunnelID, sessionID uint16, ipPkt []byte) ([]byte, error) {
	sa.mu.Lock()
	defer sa.mu.Unlock()
	if sa.seq == math.MaxUint32 {
		return nil, fmt.Errorf("ESP sequence number exhausted — SA must be rekeyed")
	}
	sa.seq++
	seq := sa.seq

	blockLen := sa.block.BlockSize()
	iv := sa.ivBuf[:blockLen]
	if err := sa.getIV(iv); err != nil {
		return nil, err
	}

	payloadLen := 16 + len(ipPkt)
	total := payloadLen + 2 // + pad-length byte + next-header byte
	padNeeded := (blockLen - (total % blockLen)) % blockLen
	plainLen := payloadLen + padNeeded + 2
	if cap(sa.encPlainBuf) < plainLen {
		sa.encPlainBuf = make([]byte, plainLen+2048)
	}
	plain := sa.encPlainBuf[:plainLen]

	// Inner UDP header (8 bytes): srcPort -> dstPort
	binary.BigEndian.PutUint16(plain[0:2], srcPort)
	binary.BigEndian.PutUint16(plain[2:4], dstPort)
	binary.BigEndian.PutUint16(plain[4:6], uint16(payloadLen))
	binary.BigEndian.PutUint16(plain[6:8], 0) // checksum 0

	// L2TP header (6 bytes): flags=0x0002, tunnelID, sessionID
	binary.BigEndian.PutUint16(plain[8:10], 0x0002)
	binary.BigEndian.PutUint16(plain[10:12], tunnelID)
	binary.BigEndian.PutUint16(plain[12:14], sessionID)

	// PPP header (2 bytes): ProtoIP 0x0021
	binary.BigEndian.PutUint16(plain[14:16], 0x0021)

	// IP packet payload
	copy(plain[16:16+len(ipPkt)], ipPkt)

	for i := 0; i < padNeeded; i++ {
		plain[payloadLen+i] = byte(i + 1)
	}
	plain[payloadLen+padNeeded] = byte(padNeeded)
	plain[payloadLen+padNeeded+1] = 17 // protoUDP

	outLen := 8 + blockLen + plainLen + sa.icvLen
	out := make([]byte, outLen)
	binary.BigEndian.PutUint32(out[0:4], sa.SPI)
	binary.BigEndian.PutUint32(out[4:8], seq)
	copy(out[8:8+blockLen], iv)
	body := out[8+blockLen : 8+blockLen+plainLen]
	cipher.NewCBCEncrypter(sa.block, iv).CryptBlocks(body, plain)

	icvOffset := 8 + blockLen + plainLen
	sa.mac.Reset()
	sa.mac.Write(out[:icvOffset])
	sum := sa.mac.Sum(sa.macBuf[:0])
	copy(out[icvOffset:], sum[:sa.icvLen])
	return out, nil
}

// Decrypt reverses Encrypt: verifies the ICV, checks the replay window,
// decrypts, strips padding, and returns the inner payload plus its
// next-header protocol number.
func (sa *SA) Decrypt(pkt []byte) (payload []byte, nextHeader byte, err error) {
	sa.mu.Lock()
	defer sa.mu.Unlock()
	blockLen := sa.block.BlockSize()
	if len(pkt) < 8+blockLen+blockLen+sa.icvLen {
		return nil, 0, fmt.Errorf("ESP packet too short: %d bytes", len(pkt))
	}
	spi := binary.BigEndian.Uint32(pkt[0:4])
	if spi != sa.SPI {
		return nil, 0, fmt.Errorf("ESP SPI mismatch: got %08x want %08x", spi, sa.SPI)
	}
	seq := binary.BigEndian.Uint32(pkt[4:8])
	if err := sa.replay.check(seq); err != nil {
		return nil, 0, err
	}

	icvOffset := len(pkt) - sa.icvLen
	sa.mac.Reset()
	sa.mac.Write(pkt[:icvOffset])
	sum := sa.mac.Sum(sa.macBuf[:0])
	if !hmac.Equal(sum[:sa.icvLen], pkt[icvOffset:]) {
		return nil, 0, fmt.Errorf("ESP ICV verification failed (wrong key, or corrupted/tampered packet)")
	}

	iv := pkt[8 : 8+blockLen]
	ciphertext := pkt[8+blockLen : icvOffset]
	if len(ciphertext)%blockLen != 0 {
		return nil, 0, fmt.Errorf("ESP ciphertext length %d not a multiple of block size %d", len(ciphertext), blockLen)
	}
	if cap(sa.decPlainBuf) < len(ciphertext) {
		sa.decPlainBuf = make([]byte, len(ciphertext)+2048)
	}
	plain := sa.decPlainBuf[:len(ciphertext)]
	cipher.NewCBCDecrypter(sa.block, iv).CryptBlocks(plain, ciphertext)

	padLen := int(plain[len(plain)-2])
	nextHeader = plain[len(plain)-1]
	if padLen+2 > len(plain) {
		return nil, 0, fmt.Errorf("ESP padding length %d exceeds plaintext", padLen)
	}
	payloadLen := len(plain) - 2 - padLen
	payload = make([]byte, payloadLen)
	copy(payload, plain[:payloadLen])
	sa.replay.accept(seq)
	return payload, nextHeader, nil
}
