#!/usr/bin/env bash
# cyberia installer
# ( cyberia is xvoidsx's nightshadeNeon Matrix client — a friendly fork of pkulak's matui )
# ( https://github.com/xvoidsx/cyberia )
set -euo pipefail
x="= = = = = cyberia installer = = = = ="

PRIV="sudo"
command -v doas >/dev/null 2>&1 && PRIV="doas"

VERSION="cyberia-v1.1.2"
URL="https://github.com/xvoidsx/cyberia/releases/download/${VERSION}/cyberia"

main() {
    echo "$x"; sleep 1
    tmp="$(mktemp)"
    trap 'rm -f "$tmp"' EXIT
    # x86_64 only for now — the release workflow builds on ubuntu-latest;
    # aarch64 warns and skips until an ARM build lands.
    if [ "$(uname -m)" != "x86_64" ]; then
        echo "  cyberia has no $(uname -m) build yet — skipping" >&2
        exit 1
    fi
    echo "  downloading cyberia ${VERSION}..."
    curl -fsSL -o "$tmp" "$URL"
    echo "  installing to /usr/local/bin/cyberia..."
    $PRIV install -m 0755 "$tmp" /usr/local/bin/cyberia
    command -v cyberia >/dev/null 2>&1 \
        || { echo "  cyberia did not land on PATH — install failed" >&2; exit 1; }
    echo "  cyberia installed. run 'cyberia' to sign in."
}
# - - - - - |
main
