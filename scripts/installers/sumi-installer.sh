#!/usr/bin/env bash
# sumi installer
# ( sumi 墨 is xvoidsx's Electron writing app — distraction-free Markdown, native open/save )
# ( https://github.com/xvoidsx/sumi )
set -euo pipefail
x="= = = = = sumi installer = = = = ="

PRIV="sudo"
command -v doas >/dev/null 2>&1 && PRIV="doas"

VERSION="v1.0.0"
VER="${VERSION#v}"
URL="https://github.com/xvoidsx/sumi/releases/download/${VERSION}/sumi_${VER}_amd64.deb"

main() {
    echo "$x"; sleep 1
    tmp="$(mktemp --suffix=.deb)"
    trap 'rm -f "$tmp"' EXIT
    # x86_64 only for now — the release workflow builds on ubuntu-latest;
    # aarch64 warns and skips until an ARM build lands.
    if [ "$(uname -m)" != "x86_64" ]; then
        echo "  sumi has no $(uname -m) build yet — skipping" >&2
        exit 1
    fi
    echo "  downloading sumi ${VERSION}..."
    curl -fsSL -o "$tmp" "$URL"
    echo "  installing sumi_${VER}_amd64.deb..."
    $PRIV dpkg -i "$tmp"
    dpkg -s sumi >/dev/null 2>&1 \
        || { echo "  sumi did not install — dpkg failed" >&2; exit 1; }
    echo "  sumi installed. launch it from your app menu."
}
# - - - - - |
main
