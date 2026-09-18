#!/usr/bin/env bash
# Single toggle for hey-lain: tap Alt+V to start, tap again to stop+process.
# Replaces the old press/release pair (release-ordering + double-fire bugs).
# Sway side must use --no-repeat so key-repeat can't flap the toggle.
set -uo pipefail
HERE="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$HERE/bin"
RUNTIME="${XDG_RUNTIME_DIR:-/tmp}/hey-lain"
LOCK="$RUNTIME/processing.lock"
DEBOUNCE="$RUNTIME/last-toggle"
mkdir -p "$RUNTIME"
# Logs live in the user's state dir: the install deploys hey-lain root-owned
# under /usr/share/navi, so $HERE/log isn't writable at runtime. Children
# (hey-lain.sh, record-start.sh, ...) inherit this via the environment.
HEY_LAIN_LOG_DIR="${HEY_LAIN_LOG_DIR:-${XDG_STATE_HOME:-$HOME/.local/share}/hey-lain/log}"
mkdir -p "$HEY_LAIN_LOG_DIR" 2>/dev/null || HEY_LAIN_LOG_DIR="$HERE/log"
export HEY_LAIN_LOG_DIR
LOG="${HEY_LAIN_LOG_DIR:-$HERE/log}/hey-lain.log"
log() { echo "$(date '+%F %T') toggle: $*" >> "$LOG"; }

now=$(date +%s)
if [ -f "$DEBOUNCE" ]; then
  last=$(cat "$DEBOUNCE" 2>/dev/null || echo 0)
  if [ $((now - last)) -lt 1 ]; then
    exit 0  # bounce / double-fire guard
  fi
fi
echo "$now" > "$DEBOUNCE"

recording_active() {
  local pid
  # pidfile hint, verified against /proc (no pgrep self-match games)
  if [ -f "$RUNTIME/rec.pid" ]; then
    pid=$(cat "$RUNTIME/rec.pid" 2>/dev/null || true)
    if [ -n "${pid:-}" ] && [ -f "/proc/$pid/cmdline" ] \
       && tr '\0' ' ' < "/proc/$pid/cmdline" 2>/dev/null | grep -qE "pw-record|arecord"; then
      return 0
    fi
  fi
  return 1
}

# Don't stack a new recording while Lain is thinking/speaking.
if [ -f "$LOCK" ] && ! flock -n "$LOCK" true 2>/dev/null; then
  log "tap ignored: processing"
  notify-send "Hey Lain!" "Working on it — one sec…" -t 1500
  exit 0
fi

if recording_active; then
  log "tap -> stop+process"
  exec "$BIN/hey-lain.sh"
else
  log "tap -> start recording"
  exec "$BIN/record-start.sh"
fi
