#!/usr/bin/env bash
# backlight.sh — polybar backlight module (waybar parity).
#
# Shows "{percent}% {icon}" like waybar's backlight module, using the same
# Material brightness icons. Scroll adjusts via brightnessctl.
# One-shot; polybar polls it.
set -uo pipefail

# 9 Material brightness icons, dim -> bright (same set as waybar)
ICONS=("" "" "" "" "" "" "" "" "")

if ! command -v brightnessctl >/dev/null 2>&1; then
    exit 0
fi

pct="$(brightnessctl -m 2>/dev/null | cut -d, -f4 | tr -d '%')"
[ -n "$pct" ] || exit 0

idx=$(( pct * 8 / 100 ))
[ "$idx" -gt 8 ] && idx=8
[ "$idx" -lt 0 ] && idx=0

printf '%s%% %s\n' "$pct" "${ICONS[$idx]}"
