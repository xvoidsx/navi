#!/usr/bin/env bash
# navi-lain — the Lain launcher for Hermes
#
# Hermes (Nous Research, MIT) is the engine; Lain is the navi experience.
# This wrapper:
#   1. Shows the LAIN ASCII mark in nightshadeNeon on startup
#   2. Ensures the Lain identity layer is in place (~/.config/hermes/lain)
#   3. Launches hermes with the user's chosen model/provider
#
# The binary stays `hermes`. This is the experience layer, not a fork.
##################################
set -e

LAIN_DIR="$HOME/.config/hermes/lain"
REPO_LAIN="/usr/share/navi/wired/lain"

# ensure the identity layer exists (first-run or repaired)
if [ -d "$REPO_LAIN" ]; then
    # theme/banner assets
    if [ ! -f "$LAIN_DIR/ascii-lain.txt" ]; then
        mkdir -p "$LAIN_DIR"
        cp -r "$REPO_LAIN/." "$LAIN_DIR/"
    fi
    # Hermes-native persona — never overwrite the user's own.
    # SOUL.md at ~/.hermes/; USER.md and MEMORY.md at ~/.hermes/memories/
    mkdir -p "$HOME/.hermes" "$HOME/.hermes/memories"
    if [ -f "$REPO_LAIN/SOUL.md" ] && [ ! -f "$HOME/.hermes/SOUL.md" ]; then
        cp "$REPO_LAIN/SOUL.md" "$HOME/.hermes/SOUL.md"
    fi
    for _f in USER.md MEMORY.md; do
        if [ -f "$REPO_LAIN/$_f" ] && [ ! -f "$HOME/.hermes/memories/$_f" ]; then
            cp "$REPO_LAIN/$_f" "$HOME/.hermes/memories/$_f"
        fi
    done
    unset _f
    # nightshadeNeon skin — always refresh (it's ours, not the user's)
    if [ -f "$REPO_LAIN/skins/nightshadeNeon.yaml" ]; then
        mkdir -p "$HOME/.hermes/skins"
        cp "$REPO_LAIN/skins/nightshadeNeon.yaml" "$HOME/.hermes/skins/"
    fi
fi

# the mark — neon pink on dark, like everything else in the wired
if [ -f "$LAIN_DIR/ascii-lain.txt" ] && [ -t 1 ]; then
    printf '\033[38;2;255;16;240m'
    cat "$LAIN_DIR/ascii-lain.txt"
    printf '\033[0m'
    echo "  Lain on Hermes — xvoidsx's assistant, running on your machine."
    echo ""
fi

# hand off to the engine
# The persona comes from ~/.hermes/SOUL.md (native Hermes) — every hermes
# invocation is Lain. This wrapper adds the LAIN banner on top.
exec hermes "$@"
