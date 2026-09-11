#!/usr/bin/env bash
# navi-wallpaper — startup wallpaper for the wired
#     by rav3ndust.xyz (xvoidsx)
#
# Animated-first: tries mpvpaper with the default gifpaper, verifies it is
# actually running, and only then falls back to a static swaybg wallpaper.
# Never runs both at once.
#
# The health check is deliberately patient: on old graphics (e.g. GMA 4500)
# mpvpaper can survive a naive 2-second liveness probe and then die minutes
# later from EGL_BAD_MATCH, leaving a black desktop. We poll for several
# seconds AND watch the log for EGL/context failures. A confirmed failure
# is remembered in ~/.local/share/navi-wallpaper.noanim so later boots
# skip the doomed probe and go straight to static. Picking a wallpaper
# manually with gifpaperslain clears the marker.

set -u

SHARE="/usr/share/navi/wired"
ANIMATED="$SHARE/wp/gifpaperslain/navi-lain-rgbsplit.gif"
STATIC="$SHARE/wp/navi-lain-rgbsplit.jpg"

LOCAL_SHARE="$HOME/.local/share"
LOG="$LOCAL_SHARE/navi-wallpaper.log"
NOANIM="$LOCAL_SHARE/navi-wallpaper.noanim"

log() { printf 'navi-wallpaper: %s\n' "$*" >&2; }

# don't stack instances on config reload
if pgrep -x mpvpaper >/dev/null 2>&1 || pgrep -x swaybg >/dev/null 2>&1; then
  log "a wallpaper daemon is already running — leaving it alone"
  exit 0
fi

static_fallback() {
  # static fallback (swaybg only — never alongside mpvpaper)
  if [ -f "$STATIC" ] && command -v swaybg >/dev/null 2>&1; then
    log "using static fallback ($STATIC)"
    exec swaybg -i "$STATIC" -m fill
  fi
  log "no usable wallpaper found — desktop will be bare"
  exit 1
}

# EGL/context failure signatures seen in mpvpaper's log on broken stacks
egl_failed() { # <first-new-log-line>
  tail -n +"$1" "$LOG" 2>/dev/null | grep -qiE "failed to create EGL|EGL_BAD|failed to create context|no suitable EGL|libEGL warning.*failed"
}

# 1. animated default
if [ -e "$NOANIM" ]; then
  log "animated wallpapers marked unsupported on this machine — static it is"
  static_fallback
fi

if command -v mpvpaper >/dev/null 2>&1 && [ -f "$ANIMATED" ]; then
  mkdir -p "$LOCAL_SHARE"
  # only failures logged during THIS probe count — old boots don't trip us
  log_start=$(( $(wc -l <"$LOG" 2>/dev/null || echo 0) + 1 ))
  : >>"$LOG"
  mpvpaper ALL -o "loop panscan=1" "$ANIMATED" >>"$LOG" 2>&1 &
  mpvpid=$!

  dead=0
  for _ in $(seq 1 12); do
    sleep 1
    if ! kill -0 "$mpvpid" 2>/dev/null; then dead=1; break; fi
    if egl_failed "$log_start"; then
      log "EGL/context failure in mpvpaper log — it will not survive"
      kill "$mpvpid" 2>/dev/null
      dead=1
      break
    fi
  done

  if [ "$dead" -eq 0 ] && kill -0 "$mpvpid" 2>/dev/null; then
    log "animated wallpaper running (mpvpaper)"
    # late-death reaper: if mpvpaper still dies shortly after we hand off,
    # swap in the static fallback instead of leaving a black desktop.
    # Harmless if the user picks another wallpaper (mpvpaper still alive).
    # An EGL signature in the log means the broken-stack death, so the
    # machine gets marked and later boots skip the probe entirely.
    ( sleep 25
      if ! pgrep -x mpvpaper >/dev/null 2>&1 && ! pgrep -x swaybg >/dev/null 2>&1; then
        if tail -n +"$log_start" "$LOG" 2>/dev/null | grep -qiE "failed to create EGL|EGL_BAD|failed to create context"; then
          touch "$NOANIM"
        fi
        if [ -f "$STATIC" ] && command -v swaybg >/dev/null 2>&1; then
          swaybg -i "$STATIC" -m fill >/dev/null 2>&1 &
        fi
      fi ) &
    disown 2>/dev/null || true
    exit 0
  fi

  log "mpvpaper failed to stay up — see $LOG; falling back to static"
  touch "$NOANIM"
  log "marked animated wallpapers unsupported on this machine ($NOANIM)"
  static_fallback
else
  [ -f "$ANIMATED" ] || log "no animated wallpaper at $ANIMATED"
  command -v mpvpaper >/dev/null 2>&1 || log "mpvpaper not installed"
  static_fallback
fi
