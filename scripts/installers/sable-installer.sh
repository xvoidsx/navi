#!/usr/bin/env bash
# sable installer
# ( installs Sable, navi's default GUI Matrix client, from Sable's own apt repo )
##################################
set -e
x="= = = = = sable installer = = = = ="

install_sable () {
    # grab apt-transport-https if we don't already have it on the system
    doas apt install -y wget apt-transport-https
    # - - -
    doas mkdir -p /etc/apt/keyrings
    doas wget -O /etc/apt/keyrings/forgejo-SableClient.asc https://git.sable.moe/api/packages/SableClient/debian/repository.key
    echo "deb [signed-by=/etc/apt/keyrings/forgejo-SableClient.asc] https://git.sable.moe/api/packages/SableClient/debian stable main" | doas tee /etc/apt/sources.list.d/sable.list
    doas apt update; doas apt install -y sable
}

main () {
    echo "$x"; sleep 1
    install_sable
    echo "  sable has been installed - you can find it now in your navi launcher."
}
# - - - - - |
main
