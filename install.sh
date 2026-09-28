#!/usr/bin/env bash
# ==============================================================================
# One-liner install: detects this Mac's architecture, installs CLI + Menu Bar UI
#
#   VPN_ARCH=arm64|amd64   skip architecture auto-detection
#
# Every download is checked against the release's SHA256SUMS. Trust note:
# this script is itself served by the same GitHub repository, so that guards
# against a corrupted download, not a compromised repository. Later
# `vpn update`s are stronger: they verify the release's ed25519 signature
# with a key compiled into the installed binary.
# ==============================================================================

set -euo pipefail

BASE_URL="https://github.com/tms-ninhle/vpn/releases/latest/download"
INSTALL_PATH="/usr/local/bin/vpn"
OWNER_FILE="/etc/vpn-owner-uid"
APP_DIR="/Applications/TMS VPN.app"
APP_ASSET="TMS-VPN.app.zip"

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

echo "=================================================="
echo "🛡️  INSTALLING TMS VPN (CLI & MENU BAR UI, $ARCH)"
echo "=================================================="

curl -fsSL -o "$DOWNLOAD/SHA256SUMS" "$BASE_URL/SHA256SUMS"

# --- Step 1: Install `vpn` CLI Backend (setuid-root) ---
echo "📦 [1/2] Downloading VPN CLI engine ($CLI_ASSET)..."
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
sudo chmod 644 /var/run/vpn/state.json 2>/dev/null || true
echo "  -> CLI Installed: $INSTALL_PATH ($("$INSTALL_PATH" version))"

# --- Step 2: Install Menu Bar UI (prebuilt universal app) ---
# Runs as this user, not setuid, so verifying in the user's own download
# directory is enough here.
echo "🎨 [2/2] Installing TMS VPN Menu Bar UI..."
if curl -fsSL -o "$DOWNLOAD/$APP_ASSET" "$BASE_URL/$APP_ASSET"; then
  verify_sha256 "$DOWNLOAD/$APP_ASSET" "$APP_ASSET"
  rm -rf "$APP_DIR"
  # ditto (not unzip) restores the bundle exactly as the release workflow
  # packed it (`ditto -c` — see .github/workflows/release.yml), keeping
  # its executable bits and resource forks intact.
  ditto -x -k "$DOWNLOAD/$APP_ASSET" "$(dirname "$APP_DIR")"
  echo "  -> UI Installed to $APP_DIR"
else
  echo "⚠️  Could not download Menu Bar UI from $BASE_URL/$APP_ASSET — is a release published yet?"
fi

echo "=================================================="
echo "🎉 SUCCESS: TMS VPN Engine & Menu Bar UI Installed!"
echo "👉 Menu Bar App: $APP_DIR"
echo "👉 CLI Engine: $INSTALL_PATH"
echo "=================================================="

open -a "$APP_DIR" 2>/dev/null || true
