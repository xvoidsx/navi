#!/usr/bin/env bash
# cliamp installer
# ( cliamp is a terminal music player )
# ( shipping by default in naviOS )
##########################################
set -e
x="= = = = = cliamp installer = = = = ="
install_cliamp () {
  local link="https://raw.githubusercontent.com/bjarneo/cliamp/HEAD/install.sh"
  curl -fsSL "$link" | sh
}
main () {
  echo "$x"; sleep 1
  install_cliamp
}
# - - - - - |
main
