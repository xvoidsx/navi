#!/usr/bin/env bash
# helium installer
# ( installs Helium, a  minimal, Chromium-based browser )
#######################################################
set -e
x="= = = = = helium installer = = = = ="

install_helium() {
  # installs helium's apt repo
  local helium_pk="https://raw.githubusercontent.com/imputnet/helium-linux/main/pubkey.asc"
  local pkg="helium-bin"
  curl -fsSL "helium_pk" | doas gpg --dearmor -o /usr/share/keyrings/helium.gpg
  echo "deb [signed-by=/usr/share/keyrings/helium.gpg] https://pkg.helium.computer/deb stable main" | doas tee /etc/apt/sources.list.d/helium.list
  # install the package
  doas apt update && doas apt install -y "$pkg"
}

main() {
  echo "$x"
  sleep 1
  install_helium
  echo "  helium has been installed - you can now find it in your navi launcher."
}
# XXX - - - entry
main
