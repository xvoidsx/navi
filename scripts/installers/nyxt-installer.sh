#!/usr/bin/env bash
# nyxt installer
# ( installs Nyxt, the hacker's keyboard-driven extensible browser )
#######################################################
set -e
x="= = = = = nyxt installer = = = = ="

install_nyxt() {
  doas apt update && doas apt install -y nyxt
}

main() {
  echo "$x"
  sleep 1
  install_nyxt
  echo "  nyxt has been installed - you can now find it in your navi launcher."
}
# XXX - - - entry
main
