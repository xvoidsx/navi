#!/usr/bin/env bash
# tailscale installer
# ( installs Tailscale, the zero-config mesh VPN )
##################################
set -e
x="= = = = = tailscale installer = = = = ="

install_tailscale () {
    # tailscale's official apt repo (trixie — navi's debian base)
    doas apt install -y curl
    curl -fsSL https://pkgs.tailscale.com/stable/debian/trixie.noarmor.gpg \
        | doas tee /usr/share/keyrings/tailscale-archive-keyring.gpg >/dev/null
    curl -fsSL https://pkgs.tailscale.com/stable/debian/trixie.tailscale-keyring.list \
        | doas tee /etc/apt/sources.list.d/tailscale.list
    doas apt update; doas apt install -y tailscale
    doas systemctl enable --now tailscaled
}

main () {
    echo "$x"; sleep 1
    install_tailscale
    echo "  tailscale has been installed - run 'tailscale up' to join your tailnet."
}
# - - - - - |
main
