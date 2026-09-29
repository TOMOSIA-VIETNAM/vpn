#!/usr/bin/env bash
# Encodes a finished render into the files the landing page (webapp/) serves in its hero:
#   web/promo-<lang>.mp4         H.264 1080p, AAC 128k, faststart (plays while downloading)
#   web/promo-<lang>.webm        VP9 1080p, Opus 96k
#   web/promo-<lang>-mobile.mp4  H.264 720p, AAC 96k, for narrow screens
#   web/promo-<lang>-poster.jpg  first-paint and reduced-motion still, taken at POSTER_AT seconds
# The web page starts the video muted; the audio stays so its unmute button works.
#
# Usage: scripts/encode-web.sh renders/tomosia-vpn-promo-vi-v3.mp4 vi [POSTER_AT]
set -euo pipefail
cd "$(dirname "$0")/.."
src="$1"
lang="$2"
poster_at="${3:-20.0}" # the product reveal: logo and name on the light wallpaper
out=web
mkdir -p "$out"
ff() { ffmpeg -hide_banner -loglevel error -y "$@"; }

ff -i "$src" -c:v libx264 -preset slow -crf 26 -pix_fmt yuv420p -profile:v high \
  -c:a aac -b:a 128k -movflags +faststart "$out/promo-$lang.mp4"
ff -i "$src" -c:v libvpx-vp9 -crf 36 -b:v 0 -row-mt 1 -deadline good -cpu-used 2 \
  -c:a libopus -b:a 96k "$out/promo-$lang.webm"
ff -i "$src" -vf scale=-2:720 -c:v libx264 -preset slow -crf 28 -pix_fmt yuv420p -profile:v main \
  -c:a aac -b:a 96k -movflags +faststart "$out/promo-$lang-mobile.mp4"
ff -ss "$poster_at" -i "$src" -frames:v 1 -q:v 4 "$out/promo-$lang-poster.jpg"

ls -l "$out"/promo-"$lang"*
