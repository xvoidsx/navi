#!/usr/bin/env bash
# navi-Qx
# navi's 'quake' mode, for the X11 session.
set -u

# pi ships its own node under a versioned dir; resolve it without hardcoding the version
_pi_node_dir="$(ls -d "$HOME"/.local/share/pi-node/node-v*-linux-x64 2>/dev/null | sort -V | tail -n 1)"
if [ -n "$_pi_node_dir" ]; then
    PATH="$_pi_node_dir/bin:$PATH"
fi
unset _pi_node_dir
export PATH="$HOME/.local/bin:$HOME/.opencode/bin:/usr/local/bin:/usr/bin:/bin:$PATH"

PI="pi"
OMP="omp"
OC="opencode"
CX="codex"
AGY="agy"

APP_ID="navi-Q"

case "${1:-}" in
    agent)
        COMMAND=("$OC")
        ;;

    terminal)
        COMMAND=("${SHELL:-/bin/bash}")
        ;;

    *)
        echo "Usage: navi-Q-x11 {agent|terminal}"
        exit 1
        ;;
esac

WINDOW_ID="$APP_ID-$1"

# If the quake terminal already exists, toggle it.
if i3-msg -t get_tree | grep -q "\"class\":\"navi-Q-$1\""; then
    i3-msg "[class=\"navi-Q-$1\"] scratchpad show"
    exit 0
fi

# Spawn the requested terminal.
alacritty \
    --class "$WINDOW_ID" \
    --title "$WINDOW_ID" \
    --working-directory "$HOME" \
    -e "${COMMAND[@]}" &

# Give i3 a moment to see the window.
for _ in {1..100}; do
    if i3-msg -t get_tree | grep -q "\"class\":\"$WINDOW_ID\""; then
        break
    fi
    sleep 0.05
done

# Turn it into the quake surface.
i3-msg "[class=\"$WINDOW_ID\"] floating enable"
i3-msg "[class=\"$WINDOW_ID\"] border pixel 0"
i3-msg "[class=\"$WINDOW_ID\"] resize set 100 ppt 40 ppt"
i3-msg "[class=\"$WINDOW_ID\"] move position 0 0"

# Put it away until summoned.
i3-msg "[class=\"$WINDOW_ID\"] move scratchpad"

# And summon it.
i3-msg "[class=\"$WINDOW_ID\"] scratchpad show"
