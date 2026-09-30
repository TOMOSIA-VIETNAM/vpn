#!/usr/bin/env bash
# ==============================================================================
# TOMOSIA VPN: Universal Binary Build Script (ARM64 + Intel x86_64)
# ==============================================================================

set -euo pipefail

OUTPUT_NAME="tms-vpn-bar"
# Only the bundle folder carries the product name with a space, so Finder, the disk image
# and macOS dialogs read "TOMOSIA VPN". The executable and the release assets stay
# space-free: they appear in install URLs and killall.
APP_NAME="TOMOSIA VPN.app"
EXECUTABLE="TOMOSIA-VPN"
DISPLAY_NAME="TOMOSIA VPN"
BUILD_DIR="${BUILD_DIR:-./build}"

# Version shown in the app footer (CFBundleShortVersionString). The release
# workflow passes the tag without its "v" (VERSION=1.2.3); local builds fall
# back to the latest tag reachable from HEAD, or 0.0.0 outside a tagged clone.
VERSION="${VERSION:-$(git describe --tags --abbrev=0 2>/dev/null || true)}"
VERSION="${VERSION#v}"
if ! [[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    VERSION="0.0.0"
fi

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

# The app drives the `vpn` CLI, which must sit setuid-root in /usr/local/bin. A drag-to-
# Applications install has no installer step, so the CLI travels inside the app and the
# app installs it (with an admin prompt) on first launch — see CLIInstaller in main.swift.
# Skipped when Go is missing, so a Swift-only build still works.
if command -v go >/dev/null 2>&1; then
    CLI_VERSION="v${VERSION}"
    echo "==> Compiling CLI (arm64 + amd64) to bundle in the app"
    for arch in arm64 amd64; do
        CGO_ENABLED=0 GOOS=darwin GOARCH=$arch go build -trimpath \
            -ldflags "-s -w -X main.version=${CLI_VERSION}" \
            -o "$BUILD_DIR/vpn-$arch" ./cmd/vpn
    done
    lipo -create "$BUILD_DIR/vpn-arm64" "$BUILD_DIR/vpn-amd64" -output "$BUILD_DIR/vpn"
    BUNDLE_CLI=1
else
    echo "warning: Go not found — the app will not bundle the CLI (install it with install.sh)." >&2
    BUNDLE_CLI=0
fi

echo "==> [3/3] Packaging $APP_NAME"
mkdir -p "$BUILD_DIR/$APP_NAME/Contents/MacOS"
mkdir -p "$BUILD_DIR/$APP_NAME/Contents/Resources"

cp "$BUILD_DIR/$OUTPUT_NAME" "$BUILD_DIR/$APP_NAME/Contents/MacOS/$EXECUTABLE"
chmod +x "$BUILD_DIR/$APP_NAME/Contents/MacOS/$EXECUTABLE"
cp "assets/AppIcon.icns" "$BUILD_DIR/$APP_NAME/Contents/Resources/AppIcon.icns"
# Logo and status item artwork come from assets/render-icons.swift.
cp "assets/logo.png" "$BUILD_DIR/$APP_NAME/Contents/Resources/Logo.png"
cp assets/menubar-*.png "$BUILD_DIR/$APP_NAME/Contents/Resources/"
if [[ "$BUNDLE_CLI" == 1 ]]; then
    cp "$BUILD_DIR/vpn" "$BUILD_DIR/$APP_NAME/Contents/Resources/vpn"
    chmod 755 "$BUILD_DIR/$APP_NAME/Contents/Resources/vpn"
fi

cat <<EOF > "$BUILD_DIR/$APP_NAME/Contents/Info.plist"
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleExecutable</key>
    <string>$EXECUTABLE</string>
    <key>CFBundleIconFile</key>
    <string>AppIcon</string>
    <!-- Kept from the original app name so existing installs keep their macOS preferences. -->
    <key>CFBundleIdentifier</key>
    <string>com.tms.vpn.menubar</string>
    <key>CFBundleName</key>
    <string>$DISPLAY_NAME</string>
    <key>CFBundleDisplayName</key>
    <string>$DISPLAY_NAME</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleShortVersionString</key>
    <string>${VERSION}</string>
    <key>CFBundleVersion</key>
    <string>${VERSION}</string>
    <key>LSUIElement</key>
    <true/>
    <key>NSHighResolutionCapable</key>
    <true/>
</dict>
</plist>
EOF

# Ad-hoc signature over the finished bundle (keeps Apple Silicon happy about the added
# resources). Not a Developer ID signature: first launch still needs right-click → Open.
# A failure here used to be swallowed (`|| true`), shipping an app whose seal
# is broken; it is now fatal, and the result is verified.
if ! codesign --force --deep --sign - "$BUILD_DIR/$APP_NAME"; then
    echo "error: ad-hoc code signing of $APP_NAME failed" >&2
    exit 1
fi
codesign --verify --deep --strict "$BUILD_DIR/$APP_NAME" || { echo "error: $APP_NAME does not verify after signing" >&2; exit 1; }

echo "Built binary: $BUILD_DIR/$OUTPUT_NAME"
echo "Built app:    $BUILD_DIR/$APP_NAME (v$VERSION)"
