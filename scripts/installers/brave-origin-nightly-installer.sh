#!/usr/bin/env bash
# brave origin nightly installer
# ( installs Brave Origin Nightly — the stripped-down Brave, bleeding edge )
###########################################################################
set -e
x="= = = = = brave origin nightly installer = = = = ="

install_brave_origin_nightly() {
  local pkg="brave-origin-nightly"
  # import brave nightly keyring (official Brave nightly apt repo)
  doas curl -fsSLo /usr/share/keyrings/brave-browser-nightly-archive-keyring.gpg \
    https://brave-browser-apt-nightly.s3.brave.com/brave-browser-nightly-archive-keyring.gpg
  doas curl -fsSLo /etc/apt/sources.list.d/brave-browser-nightly.sources \
    https://brave-browser-apt-nightly.s3.brave.com/brave-browser.sources
  # install brave origin nightly
  doas apt update && doas apt install -y "$pkg"
  # navi vision for origin nightly — shares release origin's profile dir
  source "$(dirname "${BASH_SOURCE[0]}")/brave-seed-lib.sh"
  seed_brave_variant "Brave-Origin"
}

main() {
  echo "$x"
  sleep 1
  install_brave_origin_nightly
  echo "  Brave Origin Nightly has been installed - you can now find it in your navi launcher."
}
# XXX - - - entry
main
