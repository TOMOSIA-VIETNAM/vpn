package ppp

import (
	"crypto/des"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf16"

	"golang.org/x/crypto/md4" //nolint:staticcheck // MD4 is mandated by MS-CHAPv2 (RFC 2759) for the legacy NT password hash — not our choice, and not used for anything but interop with this specific auth protocol.
)

// MSCHAPv2Response is the 49-byte Response field of a CHAP Response packet
// when Algorithm = CHAPAlgoMSCHAPv2 (RFC 2759 §8.1).
type MSCHAPv2Response struct {
	PeerChallenge [16]byte
	NTResponse    [24]byte
	Flags         uint8 // always 0, RFC 2759 §8.1

	// expectedAuthenticator is the 20-byte AuthenticatorResponse (RFC 2759
	// §8.7) a genuine LNS must echo back as "S=<40 hex>" in its Success
	// packet — never sent on the wire by us, only compared against the
	// peer's claim by VerifySuccess.
	expectedAuthenticator [20]byte
}

// GenerateMSCHAPv2Response computes the full response to an MS-CHAPv2
// challenge from the LNS, RFC 2759 §4-§8: ChallengeHash, then
// NtPasswordHash, then the triple-DES ChallengeResponse.
func GenerateMSCHAPv2Response(authenticatorChallenge []byte, username, password string) (*MSCHAPv2Response, error) {
	var peerChallenge [16]byte
	if _, err := rand.Read(peerChallenge[:]); err != nil {
		return nil, fmt.Errorf("generate PeerChallenge: %w", err)
	}
	return generateMSCHAPv2Response(peerChallenge, authenticatorChallenge, username, password)
}

// generateMSCHAPv2Response is GenerateMSCHAPv2Response with the
// PeerChallenge supplied by the caller, so the RFC 2759 §9.2 test vector can
// exercise the exact production path.
func generateMSCHAPv2Response(peerChallenge [16]byte, authenticatorChallenge []byte, username, password string) (*MSCHAPv2Response, error) {
	challenge, err := challengeHash(peerChallenge[:], authenticatorChallenge, username)
	if err != nil {
		return nil, err
	}
	passwordHash, err := ntPasswordHash(password)
	if err != nil {
		return nil, err
	}
	ntResponse, err := challengeResponse(challenge, passwordHash)
	if err != nil {
		return nil, err
	}

	r := &MSCHAPv2Response{PeerChallenge: peerChallenge}
	copy(r.NTResponse[:], ntResponse)
	copy(r.expectedAuthenticator[:], authenticatorResponse(passwordHash, ntResponse, challenge))
	return r, nil
}

// VerifySuccess checks the LNS's Success message (RFC 2759 §5: "S=<auth
// string> M=<message>") against the AuthenticatorResponse this response
// implies. Only a peer that knows the account's NT password hash can
// produce it, so this is MS-CHAPv2's mutual-authentication step: without
// it, anyone holding the (often shared) IPsec PSK could impersonate the
// server and have this client treat the link as authenticated.
func (r *MSCHAPv2Response) VerifySuccess(message []byte) error {
	msg := string(message)
	i := strings.Index(msg, "S=")
	if i < 0 || len(msg) < i+2+40 {
		return fmt.Errorf("MS-CHAPv2 Success message carries no authenticator response (S=)")
	}
	got, err := hex.DecodeString(msg[i+2 : i+2+40])
	if err != nil {
		return fmt.Errorf("MS-CHAPv2 Success message has a malformed authenticator response: %w", err)
	}
	if !hmac.Equal(got, r.expectedAuthenticator[:]) {
		return fmt.Errorf("MS-CHAPv2 authenticator response mismatch — the peer does not know this account's password (possible server impersonation)")
	}
	return nil
}

// authenticatorResponse is RFC 2759 §8.7's GenerateAuthenticatorResponse,
// returning the raw 20-byte digest (the wire form is its uppercase hex).
func authenticatorResponse(passwordHash, ntResponse, challenge []byte) []byte {
	magic1 := []byte("Magic server to client signing constant")
	magic2 := []byte("Pad to make it do more than one iteration")

	h4 := md4.New()
	h4.Write(passwordHash)
	passwordHashHash := h4.Sum(nil)

	d := sha1.New()
	d.Write(passwordHashHash)
	d.Write(ntResponse)
	d.Write(magic1)
	digest := d.Sum(nil)

	d = sha1.New()
	d.Write(digest)
	d.Write(challenge)
	d.Write(magic2)
	return d.Sum(nil)
}

