#!/usr/bin/env bash
# Verify the configured voice backend can synthesize before a run proceeds.
set -uo pipefail
HERE="$(cd "$(dirname "$0")/.." && pwd)"
RUNTIME="${XDG_RUNTIME_DIR:-/tmp}/hey-lain"
LOG="${HEY_LAIN_LOG:-$HERE/log/hey-lain.log}"
CFG="$HERE/config.json"
mkdir -p "$RUNTIME" "$(dirname "$LOG")"
log() { echo "$(date '+%F %T') tts-ready: $*" >> "$LOG"; }
fail() { log "FAILED: $*"; echo "TTS unavailable: $*" >&2; exit 1; }

ENGINE="${TTS_ENGINE:-$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("tts_engine", "piper"))' "$CFG" 2>/dev/null || echo piper)}"
command -v aplay >/dev/null 2>&1 || fail "aplay is not installed"

if [ "$ENGINE" = "piper" ]; then
  MODEL="$HERE/$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("piper_voice", "voices/en_US-libritts_r-medium.onnx"))' "$CFG" 2>/dev/null || echo voices/en_US-libritts_r-medium.onnx)"
  [ -s "$MODEL" ] || fail "Piper model missing: $MODEL"
  PYTHON="${HEY_LAIN_PYTHON:-$HERE/venv/bin/python}"
  [ -x "$PYTHON" ] || PYTHON="$(command -v python3 || true)"
  [ -n "$PYTHON" ] || fail "Python is not installed"
  "$PYTHON" -c 'from piper import PiperVoice' >/dev/null 2>&1 || fail "piper-tts is not installed in $PYTHON; run install.sh --skip-apt"
  "$HERE/bin/piper-serve.sh" start >/dev/null 2>>"$LOG" || fail "Piper service failed to start"
  curl -fsS --max-time 5 "http://127.0.0.1:${PIPER_SERVICE_PORT:-8766}/health" 2>/dev/null | grep -q '"status": "ok"' || fail "Piper health check failed"
  if ! curl -fsS --max-time 20 -X POST "http://127.0.0.1:${PIPER_SERVICE_PORT:-8766}/speak" -H 'Content-Type: application/json' --data '{"text":"ready"}' -o "$RUNTIME/tts-ready.pcm" 2>>"$LOG" || [ ! -s "$RUNTIME/tts-ready.pcm" ]; then
    rm -f "$RUNTIME/tts-ready.pcm"
    fail "Piper model synthesis failed"
  fi
  rm -f "$RUNTIME/tts-ready.pcm"
  log "Piper model ready"
  exit 0
fi

if [ "$ENGINE" = "kokoro" ]; then
  "$HERE/bin/kokoro-serve.sh" start >/dev/null 2>>"$LOG" || fail "Kokoro service failed to start"
  curl -fsS --max-time 5 "http://127.0.0.1:${KOKORO_SERVICE_PORT:-8765}/health" 2>/dev/null | grep -q '"status": "ok"' || fail "Kokoro health check failed"
  log "Kokoro service ready"
  exit 0
fi
fail "unknown TTS engine: $ENGINE"
