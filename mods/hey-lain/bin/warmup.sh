#!/usr/bin/env bash
# warmup.sh — preload STT, start the warm Kokoro server, pre-seed the TTS
# cache with Lain's most-spoken lines, and keep the LLM resident.
# Runs detached in the background; safe to exec from sway config:
#   exec /path/to/hey-lain/bin/warmup.sh
set -uo pipefail
HERE="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$HERE/bin"
LOG="$HERE/log/hey-lain.log"
RUNTIME="${XDG_RUNTIME_DIR:-/tmp}/hey-lain"
log() { echo "$(date '+%F %T') warmup: $*" >> "$LOG"; }

log "start"
# STT: 1.5s of silence passes the 0.4s guard and forces a full
# faster-whisper model + VAD load (result discarded).
WAV=/tmp/hey-lain-warmup.wav
python3 -c "import wave; w=wave.open('$WAV','wb'); w.setnchannels(1); w.setsampwidth(2); w.setframerate(16000); w.writeframes(b'\x00'*48000); w.close()"
"$HERE/venv/bin/python" "$BIN/listen.py" "$WAV" >>"$LOG" 2>&1 || true
rm -f "$WAV"
log "stt warm"
# TTS servers (warm models, no per-utterance load) + cache seed.
"$BIN/tts-ready.sh" >>"$LOG" 2>&1 || log "voice readiness FAILED"
"$BIN/piper-serve.sh" start >>"$LOG" 2>&1 || log "piper serve start FAILED"
"$BIN/kokoro-serve.sh" start >>"$LOG" 2>&1 || log "kokoro serve start FAILED"
RUNTIME="$RUNTIME" "$BIN/piper-seed-cache.sh" >>"$LOG" 2>&1 || log "cache seed FAILED"
log "tts done"
# LLM: tiny ping keeps gemma resident (brain also sends keep_alive=30m).
curl -s --max-time 60 http://127.0.0.1:11434/api/generate \
  -d '{"model":"gemma3:270m","prompt":"hi","stream":false,"options":{"num_predict":1},"keep_alive":"30m"}' \
  >>"$LOG" 2>&1 || log "llm warm FAILED"
log "done"
