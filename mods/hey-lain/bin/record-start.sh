#!/usr/bin/env bash
# Start a push-to-talk recording. Idempotent. One unique wav per session.
set -euo pipefail
HERE="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$HERE/bin"
RUNTIME="${XDG_RUNTIME_DIR:-/tmp}/hey-lain"
LOG="${HEY_LAIN_LOG_DIR:-$HERE/log}/hey-lain.log"
PIDFILE="$RUNTIME/rec.pid"
CURRENT="$RUNTIME/current"
CFG="$HERE/config.json"
mkdir -p "$RUNTIME" "$(dirname "$LOG")"

log() { echo "$(date '+%F %T') start: $*" >> "$LOG"; }

if [ -f "$PIDFILE" ]; then
  _pid=$(cat "$PIDFILE" 2>/dev/null || true)
  if [ -n "${_pid:-}" ] && [ -f "/proc/$_pid/cmdline" ] \
     && tr '\0' ' ' < "/proc/$_pid/cmdline" 2>/dev/null | grep -qE "pw-record|arecord"; then
    "$BIN/listen-overlay.sh" show 2>/dev/null || true
    exit 0
  fi
  rm -f "$PIDFILE"  # stale pidfile, recorder is gone
fi

RATE=$(python3 -c "import json;print(json.load(open('$CFG'))['record_rate'])")
CH=$(python3 -c "import json;print(json.load(open('$CFG'))['record_channels'])")

WAV="$RUNTIME/input-$(date +%Y%m%d-%H%M%S)-$$.wav"
rm -f "$RUNTIME"/input-*.wav
echo "$WAV" > "$CURRENT"

if command -v pw-record >/dev/null; then
  pw-record --rate "$RATE" --channels "$CH" "$WAV" &
  echo $! > "$PIDFILE"
else
  arecord -r "$RATE" -c "$CH" -f S16_LE -t wav "$WAV" &
  echo $! > "$PIDFILE"
fi
sleep 0.5
_NEWPID=$(cat "$PIDFILE")
if ! [ -f "/proc/$_NEWPID/cmdline" ]; then
  log "recorder died instantly, aborting"
  notify-send "Hey Lain!" "Mic failed to start." -t 2000
  rm -f "$PIDFILE" "$WAV" "$CURRENT"
  exit 1
fi
log "rec pid $_NEWPID -> $WAV"
# Live mic levels for the overlay waveform (stdlib-only python, ~0.3% CPU).
# Guarded: a monitor from a half-dead session is reused, never doubled.
if [ ! -f "$RUNTIME/mic-level.pid" ] || ! kill -0 "$(cat "$RUNTIME/mic-level.pid" 2>/dev/null)" 2>/dev/null; then
  python3 "$BIN/mic-level.py" "$WAV" "$RUNTIME/mic-level" >>"$LOG" 2>&1 &
  echo $! > "$RUNTIME/mic-level.pid"
fi
"$BIN/listen-overlay.sh" show 2>/dev/null || true
notify-send "Hey Lain!" "…listening (tap Alt+V again to send)" -t 2000
