#!/usr/bin/env bash
# navi-extras — optional post-install extras for navi.
#
# The app/agent installers used to run (with per-item prompts) at the tail
# of install.sh, where one failure aborted the whole install. Now they live
# here, in a menu the user runs whenever they want them. Nothing here runs
# unless picked.
#
# No `set -e` on purpose: one broken installer must never kill the menu.
#
# Usage:
#   navi-extras                 interactive menu
#   navi-extras --list          one line per extra: <id>\t<name>\t<installed|missing>
#   navi-extras --install <id>  install one extra without the menu
#   navi-extras --help          this text

x="= = = = = navi extras = = = = ="

# id|name|description|detect-snippet|installer|installer-args
EXTRAS=(
  "firefox-nightly|Firefox Nightly|Bleeding-edge Firefox, straight from Mozilla's apt repo|command -v firefox-nightly >/dev/null 2>&1|nightly-browsers.sh|firefox"
  "brave-nightly|Brave Nightly|Bleeding-edge Brave, for life on the edge|command -v brave-browser-nightly >/dev/null 2>&1|nightly-browsers.sh|brave"
  "chrome|Google Chrome|The browser half the planet uses, from Google's own apt repo|command -v google-chrome >/dev/null 2>&1|chrome-installer.sh|"
  "edge|Microsoft Edge|Microsoft's Chromium-based browser, from Microsoft's own apt repo|command -v microsoft-edge >/dev/null 2>&1|msedge-installer.sh|"
  "helium|Helium|Minimal Chromium-based browser, from Helium's own apt repo|command -v helium >/dev/null 2>&1 || command -v helium-bin >/dev/null 2>&1|helium-installer.sh|"
  "ani-cli|ani-cli|Watch anime in the terminal|command -v ani-cli >/dev/null 2>&1 || [ -x /usr/local/bin/ani-cli ]|ani-cli-installer.sh|"
  "charm|Charm toolkit|gum, glow, mods — shell superpowers from charm.sh|command -v gum >/dev/null 2>&1|charm-installer.sh|"
  "cliamp|cliamp|Terminal music player|command -v cliamp >/dev/null 2>&1|cliamp-installer.sh|"
  "telegram|Telegram|Fast, cloud-synced messaging — native desktop app|command -v telegram >/dev/null 2>&1|telegram-installer.sh|"
  "element|Element|Decentralized chat over Matrix — native desktop app|command -v element-desktop >/dev/null 2>&1|element-installer.sh|"
  "tailscale|Tailscale|Zero-config mesh VPN — your own private network across every device|command -v tailscale >/dev/null 2>&1|tailscale-installer.sh|"
)

# where do the installer scripts live?
# deployed systems: /usr/share/navi/installers (put there by install.sh).
# dev/ISO context falls back to the repo tree or the staged ISO payload.
_here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALLER_DIR=""
for _d in "$_here/installers" /usr/share/navi/installers /opt/navi-iso/scripts/installers; do
  if [ -d "$_d" ]; then INSTALLER_DIR="$_d"; break; fi
done

pink='\033[95m'; green='\033[92m'; dim='\033[2m'; bold='\033[1m'; reset='\033[0m'

field() { # <entry> <n> — pull the nth | field
  awk -F'|' -v n="$2" '{print $n}' <<<"$1"
}

is_installed() { # <entry>
  eval "$(field "$1" 4)"
}

find_extra() { # <id> — print the entry or nothing
  local e id
  for e in "${EXTRAS[@]}"; do
    id="$(field "$e" 1)"
    [ "$id" = "$1" ] && { printf '%s\n' "$e"; return 0; }
  done
  return 1
}

cmd_list() {
  local e id name
  for e in "${EXTRAS[@]}"; do
    id="$(field "$e" 1)"; name="$(field "$e" 2)"
    if is_installed "$e"; then
      printf '%s\t%s\tinstalled\n' "$id" "$name"
    else
      printf '%s\t%s\tmissing\n' "$id" "$name"
    fi
  done
}

