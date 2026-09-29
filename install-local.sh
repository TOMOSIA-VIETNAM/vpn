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
./build.sh
GOOS=darwin GOARCH=arm64 go build -o ./build/vpn-arm64 ./cmd/vpn
GOOS=darwin GOARCH=amd64 go build -o ./build/vpn-amd64 ./cmd/vpn
lipo -create ./build/vpn-arm64 ./build/vpn-amd64 -output ./build/vpn

echo "==> [2/3] Installing CLI to /usr/local/bin/vpn"
sudo install -o root -g wheel -m 4755 ./build/vpn /usr/local/bin/vpn
echo "$OWNER_UID" | sudo tee /etc/vpn-owner-uid >/dev/null
sudo chown root:wheel /etc/vpn-owner-uid
sudo chmod 600 /etc/vpn-owner-uid
sudo chmod 755 /var/run/vpn 2>/dev/null || true
sudo chmod 644 /var/run/vpn/state.json 2>/dev/null || true

echo "==> [3/3] Installing app to /Applications/TMS VPN.app"
sudo rm -rf "/Applications/TMS VPN.app"
sudo cp -R "./build/TMS VPN.app" /Applications/
sudo chown -R "$OWNER_UID:staff" "/Applications/TMS VPN.app"

echo "Installed TMS VPN from local build."
echo "  CLI: /usr/local/bin/vpn"
echo "  App: /Applications/TMS VPN.app"
