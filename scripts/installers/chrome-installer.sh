#!/usr/bin/env bash
# google chrome installer
# ( the browser half the planet uses — from Google's own apt repo )
##################################
set -e
x="= = = = = google chrome installer = = = = ="

PRIV="sudo"
command -v doas >/dev/null 2>&1 && PRIV="doas"

install_chrome () {
  # 1. Google's apt repo (idempotent — skip if already present)
  if [ ! -f /etc/apt/sources.list.d/google-chrome.list ]; then
    echo "  adding Google's apt repository..."
    $PRIV mkdir -p /usr/share/keyrings
    curl -fsSL https://dl.google.com/linux/linux_signing_key.pub \
      | $PRIV gpg --dearmor -o /usr/share/keyrings/google-chrome.gpg
    echo "deb [arch=amd64 signed-by=/usr/share/keyrings/google-chrome.gpg] https://dl.google.com/linux/chrome/deb/ stable main" \
      | $PRIV tee /etc/apt/sources.list.d/google-chrome.list >/dev/null
  else
    echo "  google chrome apt repository already present — leaving it alone"
  fi

  # 2. the browser itself (updates with the rest of the system from here on)
  echo "  installing google-chrome-stable..."
  $PRIV apt-get update -qq
  $PRIV apt-get install -y google-chrome-stable
}

main () {
    echo "$x"; sleep 1
    install_chrome
    echo "  chrome is in — look for it in the launcher."
}
# - - - - - |
main
