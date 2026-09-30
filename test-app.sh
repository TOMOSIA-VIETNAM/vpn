#!/usr/bin/env bash
# ==============================================================================
# Tests for the menu bar app's logic (app-tests/main.swift).
#
#   ./test-app.sh      run ./build.sh first so the bundle signature checks run too
# ==============================================================================
set -euo pipefail
cd "$(dirname "$0")"

BUILD_DIR="${BUILD_DIR:-./build}"
OUT="$(mktemp -d -t tmsvpn-apptests)"
trap 'rm -rf "$OUT"' EXIT

# Everything in app/ except its entry point; app-tests/main.swift is the entry point here.
SOURCES=()
for f in app/*.swift; do
    [[ "$f" == "app/main.swift" ]] || SOURCES+=("$f")
done

swiftc -target "$(uname -m)-apple-macos12.0" -framework Cocoa -framework SwiftUI -framework Security \
    "${SOURCES[@]}" app-tests/main.swift -o "$OUT/app-tests"

if [[ -d "$BUILD_DIR/TOMOSIA VPN.app" ]]; then
    "$OUT/app-tests" "$BUILD_DIR/TOMOSIA VPN.app"
else
    "$OUT/app-tests"
fi
