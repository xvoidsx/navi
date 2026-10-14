#!/usr/bin/env bash
# zen installer
# ( installs Zen Browser, the Firefox-based browser, via Flathub )
#######################################################
set -e
x="= = = = = zen installer = = = = ="

install_zen() {
  # user-local flatpak install — no root needed, updates via flatpak
  flatpak install -y --user flathub io.github.zen_browser.zen
}

main() {
  echo "$x"
  sleep 1
  install_zen
  echo "  zen browser has been installed - you can now find it in your navi launcher."
}
# XXX - - - entry
main
