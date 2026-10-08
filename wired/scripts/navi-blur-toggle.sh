#!/usr/bin/env bash
# navi-blur-toggle
#     by rav3ndust.xyz (xvoidsx)
# Flips picom background blur on/off in the X11 session and restarts picom.
# Blur is the most GPU-expensive compositor effect — this is the quick
# escape hatch for weak hardware. Everything else (shadows, corners,
# dimming, fading) stays as configured.
set -e

CONF="$HOME/.config/picom/picom.conf"

if [ ! -f "$CONF" ]; then
  notify-send "wired helper" "No picom config found at ~/.config/picom/picom.conf"
  exit 1
fi

if grep -q "background = true" "$CONF"; then
  sed -i 's/background = true/background = false/' "$CONF"
  notify-send "wired helper" "Compositor blur OFF — easy on the GPU."
else
  sed -i 's/background = false/background = true/' "$CONF"
  notify-send "wired helper" "Compositor blur ON — frosted glass engaged."
fi

# Restart picom so the change takes effect
pkill -x picom 2>/dev/null || true
sleep 0.3
picom --config "$CONF" >/dev/null 2>&1 &
