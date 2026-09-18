#!/usr/bin/env bash
# Tap #2: stop rec -> STT (delete wav IMMEDIATELY) -> brain -> actions -> TTS.
# Single-flight via flock: overlapping taps can't double-process.
set -uo pipefail
HERE="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$HERE/bin"
VENV_PY="$HERE/venv/bin/python"
TTS_PY="${HEY_LAIN_TTS_PYTHON:-$VENV_PY}"
RUNTIME="${XDG_RUNTIME_DIR:-/tmp}/hey-lain"
LOG="${HEY_LAIN_LOG_DIR:-$HERE/log}/hey-lain.log"
LOCK="$RUNTIME/processing.lock"
mkdir -p "$RUNTIME"
export HEY_LAIN_LOG="$LOG"  # speak.py appends its stage timings here

exec 9>"$LOCK"
if ! flock -n 9; then
  exit 0  # another run is already processing
fi

log() { echo "$(date '+%F %T') hey-lain: $*" >> "$LOG"; }
T0=$SECONDS  # stage timing: reply latency is the sum of these

WAV=""
cleanup() {
  # On the success path the overlay is SPEAKING (conversation view) — leave
  # it up. Every other exit means failure: take it down.
  if [ "$(cat "$RUNTIME/overlay-state" 2>/dev/null)" != "SPEAKING" ] && [ "$(cat "$RUNTIME/overlay-state" 2>/dev/null)" != "WAITING" ]; then
    "$BIN/listen-overlay.sh" hide 2>/dev/null || true
  fi
  [ -n "${WAV:-}" ] && rm -f "$WAV" 2>/dev/null || true
  rm -f "$RUNTIME"/input-*.wav "$RUNTIME/rec.pid" "$RUNTIME/current" "$RUNTIME"/speech.txt 2>/dev/null || true
}
trap cleanup EXIT INT TERM

WAV="$("$BIN/record-stop.sh" 2>>"$LOG")" || { notify-send "Hey Lain!" "No audio captured."; exit 0; }
log "stage stop took $((SECONDS-T0))s"; T0=$SECONDS
"$BIN/listen-overlay.sh" state HEARD 2>/dev/null || true
log "transcribing $WAV"

if [ ! -x "$VENV_PY" ]; then
  notify-send "Hey Lain!" "STT not installed yet — run ~/hey-lain/setup.sh"
  exit 1
fi
TEXT=$("$VENV_PY" "$BIN/listen.py" "$WAV" 2>>"$LOG" || true)
rm -f "$WAV"; WAV=""  # immediate delete, before any LLM/network work
TEXT="$(echo "$TEXT" | xargs || true)"
log "heard: ${TEXT:-<empty>}"
log "stage stt took $((SECONDS-T0))s"; T0=$SECONDS
if ! "$BIN/tts-ready.sh"; then
  notify-send "Hey Lain!" "My voice model is not ready — run install.sh --skip-apt."
  "$BIN/listen-overlay.sh" hide 2>/dev/null || true
  exit 1
fi
if [ -z "$TEXT" ]; then
  "$BIN/listen-overlay.sh" hide 2>/dev/null || true
  "$TTS_PY" "$BIN/speak.py" "I didn't catch that. Could you say it again?"
  exit 0
fi
"$BIN/listen-overlay.sh" state THINKING "$TEXT" 2>/dev/null || true
notify-send "Hey Lain! (you)" "$TEXT" -t 3000

REPLY=$("$BIN/brain.sh" "$TEXT" 2>>"$LOG" || true)
if [ -z "${REPLY// }" ]; then
  REPLY="Sorry, my brain hiccupped. Try again?"
fi
log "reply: $REPLY"
log "stage brain took $((SECONDS-T0))s"; T0=$SECONDS
# Flash what Lain is about to do, THEN act (overlay hides as action starts).
ACTING=$(echo "$REPLY" | grep -oE '^\[ACTION:[^]]+\]' | head -n 1 || true)
"$BIN/listen-overlay.sh" state ACTING "${ACTING:-speaking…}" 2>/dev/null || true
sleep 0.6
# NOTE: actions.sh must NOT run inside $(...) — bash kills backgrounded
# children when a command-substitution subshell exits, which silently
# murdered every `swaymsg exec ... &` / `swaylock &`. Temp file instead.
"$BIN/actions.sh" <<<"$REPLY" > "$RUNTIME/speech.txt" 2>>"$LOG"
SPEECH=$(cat "$RUNTIME/speech.txt" 2>/dev/null || true)
rm -f "$RUNTIME/speech.txt"
if [ -z "${SPEECH// }" ]; then SPEECH="$REPLY"; fi
# overlay-hide already took the window down: don't re-show it for SPEAKING.
if [[ "$REPLY" =~ ^\[ACTION:\ overlay-hide\] ]]; then
  "$TTS_PY" "$BIN/speak.py" "$SPEECH"
  exit 0
fi
# Keep the aesthetic visualizer up after playback so the user can tap Alt+V
# to talk back (record-start resets the overlay). Lock stays
# held through speech: no barge-in (mic + speakers = echo into the next
# transcript); taps during speech get "working".
# Auto-dismiss is OFF by default: the user closes the window manually
# (voice "dismiss"/"thanks", next tap, or bin/listen-overlay.sh hide).
# Set OVERLAY_AUTOHIDE_SECS=30 to re-enable the abandoned-window watcher.
"$BIN/listen-overlay.sh" state SPEAKING "" 2>/dev/null || true
if [ "${OVERLAY_AUTOHIDE_SECS:-0}" -gt 0 ] 2>/dev/null; then
( exec 9>&-  # watcher must not inherit the processing lock
  sleep "$OVERLAY_AUTOHIDE_SECS"
  if [ "$(cat "$RUNTIME/overlay-state" 2>/dev/null)" = "SPEAKING" ]; then
    "$BIN/listen-overlay.sh" hide 2>/dev/null || true
  fi ) &>/dev/null &
fi
"$TTS_PY" "$BIN/speak.py" "$SPEECH"
"$BIN/listen-overlay.sh" state WAITING "" 2>/dev/null || true
log "stage speak took $((SECONDS-T0))s"
exit 0
