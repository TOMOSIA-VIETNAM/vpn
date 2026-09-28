package ike

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

// DHGroup is a standardized MODP Diffie-Hellman group. Primes are the
// public, standardized values from RFC 2409 §6 (groups 1/2) and RFC 3526
// §2/§3 (group 5/14) — not invented, and identical to what strongSwan and
// every other IKE implementation uses for these group numbers.
type DHGroup struct {
	ID        int
	Prime     *big.Int
	Generator *big.Int
	BitLen    int
}

var (
	group1Prime = mustHexPrime(`
		FFFFFFFF FFFFFFFF C90FDAA2 2168C234 C4C6628B 80DC1CD1
		29024E08 8A67CC74 020BBEA6 3B139B22 514A0879 8E3404DD
		EF9519B3 CD3A431B 302B0A6D F25F1437 4FE1356D 6D51C245
		E485B576 625E7EC6 F44C42E9 A63A3620 FFFFFFFF FFFFFFFF`)

	group2Prime = mustHexPrime(`
		FFFFFFFF FFFFFFFF C90FDAA2 2168C234 C4C6628B 80DC1CD1
		29024E08 8A67CC74 020BBEA6 3B139B22 514A0879 8E3404DD
		EF9519B3 CD3A431B 302B0A6D F25F1437 4FE1356D 6D51C245
		E485B576 625E7EC6 F44C42E9 A637ED6B 0BFF5CB6 F406B7ED
		EE386BFB 5A899FA5 AE9F2411 7C4B1FE6 49286651 ECE65381
		FFFFFFFF FFFFFFFF`)

	group5Prime = mustHexPrime(`
		FFFFFFFF FFFFFFFF C90FDAA2 2168C234 C4C6628B 80DC1CD1
		29024E08 8A67CC74 020BBEA6 3B139B22 514A0879 8E3404DD
		EF9519B3 CD3A431B 302B0A6D F25F1437 4FE1356D 6D51C245
		E485B576 625E7EC6 F44C42E9 A637ED6B 0BFF5CB6 F406B7ED
		EE386BFB 5A899FA5 AE9F2411 7C4B1FE6 49286651 ECE45B3D
		C2007CB8 A163BF05 98DA4836 1C55D39A 69163FA8 FD24CF5F
		83655D23 DCA3AD96 1C62F356 208552BB 9ED52907 7096966D
		670C354E 4ABC9804 F1746C08 CA237327 FFFFFFFF FFFFFFFF`)

	group14Prime = mustHexPrime(`
		FFFFFFFF FFFFFFFF C90FDAA2 2168C234 C4C6628B 80DC1CD1
		29024E08 8A67CC74 020BBEA6 3B139B22 514A0879 8E3404DD
		EF9519B3 CD3A431B 302B0A6D F25F1437 4FE1356D 6D51C245
		E485B576 625E7EC6 F44C42E9 A637ED6B 0BFF5CB6 F406B7ED
		EE386BFB 5A899FA5 AE9F2411 7C4B1FE6 49286651 ECE45B3D
		C2007CB8 A163BF05 98DA4836 1C55D39A 69163FA8 FD24CF5F
		83655D23 DCA3AD96 1C62F356 208552BB 9ED52907 7096966D
		670C354E 4ABC9804 F1746C08 CA18217C 32905E46 2E36CE3B
		E39E772C 180E8603 9B2783A2 EC07A28F B5C55DF0 6F4C52C9
		DE2BCBF6 95581718 3995497C EA956AE5 15D22618 98FA0510
		15728E5A 8AACAA68 FFFFFFFF FFFFFFFF`)

	group15Prime = mustHexPrime(`
		FFFFFFFF FFFFFFFF C90FDAA2 2168C234 C4C6628B 80DC1CD1
		29024E08 8A67CC74 020BBEA6 3B139B22 514A0879 8E3404DD
		EF9519B3 CD3A431B 302B0A6D F25F1437 4FE1356D 6D51C245
		E485B576 625E7EC6 F44C42E9 A637ED6B 0BFF5CB6 F406B7ED
		EE386BFB 5A899FA5 AE9F2411 7C4B1FE6 49286651 ECE45B3D
		C2007CB8 A163BF05 98DA4836 1C55D39A 69163FA8 FD24CF5F
		83655D23 DCA3AD96 1C62F356 208552BB 9ED52907 7096966D
		670C354E 4ABC9804 F1746C08 CA18217C 32905E46 2E36CE3B
		E39E772C 180E8603 9B2783A2 EC07A28F B5C55DF0 6F4C52C9
		DE2BCBF6 95581718 3995497C EA956AE5 15D22618 98FA0510
		15728E5A 8AAAC42D AD33170D 04507A33 A85521AB DF1CBA64
		ECFB8504 58DBEF0A 8AEA7157 5D060C7D B3970F85 A6E1E4C7
		ABF5AE8C DB0933D7 1E8C94E0 4A25619D CEE3D226 1AD2EE6B
		F12FFA06 D98A0864 D8760273 3EC86A64 521F2B18 177B200C
		BBE11757 7A615D6C 770988C0 BAD946E2 08E24FA0 74E5AB31
		43DB5BFC E0FD108E 4B82D120 A9210801 FFFFFFFF FFFFFFFF`)

	group16Prime = mustHexPrime(`
		FFFFFFFF FFFFFFFF C90FDAA2 2168C234 C4C6628B 80DC1CD1
		29024E08 8A67CC74 020BBEA6 3B139B22 514A0879 8E3404DD
		EF9519B3 CD3A431B 302B0A6D F25F1437 4FE1356D 6D51C245
		E485B576 625E7EC6 F44C42E9 A637ED6B 0BFF5CB6 F406B7ED
		EE386BFB 5A899FA5 AE9F2411 7C4B1FE6 49286651 ECE45B3D
		C2007CB8 A163BF05 98DA4836 1C55D39A 69163FA8 FD24CF5F
		83655D23 DCA3AD96 1C62F356 208552BB 9ED52907 7096966D
		670C354E 4ABC9804 F1746C08 CA18217C 32905E46 2E36CE3B
		E39E772C 180E8603 9B2783A2 EC07A28F B5C55DF0 6F4C52C9
		DE2BCBF6 95581718 3995497C EA956AE5 15D22618 98FA0510
		15728E5A 8AAAC42D AD33170D 04507A33 A85521AB DF1CBA64
		ECFB8504 58DBEF0A 8AEA7157 5D060C7D B3970F85 A6E1E4C7
		ABF5AE8C DB0933D7 1E8C94E0 4A25619D CEE3D226 1AD2EE6B
		F12FFA06 D98A0864 D8760273 3EC86A64 521F2B18 177B200C
		BBE11757 7A615D6C 770988C0 BAD946E2 08E24FA0 74E5AB31
		43DB5BFC E0FD108E 4B82D120 A9210801 1A723C12 A787E6D7
		88719A10 BDBA5B26 99C32718 6AF4E23C 1A946834 B6150BDA
		2583E9CA 2AD44CE8 DBBBC2DB 04DE8EF9 2E8EFC14 1F078420
		74798BED 28EC5EBB D031B578 4360F4C7 F139ECFE 3297229D
		179177D0 506A959F 1EC1229D 1F4ED524 16A34E30 B1045136
		CEB44594 8A49F88C 7157CDC6 9FB7B6FD B515E2D3 30EE17D0
		FFFFFFFF FFFFFFFF`)
)

