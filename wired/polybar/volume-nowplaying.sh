#!/usr/bin/env bash
# volume-nowplaying.sh — polybar volume module for the wired.
#
# Like waybar's custom/volume, the now-playing info lives IN the volume
# module, not as its own bar slot: "🔊 74% · Artist – Title" while music
# plays, plain "🔊 74%" when the wired is quiet.
#
# One-shot (polybar polls it); the nowplaying text comes through the
# waybar-json adapter so waybar-nowplaying.sh stays the single source.
set -uo pipefail

ADAPTER="/usr/share/navi/wired/polybar/waybar-json.sh"
NOWPLAYING="/usr/share/navi/wired/waybar/waybar-nowplaying.sh"

vol="$(pamixer --get-volume 2>/dev/null || echo "")"
mute="$(pamixer --get-mute 2>/dev/null || echo "false")"

if [ "$mute" = "true" ]; then
    text="🔇 mute"
elif [ -n "$vol" ]; then
    text="🔊 ${vol}%"
else
    text="🔇 no audio"
fi

if [ -f "$ADAPTER" ] && [ -x "$NOWPLAYING" ]; then
    np="$("$ADAPTER" "$NOWPLAYING" 2>/dev/null)"
    # waybar-nowplaying emits "♫" alone when idle — only append real tracks
    if [ -n "$np" ] && [ "$np" != "♫" ]; then
        text="$text · $np"
    fi
fi

printf '%s\n' "$text"
