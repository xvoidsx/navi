#!/usr/bin/env bash
#
# waybar custom/nowplaying module — the wired's ear on the music.
#
# A fixed-width slot: the text is always padded/truncated to SLOT
# characters, so neighboring modules never slide when a track starts,
# stops, or has a long name. Idle shows a dim ♫; playing or paused shows
# ♫ artist — title, truncated. Click opens the full navi-nowplaying mod.
#
# Reads MPRIS through playerctl, so it follows whatever is actually
# playing — Chromium, cmus, mpv, spotify — the same bus the mod reads.

set -uo pipefail

SLOT=22 # display width of the slot, in characters
export SLOT

json_escape() { printf '%s' "$1" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read()))'; }

emit() { # <text> <tooltip> <class>
  printf '{"text": %s, "tooltip": %s, "class": %s}\n' \
    "$(json_escape "$1")" "$(json_escape "$2")" "$(json_escape "$3")"
}

# fit pads or truncates the text to exactly SLOT characters. Done in
# python: bash's printf %-Ns counts bytes under a C locale, which would
# short-pad any text with multibyte glyphs in it.
fit() {
  SLOT="$SLOT" printf '%s' "$1" | python3 -c '
import os, sys
n = int(os.environ["SLOT"])
s = sys.stdin.read()[:n]
sys.stdout.write(s + " " * (n - len(s)))
'
}

fmt_time() { # <seconds, may be float> -> m:ss
  local t="${1%%.*}"
  case "$t" in '' | *[!0-9]*) t=0 ;; esac
  printf '%d:%02d' "$((t / 60))" "$((t % 60))"
}

if ! command -v playerctl >/dev/null 2>&1; then
  emit "$(fit '♫')" "playerctl isn't installed — navi-nowplaying needs it" "idle"
  exit 0
fi

player="$(playerctl -l 2>/dev/null | head -n 1)"
if [ -z "$player" ]; then
  emit "$(fit '♫')" "nothing playing — the wired is quiet (click to open navi-nowplaying)" "idle"
  exit 0
fi

status="$(playerctl -p "$player" status 2>/dev/null || echo Stopped)"
# SOH as the field separator: it never appears in real metadata
SEP="$(printf '\001')"
meta="$(playerctl -p "$player" metadata --format "{{artist}}${SEP}{{title}}${SEP}{{album}}" 2>/dev/null)"
artist="${meta%%"$SEP"*}"
rest="${meta#*"$SEP"}"
title="${rest%%"$SEP"*}"
album="${rest#*"$SEP"}"

label="$title"
if [ -n "$artist" ]; then
  label="$artist — $title"
fi
if [ -z "${label// }" ]; then
  label="$player"
fi

pos="$(playerctl -p "$player" position 2>/dev/null || echo 0)"
len_us="$(playerctl -p "$player" metadata mpris:length 2>/dev/null || echo 0)"
case "$len_us" in '' | *[!0-9]*) len_us=0 ;; esac
len="$((len_us / 1000000))"

short="${player%%.instance*}"
tip="$label"
if [ -n "$album" ]; then
  tip="$tip
$album"
fi
tip="$tip
$short · $(fmt_time "$pos") / $(fmt_time "$len")"

case "$status" in
Playing)
  emit "$(fit "♫ $label")" "$tip" "playing"
  ;;
Paused)
  emit "$(fit "♫ $label")" "$tip (paused)" "paused"
  ;;
*)
  emit "$(fit '♫')" "nothing playing — the wired is quiet (click to open navi-nowplaying)" "idle"
  ;;
esac
