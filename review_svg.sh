#!/bin/bash
# Extract keyframes from rendered video and review via vision API
# Usage: bash review_svg.sh [video-path]

set -euo pipefail

VIDEO="${1:-/tmp/zen-svg-test2.mp4}"
OUTDIR="/tmp/zen-svg-review"
mkdir -p "$OUTDIR"

echo "== Extracting frames at 2 fps (1 per 0.5s) =="
rm -f "$OUTDIR"/frame-*.png
ffmpeg -y -i "$VIDEO" -vf "fps=2" -q:v 3 "$OUTDIR/frame-%04d.png" 2>/dev/null

echo "== Computing total frames extracted =="
FILES=($(ls "$OUTDIR"/frame-*.png 2>/dev/null))
TOTAL=${#FILES[@]}
echo "  Extracted $TOTAL frames"

if [ "$TOTAL" -eq 0 ]; then
    echo "ERROR: No frames extracted. Check video path."
    exit 1
fi

# Get first 10 for bridge API (max 10 per request)
SELECTED=("${FILES[@]:0:10}")
echo "== Uploading ${#SELECTED[@]} frames to Gemini for visual review =="

# Build JSON with path array
# Use python to build the JSON since it's easier with file paths
python3 -c "
import json, sys, os

files = sys.argv[1:]
body = {
    'action': 'chat',
    'provider': 'gemini',
    'message': 'You are a rendering quality assurance tool. Review the video frames for SVG rendering accuracy. Check for: 1) Are the shield shield graphics rendering properly? 2) Are the variant colors (red/green) correctly applied to the shield? 3) Are the non-variant shield shapes present? 4) Overall rendering quality. Provide a status (PASS/FAIL) and a short summary.',
    'path': files,
    'take_screenshot': False
}
print(body)
" "${SELECTED[@]}" > /tmp/zen-svg-review-body.json

echo "  JSON body size: $(wc -c < /tmp/zen-svg-review-body.json) bytes"
echo "  Uploading..."

# POST to bridge API
curl -s -X POST http://127.0.0.1:9999 \
  -H "Content-Type: application/json" \
  -d @/tmp/zen-svg-review-body.json | python3 -m json.tool 2>/dev/null || echo "  Raw response: $(cat /tmp/zen-svg-review-raw.txt)"

echo ""
echo "== Done =="
