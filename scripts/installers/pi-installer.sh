#!/usr/bin/env bash
# pi installer
# ( pi is the pi agent — xvoidsx is testing it as an alternative Lain runtime )
# ( https://pi.dev )
#
# NOTE: pi's official installer is interactive (it can wait for a keypress
# on /dev/tty). That's why it lives here in navi-extras instead of the
# default install path — run it when you're ready to answer its prompts.
set -euo pipefail
x="= = = = = pi installer = = = = ="

PRIV="sudo"
command -v doas >/dev/null 2>&1 && PRIV="doas"

main() {
    echo "$x"; sleep 1
    if command -v pi >/dev/null 2>&1; then
        echo "  pi already installed at $(command -v pi)"
        exit 0
    fi
    tmp="$(mktemp)"
    trap 'rm -f "$tmp"' EXIT
    echo "  downloading pi installer from pi.dev..."
    curl -fsSL --connect-timeout 20 --max-time 600 \
        --retry 3 --retry-all-errors \
        -o "$tmp" https://pi.dev/install.sh
    echo "  running pi installer (it may ask you questions)..."
    $PRIV sh "$tmp"
    command -v pi >/dev/null 2>&1 \
        || { echo "  pi did not land on PATH — install may have failed" >&2; exit 1; }
    echo "  pi installed at $(command -v pi)."
}
# - - - - - |
main
