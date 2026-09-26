#!/bin/sh
# navishot - navi screenshot picker (v1)
# Super+P pops a mode menu; every mode saves to $HOME/Pictures/Screenshots
# and copies the shot to the clipboard.
#   Select Area        - drag a region
#   Select Window      - click a window (Wayland) / capture focused window (X11)
#   Grab Entire Screen - all outputs
set -eu

DIR="$HOME/Pictures/Screenshots"
mkdir -p "$DIR"

MODE=$(printf "Select Area\nSelect Window\nGrab Entire Screen" \
    | dmenu -fn "NotoSans-10" -i -nb black -nf pink -sb green -sf red -p "navishot: ") || exit 0
[ -z "$MODE" ] && exit 0

FILE="$DIR/$(date -Ins).png"

if [ -n "${WAYLAND_DISPLAY:-}" ]; then
    # ---- Wayland (sway) via grimshot: save + copy + notify in one ----
    case "$MODE" in
        "Select Area")        grimshot savecopy area   "$FILE" --notify ;;
        "Select Window")      grimshot savecopy window "$FILE" --notify ;;
        "Grab Entire Screen") grimshot savecopy screen "$FILE" --notify ;;
    esac
else
    # ---- X11 (i3) ----
    case "$MODE" in
        "Select Area")
            flameshot gui -p "$DIR" ;;
        "Select Window")
            maim -u -i "$(xdotool getactivewindow)" "$FILE"
            xclip -selection clipboard -t image/png -i "$FILE"
            notify-send "navishot" "Window screenshot saved to Pictures/Screenshots." ;;
        "Grab Entire Screen")
            flameshot full -c -p "$DIR"
            notify-send "navishot" "Screenshot saved to Pictures/Screenshots." ;;
    esac
fi
