package ike

import (
	"bytes"
	"testing"
)

// A responder SA payload choosing t, built with the same marshalers the
// initiator uses for its own offer.
func responderSA(t *testing.T, tr Transform) []byte {
	t.Helper()
	body, err := marshalESPSA([]Transform{tr}, 0x01020304)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestESPProposalNegotiatesNamedIntegrity(t *testing.T) {
	for _, c := range []struct {
		proposal string
		hash     int
		authKey  int
	}{
		{"aes256-sha256", HashSHA256, 32},
		{"aes128-sha1", HashSHA1, 20},
		{"3des-sha1", HashSHA1, 20},
		{"aes128-md5", HashMD5, 16},
	} {
		offer, err := espProposalFor(c.proposal)
		if err != nil {
			t.Fatal(err)
		}
		got, err := parseChosenESPSA(responderSA(t, offer))
		if err != nil {
			t.Fatalf("%s: %v", c.proposal, err)
		}
		if got.Transform.Hash != c.hash {
			t.Fatalf("%s: negotiated hash %d, want %d (proposal name must mean its integrity algorithm)", c.proposal, got.Transform.Hash, c.hash)
		}
		if !offeredESP(got.Transform, []Transform{offer}) {
			t.Fatalf("%s: own offer not recognized as offered", c.proposal)
		}
		if _, authLen := espKeyLens(got.Transform); authLen != c.authKey {
			t.Fatalf("%s: auth key %d bytes, want %d", c.proposal, authLen, c.authKey)
		}
	}
}

func TestOfferedESPRejectsSubstitutedTransform(t *testing.T) {
	aes256sha256, _ := espProposalFor("aes256-sha256")
	offered := []Transform{aes256sha256}
	for name, tr := range map[string]Transform{
		"single DES":       {Encryption: EncDES, Hash: HashSHA1},
		"weaker key size":  {Encryption: EncAES, KeyBits: 128, Hash: HashSHA256},
		"weaker integrity": {Encryption: EncAES, KeyBits: 256, Hash: HashSHA1},
	} {
		if offeredESP(tr, offered) {
			t.Errorf("%s accepted though never offered", name)
		}
	}
}

func TestVerifyQuickModeHash2(t *testing.T) {
	skeyidA := bytes.Repeat([]byte{0x5a}, 20)
	ni := bytes.Repeat([]byte{0x01}, 16)
	msgID := uint32(0xCAFEBABE)

	rest := append(marshalPayload(PayloadNonce, []byte("sa-body")), marshalPayload(PayloadNone, []byte("responder-nonce"))...)
	hash2, _ := prf(HashSHA1, skeyidA, append(append(beUint32(msgID), ni...), rest...))
	plain := append(marshalPayload(PayloadSA, hash2), rest...)
	plain = padToBlock(plain, 16) // encryption padding must not enter the hash

	check := func(p []byte) error {
		payloads, err := SplitPayloads(PayloadHash, p)
		if err != nil {
			return err
		}
		return verifyQuickModeHash2(HashSHA1, skeyidA, msgID, ni, PayloadHash, payloads, p)
	}
	if err := check(plain); err != nil {
		t.Fatalf("genuine QM2 rejected: %v", err)
	}
	tampered := append([]byte{}, plain...)
	tampered[len(marshalPayload(PayloadSA, hash2))+5] ^= 0x01 // flip a bit in the SA body
	if err := check(tampered); err == nil {
		t.Fatal("tampered QM2 accepted")
	}
}

func TestESPProposalRejectsUnsupportedIntegrity(t *testing.T) {
	if _, err := espProposalFor("aes128-sha512"); err == nil {
		t.Fatal("aes128-sha512 accepted though ESP has no SHA512 integrity")
	}
}
