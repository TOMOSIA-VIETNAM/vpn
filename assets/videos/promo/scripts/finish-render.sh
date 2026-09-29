#!/bin/sh
# Finishes a HyperFrames render: copies the untouched mix back in (the renderer re-encodes the audio and
# raises its peak), writes the social encode next to it, and prints format, loudness, black frames and SSIM.
# Usage: scripts/finish-render.sh renders/<name>.mp4 <mix.m4a>
set -eu
master=$1
mix=$2
social="${master%.mp4}-social.mp4"
tmp="${master%.mp4}.remux.mp4"
ffmpeg -hide_banner -loglevel error -y -i "$master" -i "$mix" -map 0:v:0 -map 1:a:0 -c copy -movflags +faststart "$tmp"
mv "$tmp" "$master"
ffmpeg -hide_banner -loglevel error -y -i "$master" -c:v libx264 -preset slow -crf 21 -maxrate 6M -bufsize 12M \
  -pix_fmt yuv420p -profile:v high -level 4.1 -g 60 -c:a copy -movflags +faststart "$social"
for f in "$master" "$social"; do
  echo "== $f $(du -h "$f" | cut -f1)"
  ffprobe -v error -show_entries stream=codec_name,width,height,r_frame_rate,sample_rate,channels \
    -show_entries format=duration -of csv=p=0 "$f"
  ffmpeg -hide_banner -nostats -i "$f" -af ebur128=peak=true -f null - 2>&1 | sed -n '/Summary/,$p' | grep -E "I:|Peak:"
done
printf 'black segments: '
ffmpeg -hide_banner -nostats -i "$master" -vf blackdetect=d=0.1:pix_th=0.05 -an -f null - 2>&1 | grep -c black_start || true
printf 'social SSIM '
ffmpeg -hide_banner -nostats -i "$social" -i "$master" -lavfi ssim -f null - 2>&1 | grep -o "All:[0-9.]*"
