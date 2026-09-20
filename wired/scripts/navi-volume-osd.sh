#!/usr/bin/env bash
# navi-volume-osd.sh — watch the default sink and pop the volume meter.
# Catches every source (waybar scroll, media keys, navi-audio) because it
# listens to the PipeWire/Pulse event stream, not the keybindings.
set -u
HERE="$(cd "$(dirname "$0")" && pwd)"
OSD="$HERE/navi-volume-osd.py"
command -v pactl >/dev/null 2>&1 || exit 0
[ -x "$OSD" ] || exit 0

last=""
pactl subscribe 2>/dev/null | while read -r line; do
  case "$line" in
    *"on sink"*)
      vol="$(pactl get-sink-volume @DEFAULT_SINK@ 2>/dev/null \
             | grep -o '[0-9]\+%' | head -n1 | tr -d '%')"
      [ -n "$vol" ] || continue
      muted=0
      [ "$(pactl get-sink-mute @DEFAULT_SINK@ 2>/dev/null \
             | awk '{print $2}')" = "yes" ] && muted=1
      key="$vol/$muted"
      [ "$key" = "$last" ] && continue  # collapse event bursts
      last="$key"
      "$OSD" --show "$vol" "$muted" &
      ;;
  esac
done
