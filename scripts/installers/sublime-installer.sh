#!/usr/bin/env bash
# Sublime Text installer
# ( installs Sublime Text, the fast proprietary code editor )
#######################################################
set -e
x="= = = = = Sublime Text installer = = = = ="

install_sublime() {
    local pkg="sublime-text"
    # import SublimeHQ gpg key to keyring
    curl -fsSL https://download.sublimetext.com/sublimehq-pub.gpg \
  | gpg --dearmor \
  | doas tee /usr/share/keyrings/sublimehq-archive-keyring.gpg > /dev/null
    # add Sublime Text apt repo (stable channel)
    echo "deb [signed-by=/usr/share/keyrings/sublimehq-archive-keyring.gpg] https://download.sublimetext.com/ apt/stable/" \
  | doas tee /etc/apt/sources.list.d/sublime-text.list > /dev/null
    # install Sublime Text
    doas apt update && doas apt install -y "$pkg"
}

main() {
  echo "$x"
  sleep 1
  install_sublime
  echo "  Sublime Text has been installed - you can now find it in your navi launcher."
}
# XXX - - - entry
main
