#!/usr/bin/env bash
# edge installer
# ( installs Edge, MS' chromium-based browser )
#######################################################
set -e
x="= = = = = edge installer = = = = ="

check_memory() {
    # field report (2026-09-24, cloudbook bench): on ~2GB machines the 193MB
    # Edge deb can get dpkg-deb's decompress subprocess OOM-killed mid-unpack
    # ("subprocess was killed by signal (Killed)"), failing the whole apt
    # run. warn, don't block — with caches dropped or swap added it may
    # still squeak through.
    local avail_kb swap_kb
    avail_kb=$(awk '/MemAvailable/ {print $2}' /proc/meminfo 2>/dev/null || echo 0)
    swap_kb=$(awk '/SwapTotal/ {print $2}' /proc/meminfo 2>/dev/null || echo 0)
    if [ "${avail_kb:-0}" -gt 0 ] && [ "$avail_kb" -lt 1572864 ] && [ "${swap_kb:-0}" -eq 0 ]; then
        printf '  warning: only %s MB RAM free and no swap — unpacking this\n' "$((avail_kb / 1024))"
        printf '  193MB package may get OOM-killed on small machines. if apt dies with\n'
        printf "  'dpkg-deb: error: <decompress> subprocess was killed by signal (Killed)',\n"
        printf '  add temporary swap and re-run this installer:\n'
        printf '    doas fallocate -l 2G /swapfile && doas chmod 600 /swapfile && doas mkswap /swapfile && doas swapon /swapfile\n'
        sleep 2
    fi
}

install_msedge() {
    check_memory
    local pkg="microsoft-edge-stable"
    # import MS gpg key to keyring
    curl -fsSL https://packages.microsoft.com/keys/microsoft.asc \
  | gpg --dearmor \
  | doas tee /usr/share/keyrings/microsoft-edge.gpg > /dev/null
    # add edge apt repo
    echo "deb [arch=amd64 signed-by=/usr/share/keyrings/microsoft-edge.gpg] \
https://packages.microsoft.com/repos/edge stable main" \
  | doas tee /etc/apt/sources.list.d/microsoft-edge.list > /dev/null
    # install edge
    doas apt update && doas apt install -y "$pkg" 
 }

main() {
  echo "$x"
  sleep 1
  install_msedge
  echo "  MS Edge has been installed - you can now find it in your navi launcher."
}
# XXX - - - entry
main
