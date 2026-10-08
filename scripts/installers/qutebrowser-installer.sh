#!/usr/bin/env bash
# qutebrowser installer
# ( installs qutebrowser, the keyboard-driven Vim-like browser )
#######################################################
set -e
x="= = = = = qutebrowser installer = = = = ="

install_qutebrowser() {
  doas apt update && doas apt install -y qutebrowser
}

main() {
  echo "$x"
  sleep 1
  install_qutebrowser
  echo "  qutebrowser has been installed - you can now find it in your navi launcher."
}
# XXX - - - entry
main
