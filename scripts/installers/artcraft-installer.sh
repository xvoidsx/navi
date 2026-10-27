#!/usr/bin/env bash
# ArtCraft Launcher installer
# ( installs the ArtCraft Launcher — one place to install, update and
#   open all 13 Craft apps — from the official .deb published by the
#   ArtCraft team (storytold/craft-launcher) on GitHub )
#######################################################
set -e
x="= = = = = ArtCraft Launcher installer = = = = ="

install_artcraft() {
    # the .deb ships for x86_64 and aarch64 — map dpkg's arch names
    # onto the release asset names (amd64 -> x86_64, arm64 -> aarch64)
    local arch asset_arch
    arch="$(dpkg --print-architecture)"
    case "$arch" in
        amd64) asset_arch="x86_64" ;;
        arm64) asset_arch="aarch64" ;;
        *)
            echo "  No ArtCraft Launcher .deb for architecture '$arch' — the releases carry x86_64 and aarch64 builds."
            return 1
            ;;
    esac
    # resolve the latest release's .deb — the asset name carries the
    # version (artcraft-launcher-<ver>-linux-<arch>.deb), so there is
    # no fixed URL
    local api="https://api.github.com/repos/storytold/craft-launcher/releases/latest"
    local url
    url=$(curl -fsSL "$api" | grep -o '"browser_download_url": "[^"]*"' | grep "linux-${asset_arch}\\.deb" | head -1 | cut -d'"' -f4)
    if [ -z "$url" ]; then
        echo "  Could not resolve the latest ArtCraft Launcher .deb from GitHub — check your connection and try again."
        return 1
    fi
    local tmp
    tmp=$(mktemp -d)
    echo "  Downloading ${url##*/} ..."
    curl -fL "$url" -o "$tmp/${url##*/}"
    # verify against the release's published checksums before installing
    curl -fsSL "https://github.com/storytold/craft-launcher/releases/latest/download/SHA256SUMS.txt" -o "$tmp/SHA256SUMS.txt" 2>/dev/null || true
    if [ -s "$tmp/SHA256SUMS.txt" ] && grep -q "${url##*/}" "$tmp/SHA256SUMS.txt"; then
        (cd "$tmp" && grep "${url##*/}" SHA256SUMS.txt > SHA256SUMS.check && sha256sum -c SHA256SUMS.check) || {
            echo "  Checksum mismatch for ${url##*/} — refusing to install. Try again, or fetch the .deb by hand."
            rm -rf "$tmp"
            return 1
        }
    else
        echo "  (could not fetch the release checksums — installing unverified)"
    fi
    # apt (unlike dpkg -i) pulls the .deb's dependencies itself
    doas apt install -y "$tmp/${url##*/}"
    rm -rf "$tmp"
}

main() {
  echo "$x"
  sleep 1
  install_artcraft
  echo "  ArtCraft Launcher has been installed - you can now find it in your navi launcher."
}
# XXX - - - entry
main
