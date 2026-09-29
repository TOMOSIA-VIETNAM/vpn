#!/usr/bin/env bash
# ==============================================================================
# TMS VPN: Universal Binary Build Script (ARM64 + Intel x86_64)
# ==============================================================================

set -euo pipefail

OUTPUT_NAME="tms-vpn-bar"
APP_NAME="TMS VPN.app"
BUILD_DIR="./build"

echo "==> [1/3] Preparing build directory"
rm -rf "$BUILD_DIR"
mkdir -p "$BUILD_DIR"

if [[ "$(uname)" != "Darwin" ]]; then
    echo "error: the menu bar app builds only on macOS (needs swiftc)." >&2
    exit 1
fi

# CI builds main.swift on the macos-26 runner image (Xcode 26, Swift 6).
# Swift 5.9 is known to reject it (strict-concurrency errors); versions in
# between are untested, so warn rather than refuse.
SWIFT_MAJOR="$(swiftc --version 2>/dev/null | sed -nE 's/.*Swift version ([0-9]+)\..*/\1/p' | head -1)"
if [[ -z "$SWIFT_MAJOR" || "$SWIFT_MAJOR" -lt 6 ]]; then
    echo "warning: swiftc $(swiftc --version 2>/dev/null | sed -nE 's/.*Swift version ([0-9.]+).*/\1/p' | head -1) detected — CI builds this app with Xcode 26 (Swift 6); older toolchains may fail to compile it." >&2
fi

echo "==> [2/3] Compiling universal binary (arm64 + x86_64)"

swiftc -O -target arm64-apple-macos12.0 -framework Cocoa -framework SwiftUI main.swift -o "$BUILD_DIR/${OUTPUT_NAME}-arm64"
swiftc -O -target x86_64-apple-macos12.0 -framework Cocoa -framework SwiftUI main.swift -o "$BUILD_DIR/${OUTPUT_NAME}-x86_64"

lipo -create "$BUILD_DIR/${OUTPUT_NAME}-arm64" "$BUILD_DIR/${OUTPUT_NAME}-x86_64" -output "$BUILD_DIR/$OUTPUT_NAME"

echo "==> [3/3] Packaging $APP_NAME"
mkdir -p "$BUILD_DIR/$APP_NAME/Contents/MacOS"
mkdir -p "$BUILD_DIR/$APP_NAME/Contents/Resources"

cp "$BUILD_DIR/$OUTPUT_NAME" "$BUILD_DIR/$APP_NAME/Contents/MacOS/TMS VPN"
chmod +x "$BUILD_DIR/$APP_NAME/Contents/MacOS/TMS VPN"
cp "assets/AppIcon.icns" "$BUILD_DIR/$APP_NAME/Contents/Resources/AppIcon.icns"
# Logo and status item artwork come from assets/render-icons.swift.
cp "assets/logo.png" "$BUILD_DIR/$APP_NAME/Contents/Resources/Logo.png"
cp assets/menubar-*.png "$BUILD_DIR/$APP_NAME/Contents/Resources/"

cat <<EOF > "$BUILD_DIR/$APP_NAME/Contents/Info.plist"
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleExecutable</key>
    <string>TMS VPN</string>
    <key>CFBundleIconFile</key>
    <string>AppIcon</string>
    <key>CFBundleIdentifier</key>
    <string>com.tms.vpn.menubar</string>
    <key>CFBundleName</key>
    <string>TMS VPN</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleShortVersionString</key>
    <string>1.0.0</string>
    <key>LSUIElement</key>
    <true/>
    <key>NSHighResolutionCapable</key>
    <true/>
</dict>
</plist>
EOF

echo "Built binary: $BUILD_DIR/$OUTPUT_NAME"
echo "Built app:    $BUILD_DIR/$APP_NAME"
