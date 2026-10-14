#!/usr/bin/env bash
# lynx installer
# ( installs Lynx, the classic terminal web browser )
#######################################################
set -e
x="= = = = = lynx installer = = = = ="

install_lynx() {
  doas apt update && doas apt install -y lynx
}

main() {
  echo "$x"
  sleep 1
  install_lynx
  echo "  lynx has been installed - run it with: lynx <url>"
}
# XXX - - - entry
main
