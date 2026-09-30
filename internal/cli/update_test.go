package cli

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"vpn/internal/release"
)

// fakeRelease serves a release signed by a throwaway key and points
// verifyManifest at that key for the duration of the test.
func fakeRelease(t *testing.T, version string, bin []byte, mutate func(files map[string][]byte)) string {
	t.Helper()
	seed := bytes.Repeat([]byte{3}, ed25519.SeedSize)
	pub := base64.StdEncoding.EncodeToString(ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey))
	sum := sha256.Sum256(bin)
	manifest := release.Format(version, []string{"vpn-darwin-arm64"}, map[string]string{"vpn-darwin-arm64": hex.EncodeToString(sum[:])})
	sig, err := release.Sign(seed, manifest)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		release.ManifestName:  manifest,
		release.SignatureName: sig,
		"vpn-darwin-arm64":    bin,
	}
	if mutate != nil {
		mutate(files)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, ok := files[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(data)
	}))
	t.Cleanup(srv.Close)

	orig := verifyManifest
	verifyManifest = func(m, s []byte) (*release.Manifest, error) { return release.VerifyWith(pub, m, s) }
	t.Cleanup(func() { verifyManifest = orig })
	return srv.URL + "/"
}

func withVersion(t *testing.T, v string) {
	t.Helper()
	orig := Version
	Version = v
	t.Cleanup(func() { Version = orig })
}

func TestDownloadVerifiedAcceptsGenuineNewerRelease(t *testing.T) {
	withVersion(t, "v1.0.0")
	bin := []byte("genuine binary")
	base := fakeRelease(t, "v1.1.0", bin, nil)
	got, version, err := downloadVerified(base, "vpn-darwin-arm64", false)
	if err != nil {
		t.Fatal(err)
	}
	if version != "v1.1.0" || !bytes.Equal(got, bin) {
		t.Fatalf("got version %q, %d bytes", version, len(got))
	}
}

func TestDownloadVerifiedRejects(t *testing.T) {
	withVersion(t, "v1.0.0")
	cases := map[string]struct {
		version string
		mutate  func(map[string][]byte)
	}{
		"swapped binary":        {"v1.1.0", func(f map[string][]byte) { f["vpn-darwin-arm64"] = []byte("malware") }},
		"edited manifest":       {"v1.1.0", func(f map[string][]byte) { f[release.ManifestName] = append(f[release.ManifestName], '\n') }},
		"missing signature":     {"v1.1.0", func(f map[string][]byte) { delete(f, release.SignatureName) }},
		"replayed older signed": {"v0.9.0", nil},
		"same version":          {"v1.0.0", nil},
	}
	for name, c := range cases {
		base := fakeRelease(t, c.version, []byte("genuine binary"), c.mutate)
		if _, _, err := downloadVerified(base, "vpn-darwin-arm64", false); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestDownloadVerifiedForceAllowsReinstall(t *testing.T) {
	withVersion(t, "v1.0.0")
	base := fakeRelease(t, "v1.0.0", []byte("genuine binary"), nil)
	if _, _, err := downloadVerified(base, "vpn-darwin-arm64", true); err != nil {
		t.Fatalf("--force reinstall rejected: %v", err)
	}
}

func TestDownloadVerifiedReportsUpToDate(t *testing.T) {
	withVersion(t, "v1.0.0")
	base := fakeRelease(t, "v1.0.0", []byte("genuine binary"), nil)
	if _, _, err := downloadVerified(base, "vpn-darwin-arm64", false); !errors.Is(err, errUpToDate) {
		t.Fatalf("same version: got %v, want errUpToDate (exit 0, not a failure)", err)
	}
}

func TestDownloadAppSavesOnlyVerifiedZip(t *testing.T) {
	withVersion(t, "v1.0.0")
	zip := []byte("app zip")
	sum := sha256.Sum256(zip)

	serve := func(mutate func(map[string][]byte)) string {
		return fakeRelease(t, "v1.1.0", []byte("bin"), func(f map[string][]byte) {
			f[appAsset] = zip
			// Re-sign a manifest that lists the zip.
			seed := bytes.Repeat([]byte{3}, ed25519.SeedSize)
			m := release.Format("v1.1.0", []string{appAsset}, map[string]string{appAsset: hex.EncodeToString(sum[:])})
			f[release.ManifestName] = m
			f[release.SignatureName], _ = release.Sign(seed, m)
			if mutate != nil {
				mutate(f)
			}
		})
	}

	path := t.TempDir() + "/app.zip"
	if err := downloadApp(serve(nil), path); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, zip) {
		t.Fatalf("saved %q", got)
	}

	bad := t.TempDir() + "/app.zip"
	if err := downloadApp(serve(func(f map[string][]byte) { f[appAsset] = []byte("tampered") }), bad); err == nil {
		t.Fatal("accepted a zip that does not match the signed manifest")
	}
	if _, err := os.Stat(bad); err == nil {
		t.Fatal("wrote an unverified zip to disk")
	}
}
