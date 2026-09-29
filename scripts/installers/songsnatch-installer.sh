#!/usr/bin/env bash
# songsnatch installer (navi wrapper)
# ( songsnatch grabs audio from YouTube via yt-dlp, saves to ~/Music )
# ( by rav3ndust.xyz — xvoidsx/songsnatch )
#
# Delegates to the upstream installer — single source of truth lives
# in the songsnatch repo.
##########################################
set -e
x="= = = = = songsnatch installer = = = = ="
main() {
  echo "$x"; sleep 1
  curl -fsSL https://raw.githubusercontent.com/xvoidsx/songsnatch/main/install.sh | bash
}
# - - - - - |
main
