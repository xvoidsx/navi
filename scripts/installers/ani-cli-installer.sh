#!/usr/bin/env bash
# ani-cli installer
# ( ani-cli is a CLI tool for watching anime )
# ( https://github.com/pystardust/ani-cli )
set -euo pipefail
x="= = = = = ani-cli installer = = = = ="

PRIV="sudo"
command -v doas >/dev/null 2>&1 && PRIV="doas"

command -v git >/dev/null 2>&1 || { echo "  git is required to install ani-cli" >&2; exit 1; }

main() {
    echo "$x"; sleep 1
    tmp="$(mktemp -d)"
    trap 'rm -rf "$tmp"' EXIT
    echo "  cloning ani-cli..."
    git clone --depth 1 https://github.com/pystardust/ani-cli.git "$tmp/ani-cli"
    echo "  installing to /usr/local/bin/ani-cli..."
    $PRIV install -m 0755 "$tmp/ani-cli/ani-cli" /usr/local/bin/ani-cli
    # never claim success unless the binary actually landed on PATH —
    # a silent no-op here is exactly how this installer broke before.
    command -v ani-cli >/dev/null 2>&1 \
        || { echo "  ani-cli did not land on PATH — install failed" >&2; exit 1; }
    echo "  ani-cli installed."
}
# - - - - - |
main
