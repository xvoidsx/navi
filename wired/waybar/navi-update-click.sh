#!/usr/bin/env bash
#
# navi-update-click — waybar custom/update on-click entry point.
#
# why this exists: the old on-click ran `alacritty -e navi-update`
# directly, and navi-update runs under `set -e`. any failure — a network
# blip during git fetch, an apt lock, anything — exited the script, and
# alacritty -e closes the window the instant its child exits. the error
# was invisible and the click looked like a crash. this wrapper tees the
# whole run to a log and keeps the window open on failure so the actual
# error stays on screen.
#
# usage (from waybar config.jsonc):
#   mod-open.sh "navi-update" /usr/share/navi/wired/waybar/navi-update-click.sh
#
# note: only stdout is piped to tee — stdin stays on the terminal, so
# navi-update's own end-of-run "press any key" pause still works.

LOG_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/navi"
LOG_FILE="$LOG_DIR/navi-update-click.log"

mkdir -p "$LOG_DIR" 2>/dev/null || true
printf '=== navi-update click run: %s ===\n' "$(date '+%F %T')" | tee "$LOG_FILE"

# no `set -e` here on purpose: we want to catch the failure, not die on it.
navi-update 2>&1 | tee -a "$LOG_FILE"
rc="${PIPESTATUS[0]}"
printf '=== exit: %s ===\n' "$rc" | tee -a "$LOG_FILE"

if [ "$rc" -ne 0 ] && [ -t 0 ]; then
  echo
  echo "navi-update failed (exit $rc) — full log: $LOG_FILE"
  read -r -n1 -s -p "press any key to close…" _
  echo
fi
exit "$rc"
