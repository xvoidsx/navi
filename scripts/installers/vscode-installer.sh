#!/usr/bin/env bash
# VS Code installer
# ( installs VS Code, Microsoft's Electron-based code editor )
#######################################################
set -e
x="= = = = = VS Code installer = = = = ="

install_vscode() {
    local pkg="code"
    # import MS gpg key to keyring
    curl -fsSL https://packages.microsoft.com/keys/microsoft.asc \
  | gpg --dearmor \
  | doas tee /usr/share/keyrings/packages.microsoft.gpg > /dev/null
    # add VS Code apt repo
    echo "deb [arch=amd64 signed-by=/usr/share/keyrings/packages.microsoft.gpg] \
https://packages.microsoft.com/repos/vscode stable main" \
  | doas tee /etc/apt/sources.list.d/vscode.list > /dev/null
    # install VS Code
    doas apt update && doas apt install -y "$pkg"
}

main() {
  echo "$x"
  sleep 1
  install_vscode
  echo "  VS Code has been installed - you can now find it in your navi launcher."
}
# XXX - - - entry
main
