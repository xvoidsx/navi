#!/usr/bin/env bash
# Stop ALL hey-lain recorders (pidfile + pattern sweep, no orphans),
# print the session wav path. Caller must delete it right after STT.
# NOTE: overlay stays UP past this point (HEARD/THINKING/ACTING states).
# hey-lain.sh owns it from here (or its trap on failure).
# Waits are deliberately short: pw-record exits on SIGINT in ~100ms, and
# every 100ms here delays the reply. Orphan sweep is best-effort.
set -euo pipefail
HERE="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$HERE/bin"
RUNTIME="${XDG_RUNTIME_DIR:-/tmp}/hey-lain"
LOG="$HERE/log/hey-lain.log"
PIDFILE="$RUNTIME/rec.pid"
CURRENT="$RUNTIME/current"

log() { echo "$(date '+%F %T') stop: $*" >> "$LOG"; }

# NOTE: overlay stays UP past this point (HEARD/THINKING/ACTING states).
# hey-lain.sh owns hiding it when speech begins (or its trap on failure).

if [ -f "$PIDFILE" ]; then
  PID=$(cat "$PIDFILE" 2>/dev/null || true)
  if [ -n "${PID:-}" ] && kill -0 "$PID" 2>/dev/null; then
    kill -INT "$PID" 2>/dev/null || true
    for _ in $(seq 1 8); do kill -0 "$PID" 2>/dev/null || break; sleep 0.1; done
    kill -TERM "$PID" 2>/dev/null || true
    sleep 0.1
    kill -KILL "$PID" 2>/dev/null || true
  fi
  rm -f "$PIDFILE"
fi
# Pattern sweep: never leave orphan recorders behind.
pkill -INT -f "pw-record.*hey-lain" 2>/dev/null || true
sleep 0.2
pkill -KILL -f "pw-record.*hey-lain" 2>/dev/null || true
pkill -KILL -f "arecord.*hey-lain" 2>/dev/null || true

WAV=""
[ -f "$CURRENT" ] && WAV=$(cat "$CURRENT" 2>/dev/null || true)
rm -f "$CURRENT"
if [ -z "${WAV:-}" ] || [ ! -s "$WAV" ]; then
  [ -n "${WAV:-}" ] && rm -f "$WAV"
  log "nothing recorded"
  echo "no audio recorded" >&2
  exit 1
fi
log "captured $WAV ($(stat -c%s "$WAV") bytes)"
echo "$WAV"
