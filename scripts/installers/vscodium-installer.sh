#!/usr/bin/env bash
# VSCodium installer
# ( installs VSCodium, the community build of VS Code without MS telemetry )
#######################################################
set -e
x="= = = = = VSCodium installer = = = = ="

install_vscodium() {
    local pkg="codium"
    # import VSCodium gpg key to keyring
    curl -fsSL https://gitlab.com/paulcarroty/vscodium-deb-rpm-repo/raw/master/pub.gpg \
  | gpg --dearmor \
  | doas tee /usr/share/keyrings/vscodium-archive-keyring.gpg > /dev/null
    # add VSCodium apt repo (DEB822 format, per upstream docs for Debian 13+)
    printf '%s\n' \
      'Types: deb' \
      'URIs: https://download.vscodium.com/debs' \
      'Suites: vscodium' \
      'Components: main' \
      'Architectures: amd64 arm64' \
      'Signed-By: /usr/share/keyrings/vscodium-archive-keyring.gpg' \
  | doas tee /etc/apt/sources.list.d/vscodium.sources > /dev/null
    # install VSCodium
    doas apt update && doas apt install -y "$pkg"
}

main() {
  echo "$x"
  sleep 1
  install_vscodium
  echo "  VSCodium has been installed - you can now find it in your navi launcher."
}
# XXX - - - entry
main
