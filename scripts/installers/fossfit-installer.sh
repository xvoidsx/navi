#!/usr/bin/env bash
# FOSSfit installer
# ( FOSSfit is xvoidsx's Electron fitness app — workouts, weight charts, meditation )
# ( https://github.com/xvoidsx/FOSSfit )
set -euo pipefail
x="= = = = = FOSSfit installer = = = = ="

PRIV="sudo"
command -v doas >/dev/null 2>&1 && PRIV="doas"

VERSION="v2.4.0"
VER="${VERSION#v}"
URL="https://github.com/xvoidsx/FOSSfit/releases/download/${VERSION}/fossfit_${VER}_amd64.deb"

main() {
    echo "$x"; sleep 1
    tmp="$(mktemp --suffix=.deb)"
    trap 'rm -f "$tmp"' EXIT
    # x86_64 only for now — the release workflow builds on ubuntu-latest;
    # aarch64 warns and skips until an ARM build lands.
    if [ "$(uname -m)" != "x86_64" ]; then
        echo "  FOSSfit has no $(uname -m) build yet — skipping" >&2
        exit 1
    fi
    echo "  downloading FOSSfit ${VERSION}..."
    curl -fsSL -o "$tmp" "$URL"
    echo "  installing fossfit_${VER}_amd64.deb..."
    $PRIV dpkg -i "$tmp"
    dpkg -s fossfit >/dev/null 2>&1 \
        || { echo "  FOSSfit did not install — dpkg failed" >&2; exit 1; }
    echo "  FOSSfit installed. launch it from your app menu."
}
# - - - - - |
main
