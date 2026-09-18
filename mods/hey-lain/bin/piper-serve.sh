#!/usr/bin/env bash
# Managed Piper localhost TTS server (resident voice, no per-call reload).
# Usage: piper-serve.sh start|stop|status
set -uo pipefail
HERE="$(cd "$(dirname "$0")/.." && pwd)"
RUNTIME="${XDG_RUNTIME_DIR:-/tmp}/hey-lain"
PIDFILE="$RUNTIME/piper-serve.pid"
LOG="${HEY_LAIN_LOG_DIR:-$HERE/log}/piper-serve.log"
PORT="${PIPER_SERVICE_PORT:-8766}"
mkdir -p "$RUNTIME" "$(dirname "$LOG")"

alive() {
  [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null
}

case "${1:-status}" in
  start)
    if alive; then echo "piper-serve already running"; exit 0; fi
    PYTHON="${HEY_LAIN_PYTHON:-$HERE/venv/bin/python}"
    [ -x "$PYTHON" ] || PYTHON="$(command -v python3)"
    "$PYTHON" -c 'from piper import PiperVoice' >/dev/null 2>&1 || {
      echo "piper-tts is not installed in $PYTHON; run install.sh --skip-apt" >&2
      exit 1
    }
    # The daemon must NEVER inherit the caller's file descriptors: hey-lain.sh
    # holds its single-flight flock on fd 9, and a daemon that inherits it
    # pins the lock forever — every later Alt+V then reports "Working on it"
    # until the daemon is killed (flock lives on the open file description,
    # released only when the LAST fd referencing it closes). Closing an
    # fd that was never opened is a harmless no-op.
    exec 9>&- 2>/dev/null || true
    setsid "$PYTHON" "$HERE/bin/piper-serve.py" >>"$LOG" 2>&1 < /dev/null &
    sleep 0.4
    pgrep -f "piper-serve.py" 2>/dev/null | head -n 1 > "$PIDFILE"
    for _ in $(seq 1 30); do
      curl -s --max-time 2 "http://127.0.0.1:$PORT/health" 2>/dev/null | grep -q '"status": "ok"' && {
        echo "piper-serve up on :$PORT"; exit 0; }
      sleep 0.5
    done
    echo "piper-serve failed to come up (see $LOG)" >&2
    exit 1 ;;
  stop)
    [ -f "$PIDFILE" ] && kill "$(cat "$PIDFILE")" 2>/dev/null || true
    sleep 0.5
    [ -f "$PIDFILE" ] && kill -9 "$(cat "$PIDFILE")" 2>/dev/null || true
    rm -f "$PIDFILE"
    pkill -f "piper-serve.py" 2>/dev/null || true
    echo "piper-serve stopped" ;;
  status)
    if alive; then echo "piper-serve running (pid $(cat "$PIDFILE"))";
    else echo "piper-serve down"; exit 1; fi ;;
esac