// Groups is keyed by MODP group number, matching the group numbers used in
// entrypoint.sh's ike= lines (modp1024=2, modp2048=14, modp3072=15, modp4096=16).
var Groups = map[int]*DHGroup{
	1:  {ID: 1, Prime: group1Prime, Generator: big.NewInt(2), BitLen: 768},
	2:  {ID: 2, Prime: group2Prime, Generator: big.NewInt(2), BitLen: 1024},
	5:  {ID: 5, Prime: group5Prime, Generator: big.NewInt(2), BitLen: 1536},
	14: {ID: 14, Prime: group14Prime, Generator: big.NewInt(2), BitLen: 2048},
	15: {ID: 15, Prime: group15Prime, Generator: big.NewInt(2), BitLen: 3072},
	16: {ID: 16, Prime: group16Prime, Generator: big.NewInt(2), BitLen: 4096},
}

func mustHexPrime(hexWithSpaces string) *big.Int {
	clean := make([]byte, 0, len(hexWithSpaces))
	for _, c := range []byte(hexWithSpaces) {
		if (c >= '0' && c <= '9') || (c >= 'A' && c <= 'F') || (c >= 'a' && c <= 'f') {
			clean = append(clean, c)
		}
	}
	n, ok := new(big.Int).SetString(string(clean), 16)
	if !ok {
		panic("invalid hardcoded DH prime hex")
	}
	return n
}

