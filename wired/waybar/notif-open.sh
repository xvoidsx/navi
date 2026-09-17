#!/usr/bin/env bash
#
# notif-open.sh — open navi-notifs in a floating terminal.
#
# The sway config carries a for_window [title="navi-notifs"] rule for stock
# configs, but customized configs don't get it (the updater preserves them
# by design). So the launcher enforces floating itself: open the window,
# then tell the compositor to float + center it. Works on sway and i3;
# anywhere else it just opens the terminal like before.

set -uo pipefail

alacritty --title "navi-notifs" -e navi-notifs &
sleep 0.5

if [ -n "${SWAYSOCK:-}" ] && command -v swaymsg >/dev/null 2>&1; then
  swaymsg '[title="navi-notifs"] floating enable, resize set 620 760, move position center' >/dev/null 2>&1
elif command -v i3-msg >/dev/null 2>&1; then
  i3-msg '[title="navi-notifs"] floating enable' >/dev/null 2>&1
fi
