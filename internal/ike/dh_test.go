package ike

import (
	"math/big"
	"testing"
)

// A responder that sends 0, 1 or p-1 forces a shared secret it can predict
// without holding a private exponent, so key agreement must reject it
// instead of deriving keys from it.
func TestSharedSecretRejectsDegeneratePeerValues(t *testing.T) {
	group := Groups[2]
	kp, err := GenerateKeyPair(group)
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	size := group.BitLen / 8

	cases := map[string]*big.Int{
		"zero":      big.NewInt(0),
		"one":       big.NewInt(1),
		"p minus 1": new(big.Int).Sub(group.Prime, big.NewInt(1)),
		"p":         group.Prime,
	}
	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := kp.SharedSecret(leftPad(value.Bytes(), size)); err == nil {
				t.Fatalf("SharedSecret accepted peer value %s", name)
			}
		})
	}
}

func TestSharedSecretLength(t *testing.T) {
	a, err := GenerateKeyPair(Groups[2])
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	size := Groups[2].BitLen / 8

	// An unpadded public value (leading zero bytes dropped) is the same
	// integer and must agree on the same secret as the padded one.
	peer := big.NewInt(123456789) // well inside [2, p-2], far shorter than size
	unpadded := peer.Bytes()
	padded := leftPad(unpadded, size)
	want, err := a.SharedSecret(padded)
	if err != nil {
		t.Fatal(err)
	}
	got, err := a.SharedSecret(unpadded)
	if err != nil {
		t.Fatalf("unpadded KE value rejected: %v", err)
	}
	if string(got) != string(want) {
		t.Fatal("unpadded KE value produced a different shared secret")
	}

	if _, err := a.SharedSecret(make([]byte, size+1)); err == nil {
		t.Fatal("SharedSecret accepted an overlong KE payload")
	}
}

// Both sides must still agree on the same secret for a normal exchange across all supported groups.
func TestSharedSecretAgrees(t *testing.T) {
	for _, groupID := range []int{1, 2, 5, 14, 15, 16} {
		group := Groups[groupID]
		if group == nil {
			t.Fatalf("group %d is nil", groupID)
		}
		a, err := GenerateKeyPair(group)
		if err != nil {
			t.Fatalf("group %d GenerateKeyPair A: %v", groupID, err)
		}
		b, err := GenerateKeyPair(group)
		if err != nil {
			t.Fatalf("group %d GenerateKeyPair B: %v", groupID, err)
		}
		fromA, err := a.SharedSecret(b.PublicBytes())
		if err != nil {
			t.Fatalf("group %d a.SharedSecret: %v", groupID, err)
		}
		fromB, err := b.SharedSecret(a.PublicBytes())
		if err != nil {
			t.Fatalf("group %d b.SharedSecret: %v", groupID, err)
		}
		if string(fromA) != string(fromB) {
			t.Fatalf("group %d peers derived different shared secrets", groupID)
		}
	}
}
