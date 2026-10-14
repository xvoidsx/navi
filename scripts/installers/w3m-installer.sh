#!/usr/bin/env bash
# w3m installer
# ( installs w3m, the terminal web browser with inline image support )
#######################################################
set -e
x="= = = = = w3m installer = = = = ="

install_w3m() {
  doas apt update && doas apt install -y w3m
}

main() {
  echo "$x"
  sleep 1
  install_w3m
  echo "  w3m has been installed - run it with: w3m <url>"
}
# XXX - - - entry
main
