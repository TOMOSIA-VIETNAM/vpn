#!/usr/bin/env bash
# ==============================================================================
# TOMOSIA VPN Complete Uninstaller Script
# Removes CLI engine, Menu Bar App, runtime state, configuration and the
# PSK/password entries in Keychain.
#
# CLI-side cleanup is delegated to `vpn uninstall -y` — the one place that
# knows every file it owns, disconnects gracefully (restoring routes/DNS
# and ending the PPP session) and deletes the Keychain secrets. Run this as
# the user who installed vpn, not with sudo: the Keychain and ~/.config/vpn
# belong to that user; the CLI raises privilege itself where needed.
# ==============================================================================

set -euo pipefail

CLI="/usr/local/bin/vpn"
APP_DIR="/Applications/TOMOSIA VPN.app"
# Where earlier releases installed the app.
LEGACY_APP_DIRS=("/Applications/TMS VPN.app" "/Applications/TOMOSIA-VPN.app")

if [ "$(id -u)" = 0 ]; then
  echo "Run this as your normal user (it will ask for sudo only if needed)." >&2
  exit 1
fi

echo "Uninstalling TOMOSIA VPN"

echo "==> [1/3] Quitting menu bar app"
killall "TOMOSIA-VPN" "TMS VPN" 2>/dev/null || true

# The app goes first, so `vpn uninstall` below no longer sees it and does
# not print its "the menu bar app is still installed" hint.
echo "==> [2/3] Removing menu bar app"
rm -rf "$APP_DIR" "${LEGACY_APP_DIRS[@]}" 2>/dev/null || sudo rm -rf "$APP_DIR" "${LEGACY_APP_DIRS[@]}"
sudo rm -f "/usr/local/bin/tms-vpn-bar"

echo "==> [3/3] Removing CLI, state, log, profiles and Keychain secrets"
if [ -x "$CLI" ] && "$CLI" uninstall -y; then
  :
else
  # The CLI is missing or unusable: remove its files by hand. Keychain
  # entries (service names vpn.psk.* / vpn.pwd.*) cannot be enumerated
  # reliably from here — delete them in Keychain Access if any remain.
  echo "warning: vpn uninstall unavailable — removing files directly." >&2
  # Tear the tunnel down first so routes/DNS are restored while the binary
  # still exists. Run as root: a real uid of 0 skips the owner check that
  # may be exactly why `vpn uninstall` failed above.
  if [ -x "$CLI" ]; then
    sudo "$CLI" disconnect || true
    sudo "$CLI" repair || true
  fi
  sudo rm -f "$CLI" /etc/vpn-owner-uid /var/log/vpn.log /var/log/vpn.log.1
  sudo rm -rf /var/run/vpn
  rm -rf "$HOME/.config/vpn"
fi

echo "TOMOSIA VPN uninstalled."
