#!/usr/bin/env bash
#
# waybar custom/update module — one icon, three channels.
#
# Asks navi-update --check what is available (navi files, Debian
# packages, Flatpaks) and emits waybar JSON. Results are cached for
# 15 minutes so the bar stays cheap; click to run the update.
#
# This is the shell v1. The eiri-era navi mods will replace it with a
# Go/bubbletea TUI like the rest of the mods lineup.

set -uo pipefail

CACHE_DIR="$HOME/.cache/navi"
CACHE="$CACHE_DIR/update-check.cache"
CACHE_TTL=900   # 15 minutes

json_escape() { python3 -c 'import json,sys; print(json.dumps(sys.stdin.read().strip()))' <<<"$1"; }

emit() { # <text> <tooltip> <class>
  printf '{"text": %s, "tooltip": %s, "class": %s}\n' \
    "$(json_escape "$(stack_text "$1")")" "$(json_escape "$2")" "$(json_escape "$3")"
}

# stack_text <"icon value"> — value first, always: "value\nicon" on a
# vertical bar (text over symbol, top-to-bottom like the clock), "value
# icon" on a horizontal bar. Single glyphs pass through unchanged.
# The bar position is navi-bar's state file; a missing file means top.
stack_text() {
  local pos icon rest
  pos=$(cat "${NAVI_BAR_STATE:-$HOME/.local/share/navi/waybar/position}" 2>/dev/null | head -1)
  icon="${1%% *}"; rest="${1#* }"
  if [ "$rest" = "$1" ]; then printf '%s' "$1"; return; fi
  case "$pos" in
    left|right) printf '%s\n%s' "$rest" "$icon" ;;
    *) printf '%s %s' "$rest" "$icon" ;;
  esac
}

mkdir -p "$CACHE_DIR"
now="$(date +%s)"
if [ -f "$CACHE" ] && [ "$(( now - $(stat -c %Y "$CACHE" 2>/dev/null || echo 0) ))" -lt "$CACHE_TTL" ]; then
  # shellcheck disable=SC1090
  source "$CACHE"
else
  if command -v navi-update >/dev/null 2>&1; then
    navi-update --check > "$CACHE" 2>/dev/null || \
      printf 'NAVI_UPDATES=0\nNAVI_NEW=\nAPT_UPDATES=0\nFLATPAK_UPDATES=0\n' > "$CACHE"
  else
    printf 'NAVI_UPDATES=0\nNAVI_NEW=\nAPT_UPDATES=0\nFLATPAK_UPDATES=0\n' > "$CACHE"
  fi
  # shellcheck disable=SC1090
  source "$CACHE"
fi

NAVI_UPDATES="${NAVI_UPDATES:-0}"; NAVI_NEW="${NAVI_NEW:-}"
APT_UPDATES="${APT_UPDATES:-0}"; FLATPAK_UPDATES="${FLATPAK_UPDATES:-0}"

total=$(( NAVI_UPDATES + APT_UPDATES + FLATPAK_UPDATES ))

if [ "$total" -eq 0 ]; then
  emit "✓" "system up to date" "up-to-date"
  exit 0
fi

parts=()
[ "$NAVI_UPDATES" -eq 1 ] && parts+=("navi $NAVI_NEW")
[ "$APT_UPDATES" -gt 0 ] && parts+=("$APT_UPDATES debian")
[ "$FLATPAK_UPDATES" -gt 0 ] && parts+=("$FLATPAK_UPDATES flatpak")
tip="$(IFS=' · '; echo "${parts[*]}") — click to update"

emit "↓ $total" "$tip" "updates-available"
