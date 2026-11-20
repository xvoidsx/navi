#!/usr/bin/env bash
# navi-float-mode.sh — toggle the "float" (normie) mode.
#
# Float mode: every window floats with a titlebar, gaps go off, and new
# windows open floating. Toggle again and everything goes back to tiling.
# Reversible at runtime; nothing here edits any config file.
#
# Why criteria, not a tree walk: sway tree nodes include "floating_con",
# and any walker that only accepts "con" silently drops every floating
# window. The [tiling]/[floating] criteria sidestep that trap entirely.
#
# Known edges (verify on hardware):
# - The runtime for_window rule (new windows float) is cleared by
#   `swaymsg reload` on the way out. If it ever persists, new windows
#   will keep floating after toggling off — say so and we'll rethink.
# - Scratchpad windows are floating windows in a hidden workspace, so
#   the "off" unfloat can pull a hidden one onto the screen. The
#   scratchpad is empty on a fresh test (its binds are new in this same
#   change); if it bites, v2 walks the tree and excludes __i3_scratch.
#
# Usage: navi-float-mode.sh [on|off] — no argument toggles.
set -u

RUNDIR="${XDG_RUNTIME_DIR:-/tmp}"
[ -d "$RUNDIR" ] || RUNDIR="/tmp"
STATE="$RUNDIR/navi-float-mode"
LOG="${HOME}/.local/share/navi/float-mode.log"

note() { # note <message> — one log line + a panel-visible note
  mkdir -p "$(dirname "$LOG")"
  printf '%s %s\n' "$(date '+%F %T')" "$1" >> "$LOG"
  notify-send 'wired helper' "$1"
}

float_on() {
  swaymsg '[tiling] floating enable'
  swaymsg '[floating] border normal'
  swaymsg default_border normal
  swaymsg 'gaps inner all set 0; gaps outer all set 0'
  swaymsg 'for_window [class=".*"] floating enable'
  touch "$STATE"
  note 'Float mode on — every window floats now. Super+Shift+M goes back.'
}

float_off() {
  swaymsg reload
  swaymsg '[floating] floating disable'
  swaymsg default_border pixel 3
  swaymsg '[tiling] border pixel 2'
  swaymsg 'gaps inner all set 6; gaps outer all set 9'
  rm -f "$STATE"
  note 'Float mode off — back to tiling.'
}

case "${1:-}" in
  on)  float_on ;;
  off) float_off ;;
  *)   if [ -f "$STATE" ]; then float_off; else float_on; fi ;;
esac
