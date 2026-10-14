#!/usr/bin/env bash
# edge dev installer
# ( installs Edge Dev, MS' weekly dev-channel chromium-based browser )
########################################################################
set -e
x="= = = = = edge dev installer = = = = ="

install_msedge_dev() {
    local pkg="microsoft-edge-dev"
    # import MS gpg key to keyring (shared with the stable channel)
    curl -fsSL https://packages.microsoft.com/keys/microsoft.asc \
  | gpg --dearmor \
  | doas tee /usr/share/keyrings/microsoft-edge.gpg > /dev/null
    # add edge dev apt repo
    echo "deb [arch=amd64 signed-by=/usr/share/keyrings/microsoft-edge.gpg] \
https://packages.microsoft.com/repos/edge dev main" \
  | doas tee /etc/apt/sources.list.d/microsoft-edge-dev.list > /dev/null
    # install edge dev
    doas apt update && doas apt install -y "$pkg"
 }

main() {
  echo "$x"
  sleep 1
  install_msedge_dev
  echo "  MS Edge Dev has been installed - you can now find it in your navi launcher."
}
# XXX - - - entry
main