run_installer() { # <entry> — returns the installer's exit code
  local e="$1"
  local name script args
  name="$(field "$e" 2)"; script="$(field "$e" 5)"; args="$(field "$e" 6)"
  if [ -z "$INSTALLER_DIR" ]; then
    printf '  %b✗%b installer scripts not found — is navi fully installed?\n' "$pink" "$reset" >&2
    return 1
  fi
  if [ ! -f "$INSTALLER_DIR/$script" ]; then
    printf '  %b✗%b %s is not available in this install (missing %s)\n' "$pink" "$reset" "$name" "$script" >&2
    return 1
  fi
  # shellcheck disable=SC2086
  bash "$INSTALLER_DIR/$script" $args
}

cmd_install() { # <id>
  local e
  e="$(find_extra "$1")" || { echo "unknown extra: $1 (try --list)" >&2; return 2; }
  local name
  name="$(field "$e" 2)"
  if is_installed "$e"; then
    printf '  %b●%b %s is already installed.\n' "$green" "$reset" "$name"
    return 0
  fi
  echo "  installing $name..."
  if run_installer "$e"; then
    printf '  %b✓%b %s installed.\n' "$green" "$reset" "$name"
  else
    printf '  %b✗%b %s failed to install (exit %s) — nothing else was touched.\n' "$pink" "$reset" "$name" "$?"
  fi
}

menu() {
  [ -z "$INSTALLER_DIR" ] && {
    echo "installer scripts not found — is navi fully installed?" >&2
    return 1
  }
  while true; do
    printf '\n  %b%s%b\n' "$pink" "$x" "$reset"
    echo "  optional software, installed on your terms."
    echo "  nothing here runs unless you pick it."
    echo ""
    local i=1 e id name desc state
    for e in "${EXTRAS[@]}"; do
      id="$(field "$e" 1)"; name="$(field "$e" 2)"; desc="$(field "$e" 3)"
      if is_installed "$e"; then state="${green}[installed]${reset}"
      else state="${dim}[not installed]${reset}"; fi
      printf '  %b%d)%b %s %b\n' "$bold" "$i" "$reset" "$name" "$state"
      printf '     %b%s%b\n' "$dim" "$desc" "$reset"
      i=$((i + 1))
    done
    printf '  %bq)%b quit\n' "$bold" "$reset"
    echo ""
    printf '  pick one: '
    local choice
    IFS= read -r choice || { echo ""; return 0; }
    case "$choice" in
      q|Q) echo "  the wired will wait."; return 0 ;;
      ''|*[!0-9]*)
        echo "  that's not on the menu — try a number, or q."
        continue ;;
    esac
    if [ "$choice" -ge 1 ] && [ "$choice" -le "${#EXTRAS[@]}" ]; then
      e="${EXTRAS[$((choice - 1))]}"
      name="$(field "$e" 2)"
      if is_installed "$e"; then
        printf '  %b●%b %s is already installed.\n' "$green" "$reset" "$name"
        printf '  press enter to continue... '
        IFS= read -r _
        continue
      fi
      printf '  install %s? [y/N] ' "$name"
      local yn
      IFS= read -r yn
      case "$yn" in
        y|Y|yes|YES) cmd_install "$(field "$e" 1)" ;;
        *) echo "  skipped." ;;
      esac
      printf '  press enter to continue... '
      IFS= read -r _
    else
      echo "  that's not on the menu — try a number, or q."
    fi
  done
}

case "${1:-}" in
  --list) cmd_list ;;
  --install) [ -n "${2:-}" ] || { echo "usage: navi-extras --install <id>" >&2; exit 2; }; cmd_install "$2" ;;
  --help|-h) sed -n '2,/^$/p' "$0" | sed 's/^# \{0,1\}//' ;;
  "") menu ;;
  *) echo "unknown option: $1 (try --help)" >&2; exit 2 ;;
esac
