// Package release defines the signed release manifest shared by the CI
// signer (cmd/releasesign) and `vpn update` — one definition of the format,
// the signature scheme and version ordering, so the two sides cannot drift.
//
// A release publishes, next to its binaries:
//
//	SHA256SUMS      manifest: a "version <tag>" line, then "<sha256>  <asset>" lines
//	SHA256SUMS.sig  raw 64-byte ed25519 signature over SHA256SUMS
//
// `vpn update` verifies SHA256SUMS.sig with PublicKey (compiled into the
// already-installed, trusted binary), refuses anything that is not newer
// than itself, and only then checks the downloaded binary's hash against
// the manifest. Whoever can upload release assets but does not hold the
// signing key therefore cannot push a binary to existing installs.
package release

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// PublicKey verifies release manifests. Its private half lives only in the
// repository's RELEASE_SIGNING_KEY Actions secret (base64 of the 32-byte
// ed25519 seed — see cmd/releasesign keygen). Rotating the key means
// shipping one release signed by the old key that carries the new one.
const PublicKey = "k1pto5sjuH6enZg6r8/kQfm/7mboNyuxLnr184Y5U70="

// PreviousPublicKey is the key PublicKey replaced, which every binary shipped
// before the rotation still verifies with. It is never trusted by Verify:
// only releasesign's pre-publish check accepts it, so the one rotation
// release — signed with this key, carrying the new one — can be published.
// Empty it once that release is out.
const PreviousPublicKey = ""

const (
	ManifestName  = "SHA256SUMS"
	SignatureName = "SHA256SUMS.sig"
)

// Manifest is a parsed SHA256SUMS.
type Manifest struct {
	Version string
	SHA256  map[string]string // asset name -> lowercase hex digest
}

// Format renders m in its canonical signed form, assets in the given order.
func Format(version string, assets []string, digests map[string]string) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "version %s\n", version)
	for _, a := range assets {
		fmt.Fprintf(&b, "%s  %s\n", digests[a], a)
	}
	return b.Bytes()
}

// Sign returns the detached signature for a manifest.
func Sign(seed, manifest []byte) ([]byte, error) {
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("signing key must be a %d-byte ed25519 seed, got %d bytes", ed25519.SeedSize, len(seed))
	}
	return ed25519.Sign(ed25519.NewKeyFromSeed(seed), manifest), nil
}

// Verify checks sig over manifest against PublicKey, then parses it. Nothing
// in an unverified manifest is ever looked at.
func Verify(manifest, sig []byte) (*Manifest, error) {
	return VerifyWith(PublicKey, manifest, sig)
}

// VerifyWith is Verify against an explicit base64 public key — for tests
// that sign with a throwaway key; production code uses Verify.
func VerifyWith(publicKeyB64 string, manifest, sig []byte) (*Manifest, error) {
	pub, err := base64.StdEncoding.DecodeString(publicKeyB64)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid embedded release public key")
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), manifest, sig) {
		return nil, fmt.Errorf("release signature verification failed — refusing to install")
	}
	return parse(manifest)
}

func parse(manifest []byte) (*Manifest, error) {
	m := &Manifest{SHA256: map[string]string{}}
	sc := bufio.NewScanner(bytes.NewReader(manifest))
	for sc.Scan() {
		line := sc.Text()
		if v, ok := strings.CutPrefix(line, "version "); ok && m.Version == "" {
			m.Version = v
			continue
		}
		digest, asset, ok := strings.Cut(line, "  ")
		if !ok || len(digest) != sha256.Size*2 || asset == "" {
			return nil, fmt.Errorf("malformed %s line %q", ManifestName, line)
		}
		if _, err := hex.DecodeString(digest); err != nil {
			return nil, fmt.Errorf("malformed digest in %s: %q", ManifestName, line)
		}
		m.SHA256[asset] = strings.ToLower(digest)
	}
	if m.Version == "" {
		return nil, fmt.Errorf("%s has no version line", ManifestName)
	}
	return m, nil
}

// CheckAsset confirms data is exactly the named asset the manifest lists.
func (m *Manifest) CheckAsset(name string, data []byte) error {
	want, ok := m.SHA256[name]
	if !ok {
		return fmt.Errorf("release manifest lists no asset %q", name)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != want {
		return fmt.Errorf("%s does not match the signed release manifest (SHA-256 mismatch) — refusing to install", name)
	}
	return nil
}

// Semver is a parsed vMAJOR.MINOR.PATCH release tag.
type Semver struct{ Major, Minor, Patch int }

// ParseSemver accepts "vX.Y.Z", optionally followed by a `git describe`
// suffix ("-3-gabc123", "-dirty"), which is ignored: such a build sits
// between releases and orders as its base tag.
func ParseSemver(s string) (Semver, bool) {
	s, ok := strings.CutPrefix(s, "v")
	if !ok {
		return Semver{}, false
	}
	if i := strings.IndexByte(s, '-'); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return Semver{}, false
	}
	var n [3]int
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 {
			return Semver{}, false
		}
		n[i] = v
	}
	return Semver{n[0], n[1], n[2]}, true
}

// Compare returns -1, 0 or +1 as a is older than, equal to or newer than b.
func (a Semver) Compare(b Semver) int {
	for _, d := range [][2]int{{a.Major, b.Major}, {a.Minor, b.Minor}, {a.Patch, b.Patch}} {
		if d[0] != d[1] {
			if d[0] < d[1] {
				return -1
			}
			return 1
		}
	}
	return 0
}
