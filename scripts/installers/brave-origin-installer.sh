#!/usr/bin/env bash
# brave origin installer
# ( installs Brave Origin, a stripped-down version of Brave )
##############################################################
set -e
x="= = = = = brave origin installer = = = = ="

install_brave_origin () {
  local pkg="brave-origin"
  # import brave keyring
  doas curl -fsSLo /usr/share/keyrings/brave-browser-archive-keyring.gpg https://brave-browser-apt-release.s3.brave.com/brave-browser-archive-keyring.gpg
  doas curl -fsSLo /etc/apt/sources.list.d/brave-browser-release.sources https://brave-browser-apt-release.s3.brave.com/brave-browser.sources
  # install brave origin
  doas apt update && doas apt install -y "$pkg"
}

main () {
  echo "$x"
  sleep 1
  install_brave_origin
  echo " Brave Origin has been installed - you can now find it in your navi launcher."
  }
  # XXX - - - entry
  main
