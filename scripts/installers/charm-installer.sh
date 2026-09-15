#!/usr/bin/env bash
# charm installer
# ( installs the Charm CLI toolkit: gum, glow, mods )
# ( https://charm.sh )
##################################
set -e
x="= = = = = charm installer = = = = ="

PRIV="sudo"
command -v doas >/dev/null 2>&1 && PRIV="doas"

install_charm () {
  # 1. charm's apt repo (idempotent — skip if already present)
  if [ ! -f /etc/apt/sources.list.d/charm.list ]; then
    echo "  adding the charm apt repository..."
    $PRIV mkdir -p /etc/apt/keyrings
    curl -fsSL https://repo.charm.sh/apt/gpg.key \
      | $PRIV gpg --dearmor -o /etc/apt/keyrings/charm.gpg
    echo "deb [signed-by=/etc/apt/keyrings/charm.gpg] https://repo.charm.sh/apt/ * *" \
      | $PRIV tee /etc/apt/sources.list.d/charm.list >/dev/null
  else
    echo "  charm apt repository already present — leaving it alone"
  fi

  # 2. the essentials: gum (shell superpowers), glow (markdown in the
  #    terminal), mods (AI on the command line — very navi)
  echo "  installing gum, glow, mods..."
  $PRIV apt-get update -qq
  $PRIV apt-get install -y gum glow mods

  echo ""
  echo "  the charm shelf, should you want more:"
  echo "    skate   — a personal key-value store"
  echo "    freeze  — beautiful code screenshots"
  echo "    pop     — send files from the terminal"
  echo "    vhs     — terminal GIFs for demos"
  echo "  grab any of them with: $PRIV apt-get install <name>"
}

main () {
  echo "$x"; sleep 1
  install_charm
  echo ""
  echo "  try: gum choose \"wired\" \"navi\" \"lain\"   — you'll see."
}
# - - - - - |
main
