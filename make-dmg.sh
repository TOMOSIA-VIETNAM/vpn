#!/usr/bin/env bash
# ==============================================================================
# Packs "TMS VPN.app" into a drag-to-Applications disk image.
#
#   ./make-dmg.sh                 build the app first (build.sh), then the .dmg
#   VERSION=1.2.3 ./make-dmg.sh
#   OUT=path/to/TMS-VPN.dmg ./make-dmg.sh
#
# Output: build/TMS-VPN.dmg (the fixed name lets install links use
# releases/latest/download/TMS-VPN.dmg). The window layout (app icon → Applications
# shortcut) is set through Finder when a desktop session is available, and skipped
# quietly otherwise (CI), leaving a plain but working image.
# ==============================================================================
set -euo pipefail
cd "$(dirname "$0")"

BUILD_DIR="${BUILD_DIR:-./build}"
APP_NAME="TMS VPN.app"
VOL_NAME="TOMOSIA VPN"
OUT="${OUT:-$BUILD_DIR/TMS-VPN.dmg}"

if [[ ! -d "$BUILD_DIR/$APP_NAME" ]]; then
    BUILD_DIR="$BUILD_DIR" bash ./build.sh
fi

STAGE="$(mktemp -d -t tmsvpn-dmg)"
# A private directory holds the scratch image: `mktemp -u` only invents a name, which
# someone else could claim (or symlink) before hdiutil writes to it.
WORK="$(mktemp -d -t tmsvpn-work)"
RW="$WORK/rw.dmg"
MOUNT=""
cleanup() {
    [[ -n "$MOUNT" ]] && hdiutil detach "$MOUNT" -quiet -force 2>/dev/null || true
    rm -rf "$STAGE" "$WORK"
}
trap cleanup EXIT

echo "💿 Chuẩn bị nội dung đĩa..."
ditto "$BUILD_DIR/$APP_NAME" "$STAGE/$APP_NAME"
ln -s /Applications "$STAGE/Applications"
cp assets/AppIcon.icns "$STAGE/.VolumeIcon.icns"

SIZE_MB=$(( $(du -sm "$STAGE" | awk '{print $1}') + 20 ))
hdiutil create -quiet -srcfolder "$STAGE" -volname "$VOL_NAME" -fs HFS+ \
    -format UDRW -size "${SIZE_MB}m" "$RW"

MOUNT="$(hdiutil attach "$RW" -readwrite -noverify -noautoopen | awk -F'\t' '/\/Volumes\//{print $NF}' | tail -1)"
SetFile -a C "$MOUNT" 2>/dev/null || true # show the custom volume icon

if [[ -z "${CI:-}" ]]; then
    echo "🎨 Sắp xếp cửa sổ cài đặt (bỏ qua nếu Finder không phản hồi)..."
    ( osascript <<OSA >/dev/null 2>&1
tell application "Finder"
  tell disk "$VOL_NAME"
    open
    set current view of container window to icon view
    set toolbar visible of container window to false
    set statusbar visible of container window to false
    set the bounds of container window to {200, 140, 740, 500}
    set opts to the icon view options of container window
    set arrangement of opts to not arranged
    set icon size of opts to 112
    set position of item "$APP_NAME" of container window to {140, 180}
    set position of item "Applications" of container window to {400, 180}
    close
    open
    update without registering applications
    delay 1
    close
  end tell
end tell
OSA
    ) & pid=$!
    for _ in $(seq 1 30); do kill -0 "$pid" 2>/dev/null || break; sleep 1; done
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
fi

sync
hdiutil detach "$MOUNT" -quiet
MOUNT=""

rm -f "$OUT"
hdiutil convert "$RW" -quiet -format UDZO -imagekey zlib-level=9 -o "$OUT"
echo "✅ Đã tạo: $OUT ($(du -h "$OUT" | awk '{print $1}'))"
