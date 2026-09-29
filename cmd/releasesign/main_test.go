package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"

	"vpn/internal/release"
)

func newKey(t *testing.T) (seed []byte, pub string) {
	t.Helper()
	p, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return priv.Seed(), base64.StdEncoding.EncodeToString(p)
}

// The rotation release is signed with the previous key: the pre-publish
// check must let it through, and still refuse any other key.
func TestCheckSigningKeyWith(t *testing.T) {
	curSeed, cur := newKey(t)
	prevSeed, prev := newKey(t)
	otherSeed, _ := newKey(t)
	manifest := release.Format("v1.2.3", []string{"a"}, map[string]string{"a": strings.Repeat("0", 64)})
	sign := func(seed []byte) []byte {
		sig, err := release.Sign(seed, manifest)
		if err != nil {
			t.Fatal(err)
		}
		return sig
	}
	if err := checkSigningKeyWith(cur, prev, manifest, sign(curSeed)); err != nil {
		t.Fatalf("current key refused: %v", err)
	}
	if err := checkSigningKeyWith(cur, prev, manifest, sign(prevSeed)); err != nil {
		t.Fatalf("previous key refused during rotation: %v", err)
	}
	if err := checkSigningKeyWith(cur, prev, manifest, sign(otherSeed)); err == nil {
		t.Fatal("an unrelated key accepted")
	}
	if err := checkSigningKeyWith(cur, "", manifest, sign(prevSeed)); err == nil {
		t.Fatal("previous key accepted with no rotation under way")
	}
}