// Marshal encodes the 49-byte Response field: PeerChallenge(16) |
// Reserved(8, zero) | NTResponse(24) | Flags(1), RFC 2759 §8.1.
func (r *MSCHAPv2Response) Marshal() []byte {
	b := make([]byte, 49)
	copy(b[0:16], r.PeerChallenge[:])
	// b[16:24] Reserved, zero
	copy(b[24:48], r.NTResponse[:])
	b[48] = r.Flags
	return b
}

// challengeHash is RFC 2759 §8.2's ChallengeHash: SHA1(PeerChallenge |
// AuthenticatorChallenge | Username), truncated to the first 8 bytes. Only
// the username's account-name portion is used if it contains a domain
// (RFC 2759 doesn't specify domain stripping explicitly, but every real
// implementation strips a "DOMAIN\" prefix if present — this client's
// account names from the reference config never include one, so this is a
// no-op here, kept only for correctness if a future profile's username
// does).
func challengeHash(peerChallenge, authenticatorChallenge []byte, username string) ([]byte, error) {
	if i := indexByte(username, '\\'); i >= 0 {
		username = username[i+1:]
	}
	h := sha1.New()
	h.Write(peerChallenge)
	h.Write(authenticatorChallenge)
	h.Write([]byte(username))
	sum := h.Sum(nil)
	return sum[:8], nil
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// ntPasswordHash is RFC 2759 §8.3's NtPasswordHash: MD4 of the password
// encoded as UTF-16LE (matching how Windows stores the NT hash — this is
// mandated by the protocol, not a choice of character encoding made here).
func ntPasswordHash(password string) ([]byte, error) {
	u16 := utf16.Encode([]rune(password))
	buf := make([]byte, len(u16)*2)
	defer func() {
		for i := range buf {
			buf[i] = 0
		}
		for i := range u16 {
			u16[i] = 0
		}
	}()
	for i, v := range u16 {
		buf[2*i] = byte(v)
		buf[2*i+1] = byte(v >> 8)
	}
	h := md4.New()
	h.Write(buf)
	return h.Sum(nil), nil
}

// challengeResponse is RFC 2759 §8.5's ChallengeResponse: the 16-byte NT
// password hash, zero-padded to 21 bytes and split into three 7-byte DES
// keys, each used to DES-encrypt the 8-byte Challenge; the three 8-byte
// outputs concatenate to the 24-byte NTResponse.
func challengeResponse(challenge, passwordHash []byte) ([]byte, error) {
	zHash := make([]byte, 21)
	copy(zHash, passwordHash)
	defer func() {
		for i := range zHash {
			zHash[i] = 0
		}
	}()

	out := make([]byte, 24)
	for i := 0; i < 3; i++ {
		key56 := zHash[i*7 : i*7+7]
		key64 := expandDESKey(key56)
		block, err := des.NewCipher(key64)
		for j := range key64 {
			key64[j] = 0
		}
		if err != nil {
			return nil, fmt.Errorf("DES key setup: %w", err)
		}
		block.Encrypt(out[i*8:i*8+8], challenge)
	}
	return out, nil
}

// expandDESKey spreads a 56-bit (7-byte) key across 8 bytes with the low
// bit of each byte left as a DES parity placeholder — Go's crypto/des does
// not check parity, so its value is irrelevant to the encryption result;
// this is the same bit layout every MS-CHAP implementation uses to turn a
// 7-byte key into a DES key.
func expandDESKey(k7 []byte) []byte {
	k8 := make([]byte, 8)
	k8[0] = k7[0]
	k8[1] = k7[0]<<7 | k7[1]>>1
	k8[2] = k7[1]<<6 | k7[2]>>2
	k8[3] = k7[2]<<5 | k7[3]>>3
	k8[4] = k7[3]<<4 | k7[4]>>4
	k8[5] = k7[4]<<3 | k7[5]>>5
	k8[6] = k7[5]<<2 | k7[6]>>6
	k8[7] = k7[6] << 1
	return k8
}
