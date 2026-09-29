#!/usr/bin/env bash
# Writes the Be Vietnam Pro web fonts the Vietnamese pages use (src/fonts/be-vietnam-pro/):
# WOFF2, subset to Latin, Vietnamese and the punctuation the copy uses. The source
# TTFs are the ones the promo video project ships (OFL, license copied alongside).
# Needs fonttools with brotli: pip install fonttools brotli
set -euo pipefail
cd "$(dirname "$0")/.."
src=../assets/videos/promo/assets/fonts
out=src/fonts/be-vietnam-pro
unicodes="U+0000-00FF,U+0102-0103,U+0110-0111,U+0128-0129,U+0131,U+0152-0153,U+0168-0169,U+01A0-01A1,U+01AF-01B0,U+02C6,U+02DA,U+02DC,U+0300-0301,U+0303-0304,U+0308-0309,U+0323,U+1EA0-1EF9,U+2000-206F,U+20AB,U+20AC,U+2122,U+2190-2193,U+2212"
mkdir -p "$out"
for weight in Regular Medium SemiBold Bold; do
  pyftsubset "$src/BeVietnamPro-$weight.ttf" --unicodes="$unicodes" --flavor=woff2 \
    --layout-features='kern,liga,calt,ccmp,locl,mark,mkmk,tnum' --output-file="$out/BeVietnamPro-$weight.woff2"
done
cp "$src/BeVietnamPro-OFL.txt" "$out/OFL.txt"
ls -l "$out"
