package ike

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/des"
	"crypto/hmac"
	"crypto/md5" //nolint:gosec // required: legacy VPN servers (Windows RRAS, Cisco IOS, pfSense) use MD5 for IKE Phase 1. MD5 is weak but necessary for interoperability.
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"hash"
)

// newHash returns the stdlib hash.Hash for an IKE hash algorithm value.
// Every primitive here comes from crypto/*; nothing is hand-rolled.
func newHash(alg int) (func() hash.Hash, int, error) {
	switch alg {
	case HashMD5:
		// MD5 is cryptographically weak but required by many legacy VPN servers
		// (Windows Server RRAS, Cisco IOS with older configs, pfSense defaults,
		// MikroTik). Without MD5, IKE proposal mismatch occurs with these servers.
		return md5.New, md5.Size, nil //nolint:gosec
	case HashSHA1:
		return sha1.New, sha1.Size, nil
	case HashSHA256:
		return sha256.New, sha256.Size, nil
	case HashSHA384:
		return sha512.New384, sha512.Size384, nil
	case HashSHA512:
		return sha512.New, sha512.Size, nil
	default:
		return nil, 0, fmt.Errorf("unsupported hash algorithm %d", alg)
	}
}

// prf is IKEv1's keyed pseudo-random function: by default, unless a
// separate PRF is negotiated (this client never negotiates one, matching
// the reference config), PRF = HMAC using the negotiated hash algorithm,
// RFC 2409 §5.
func prf(hashAlg int, key, data []byte) ([]byte, error) {
	newH, _, err := newHash(hashAlg)
	if err != nil {
		return nil, err
	}
	mac := hmac.New(newH, key)
	mac.Write(data)
	return mac.Sum(nil), nil
}

// digest computes a plain (unkeyed) hash — used for the phase-1 IV seed
// (hash(g^xi | g^xr)) and for the quick-mode IV seed, RFC 2409 §5.
func digest(hashAlg int, data []byte) ([]byte, error) {
	newH, _, err := newHash(hashAlg)
	if err != nil {
		return nil, err
	}
	h := newH()
	h.Write(data)
	return h.Sum(nil), nil
}

// expandKey stretches SKEYID_e (or any PRF output) to the number of bytes a
// cipher needs, via the Ka = K1|K2|... expansion of RFC 2409 Appendix B:
//
//	K1 = prf(SKEYID_e, 0)     (0 = a single zero octet, not "K0")
//	K2 = prf(SKEYID_e, K1)
//	K3 = prf(SKEYID_e, K2)
//	Ka = K1 | K2 | K3 | ...
//
// Note SKEYID_e itself is never part of Ka, and every Kn is keyed by the
// original SKEYID_e, not by the previous Kn. This only matters for a cipher
// whose key is longer than the negotiated hash's output (e.g. AES-256 with
// SHA-1, or 3DES's 24-byte key with either hash in this client's proposals).
func expandKey(hashAlg int, key []byte, neededBytes int) ([]byte, error) {
	if len(key) >= neededBytes {
		// RFC 2409 Appendix B: the K1/K2/... expansion only applies when
		// the negotiated hash's output is shorter than the cipher key;
		// otherwise Ka is simply SKEYID_e truncated to the needed length.
		return key[:neededBytes], nil
	}
	var out []byte
	prev := []byte{0}
	for len(out) < neededBytes {
		next, err := prf(hashAlg, key, prev)
		if err != nil {
			return nil, err
		}
		out = append(out, next...)
		prev = next
	}
	return out[:neededBytes], nil
}

func cipherKeyLen(t Transform) int {
	switch t.Encryption {
	case Enc3DES:
		return 24
	case EncDES:
		return 8
	case EncAES:
		if t.KeyBits == 0 {
			return 16
		}
		return t.KeyBits / 8
	default:
		return 0
	}
}

func blockSize(t Transform) int {
	switch t.Encryption {
	case EncAES:
		return aes.BlockSize
	case Enc3DES, EncDES:
		return des.BlockSize
	default:
		return 0
	}
}

func newBlockCipher(t Transform, key []byte) (cipher.Block, error) {
	switch t.Encryption {
	case EncAES:
		return aes.NewCipher(key)
	case Enc3DES:
		return des.NewTripleDESCipher(key)
	case EncDES:
		return des.NewCipher(key)
	default:
		return nil, fmt.Errorf("unsupported encryption algorithm %d", t.Encryption)
	}
}

// cbcEncrypt/cbcDecrypt operate on data that is already a multiple of the
// block size — IKEv1 payloads are padded to the block size before
// encryption (RFC 2409 §5.3), handled by the caller so padding-length
// bookkeeping stays next to the payload framing it affects.
func cbcEncrypt(t Transform, key, iv, plaintext []byte) ([]byte, error) {
	block, err := newBlockCipher(t, key)
	if err != nil {
		return nil, err
	}
	if len(plaintext)%block.BlockSize() != 0 {
		return nil, fmt.Errorf("plaintext length %d not a multiple of block size %d", len(plaintext), block.BlockSize())
	}
	out := make([]byte, len(plaintext))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, plaintext)
	return out, nil
}

func cbcDecrypt(t Transform, key, iv, ciphertext []byte) ([]byte, error) {
	block, err := newBlockCipher(t, key)
	if err != nil {
		return nil, err
	}
	if len(ciphertext)%block.BlockSize() != 0 {
		return nil, fmt.Errorf("ciphertext length %d not a multiple of block size %d", len(ciphertext), block.BlockSize())
	}
	out := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, ciphertext)
	return out, nil
}

func padToBlock(data []byte, blockLen int) []byte {
	pad := blockLen - (len(data) % blockLen)
	if pad == 0 {
		pad = blockLen
	}
	return append(append([]byte{}, data...), make([]byte, pad)...)
}
