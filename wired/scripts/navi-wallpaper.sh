#!/usr/bin/env bash
# navi-wallpaper — startup wallpaper for the wired
#     by rav3ndust.xyz (xvoidsx)
#
# Animated-first: tries mpvpaper with the default gifpaper, verifies it is
# actually running, and only then falls back to a static swaybg wallpaper.
# Never runs both at once.

set -u

SHARE="/usr/share/navi/wired"
ANIMATED="$SHARE/wp/gifpaperslain/navi-lain.gif"
STATIC="$SHARE/wp/SElain3.jpg"

log() { printf 'navi-wallpaper: %s\n' "$*" >&2; }

# don't stack instances on config reload
if pgrep -x mpvpaper >/dev/null 2>&1 || pgrep -x swaybg >/dev/null 2>&1; then
  log "a wallpaper daemon is already running — leaving it alone"
  exit 0
fi

# 1. animated default
if command -v mpvpaper >/dev/null 2>&1 && [ -f "$ANIMATED" ]; then
  mpvpaper ALL -o "loop panscan=1" "$ANIMATED" >/dev/null 2>&1 &
  sleep 2
  if pgrep -x mpvpaper >/dev/null 2>&1; then
    log "animated wallpaper running (mpvpaper)"
    exit 0
  fi
  log "mpvpaper failed to stay up — falling back to static"
else
  [ -f "$ANIMATED" ] || log "no animated wallpaper at $ANIMATED"
  command -v mpvpaper >/dev/null 2>&1 || log "mpvpaper not installed"
fi

# 2. static fallback (swaybg only — never alongside mpvpaper)
if [ -f "$STATIC" ] && command -v swaybg >/dev/null 2>&1; then
  log "using static fallback ($STATIC)"
  exec swaybg -i "$STATIC" -m fill
fi

log "no usable wallpaper found — desktop will be bare"
exit 1
