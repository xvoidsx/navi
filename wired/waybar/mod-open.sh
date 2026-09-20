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
#   2. after launching, poll the tree briefly and float the window by PID
#      (immune to the TUI renaming the window) in case the rule didn't
#      take.
# the compositor is probed, not guessed: env vars lie (stale SWAYSOCK,
# i3-msg installed while running sway). a get_version round-trip doesn't.
# anywhere without sway/i3 it just opens the terminal like before.

set -uo pipefail

TITLE="${1:?usage: mod-open.sh <window-title> <command> [args...]}"
shift
[ "$#" -ge 1 ] || { echo "usage: mod-open.sh <window-title> <command> [args...]" >&2; exit 1; }

FLOAT_CMDS="floating enable, resize set 640 760, move position center"
LOG_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/navi"
LOG_FILE="$LOG_DIR/mod-open.log"

log() {
  mkdir -p "$LOG_DIR" 2>/dev/null || true
  printf '%s mod-open: %s\n' "$(date '+%F %T')" "$*" >>"$LOG_FILE" 2>/dev/null || true
}

COMPOSITOR="none"
if swaymsg -t get_version >/dev/null 2>&1; then
  COMPOSITOR="sway"
elif i3-msg -t get_version >/dev/null 2>&1; then
  COMPOSITOR="i3"
fi

if [ "$COMPOSITOR" = "sway" ]; then
  # runtime rule: catches windows created after this point. harmless if
  # the title never matches (the pid backstop below is the net).
  swaymsg "for_window [title=\"$TITLE\"] $FLOAT_CMDS" >/dev/null 2>&1
elif [ "$COMPOSITOR" = "i3" ]; then
  i3-msg "for_window [title=\"$TITLE\"] floating enable" >/dev/null 2>&1
fi

# Agent keys: panel/rofi-launched apps never see ~/.bashrc, so inject the
# system-wide key store here - every mod (and anything else opened through
# this launcher) inherits it. Managed by Navi Agent Configuration.
AGENTS_ENV="${XDG_CONFIG_HOME:-$HOME/.config}/navi/agents.env"
if [ -f "$AGENTS_ENV" ]; then
  set -a; . "$AGENTS_ENV" 2>/dev/null; set +a
fi

alacritty --title "$TITLE" -e "$@" &
term_pid=$!

if [ "$COMPOSITOR" = "sway" ]; then
  # backstop: slow iron can take seconds to map the window. match by PID,
  # not title — the pid can't be renamed out from under us. up to ~6s.
  # the [^0-9] anchor keeps pid 1234 from matching 12345.
  floated="no"
  for _ in $(seq 1 12); do
    if swaymsg -t get_tree 2>/dev/null | grep -q "\"pid\": *${term_pid}[^0-9]"; then
      swaymsg "[pid=$term_pid] $FLOAT_CMDS" >/dev/null 2>&1
      floated="yes"
      break
    fi
    kill -0 "$term_pid" 2>/dev/null || break
    sleep 0.5
  done
  log "title='$TITLE' compositor=sway pid=$term_pid floated=$floated"
elif [ "$COMPOSITOR" = "i3" ]; then
  sleep 0.6
  i3-msg "[title=\"$TITLE\"] floating enable" >/dev/null 2>&1
  log "title='$TITLE' compositor=i3 pid=$term_pid"
else
  log "title='$TITLE' compositor=none pid=$term_pid (plain open)"
fi
