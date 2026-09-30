package release

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// testKey is a throwaway key so tests never depend on the real signing key.
func testKey(t *testing.T) (seed []byte, pubB64 string) {
	t.Helper()
	seed = bytes.Repeat([]byte{7}, ed25519.SeedSize)
	pub := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	return seed, base64.StdEncoding.EncodeToString(pub)
}

func digest(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func TestSignVerifyRoundTrip(t *testing.T) {
	seed, pub := testKey(t)
	bin := []byte("binary contents")
	manifest := Format("v1.2.3", []string{"vpn-darwin-arm64"}, map[string]string{"vpn-darwin-arm64": digest(bin)})
	sig, err := Sign(seed, manifest)
	if err != nil {
		t.Fatal(err)
	}

	m, err := VerifyWith(pub, manifest, sig)
	if err != nil {
		t.Fatalf("genuine manifest rejected: %v", err)
	}
	if m.Version != "v1.2.3" {
		t.Fatalf("version = %q", m.Version)
	}
	if err := m.CheckAsset("vpn-darwin-arm64", bin); err != nil {
		t.Fatalf("genuine asset rejected: %v", err)
	}
	if err := m.CheckAsset("vpn-darwin-arm64", []byte("tampered")); err == nil {
		t.Fatal("tampered asset accepted")
	}
	if err := m.CheckAsset("vpn-darwin-amd64", bin); err == nil {
		t.Fatal("unlisted asset accepted")
	}
}

func TestVerifyRejectsTampering(t *testing.T) {
	seed, pub := testKey(t)
	manifest := Format("v1.0.0", []string{"a"}, map[string]string{"a": digest([]byte("x"))})
	sig, _ := Sign(seed, manifest)

	forged := bytes.Replace(manifest, []byte("v1.0.0"), []byte("v9.0.0"), 1)
	if _, err := VerifyWith(pub, forged, sig); err == nil {
		t.Fatal("modified manifest accepted")
	}
	otherSeed := bytes.Repeat([]byte{8}, ed25519.SeedSize)
	otherSig, _ := Sign(otherSeed, manifest)
	if _, err := VerifyWith(pub, manifest, otherSig); err == nil {
		t.Fatal("manifest signed by another key accepted")
	}
	if _, err := VerifyWith(pub, manifest, sig[:10]); err == nil {
		t.Fatal("truncated signature accepted")
	}
}

func TestEmbeddedPublicKeyIsValid(t *testing.T) {
	pub, err := base64.StdEncoding.DecodeString(PublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		t.Fatalf("PublicKey is not a base64 ed25519 public key: %v (len %d)", err, len(pub))
	}
}

func TestSemver(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v1.2.3", "v1.2.3", 0},
		{"v1.2.3", "v1.2.4", -1},
		{"v1.10.0", "v1.9.9", 1},
		{"v2.0.0", "v1.99.99", 1},
		{"v1.2.3-4-gabcdef", "v1.2.3", 0},
		{"v1.2.3-dirty", "v1.2.4", -1},
	}
	for _, c := range cases {
		a, okA := ParseSemver(c.a)
		b, okB := ParseSemver(c.b)
		if !okA || !okB {
			t.Fatalf("parse %q/%q failed", c.a, c.b)
		}
		if got := a.Compare(b); got != c.want {
			t.Errorf("Compare(%s, %s) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
	for _, bad := range []string{"", "1.2.3", "v1.2", "c4793d5", "dev", "v1.x.3", "v-1.2.3"} {
		if _, ok := ParseSemver(bad); ok {
			t.Errorf("ParseSemver(%q) accepted", bad)
		}
	}
}

func TestP256SignatureVerifiesWithOpenSSL(t *testing.T) {
	openssl, err := exec.LookPath("openssl")
	if err != nil {
		t.Skip("no openssl on PATH")
	}
	priv, pub, err := GenerateP256()
	if err != nil {
		t.Fatal(err)
	}
	manifest := Format("v1.2.3", []string{"vpn-darwin-arm64"}, map[string]string{"vpn-darwin-arm64": strings.Repeat("ab", 32)})
	sig, err := SignP256(priv, manifest)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range map[string][]byte{"pub.pem": []byte(pub), "sums": manifest, "sig": sig} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	verify := func() error {
		return exec.Command(openssl, "dgst", "-sha256", "-verify", filepath.Join(dir, "pub.pem"), "-signature", filepath.Join(dir, "sig"), filepath.Join(dir, "sums")).Run()
	}
	if err := verify(); err != nil {
		t.Fatalf("openssl rejects a valid signature: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sums"), append(manifest, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verify(); err == nil {
		t.Fatal("openssl accepted a signature over a modified manifest")
	}
}
