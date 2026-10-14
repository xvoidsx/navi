#!/usr/bin/env bash
# volume.sh — polybar volume module for the wired.
#
# Waybar parity: waybar's custom/volume shows the volume and nothing else
# ("🔊 74%" / "🔇 mute") — the now-playing experience lives in the navi-audio
# mod behind a click, not in the bar text. This module matches that exactly.
#
# One-shot (polybar polls it every 2s).
set -uo pipefail

vol="$(pamixer --get-volume 2>/dev/null || echo "")"
mute="$(pamixer --get-mute 2>/dev/null || echo "false")"

if [ "$mute" = "true" ]; then
    text="🔇 mute"
elif [ -n "$vol" ]; then
    text="🔊 ${vol}%"
else
    text="🔇 no audio"
fi

printf '%s\n' "$text"
