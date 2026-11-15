#!/bin/sh
# Записывает видеодемо «от запроса до результата» по живому стенду.
# Требует: запущенный стенд (docker compose up), Google Chrome, ffmpeg, npm-зависимости web/.
#   E2E_BASE_URL=http://127.0.0.1:8080 sh scripts/record-demo.sh
set -eu
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DIR="$(mktemp -d)"
cd "$ROOT/web"
E2E_SCREENCAST_DIR="$DIR" npx playwright test demo
mkdir -p "$ROOT/docs/media"
ffmpeg -y -loglevel error -f concat -safe 0 -i "$DIR/frames.ffconcat" -fps_mode vfr \
  -vf "scale=1440:900:force_original_aspect_ratio=decrease,pad=1440:900:(ow-iw)/2:(oh-ih)/2,fps=25,format=yuv420p" \
  -c:v libx264 -crf 24 -preset medium -movflags +faststart "$ROOT/docs/media/tropa-demo.mp4"
echo "Видео: $ROOT/docs/media/tropa-demo.mp4"
