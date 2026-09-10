#!/usr/bin/env bash
#
# install.sh — the navi desktop installer
#
# Installs the navi "wired" desktop layer on Debian 13 (trixie):
#   packages -> /usr/share/navi/wired (the iron structure) -> ~/.config/*
#   commands -> /usr/bin + /usr/share/applications (rofi-visible)
#   agent runtime: ollama, opencode, omp -> /usr/bin
#   doas, flatpak/flathub, wallpapers, first-boot behavior
#
# Idempotent: safe to re-run. Existing configs are backed up, never clobbered.
# Run as your normal user (not root). Privileged steps use doas/sudo.
#
#   ./install.sh [--yes] [--help]
#
# --yes   non-interactive: accept defaults, run every app installer

set -euo pipefail

# ---------------------------------------------------------------- config

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WIRED_DIR="$REPO_DIR/wired"
SYSTEM_DIR="$REPO_DIR/system"
SHARE_DIR="/usr/share/navi"
WIRED_SHARE="$SHARE_DIR/wired"
BACKUP_ROOT="$HOME/.config-backup-navi"
BACKUP_DIR="$BACKUP_ROOT/$(date +%Y%m%d-%H%M%S)"
ASSUME_YES=0
DOAS=""

# Every apt package, audited against Debian 13 "trixie" (2026-09-10).
# Corrections baked in: opendoas (doas is a transitional dummy), no slock
# (virtual, provided by suckless-tools), imagemagick-7.q16 (imagemagick is
# virtual). i3status + i3blocks were dropped: navi uses polybar, not i3bar,
# on the X11 session.
PKGS=(
  i3 i3lock-fancy nitrogen pamixer wget htop opendoas lsd
  nsxiv pulseaudio-utils xcompmgr picom waybar alacritty fonts-inter xterm
  arandr nemo rofi xss-lock feh pandoc volumeicon-alsa polybar blueman dunst
  flameshot meteo-qt pasystray ffmpeg kitty stterm surf conky-all suckless-tools
  lxpolkit lxappearance vim nnn cmus cava xscreensaver amfora sway swaylock
  swayidle swaybg grimshot xdg-desktop-portal-wlr qt5ct tty-clock wf-recorder
  sakura foot gsimplecal calcurse pavucontrol yaru-theme-gtk yaru-theme-icon
  glow pipx wl-clipboard wlr-randr jq imagemagick-7.q16 tmux shotman nwg-look fastfetch sddm qml6-module-qtmultimedia
  fonts-jetbrains-mono fonts-firacode fonts-noto wdisplays papirus-icon-theme
  fonts-font-awesome fonts-material-design-icons-iconfont bibata-cursor-theme
  cmatrix lynx elinks w3m libnotify-bin flatpak gnome-software-plugin-flatpak
)

# Commands deploy to /usr/bin (not /usr/local/bin) so every user on the
# machine gets them — navi is a multi-user system.

# ---------------------------------------------------------------- ui

banner() {
  cat <<'EOF'

            ██████
         ████    ████
        ███      ╱╱███
        ██    ╱╱    ██
        ███╱╱      ███
         ████    ████
            ██████

   ✦  W E L C O M E   T O   T H E   W I R E D  ✦

       n a v i   1 . 2  " m i k a "

       ナビ — everybody has already entered the wired

EOF
}

step()  { echo; echo "──▶ $1"; }
ok()    { echo "    ✓ $1"; }
info()  { echo "    · $1"; }
warn()  { echo "    ! $1"; }

confirm() {
  # confirm "question" -> 0 if yes
  if [ "$ASSUME_YES" -eq 1 ]; then return 0; fi
  local q="$1" ans
  read -r -p "    $q [Y/n] " ans
  case "${ans:-Y}" in [Yy]*) return 0 ;; *) return 1 ;; esac
}

usage() {
  sed -n '2,16p' "$0" | sed 's/^# \{0,1\}//'
  exit 0
}

# ---------------------------------------------------------------- preflight

