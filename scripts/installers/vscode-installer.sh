#!/usr/bin/env bash
# VS Code installer
# ( installs VS Code, Microsoft's Electron-based code editor )
#######################################################
set -e
x="= = = = = VS Code installer = = = = ="

check_memory() {
    # field report (2026-09-24, cloudbook bench): on ~2GB machines a large
    # deb can get dpkg-deb's decompress subprocess OOM-killed mid-unpack
    # ("subprocess was killed by signal (Killed)"), failing the whole apt
    # run. warn, don't block — with caches dropped or swap added it may
    # still squeak through.
    local avail_kb swap_kb
    avail_kb=$(awk '/MemAvailable/ {print $2}' /proc/meminfo 2>/dev/null || echo 0)
    swap_kb=$(awk '/SwapTotal/ {print $2}' /proc/meminfo 2>/dev/null || echo 0)
    if [ "${avail_kb:-0}" -gt 0 ] && [ "$avail_kb" -lt 1572864 ] && [ "${swap_kb:-0}" -eq 0 ]; then
        printf '  warning: only %s MB RAM free and no swap — unpacking this\n' "$((avail_kb / 1024))"
        printf '  ~100MB package may get OOM-killed on small machines. if apt dies with\n'
        printf "  'dpkg-deb: error: <decompress> subprocess was killed by signal (Killed)',\n"
        printf '  add temporary swap and re-run this installer:\n'
        printf '    doas fallocate -l 2G /swapfile && doas chmod 600 /swapfile && doas mkswap /swapfile && doas swapon /swapfile\n'
        sleep 2
    fi
}

install_vscode() {
    check_memory
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
