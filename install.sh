#!/usr/bin/env bash
# ==============================================================================
# One-liner install: detects this Mac's architecture, installs CLI + Menu Bar UI
#
#   VPN_ARCH=arm64|amd64   skip architecture auto-detection
#
# Every download is checked against the release's SHA256SUMS. When
# INSTALL_PUBKEY below is set, SHA256SUMS itself must also carry a valid ECDSA
# P-256 signature (SHA256SUMS.p256.sig), checked with the system openssl before
# anything is installed as root: a party who can upload release assets but does
# not hold the signing key then cannot get a binary onto a fresh Mac. While it is
# empty, this script only guards against a corrupted download, not a compromised
# repository (see CONTRIBUTING.md, "Release signing", to switch it on). Later
# `vpn update`s verify the release's ed25519 signature with a key compiled into
# the installed binary either way.
# ==============================================================================

set -euo pipefail

BASE_URL="https://github.com/TOMOSIA-VIETNAM/vpn/releases/latest/download"
INSTALL_PATH="/usr/local/bin/vpn"
OWNER_FILE="/etc/vpn-owner-uid"
APP_DIR="/Applications/TOMOSIA VPN.app"
APP_ASSET="TOMOSIA-VPN.app.zip"
# Where earlier releases installed the app (TMS VPN.app, then TOMOSIA-VPN.app); removed on
# install so an upgraded Mac does not keep two copies in the menu bar.
LEGACY_APP_DIRS=("/Applications/TMS VPN.app" "/Applications/TOMOSIA-VPN.app")

# PEM public key for SHA256SUMS.p256.sig (releasesign keygen-p256); its private half is the
# INSTALL_SIGNING_KEY Actions secret. Empty = not enforced.
INSTALL_PUBKEY='-----BEGIN PUBLIC KEY-----
MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE5QbGcKLH6tVbB/ZBTEsPHcfwo4uy
3mRWPMIdEMDs8/ZpUSK9VYZFyBKCynI+SL+VzB88RF7h3VQ2z0tg2WYUOg==
-----END PUBLIC KEY-----'

ARCH="${VPN_ARCH:-}"
if [ -z "$ARCH" ]; then
  case "$(uname -m)" in
    arm64) ARCH=arm64 ;;
    x86_64) ARCH=amd64 ;;
    *) echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;;
  esac
fi
case "$ARCH" in
  arm64|amd64) ;;
  *) echo "VPN_ARCH must be arm64 or amd64, got: $ARCH" >&2; exit 1 ;;
esac
CLI_ASSET="vpn-darwin-$ARCH"

DOWNLOAD="$(mktemp -d -t vpn-download)"
STAGE=""
cleanup() {
  rm -rf "$DOWNLOAD"
  if [ -n "$STAGE" ]; then sudo rm -rf "$STAGE"; fi
}
trap cleanup EXIT

# expected_sha256 ASSET: the digest SHA256SUMS lists for ASSET.
expected_sha256() {
  local want
  want="$(awk -v a="$1" '$2 == a { print $1 }' "$DOWNLOAD/SHA256SUMS")"
  if [ -z "$want" ]; then
    echo "SHA256SUMS lists no $1 — not installing." >&2
    exit 1
  fi
  echo "$want"
}

# verify_sha256 FILE ASSET: abort unless FILE is exactly the listed ASSET.
verify_sha256() {
  local want got
  want="$(expected_sha256 "$2")"
  got="$(shasum -a 256 "$1" | awk '{ print $1 }')"
  if [ "$got" != "$want" ]; then
    echo "Checksum mismatch for $2 (expected $want, got $got) — not installing." >&2
    exit 1
  fi
}

echo "Installing TOMOSIA VPN ($ARCH)"

curl -fsSL -o "$DOWNLOAD/SHA256SUMS" "$BASE_URL/SHA256SUMS"

