#!/usr/bin/env bash
# navi-wallpaper-coverflow — pick a wallpaper in coverflow style
#     by rav3ndust.xyz (xvoidsx)
#
# Launches the coverflow picker (Python + chromeless Chromium). On selection,
# sets the wallpaper immediately: animated GIFs via mpvpaper (persisted as the
# login default), static images via swaybg.
#
# The classic zenity picker still lives at wired/scripts/gifpaperslain.sh —
# revert to it any time by rebinding the key.
set -e

PICK_FILE="/tmp/navi-wallpaper-picked"
CHOICE="$HOME/.local/share/navi-wallpaper.choice"
NOANIM="$HOME/.local/share/navi-wallpaper.noanim"

# session detection (same as gifpaperslain.sh)
is_x11() {
  [ "${XDG_SESSION_TYPE:-}" = "x11" ] || { [ -n "${DISPLAY:-}" ] && [ -z "${WAYLAND_DISPLAY:-}" ]; }
}
if ! is_x11 && [ -z "$WAYLAND_DISPLAY" ]; then
  export WAYLAND_DISPLAY="wayland-0"
fi

# find the picker (installed location first, repo fallback)
PICKER="/usr/share/navi/wired/wallpaper-picker/picker.py"
[ -f "$PICKER" ] || PICKER="$(dirname "$0")/../wallpaper-picker/picker.py"

rm -f "$PICK_FILE"
if ! python3 "$PICKER"; then
  # cancelled or failed — leave the wallpaper alone
  exit 0
fi
[ -f "$PICK_FILE" ] || exit 0
picked="$(head -n 1 "$PICK_FILE")"
rm -f "$PICK_FILE"
[ -n "$picked" ] && [ -f "$picked" ] || exit 0

kill_last() {
  killall mpvpaper 2>/dev/null || true
  killall swaybg 2>/dev/null || true
  killall xwinwrap 2>/dev/null || true
}

case "$picked" in
  *.gif|*.GIF)
    # animated: persist as the login default (same contract as gifpaperslain.sh)
    mkdir -p "$HOME/.local/share"
    printf '%s\n' "$picked" > "$CHOICE"
    rm -f "$NOANIM"
    kill_last
    if is_x11; then
      xwinwrap -ov -ni -nf -b -s -st -sp -fs -- mpv -wid WID --loop --no-audio "$picked" >/dev/null 2>&1 &
    else
      mpvpaper ALL -o "loop panscan=1" "$picked" >/dev/null 2>&1 &
    fi
    notify-send -u low -t 2000 --transient "navi walls" "gifpaper set: $(basename "$picked")"
    ;;
  *)
    # static: swaybg now; clear the animated choice so it survives reboot
    rm -f "$CHOICE"
    kill_last
    if is_x11; then
      # X11 static: feh if present, else leave it
      if command -v feh >/dev/null 2>&1; then
        feh --bg-fill "$picked" >/dev/null 2>&1 &
      fi
    else
      swaybg -i "$picked" -m fill >/dev/null 2>&1 &
    fi
    notify-send -u low -t 2000 --transient "navi walls" "wallpaper set: $(basename "$picked")"
    ;;
esac
