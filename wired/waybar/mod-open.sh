#!/usr/bin/env bash
#
# mod-open.sh — open a navi mod in a floating terminal.
#
# usage: mod-open.sh <window-title> <command> [args...]
#
# standing rule: navi mods always float. a for_window rule in the shipped
# sway config can't be relied on — customized configs are preserved by the
# updater, by design — and sleep-then-float races slow machines (the
# window isn't mapped yet when the single swaymsg fires). so this does
# both halves:
#   1. install a runtime for_window rule BEFORE launching, so the
#      compositor floats the window the moment it appears — no race;
#   2. after launching, poll the tree briefly and float directly once
#      the window shows up, in case the runtime rule wasn't honored.
# anywhere without sway/i3 it just opens the terminal like before.

set -uo pipefail

TITLE="${1:?usage: mod-open.sh <window-title> <command> [args...]}"
shift
[ "$#" -ge 1 ] || { echo "usage: mod-open.sh <window-title> <command> [args...]" >&2; exit 1; }

FLOAT_CMDS="floating enable, resize set 640 760, move position center"

is_sway() { [ -n "${SWAYSOCK:-}" ] && command -v swaymsg >/dev/null 2>&1; }
is_i3()   { command -v i3-msg >/dev/null 2>&1; }

if is_sway; then
  # runtime rule: catches windows created after this point. harmless if
  # sway declines it (the poll below is the backstop).
  swaymsg "for_window [title=\"$TITLE\"] $FLOAT_CMDS" >/dev/null 2>&1
elif is_i3; then
  i3-msg "for_window [title=\"$TITLE\"] floating enable" >/dev/null 2>&1
fi

alacritty --title "$TITLE" -e "$@" &
term_pid=$!

if is_sway; then
  # backstop: slow iron can take seconds to map the window. poll the
  # tree (up to ~6s) and float it directly once it appears.
  for _ in $(seq 1 30); do
    if swaymsg -t get_tree 2>/dev/null | grep -qF "\"name\": \"$TITLE\""; then
      swaymsg "[title=\"$TITLE\"] $FLOAT_CMDS" >/dev/null 2>&1
      break
    fi
    kill -0 "$term_pid" 2>/dev/null || break
    sleep 0.2
  done
elif is_i3; then
  sleep 0.6
  i3-msg "[title=\"$TITLE\"] floating enable" >/dev/null 2>&1
fi