preflight() {
  step "preflight"

  if [ ! -d "$WIRED_DIR" ]; then
    echo "    ✗ wired/ not found next to install.sh — run this from the navi repo root."
    exit 1
  fi

  if [ "$EUID" -eq 0 ]; then
    echo "    ✗ run as your normal user, not root. Privileged steps will use doas/sudo."
    exit 1
  fi

  # shellcheck disable=SC1091
  . /etc/os-release
  # ID=navi means a previous run already branded this machine; anything
  # else must be plain Debian 13 (trixie).
  if [ "${ID:-}" = "navi" ]; then
    info "this machine is already navi ${VERSION_ID:-unknown} — refreshing"
  elif [ "${ID:-}" != "debian" ] || [ "${VERSION_ID:-}" != "13" ]; then
    echo "    ✗ this installer targets Debian 13 (trixie); detected: ${PRETTY_NAME:-unknown}."
    echo "    aborting."
    exit 1
  fi
  ok "Debian 13 (trixie) confirmed"

  if command -v doas >/dev/null 2>&1; then
    DOAS="doas"
  elif command -v sudo >/dev/null 2>&1; then
    DOAS="sudo"
  else
    echo "    ✗ neither doas nor sudo found — install one, then re-run."
    exit 1
  fi
  ok "privilege escalation via $DOAS"
}

# ---------------------------------------------------------------- packages

install_packages() {
  step "installing packages"
  $DOAS apt update
  # shellcheck disable=SC2068
  $DOAS apt install -y ${PKGS[@]}
  ok "${#PKGS[@]} packages installed"

  # ydotool lives in trixie-backports only; backports are on by default on navi
  if [ ! -f /etc/apt/sources.list.d/trixie-backports.list ]; then
    echo "deb http://deb.debian.org/debian trixie-backports main" \
      | $DOAS tee /etc/apt/sources.list.d/trixie-backports.list >/dev/null
    $DOAS apt update
    ok "trixie-backports enabled"
  else
    info "trixie-backports already enabled"
  fi
  $DOAS apt install -y -t trixie-backports ydotool
  ok "ydotool installed from backports"
}

# ---------------------------------------------------------------- mpvpaper

build_mpvpaper() {
  step "mpvpaper (animated wallpapers)"
  if command -v mpvpaper >/dev/null 2>&1; then
    ok "mpvpaper already installed — skipping build"
    return 0
  fi

  local src="$HOME/src/mpvpaper"
  if [ ! -d "$src" ]; then
    git clone --single-branch https://github.com/xvoidsx/mpvpaper "$src"
  else
    info "mpvpaper source already cloned at $src"
  fi

  # the fork's scripts/build_install.sh builds + installs only — wallpaper
  # selection belongs to the navi installer, never to the fork.
  bash "$src/scripts/build_install.sh"
  ok "mpvpaper built and installed"
}

# ---------------------------------------------------------------- agents

# Agent-native from the first boot: ollama runtime, opencode, and omp.
# Binaries land in /usr/bin so every user on the machine gets them.
setup_agents() {
  step "agent runtime (ollama, opencode, omp)"

  if command -v ollama >/dev/null 2>&1; then
    ok "ollama already installed"
  else
    info "installing ollama..."
    local tmp
    tmp="$(mktemp)"
    curl -fsSL https://ollama.com/install.sh -o "$tmp"
    $DOAS sh "$tmp"
    rm -f "$tmp"
    if $DOAS systemctl enable --now ollama 2>/dev/null; then
      ok "ollama installed and enabled"
    else
      warn "ollama installed but the service did not enable — run: doas systemctl enable --now ollama"
    fi
  fi

  # the official installer drops the binary in ~/.opencode/bin; promote it
  # to /usr/bin so it's on every user's PATH.
  if [ -x /usr/bin/opencode ]; then
    ok "opencode already in /usr/bin"
  else
    info "installing opencode..."
    curl -fsSL https://opencode.ai/install | bash
    $DOAS install -m 0755 "$HOME/.opencode/bin/opencode" /usr/bin/opencode
    ok "opencode -> /usr/bin/opencode"
  fi

  # the omp installer honors PI_INSTALL_DIR — straight into /usr/bin.
  if [ -x /usr/bin/omp ]; then
    ok "omp already in /usr/bin"
  else
    info "installing omp (oh-my-pi)..."
    curl -fsSL https://omp.sh/install | $DOAS env PI_INSTALL_DIR=/usr/bin sh
    ok "omp -> /usr/bin/omp"
  fi
}

# ---------------------------------------------------------------- deploy: /usr/share/navi

