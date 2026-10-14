#!/usr/bin/env bash
# epiphany installer
# ( installs GNOME Web (Epiphany), the clean WebKit-based browser )
#######################################################
set -e
x="= = = = = epiphany installer = = = = ="

install_epiphany() {
  doas apt update && doas apt install -y epiphany-browser
}

main() {
  echo "$x"
  sleep 1
  install_epiphany
  echo "  GNOME Web has been installed - you can now find it in your navi launcher."
}
# XXX - - - entry
main
