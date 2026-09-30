// Command releasesign produces the signed release manifest `vpn update`
// verifies (see internal/release).
//
//	releasesign keygen
//	    prints a fresh base64 seed (the RELEASE_SIGNING_KEY secret) on stdout
//	    and its public key (for release.PublicKey) on stderr.
//	releasesign keygen-p256
//	    prints a fresh install signing key (the INSTALL_SIGNING_KEY secret) on
//	    stdout and its PEM public key (for install.sh) on stderr.
//	releasesign sign -version vX.Y.Z -out DIR ASSET...
//	    reads the base64 seed from $RELEASE_SIGNING_KEY and writes
//	    DIR/SHA256SUMS and DIR/SHA256SUMS.sig covering every ASSET; when
//	    $INSTALL_SIGNING_KEY is set, also DIR/SHA256SUMS.p256.sig for install.sh.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"vpn/internal/release"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "releasesign:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: releasesign <keygen|sign> ...")
	}
	switch args[0] {
	case "keygen":
		return keygen()
	case "keygen-p256":
		priv, pub, err := release.GenerateP256()
		if err != nil {
			return err
		}
		fmt.Println(priv)
		fmt.Fprint(os.Stderr, "public key (paste into install.sh INSTALL_PUBKEY):\n"+pub)
		return nil
	case "sign":
		return sign(args[1:])
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func keygen() error {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	fmt.Println(base64.StdEncoding.EncodeToString(priv.Seed()))
	fmt.Fprintln(os.Stderr, "public key:", base64.StdEncoding.EncodeToString(pub))
	return nil
}

func sign(args []string) error {
	fs := flag.NewFlagSet("sign", flag.ContinueOnError)
	version := fs.String("version", "", "release tag, vX.Y.Z")
	outDir := fs.String("out", ".", "directory to write the manifest and signature into")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if _, ok := release.ParseSemver(*version); !ok {
		return fmt.Errorf("-version must be a vX.Y.Z tag, got %q", *version)
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("no assets given")
	}
	seed, err := base64.StdEncoding.DecodeString(os.Getenv("RELEASE_SIGNING_KEY"))
	if err != nil {
		return fmt.Errorf("decode $RELEASE_SIGNING_KEY: %w", err)
	}

	names := make([]string, 0, fs.NArg())
	digests := map[string]string{}
	for _, path := range fs.Args() {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		name := filepath.Base(path)
		names = append(names, name)
		digests[name] = hex.EncodeToString(sum[:])
	}
	manifest := release.Format(*version, names, digests)
	sig, err := release.Sign(seed, manifest)
	if err != nil {
		return err
	}
	// Refuse to publish anything the shipped binaries would then reject
	// (e.g. the secret holding some other key). Binaries verify with
	// release.PublicKey; during a key rotation, those already installed
	// still verify with release.PreviousPublicKey, and the rotation release
	// is signed with that one.
	if err := checkSigningKey(manifest, sig); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(*outDir, release.ManifestName), manifest, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(*outDir, release.SignatureName), sig, 0o644); err != nil {
		return err
	}
	if key := os.Getenv("INSTALL_SIGNING_KEY"); key != "" {
		p256, err := release.SignP256(key, manifest)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(*outDir, release.InstallSignatureName), p256, 0o644)
	}
	return nil
}

// checkSigningKey accepts a signature that verifies with release.PublicKey or,
// while one is set, release.PreviousPublicKey (see its comment).
func checkSigningKey(manifest, sig []byte) error {
	return checkSigningKeyWith(release.PublicKey, release.PreviousPublicKey, manifest, sig)
}

func checkSigningKeyWith(current, previous string, manifest, sig []byte) error {
	if _, err := release.VerifyWith(current, manifest, sig); err == nil {
		return nil
	}
	if previous != "" {
		if _, err := release.VerifyWith(previous, manifest, sig); err == nil {
			fmt.Fprintln(os.Stderr, "releasesign: signed with release.PreviousPublicKey — the key-rotation release")
			return nil
		}
	}
	return fmt.Errorf("signing key matches neither release.PublicKey nor release.PreviousPublicKey")
}
