#!/usr/bin/env bash
#
# waybar custom/nowplaying module — the wired's ear on the music.
#
# A fixed-width slot *while playing*: the text is padded/truncated to
# SLOT characters, so neighbors never slide as the marquee scrolls or a
# long-named track starts. But a quiet module shouldn't rent a wide
# slot — paused and idle collapse to a single glyph, so the bar gives
# the space back until the music returns. (The width change happens on
# the discrete play/pause event, never continuously.)
# Idle shows a dim ♫, paused a dim ⏸, playing shows ♫ artist — title.
# Click opens the full navi-nowplaying mod.
#
# Reads MPRIS through playerctl, so it follows whatever is actually
# playing — Chromium, cmus, mpv, spotify — the same bus the mod reads.

set -uo pipefail

SLOT=22 # display width of the slot, in characters
export SLOT
export POS # set per-tick by marquee(); exported so its python reader sees it

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
  emit "♫" "playerctl isn't installed — navi-nowplaying needs it" "idle"
  exit 0
fi

player="$(playerctl -l 2>/dev/null | head -n 1)"
if [ -z "$player" ]; then
  emit "♫" "nothing playing — the wired is quiet (click to open navi-nowplaying)" "idle"
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

# marquee renders a SLOT-char window sliding over a long label, one step
# per poll tick, so a long "artist — title" reads in full without the slot
# ever changing size. state lives in the cache: "<hash> <pos> <dwell>".
# the label hash resets the scroll on track change; dwell holds the head
# of the label for two ticks (~4s) before the scroll starts. only used
# while playing — paused and idle stay perfectly still.
marquee() {
  local label="$1"
  local cache="${XDG_CACHE_HOME:-$HOME/.cache}/navi"
  local state="$cache/nowplaying-marquee"
  mkdir -p "$cache" 2>/dev/null || true
  local hash old_hash pos dwell
  hash="$(printf '%s' "$label" | cksum | cut -d' ' -f1)"
  old_hash=""; pos=0; dwell=0
  if [ -f "$state" ]; then
    read -r old_hash pos dwell <"$state" 2>/dev/null || true
    case "$pos" in '' | *[!0-9]*) pos=0 ;; esac
    case "$dwell" in '' | *[!0-9]*) dwell=0 ;; esac
    if [ "$old_hash" != "$hash" ]; then pos=0; dwell=0; fi
  fi
  local scroll="${label}    ♫    "
  local len new_pos
  len="$(printf '%s' "$scroll" | python3 -c 'import sys; print(len(sys.stdin.read()))')"
  if [ "$dwell" -lt 2 ]; then
    new_pos=0; dwell=$((dwell + 1))
  else
    new_pos=$(( (pos + 1) % len ))
  fi
  printf '%s %d %d' "$hash" "$new_pos" "$dwell" >"$state" 2>/dev/null || true
  # POS is exported at the top of this script, so the assignment below
  # reaches the python child (a VAR=x prefix on a pipeline would only
  # reach printf, not python3).
  POS="$pos"
  printf '%s' "$scroll" | python3 -c '
import os, sys
n = int(os.environ["SLOT"]); p = int(os.environ["POS"])
s = sys.stdin.read(); dbl = s + s
sys.stdout.write(dbl[p:p+n])
'
}

case "$status" in
Playing)
  full="♫ $label"
  if [ "$(printf '%s' "$full" | python3 -c 'import sys; print(len(sys.stdin.read()))')" -gt "$SLOT" ]; then
    emit "$(marquee "$full")" "$tip" "playing"
  else
    emit "$(fit "$full")" "$tip" "playing"
  fi
  ;;
Paused)
  # collapsed: the bar gets its space back; the tooltip keeps the track.
  emit "⏸" "$tip (paused)" "paused"
  ;;
*)
  emit "♫" "nothing playing — the wired is quiet (click to open navi-nowplaying)" "idle"
  ;;
esac