deploy_share() {
  step "deploying the iron structure -> $WIRED_SHARE"
  $DOAS mkdir -p "$SHARE_DIR"
  # fresh copy of the desktop layer; user configs are deployed from here
  $DOAS rm -rf "$WIRED_SHARE"
  $DOAS cp -a "$WIRED_DIR" "$WIRED_SHARE"
  $DOAS chmod -R a+rX "$WIRED_SHARE"
  # executable bit: every repo script must run no matter how the repo was
  # fetched (clone, zip, ...). /usr/bin copies are set separately by
  # install_commands via install -m 0755.
  $DOAS find "$SHARE_DIR" -name '*.sh' -exec chmod 0755 {} +
  ok "wired/ deployed to $WIRED_SHARE"

  if [ -d "$SYSTEM_DIR" ]; then
    $DOAS cp -a "$SYSTEM_DIR"/. "$SHARE_DIR"/
    ok "system/ deployed to $SHARE_DIR"
  fi
}

# ---------------------------------------------------------------- deploy: user configs

backup_if_changed() {
  # backup_if_changed <source> <dest> — backs up dest if it exists and differs
  local src="$1" dest="$2"
  if [ -e "$dest" ] && ! cmp -s "$src" "$dest"; then
    local rel="${dest#$HOME/}"
    mkdir -p "$BACKUP_DIR/$(dirname "$rel")"
    cp -a "$dest" "$BACKUP_DIR/$rel"
    info "backed up ~/$rel"
  fi
}

deploy_config() {
  # deploy_config <repo-relpath> <dest> [mode]
  local rel="$1" dest="$2" mode="${3:-644}"
  local src="$WIRED_SHARE/$rel"
  if [ ! -e "$src" ]; then
    warn "missing in wired/: $rel — skipping"
    return 0
  fi
  mkdir -p "$(dirname "$dest")"
  backup_if_changed "$src" "$dest"
  install -m "$mode" "$src" "$dest"
  ok "${dest#$HOME/} deployed"
}

deploy_configs() {
  step "deploying user configs (backups -> $BACKUP_DIR)"

  deploy_config "sway/config"                 "$HOME/.config/sway/config"
  deploy_config "i3/config"                   "$HOME/.config/i3/config"
  deploy_config "waybar/config.jsonc"         "$HOME/.config/waybar/config.jsonc"
  deploy_config "waybar/style.css"            "$HOME/.config/waybar/style.css"
  deploy_config "polybar/config.ini"          "$HOME/.config/polybar/config.ini"
  deploy_config "polybar/battery-combined-shell.sh" "$HOME/.config/polybar/battery-combined-shell.sh" 755
  deploy_config "rofi/config.rasi"            "$HOME/.config/rofi/config.rasi"
  deploy_config "picom/picom.conf"            "$HOME/.config/picom/picom.conf"
  deploy_config "terminals/alacritty.toml"    "$HOME/.config/alacritty/alacritty.toml"
  deploy_config "terminals/foot.ini"          "$HOME/.config/foot/foot.ini"
  deploy_config "cliamp/config.toml"         "$HOME/.config/cliamp/config.toml"
  deploy_config "herdr/config.toml"          "$HOME/.config/herdr/config.toml"
  deploy_config "conky.conf"                  "$HOME/.config/conky/conky.conf"
  deploy_config "dunstrc"                     "$HOME/.config/dunst/dunstrc"
  deploy_config "tmux/tmux.conf"              "$HOME/.tmux.conf"
  deploy_config "vimrc"                       "$HOME/.vimrc"
  # best-effort agent configs; paths to be confirmed against the apps
  deploy_config "ai/opencode/tui.json"        "$HOME/.config/opencode/tui.json"
  deploy_config "ai/pi/agent/themes/nightshadeNeon.json" \
                                              "$HOME/.config/pi/agent/themes/nightshadeNeon.json"

  # bashrc: source ours idempotently instead of overwriting the user's
  local line='[ -f /usr/share/navi/wired/bashrc ] && source /usr/share/navi/wired/bashrc'
  if ! grep -qxF "$line" "$HOME/.bashrc" 2>/dev/null; then
    cp -a "$HOME/.bashrc" "$BACKUP_DIR/bashrc" 2>/dev/null || true
    printf '\n# navi wired bashrc\n%s\n' "$line" >> "$HOME/.bashrc"
    ok ".bashrc now sources navi's bashrc"
  else
    info ".bashrc already sources navi's bashrc"
  fi

  verify_wallpaper_wiring
}