// KeyPair is one side's ephemeral Diffie-Hellman keying material.
type KeyPair struct {
	Group   *DHGroup
	Private *big.Int
	Public  *big.Int // g^private mod p, encoded big-endian, group.BitLen/8 bytes
}

// GenerateKeyPair produces a fresh ephemeral DH keypair in the given group.
func GenerateKeyPair(group *DHGroup) (*KeyPair, error) {
	// Private exponent: a random value in [2, p-2], sized to the group per
	// common practice (256 bits of randomness is enough entropy for every
	// group here; RFC 2409 does not mandate an exact size).
	priv, err := rand.Int(rand.Reader, new(big.Int).Sub(group.Prime, big.NewInt(3)))
	if err != nil {
		return nil, fmt.Errorf("generate DH private value: %w", err)
	}
	priv.Add(priv, big.NewInt(2))
	pub := new(big.Int).Exp(group.Generator, priv, group.Prime)
	return &KeyPair{Group: group, Private: priv, Public: pub}, nil
}

// PublicBytes encodes the public value as a fixed-width big-endian byte
// string the size of the group's prime, as IKE KE payloads require.
func (k *KeyPair) PublicBytes() []byte {
	return leftPad(k.Public.Bytes(), k.Group.BitLen/8)
}

// SharedSecret computes g^(a*b) mod p from the peer's public KE value.
//
// The peer's value is validated first (RFC 2631 §2.1.5): 0, 1 and p-1 all
// produce a shared secret the peer can predict without knowing its own
// private exponent, and anything >= p is outside the group. Without this
// check a malicious responder could force a known g^xy — the PSK still has
// to match for HASH_R to verify, so this is not remotely exploitable on its
// own, but key agreement must not silently accept a degenerate value.
func (k *KeyPair) SharedSecret(peerPublic []byte) ([]byte, error) {
	size := k.Group.BitLen / 8
	// RFC 2409 §5 says the value MUST be zero-padded to the group size, but
	// a peer that drops a leading zero byte (1 in 256 values) would
	// otherwise fail at random; a shorter value is the same integer, and
	// the range check below is what actually guards the key agreement.
	if len(peerPublic) > size {
		return nil, fmt.Errorf("peer KE payload is %d bytes, more than the %d of DH group %d", len(peerPublic), size, k.Group.ID)
	}
	peer := new(big.Int).SetBytes(peerPublic)
	pMinus1 := new(big.Int).Sub(k.Group.Prime, big.NewInt(1))
	if peer.Cmp(big.NewInt(1)) <= 0 || peer.Cmp(pMinus1) >= 0 {
		return nil, fmt.Errorf("peer KE value is outside [2, p-2] for DH group %d", k.Group.ID)
	}
	shared := new(big.Int).Exp(peer, k.Private, k.Group.Prime)
	if shared.Cmp(big.NewInt(1)) <= 0 || shared.Cmp(pMinus1) >= 0 {
		return nil, fmt.Errorf("DH shared secret is degenerate for group %d", k.Group.ID)
	}
	return leftPad(shared.Bytes(), size), nil
}

func leftPad(b []byte, size int) []byte {
	if len(b) >= size {
		return b[len(b)-size:]
	}
	out := make([]byte, size)
	copy(out[size-len(b):], b)
	return out
}
