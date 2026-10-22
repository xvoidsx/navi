#!/usr/bin/env bash
# Obsidian installer
# ( installs Obsidian, the local-first Markdown knowledge base,
#   from the official .deb published by Dynalist Inc. on GitHub )
#######################################################
set -e
x="= = = = = Obsidian installer = = = = ="

install_obsidian() {
    # the official .deb is amd64-only — there is no arm64 .deb
    if [ "$(dpkg --print-architecture)" != "amd64" ]; then
        echo "  Obsidian's official .deb is amd64-only — on this architecture, use the Flatpak (md.obsidian.Obsidian) or the arm64 AppImage instead."
        return 1
    fi
    # resolve the latest release's .deb — the asset name carries the
    # version (obsidian_<ver>_amd64.deb), so there is no fixed URL
    local api="https://api.github.com/repos/obsidianmd/obsidian-releases/releases/latest"
    local url
    url=$(curl -fsSL "$api" | grep -o '"browser_download_url": "[^"]*"' | grep 'amd64\.deb' | head -1 | cut -d'"' -f4)
    if [ -z "$url" ]; then
        echo "  Could not resolve the latest Obsidian .deb from GitHub — check your connection and try again."
        return 1
    fi
    local tmp
    tmp=$(mktemp -d)
    echo "  Downloading ${url##*/} ..."
    curl -fL "$url" -o "$tmp/obsidian.deb"
    # apt (unlike dpkg -i) pulls the .deb's dependencies itself
    doas apt install -y "$tmp/obsidian.deb"
    rm -rf "$tmp"
}

main() {
  echo "$x"
  sleep 1
  install_obsidian
  echo "  Obsidian has been installed - you can now find it in your navi launcher."
}
# XXX - - - entry
main
