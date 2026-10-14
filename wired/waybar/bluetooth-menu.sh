#!/usr/bin/env bash
#
# bluetooth-menu.sh — right-click menu for waybar's bluetooth module.
#
# The bar already left-clicks straight into navi-bluetooth, so this menu
# only carries what the bar can't do in one click: flip the radio, and
# reopen the mod. Kept deliberately short — a menu that lists everything
# is a menu nobody reads.
#
#   open navi bluetooth  — the full manager (same as left-click)
#   turn bluetooth on    — power the radio up
#   turn bluetooth off   — power the radio down
#   rescan for devices   — kick off a discovery cycle
#
# Power and discovery go through bluetoothctl rather than navi-bluetooth
# on purpose: they must work even when the manager itself is wedged, which
# is exactly the moment you want this menu.

set -uo pipefail

WB="/usr/share/navi/wired/waybar"

# Fall back to the repo checkout so this works before a deploy too.
if [ ! -x "$WB/mod-open.sh" ] && [ -x "$(dirname "$0")/mod-open.sh" ]; then
  WB="$(cd "$(dirname "$0")" && pwd)"
fi

notify() {
  command -v notify-send >/dev/null 2>&1 && \
    notify-send "bluetooth" "$1" -t 2500 >/dev/null 2>&1
  printf '%s\n' "$1" >&2
}

powered() {
  command -v bluetoothctl >/dev/null 2>&1 || { echo "unknown"; return; }
  bluetoothctl show 2>/dev/null |
    awk -F': *' '/^[[:space:]]*Powered:/ {print tolower($2); exit}'
}

state="$(powered)"
if [ "$state" = "yes" ]; then
  items=("open navi bluetooth" "turn bluetooth off" "rescan for devices")
else
  items=("open navi bluetooth" "turn bluetooth on")
fi

choice="$(printf '%s\n' "${items[@]}" | rofi -dmenu -p "bluetooth" -i 2>/dev/null || true)"
[ -z "$choice" ] && exit 0

case "$choice" in
  "open navi bluetooth")
    if command -v navi-bluetooth >/dev/null 2>&1; then
      exec "$WB/mod-open.sh" "navi bluetooth" navi-bluetooth
    fi
    notify "navi-bluetooth is not installed"
    ;;
  "turn bluetooth on")
    bluetoothctl power on >/dev/null 2>&1 && notify "bluetooth on" \
      || notify "couldn't turn bluetooth on"
    ;;
  "turn bluetooth off")
    bluetoothctl power off >/dev/null 2>&1 && notify "bluetooth off" \
      || notify "couldn't turn bluetooth off"
    ;;
  "rescan for devices")
    bluetoothctl --timeout 5 scan on >/dev/null 2>&1
    notify "scanning for devices…"
    ;;
esac

exit 0