#!/usr/bin/env bash
# element installer 
# ( installs Element, the premier matrix client )
##################################
set -e
x="= = = = = element installer = = = = ="

install_element () {
    # grab apt-transport-https if we don't already have it on the system
    doas apt install -y wget apt-transport-https
    # - - - 
    doas wget -O /usr/share/keyrings/element-io-archive-keyring.gpg https://packages.element.io/debian/element-io-archive-keyring.gpg
    echo "deb [signed-by=/usr/share/keyrings/element-io-archive-keyring.gpg] https://packages.element.io/debian/ default main" | doas tee /etc/apt/sources.list.d/element-io.list
    doas apt update; doas apt install -y element-desktop
}

main () {
    echo "$x"; sleep 1
    install_element
    echo "  element has been installed - you can find it now in your navi launcher."
}
# - - - - - |
main
