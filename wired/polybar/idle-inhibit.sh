#!/usr/bin/env bash
# idle-inhibit.sh — polybar idle-inhibit toggle (waybar parity).
#
# Waybar's idle_inhibitor toggles Wayland idle inhibition; on X11 the
# equivalent is the screensaver/DPMS. Click toggles between inhibited
# (eye open) and normal (eye closed), persisted in a state file so it
# survives bar restarts.
set -uo pipefail

STATE="${XDG_CACHE_HOME:-$HOME/.cache}/navi-idle-inhibit"

if [ "${1:-}" = "toggle" ]; then
    if [ -f "$STATE" ]; then
        rm -f "$STATE"
        xset s on +dpms 2>/dev/null || true
        notify-send "wired helper" "Idle inhibition OFF — the screen may sleep." 2>/dev/null || true
    else
        touch "$STATE"
        xset s off -dpms 2>/dev/null || true
        notify-send "wired helper" "Idle inhibition ON — staying awake." 2>/dev/null || true
    fi
fi

# re-assert the state every poll (cheap, and heals external xset changes)
if [ -f "$STATE" ]; then
    xset s off -dpms 2>/dev/null || true
    printf '\n'   # activated — eye open
else
    printf '\n'   # deactivated — eye closed
fi
