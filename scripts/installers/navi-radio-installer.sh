#!/usr/bin/env bash
# navi-radio installer
# ( navi-radio is xvoidsx's Electron music app — internet radio + local music library )
# ( https://github.com/xvoidsx/navi-radio )
set -euo pipefail
x="= = = = = navi-radio installer = = = = ="

PRIV="sudo"
command -v doas >/dev/null 2>&1 && PRIV="doas"

VERSION="v1.0.0"
VER="${VERSION#v}"
URL="https://github.com/xvoidsx/navi-radio/releases/download/${VERSION}/navi-radio_${VER}_amd64.deb"

main() {
    echo "$x"; sleep 1
    tmp="$(mktemp --suffix=.deb)"
    trap 'rm -f "$tmp"' EXIT
    # x86_64 only for now — the release workflow builds on ubuntu-latest;
    # aarch64 warns and skips until an ARM build lands.
    if [ "$(uname -m)" != "x86_64" ]; then
        echo "  navi-radio has no $(uname -m) build yet — skipping" >&2
        exit 1
    fi
    echo "  downloading navi-radio ${VERSION}..."
    curl -fsSL -o "$tmp" "$URL"
    echo "  installing navi-radio_${VER}_amd64.deb..."
    $PRIV dpkg -i "$tmp"
    dpkg -s navi-radio >/dev/null 2>&1 \
        || { echo "  navi-radio did not install — dpkg failed" >&2; exit 1; }
    echo "  navi-radio installed. launch it from your app menu."
}
# - - - - - |
main
