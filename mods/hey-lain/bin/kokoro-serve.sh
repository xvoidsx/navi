#!/usr/bin/env bash
# Managed Kokoro localhost TTS server (warm model, no per-utterance load).
# Usage: kokoro-serve.sh start|stop|status
# speak.py uses it when healthy, falls back to local inference otherwise.
set -uo pipefail
HERE="$(cd "$(dirname "$0")/.." && pwd)"
RUNTIME="${XDG_RUNTIME_DIR:-/tmp}/hey-lain"
PIDFILE="$RUNTIME/kokoro-serve.pid"
LOG="${HEY_LAIN_LOG_DIR:-$HERE/log}/kokoro-serve.log"
PORT="${KOKORO_SERVICE_PORT:-8765}"
KOKORO="${KOKORO_BIN:-$(command -v kokoro || true)}"
mkdir -p "$RUNTIME" "$(dirname "$LOG")"

alive() {
  [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null
}

case "${1:-status}" in
  start)
    [ -n "$KOKORO" ] || { echo "kokoro is not installed; Piper remains the default." >&2; exit 1; }
    if alive; then echo "kokoro-serve already running"; exit 0; fi
    # --model full: fp32 is ~2x FASTER than int8 on pre-VNNI Intel (measured).
    setsid "$KOKORO" serve --host 127.0.0.1 --port "$PORT" --model full >>"$LOG" 2>&1 &
    echo $! > "$PIDFILE"
    for _ in $(seq 1 40); do
      curl -s --max-time 2 "http://127.0.0.1:$PORT/health" 2>/dev/null | grep -q '"status": "ok"' && {
        echo "kokoro-serve up on :$PORT"; exit 0; }
      sleep 0.5
    done
    echo "kokoro-serve failed to come up (see $LOG)" >&2
    exit 1 ;;
  stop)
    [ -f "$PIDFILE" ] && kill "$(cat "$PIDFILE")" 2>/dev/null || true
    sleep 0.5
    [ -f "$PIDFILE" ] && kill -9 "$(cat "$PIDFILE")" 2>/dev/null || true
    rm -f "$PIDFILE"
    pkill -f "kokoro serve" 2>/dev/null || true
    echo "kokoro-serve stopped" ;;
  status)
    if alive; then echo "kokoro-serve running (pid $(cat "$PIDFILE"))";
    else echo "kokoro-serve down"; exit 1; fi ;;
esac
