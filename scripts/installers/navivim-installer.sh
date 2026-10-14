#!/usr/bin/env bash
# NaviVim installer
# ( reinstalls / repairs navi's default terminal IDE: xvoidsx/navivim )
#
# NOTE: NAVIVIM_PIN and NAVIVIM_NVIM_VERSION must stay in sync with
# install.sh — NaviVim never floats on navivim's main or the moving
# neovim stable tag.
#######################################################
set -e
x="= = = = = NaviVim installer = = = = ="
NAVIVIM_PIN="82440c2a21086c26c5e3b029acc52a95d9bd460d"
NAVIVIM_NVIM_VERSION="v0.12.5"

install_navivim() {
    local tmp
    tmp="$(mktemp -d)"
    trap 'rm -rf "$tmp"' EXIT
    echo "  cloning xvoidsx/navivim at ${NAVIVIM_PIN:0:12}..."
    if ! git clone -q https://github.com/xvoidsx/navivim.git "$tmp/navivim" 2>/dev/null; then
        echo "  could not clone xvoidsx/navivim — check your network and try again." >&2
        exit 1
    fi
    git -C "$tmp/navivim" checkout -q "$NAVIVIM_PIN"
    # --system: system-wide install under /usr/local, seeds /etc/skel,
    # registers NaviVim as the default editor (same as install.sh does).
    # doas strips the environment, so NVIM_VERSION rides along via env.
    doas env NVIM_VERSION="$NAVIVIM_NVIM_VERSION" bash "$tmp/navivim/install.sh" --system
}

main() {
  echo "$x"
  sleep 1
  install_navivim
  echo "  NaviVim installed — run 'nvim' to enter the wired."
}
# XXX - - - entry
main
