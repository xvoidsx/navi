#!/usr/bin/env bash
# matui installer
# ( matui is a TUI Matrix client )
# ( https://github.com/pkulak/matui )
set -euo pipefail
x="= = = = = matui installer = = = = ="

PRIV="sudo"
command -v doas >/dev/null 2>&1 && PRIV="doas"

VERSION="v1.0.2"
URL="https://github.com/pkulak/matui/releases/download/${VERSION}/matui-x86_64-unknown-linux-gnu.tar.gz"

main() {
    echo "$x"; sleep 1
    tmp="$(mktemp -d)"
    trap 'rm -rf "$tmp"' EXIT
    echo "  downloading matui ${VERSION}..."
    curl -fsSL -o "$tmp/matui.tar.gz" "$URL"
    echo "  extracting..."
    tar -xzf "$tmp/matui.tar.gz" -C "$tmp"
    # the tarball contains the matui binary at its root
    bin="$(find "$tmp" -maxdepth 2 -name matui -type f | head -1)"
    [ -n "$bin" ] || { echo "  matui binary not found in tarball" >&2; exit 1; }
    echo "  installing to /usr/local/bin/matui..."
    $PRIV install -m 0755 "$bin" /usr/local/bin/matui
    command -v matui >/dev/null 2>&1 \
        || { echo "  matui did not land on PATH — install failed" >&2; exit 1; }
    echo "  matui installed. run 'matui' to sign in."
}
# - - - - - |
main
