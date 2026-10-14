#!/usr/bin/env bash
# navi-dmenu-run — dmenu launcher with navi awareness
# Like dmenu_run, but navi-* tools float via mod-open.sh, just like from rofi.
# Falls back to direct exec for everything else.
set -u

MOD_OPEN="/usr/share/navi/wired/waybar/mod-open.sh"
DMENU="dmenu -fn NotoSans-10 -i -nb black -nf pink -sb green -sf red"

# Build the candidate list: PATH binaries
BINLIST=$(printf '%s' "$PATH" | tr ':' '\n' | xargs -I{} ls -1 {} 2>/dev/null | sort -u)

CHOICE=$(printf '%s\n' "$BINLIST" | $DMENU -p "run: ") || exit 0
[ -z "$CHOICE" ] && exit 0

# Split command from args
BASE=${CHOICE%% *}

# Navi tools float through mod-open (it handles sizing + terminal)
case "$BASE" in
  navi-imprint)
    # needs root for block devices — match the .desktop entry
    if [ -x "$MOD_OPEN" ]; then
      # shellcheck disable=SC2086
      exec $MOD_OPEN "$BASE" doas $CHOICE
    fi
    ;;
  navi-*)
    if [ -x "$MOD_OPEN" ]; then
      # shellcheck disable=SC2086
      exec $MOD_OPEN "$BASE" $CHOICE
    fi
    ;;
esac

# Everything else: classic dmenu_run behavior
# shellcheck disable=SC2086
exec $CHOICE
