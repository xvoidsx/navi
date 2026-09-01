#!/usr/bin/env bash
# navi-quake
# opens up a terminal or agent interface which can be put away and brought back with a keybind
# alt+A for agent / alt+shift+A for terminal!
set -u
export PATH="$HOME/.local/bin:$HOME/.opencode/bin:$HOME/.local/share/pi-node/node-v22.23.1-linux-x64/bin:/usr/local/bin:/usr/bin:/bin:$PATH"
# - - agents - -
PI="pi"
OMP="omp"
OC="opencode"
CX="codex"
AGY="agy"
# - - - - - - - 
APP_ID="navi-quake"

case "${1:-}" in
    agent)
        COMMAND=("$OC")
        ;;

    terminal)
        COMMAND=("${SHELL:-/bin/bash}")
        ;;

    *)
        echo "Usage: navi-quake {agent|terminal}"
        exit 1
        ;;
esac

# If the quake terminal already exists, toggle it.
if swaymsg -t get_tree | grep -q "\"app_id\": \"$APP_ID-$1\""; then
    swaymsg "[app_id=\"$APP_ID-$1\"] scratchpad show"
    exit 0
fi

WINDOW_ID="$APP_ID-$1"

# Spawn the requested surface.
alacritty \
    --class "$WINDOW_ID" \
    --title "$WINDOW_ID" \
    --working-directory "$HOME" \
    -e "${COMMAND[@]}" &

# Give Wayland/Sway a moment to create the surface.
for _ in {1..100}; do
    if swaymsg -t get_tree | grep -q "\"app_id\": \"$WINDOW_ID\""; then
        break
    fi
    sleep 0.05
done

# Turn it into the quake surface.
swaymsg "[app_id=\"$WINDOW_ID\"] floating enable"
swaymsg "[app_id=\"$WINDOW_ID\"] border none"
swaymsg "[app_id=\"$WINDOW_ID\"] resize set width 100 ppt height 40 ppt"
swaymsg "[app_id=\"$WINDOW_ID\"] move position 0 0"

# Put it away until summoned.
swaymsg "[app_id=\"$WINDOW_ID\"] move scratchpad"

# And summon it.
swaymsg "[app_id=\"$WINDOW_ID\"] scratchpad show"
