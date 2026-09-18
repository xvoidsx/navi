#!/usr/bin/env bash
# Fetch the Piper TTS voice model — the "voice model" tts-ready.sh checks for.
# The voice is NOT bundled in the repo; it is downloaded once here and then
# kept across redeploys by setup_heylain (like the venv). Safe to re-run:
# skips when a non-empty voice file is already present.
set -uo pipefail
HERE="$(cd "$(dirname "$0")/.." && pwd)"
CFG="$HERE/config.json"
VOICE_DIR="$HERE/voices"

# Voice stem from config.json (default: en_US-libritts_r-medium). Only the
# default voice has a known download URL; a custom piper_voice must be
# placed in voices/ by hand.
STEM="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("piper_voice","voices/en_US-libritts_r-medium.onnx").split("/")[-1].removesuffix(".onnx"))' "$CFG" 2>/dev/null || echo en_US-libritts_r-medium)"

if [ -s "$VOICE_DIR/$STEM.onnx" ] && [ -s "$VOICE_DIR/$STEM.onnx.json" ]; then
  echo "voice already present: $STEM"
  exit 0
fi

if [ "$STEM" != "en_US-libritts_r-medium" ]; then
  echo "fetch-voice: no download URL known for '$STEM' —" >&2
  echo "  place $STEM.onnx and $STEM.onnx.json in $VOICE_DIR manually." >&2
  exit 1
fi

mkdir -p "$VOICE_DIR"
BASE="https://huggingface.co/rhasspy/piper-voices/resolve/main/en/en_US/libritts_r/medium"
echo "fetching Piper voice: $STEM (~60MB, one time)..."
if curl -fsSL --max-time 300 -o "$VOICE_DIR/$STEM.onnx" "$BASE/$STEM.onnx" \
&& curl -fsSL --max-time 120 -o "$VOICE_DIR/$STEM.onnx.json" "$BASE/$STEM.onnx.json" \
&& [ -s "$VOICE_DIR/$STEM.onnx" ] && [ -s "$VOICE_DIR/$STEM.onnx.json" ]; then
  echo "voice ready: $VOICE_DIR/$STEM.onnx"
  exit 0
fi
echo "fetch-voice: download failed — check the network and re-run." >&2
rm -f "$VOICE_DIR/$STEM.onnx" "$VOICE_DIR/$STEM.onnx.json"
exit 1
