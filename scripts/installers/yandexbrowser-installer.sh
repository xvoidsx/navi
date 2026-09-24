#!/usr/bin/env bash
# yandex browser installer
# ( installs yandex browser, yandex's chromium-based browser )
##############################################################
set -e
x="= = = = = yandex browser installer = = = = ="

install_yandexbrowser() {
  local pkg="yandex-browser-stable"
  # import yandex gpg key to keyring
  curl -fsSL https://repo.yandex.ru/yandex-browser/YANDEX-BROWSER-KEY.GPG \
    | gpg --dearmor \
    | doas tee /usr/share/keyrings/yandex-browser.gpg > /dev/null
  # add yandex browser apt repo
  echo "deb [arch=amd64 signed-by=/usr/share/keyrings/yandex-browser.gpg] \
https://repo.yandex.ru/yandex-browser/deb stable main" \
    | doas tee /etc/apt/sources.list.d/yandex-browser.list > /dev/null
  # install yandex browser
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