verify_wallpaper_wiring() {
  # the canonical sway config should paint the animated wallpaper first boot:
  #   exec swaybg  -i /usr/share/navi/wired/wp/lain3wp.jpg -m fill   (fallback)
  #   exec mpvpaper ALL -o "loop panscan=1" /usr/share/navi/wired/wp/gifpaperslain/navi-lain.gif
  local cfg="$HOME/.config/sway/config"
  if grep -q "mpvpaper" "$cfg" 2>/dev/null && grep -q "swaybg" "$cfg" 2>/dev/null; then
    ok "sway config wires mpvpaper + swaybg fallback"
  else
    warn "sway config lacks wallpaper exec lines — check wired/sway/config"
  fi
}

# ---------------------------------------------------------------- deploy: commands

install_commands() {
  step "installing navi commands -> /usr/bin"

  install_bin() { # <src-rel> <name>
    local src="$WIRED_SHARE/scripts/$1" name="$2"
    [ -e "$src" ] || { warn "missing script: $1 — skipping"; return 0; }
    $DOAS install -m 0755 "$src" "/usr/bin/$name"
    ok "$name"
  }

  install_bin "nslock.sh"           "nslock"
  install_bin "naviWalls.sh"        "naviWalls"
  install_bin "gifpaperslain.sh"    "gifpaperslain"
  install_bin "navi-wallpaper.sh"   "navi-wallpaper"
  install_bin "wired_power_menu.sh" "power_menu"
  install_bin "remoji.sh"           "remoji"
  install_bin "learn.sh"            "learn"
  install_bin "navi-Q.sh"           "navi-Q"
  install_bin "navi-Qx.sh"          "navi-Qx"

  # remoji reads emojis.txt via $emojidir; point it at the share dir instead
  if grep -q '$HOME/wiredWM/scripts-config/remoji' /usr/bin/remoji 2>/dev/null; then
    $DOAS sed -i 's|$HOME/wiredWM/scripts-config/remoji|/usr/share/navi/wired/scripts|' /usr/bin/remoji
    info "remoji now reads emojis.txt from the share dir"
  fi
  # learn reads the manual from the old repo path; point it at the share dir
  if grep -q "wiredWM" /usr/bin/learn 2>/dev/null; then
    $DOAS sed -i "s|\$HOME/wiredWM/scripts-config/manual|/usr/share/navi/wired/manual|" /usr/bin/learn
    info "learn now reads the manual from the share dir"
  fi

  step "registering with the navi launcher (rofi)"
  # rofi's drun mode reads .desktop files, not PATH — this is what makes the
  # commands show up in the launcher.
  for d in "$WIRED_SHARE"/applications/*.desktop; do
    [ -e "$d" ] || { warn "no .desktop files in wired/applications/"; break; }
    $DOAS install -m 0644 "$d" /usr/share/applications/
    ok "$(basename "$d") registered"
  done
}

# ---------------------------------------------------------------- system: doas, environment

setup_doas() {
  step "doas"
  if [ -f /etc/doas.conf ] && grep -q "permit persist $USER as root" /etc/doas.conf; then
    ok "doas already configured for $USER"
  else
    printf 'permit persist %s as root\n' "$USER" | $DOAS tee -a /etc/doas.conf >/dev/null
    ok "doas configured for $USER"
  fi
}

setup_environment() {
  step "environment"
  if [ -f "$WIRED_SHARE/../system/environment" ]; then
    if ! cmp -s "$WIRED_SHARE/../system/environment" /etc/environment 2>/dev/null; then
      $DOAS cp -a /etc/environment "$BACKUP_DIR/etc-environment" 2>/dev/null || true
      $DOAS install -m 0644 "$WIRED_SHARE/../system/environment" /etc/environment
      ok "/etc/environment updated (old one backed up)"
    else
      ok "/etc/environment already in place"
    fi
  else
    info "no system/environment shipped — skipping"
  fi
}

# ---------------------------------------------------------------- flatpak

setup_flatpak() {
  step "flatpak + flathub"
  if ! flatpak remote-list 2>/dev/null | grep -q flathub; then
    $DOAS flatpak remote-add --if-not-exists flathub \
      https://dl.flathub.org/repo/flathub.flatpakrepo
    ok "flathub remote added"
  else
    ok "flathub already present"
  fi

  # keep flatpak icons inside the navi theme instead of adwaita
  local override_dir="$HOME/.local/share/flatpak/overrides"
  mkdir -p "$override_dir"
  if [ ! -f "$override_dir/global" ]; then
    cat > "$override_dir/global" <<'EOF'
[Environment]
GTK_THEME=Yaru-dark
ICON_THEME=Papirus-Dark
EOF
    ok "flatpak theme overrides applied"
  else
    info "flatpak overrides already exist"
  fi
}

# ---------------------------------------------------------------- user dirs + app installers

setup_dirs() {
  step "user directories"
  mkdir -p "$HOME/Pictures/Screenshots"
  ok "~/Pictures/Screenshots ready"
}

run_app_installers() {
  step "app + agent installers"
  local dir="$REPO_DIR/scripts/installers"
  [ -d "$dir" ] || { info "no scripts/installers/ — skipping"; return 0; }
  local f
  for f in "$dir"/*.sh; do
    [ -e "$f" ] || break
    local name
    name="$(basename "$f" .sh)"
    if confirm "run installer: $name?"; then
      bash "$f"
      ok "$name finished"
    else
      info "skipped $name"
    fi
  done
}

# ---------------------------------------------------------------- login screen

# SDDM with the navi QML theme: session picker (Wayland / X11), the empty-set
# mark, and the wallpaper behind the login.
setup_sddm() {
  step "login screen (sddm)"
  $DOAS install -d -m 755 /etc/sddm.conf.d
  $DOAS install -m 644 "$WIRED_DIR/sddm/navi.conf" /etc/sddm.conf.d/navi.conf
  $DOAS rm -rf /usr/share/sddm/themes/navi
  $DOAS cp -r "$WIRED_DIR/sddm/themes/navi" /usr/share/sddm/themes/navi
  $DOAS install -m 644 "$WIRED_DIR/sessions/navi.desktop" \
    /usr/share/wayland-sessions/navi.desktop
  $DOAS install -m 644 "$WIRED_DIR/sessions/navi-x11.desktop" \
    /usr/share/xsessions/navi.desktop
  $DOAS systemctl enable sddm
  ok "sddm serves the navi login; pick Wayland or X11 at the prompt"
}

# ---------------------------------------------------------------- identity

# Brand the machine as navi: /etc/os-release, lsb-release, login banners,
# VERSION stamp, and a fastfetch config carrying the empty-set logo.
install_identity() {
  step "navi identity"
  backup_file /etc/os-release
  backup_file /etc/lsb-release
  backup_file /etc/issue
  backup_file /etc/issue.net
  $DOAS install -m 644 "$WIRED_DIR/identity/os-release"  /etc/os-release
  $DOAS install -m 644 "$WIRED_DIR/identity/lsb-release" /etc/lsb-release
  $DOAS install -m 644 "$WIRED_DIR/identity/issue"       /etc/issue
  $DOAS install -m 644 "$WIRED_DIR/identity/issue.net"   /etc/issue.net
  $DOAS install -m 644 "$WIRED_DIR/VERSION" /usr/share/navi/VERSION
  ok "/etc/os-release now reports navi"
  deploy_config "fastfetch/config.jsonc" "$HOME/.config/fastfetch/config.jsonc"
  ok "fastfetch carries the navi logo"
}

# ---------------------------------------------------------------- done

done_banner() {
  cat <<'EOF'

  ╔══════════════════════════════════════════════════════════════╗
  ║                                                              ║
  ║   ✓  navi is wired in.                                      ║
  ║                                                              ║
  ║   log out and back in (or reboot), then start sway.          ║
  ║   your animated wallpaper — navi-lain.gif — will be          ║
  ║   waiting for you.                                           ║
  ║                                                              ║
  ║   config backups live in ~/.config-backup-navi/              ║
  ║   press Super+D and try: naviWalls · remoji · learn          ║
  ║                                                              ║
  ╚══════════════════════════════════════════════════════════════╝

EOF
}

# ---------------------------------------------------------------- main

main() {
  for a in "$@"; do
    case "$a" in
      --yes)  ASSUME_YES=1 ;;
      --help) usage ;;
      *) echo "unknown option: $a (try --help)"; exit 1 ;;
    esac
  done

  banner
  if ! confirm "install the navi wired desktop on this machine?"; then
    echo "    ok, maybe next time. the wired will wait."
    exit 0
  fi

  preflight
  install_packages
  build_mpvpaper
  deploy_share
  deploy_configs
  install_identity
  install_commands
  setup_doas
  setup_agents
  setup_environment
  setup_flatpak
  setup_dirs
  setup_sddm
  run_app_installers
  done_banner
}

main "$@"
