#!/usr/bin/env bash
# VSCodium installer
# ( installs VSCodium, the community build of VS Code without MS telemetry )
#######################################################
set -e
x="= = = = = VSCodium installer = = = = ="

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

install_vscodium() {
    check_memory
    local pkg="codium"
    # import VSCodium gpg key to keyring
    curl -fsSL https://gitlab.com/paulcarroty/vscodium-deb-rpm-repo/raw/master/pub.gpg \
  | gpg --dearmor \
  | doas tee /usr/share/keyrings/vscodium-archive-keyring.gpg > /dev/null
    # add VSCodium apt repo (DEB822 format, per upstream docs for Debian 13+)
    printf '%s\n' \
      'Types: deb' \
      'URIs: https://download.vscodium.com/debs' \
      'Suites: vscodium' \
      'Components: main' \
      'Architectures: amd64 arm64' \
      'Signed-By: /usr/share/keyrings/vscodium-archive-keyring.gpg' \
  | doas tee /etc/apt/sources.list.d/vscodium.sources > /dev/null
    # install VSCodium
    doas apt update && doas apt install -y "$pkg"
}

main() {
  echo "$x"
  sleep 1
  install_vscodium
  echo "  VSCodium has been installed - you can now find it in your navi launcher."
}
# XXX - - - entry
main
