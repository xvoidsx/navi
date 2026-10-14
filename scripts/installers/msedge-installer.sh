#!/usr/bin/env bash
# edge installer
# ( installs Edge, MS' chromium-based browser )
#######################################################
set -e
x="= = = = = edge installer = = = = ="

install_msedge() {
    local pkg="microsoft-edge-stable"
    # import MS gpg key to keyring
    curl -fsSL https://packages.microsoft.com/keys/microsoft.asc \
  | gpg --dearmor \
  | doas tee /usr/share/keyrings/microsoft-edge.gpg > /dev/null
    # add edge apt repo
    echo "deb [arch=amd64 signed-by=/usr/share/keyrings/microsoft-edge.gpg] \
https://packages.microsoft.com/repos/edge stable main" \
  | doas tee /etc/apt/sources.list.d/microsoft-edge.list > /dev/null
    # install edge
    doas apt update && doas apt install -y "$pkg" 
 }

main() {
  echo "$x"
  sleep 1
  install_msedge
  echo "  MS Edge has been installed - you can now find it in your navi launcher."
}
# XXX - - - entry
main
