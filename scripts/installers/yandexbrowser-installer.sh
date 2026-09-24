#!/usr/bin/env bash
# yandex browser installer
# ( installs yandex browser, yandex's chromium-based browser )
##############################################################
set -e
x="= = = = = yandex browser installer = = = = ="

install_yandexbrowser() {
  local pkg="yandex-browser-stable"
  curl -fsSL https://repo.yandex.ru/yandex-browser/YANDEX-BROWSER-KEY.GPG | doas gpg --dearmor --yes -o /usr/share/keyrings/yandex-browser.gpg
  doas apt update && doas apt install -y "$pkg"
}

main() {
  echo "$x"
  sleep 1
  install_yandexbrowser
  echo "  Yandex Browser has been installed - you can now find it in your navi launcher."
}
# XXX - - - entry
main
