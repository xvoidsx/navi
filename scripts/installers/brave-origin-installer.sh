#!/usr/bin/env bash
# brave origin installer
# ( installs Brave Origin, a stripped-down version of Brave )
##############################################################
set -e
x="= = = = = brave origin installer = = = = ="

check_memory() {
  # brave-origin is a ~132MB download; dpkg unpacking it can OOM-kill on
  # 2GB machines (the cloudbook died this way on edge). warn, don't
  # block — with caches dropped or swap added it may still squeak through.
  local avail_kb swap_kb
  avail_kb=$(awk '/MemAvailable/ {print $2}' /proc/meminfo 2>/dev/null || echo 0)
  swap_kb=$(awk '/SwapTotal/ {print $2}' /proc/meminfo 2>/dev/null || echo 0)
  if [ "${avail_kb:-0}" -gt 0 ] && [ "$avail_kb" -lt 1572864 ] && [ "${swap_kb:-0}" -eq 0 ]; then
    printf '  warning: only %s MB RAM free and no swap — unpacking this\n' "$((avail_kb / 1024))"
    printf '  132MB package may get OOM-killed on small machines. if apt dies with\n'
    printf "  'dpkg-deb: error: <decompress> subprocess was killed by signal (Killed)',\n"
    printf '  add temporary swap and re-run this installer:\n'
    printf '    doas fallocate -l 2G /swapfile && doas chmod 600 /swapfile && doas mkswap /swapfile && doas swapon /swapfile\n'
    sleep 2
  fi
}

install_brave_origin () {
  check_memory
  local pkg="brave-origin"
  # import brave keyring
  doas curl -fsSLo /usr/share/keyrings/brave-browser-archive-keyring.gpg https://brave-browser-apt-release.s3.brave.com/brave-browser-archive-keyring.gpg
  doas curl -fsSLo /etc/apt/sources.list.d/brave-browser-release.sources https://brave-browser-apt-release.s3.brave.com/brave-browser.sources
  # install brave origin
  doas apt update && doas apt install -y "$pkg"
}

main () {
  echo "$x"
  sleep 1
  install_brave_origin
  echo " Brave Origin has been installed - you can now find it in your navi launcher."
  }
  # XXX - - - entry
  main