if [ -n "$INSTALL_PUBKEY" ]; then
  # A missing signature is a failure, never a downgrade to the unsigned path.
  curl -fsSL -o "$DOWNLOAD/SHA256SUMS.p256.sig" "$BASE_URL/SHA256SUMS.p256.sig" \
    || { echo "The release has no SHA256SUMS.p256.sig — not installing." >&2; exit 1; }
  printf '%s\n' "$INSTALL_PUBKEY" > "$DOWNLOAD/install-pubkey.pem"
  if ! /usr/bin/openssl dgst -sha256 -verify "$DOWNLOAD/install-pubkey.pem" \
       -signature "$DOWNLOAD/SHA256SUMS.p256.sig" "$DOWNLOAD/SHA256SUMS" >/dev/null 2>&1; then
    echo "The release signature does not verify — not installing." >&2
    exit 1
  fi
  echo "    Release signature verified."
else
  echo "    note: this installer does not check a release signature yet; checksums only." >&2
fi

# --- Step 1: Install `vpn` CLI Backend (setuid-root) ---
echo "==> [1/2] Downloading CLI ($CLI_ASSET)"
curl -fsSL -o "$DOWNLOAD/$CLI_ASSET" "$BASE_URL/$CLI_ASSET"
# Copy into a root-owned directory *before* verifying: the download
# directory is writable by this user, so anything checked there could be
# swapped before `sudo` installs it setuid-root. The staged copy cannot be.
STAGE="$(sudo mktemp -d /tmp/vpn-install.XXXXXX)"
sudo chmod 755 "$STAGE"
sudo install -o root -g wheel -m 0755 "$DOWNLOAD/$CLI_ASSET" "$STAGE/vpn"
verify_sha256 "$STAGE/vpn" "$CLI_ASSET"
"$STAGE/vpn" version >/dev/null # sanity check it actually runs before installing it

OWNER_UID="$(id -u)"
sudo install -o root -g wheel -m 4755 "$STAGE/vpn" "$INSTALL_PATH"
echo "$OWNER_UID" | sudo tee "$OWNER_FILE" >/dev/null
sudo chown root:wheel "$OWNER_FILE"
sudo chmod 600 "$OWNER_FILE"
sudo chmod 755 /var/run/vpn 2>/dev/null || true
# state.json is rewritten 0600, owned by the installing user, on the next connect.
echo "    Installed CLI: $INSTALL_PATH ($("$INSTALL_PATH" version))"

# --- Step 2: Install Menu Bar UI (prebuilt universal app) ---
# Runs as this user, not setuid, so verifying in the user's own download
# directory is enough here.
echo "==> [2/2] Installing menu bar app"
if curl -fsSL -o "$DOWNLOAD/$APP_ASSET" "$BASE_URL/$APP_ASSET"; then
  verify_sha256 "$DOWNLOAD/$APP_ASSET" "$APP_ASSET"
  for legacy in "${LEGACY_APP_DIRS[@]}"; do
    if [ -d "$legacy" ]; then
      killall "TMS VPN" 2>/dev/null || true
      rm -rf "$legacy" 2>/dev/null || sudo rm -rf "$legacy"
    fi
  done
  # Quit a running copy first, or it keeps running from the deleted bundle and the
  # new launch below adds a second status item.
  killall "TOMOSIA-VPN" 2>/dev/null || true
  rm -rf "$APP_DIR"
  # ditto (not unzip) restores the bundle exactly as the release workflow
  # packed it (`ditto -c` — see .github/workflows/release.yml), keeping
  # its executable bits and resource forks intact.
  ditto -x -k "$DOWNLOAD/$APP_ASSET" "$(dirname "$APP_DIR")"
  echo "    Installed app: $APP_DIR"
else
  echo "warning: could not download the menu bar app from $BASE_URL/$APP_ASSET — is a release published yet?" >&2
fi

echo "TOMOSIA VPN installed."
echo "  CLI: $INSTALL_PATH"
echo "  App: $APP_DIR"

open -a "$APP_DIR" 2>/dev/null || true
