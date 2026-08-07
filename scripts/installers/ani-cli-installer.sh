#!/usr/bin/env bash
# ani-cli installer
# ( ani-cli is a CLI tool for watching anime )
x="= = = = = ani-cli installer = = = = ="
install_ani-cli () {
    local repo="https://github.com/pystardust/ani-cli.git"
    git clone $repo
    sudo cp ani-cli/ani-cli /usr/local/bin
    rm -rf ani-cli
}
main () {
    echo "$x"; sleep 1
    install_ani-cli
}
# - - - - - |
main
