#!/usr/bin/env bash
# vivaldi installer
# ( installs Vivaldi, the Chromium-based browser, from Vivaldi's own apt repo )
#######################################################
set -e
x="= = = = = vivaldi installer = = = = ="

install_vivaldi() {
  local pkg="vivaldi-stable"
  # import Vivaldi's gpg key to keyring
  curl -fsSL https://repo.vivaldi.com/archive/linux_signing_key.pub \
    | gpg --dearmor \
    | doas tee /usr/share/keyrings/vivaldi.gpg > /dev/null
  # add vivaldi apt repo
  echo "deb [arch=amd64 signed-by=/usr/share/keyrings/vivaldi.gpg] \
https://repo.vivaldi.com/archive/deb/ stable main" \
    | doas tee /etc/apt/sources.list.d/vivaldi.list > /dev/null
  # install vivaldi
  doas apt update && doas apt install -y "$pkg"
}

main() {
  echo "$x"
  sleep 1
  install_vivaldi
  echo "  vivaldi has been installed - you can now find it in your navi launcher."
}
# XXX - - - entry
main
