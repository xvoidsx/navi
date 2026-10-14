#!/usr/bin/env bash
# falkon installer
# ( installs Falkon, KDE's lightweight QtWebEngine browser )
#######################################################
set -e
x="= = = = = falkon installer = = = = ="

install_falkon() {
  doas apt update && doas apt install -y falkon
}

main() {
  echo "$x"
  sleep 1
  install_falkon
  echo "  falkon has been installed - you can now find it in your navi launcher."
}
# XXX - - - entry
main
