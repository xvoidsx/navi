#!/usr/bin/env bash
# nightly browsers installer
# ( installs "Nightly" browsers. )
# ( we keep Firefox Nightly and Brave Nightly around )
# ( for those who want to be on the bleedin' edge of the web )
################################################################
set -e
x="= = = = = nightly browsers installer = = = = ="
firefox_nightly_install () {
  local pkg="firefox-nightly"
  sudo install -d -m 0755 /etc/apt/keyrings
  wget -q https://packages.mozilla.org/apt/repo-signing-key.gpg -O- | sudo tee /etc/apt/keyrings/packages.mozilla.org.asc > /dev/null
  sudo tee /etc/apt/sources.list.d/mozilla.sources > /dev/null << EOF
Types: deb
URIs: https://packages.mozilla.org/apt
Suites: mozilla
Components: main
Signed-By: /etc/apt/keyrings/packages.mozilla.org.asc
EOF
  sudo tee /etc/apt/preferences.d/mozilla > /dev/null << EOF
Package: *
Pin: origin packages.mozilla.org
Pin-Priority: 1000
EOF
  sudo apt update && sudo apt install -y "$pkg"
}
brave_nightly_install () {
  curl -fsS https://dl.brave.com/install.sh | CHANNEL=nightly sh
}
main () {
  # target: firefox | brave | all (default all). lets navi-extras offer each
  # browser on its own instead of forcing the pair.
  local target="${1:-all}"
  echo "$x"; sleep 1
  case "$target" in
    firefox)
      echo "Installing Firefox Nightly..."; sleep 1
      firefox_nightly_install
      ;;
    brave)
      echo "Installing Brave Browser Nightly..."; sleep 1
      brave_nightly_install
      ;;
    all|"")
      echo "Installing Firefox Nightly..."; sleep 1
      firefox_nightly_install
      echo "Installing Brave Browser Nightly..."; sleep 1
      brave_nightly_install
      ;;
    *)
      echo "usage: $0 [firefox|brave|all]" >&2
      exit 2
      ;;
  esac
  echo "Nightly browsers installed."; sleep 1
}
# - - - - - |
main "$@"
