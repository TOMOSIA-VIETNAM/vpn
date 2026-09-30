#!/usr/bin/env bash
set -euo pipefail

if [ -n "${SUDO_UID:-}" ] && [ "$SUDO_UID" != "0" ]; then
    OWNER_UID="$SUDO_UID"
elif [ "$(id -u)" != "0" ]; then
    OWNER_UID="$(id -u)"
else
    OWNER_UID="$(stat -f '%u' /dev/console 2>/dev/null || echo 501)"
fi

echo "==> [1/3] Building menu bar app and CLI (arm64 + x86_64)"
sudo rm -rf ./build
# Version shown in the app footer and `vpn version`: build.sh reads VERSION, else the latest git tag.
VERSION="${VERSION:-$(git describe --tags --abbrev=0 2>/dev/null || true)}"
VERSION="${VERSION#v}"
[[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || VERSION="0.0.0"
export VERSION
# build.sh also compiles the universal CLI (./build/vpn) that it bundles into the app;
# installing that same binary keeps /usr/local/bin/vpn identical to the bundled copy.
./build.sh
if [[ ! -x ./build/vpn ]]; then
    echo "error: build.sh did not produce ./build/vpn (is Go installed?)" >&2
    exit 1
fi

echo "==> [2/3] Installing CLI to /usr/local/bin/vpn"
sudo install -o root -g wheel -m 4755 ./build/vpn /usr/local/bin/vpn
echo "$OWNER_UID" | sudo tee /etc/vpn-owner-uid >/dev/null
sudo chown root:wheel /etc/vpn-owner-uid
sudo chmod 600 /etc/vpn-owner-uid
sudo chmod 755 /var/run/vpn 2>/dev/null || true

echo "==> [3/3] Installing app to /Applications/TOMOSIA VPN.app"
# Also drop the app installed under its earlier names (TMS VPN.app, TOMOSIA-VPN.app).
killall "TOMOSIA-VPN" "TMS VPN" 2>/dev/null || true
sudo rm -rf "/Applications/TMS VPN.app" "/Applications/TOMOSIA-VPN.app" "/Applications/TOMOSIA VPN.app"
sudo cp -R "./build/TOMOSIA VPN.app" /Applications/
sudo chown -R "$OWNER_UID:staff" "/Applications/TOMOSIA VPN.app"

echo "Installed TOMOSIA VPN from local build."
echo "  CLI: /usr/local/bin/vpn"
echo "  App: /Applications/TOMOSIA VPN.app"
