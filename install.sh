#!/usr/bin/env bash
#
# install.sh — the navi desktop installer
#
# Installs the navi "wired" desktop layer on Debian 13 (trixie):
#   packages -> /usr/share/navi/wired (the iron structure) -> ~/.config/*
#   commands -> /usr/bin + /usr/share/applications (rofi-visible)
#   agent runtime: ollama, opencode, omp, goose -> /usr/bin
#   doas, flatpak/flathub, wallpapers, first-boot behavior
#
# Idempotent: safe to re-run. Existing configs are backed up, never clobbered.
# Run as your normal user (not root). Privileged steps use doas/sudo.
#
#   ./install.sh [--yes] [--deploy-only] [--help]
#
# --yes   non-interactive: accept defaults, run every app installer
# --deploy-only   refresh an installed machine to this repo's state:
#                 no provisioning, no prompts. navi-update shells out
#                 to this mode; in it, user-modified configs are kept.

set -euo pipefail

INSTALL_T0=$SECONDS   # provision stopwatch: total time lands in done_banner

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
DEPLOY_ONLY=0
# wired/VERSION is the single source of truth for the navi version
# (e.g. 1.3 "mika"). the banner, the deploy manifest, and
# /etc/navi/version all derive from it — never hardcode it elsewhere.
NAVI_VERSION="$(cat "$WIRED_DIR/VERSION" 2>/dev/null || echo "unknown")"
# update mode (set by --deploy-only): deploy_config will not overwrite a
# config the user has modified — manifest checksums are the baseline.
NAVI_UPDATE_MODE=0
# the deploy manifest: sha256, navi version, repo relpath, dest, mode —
# one line per deployed file. it lets navi-update tell "untouched" from
# "the user customized this", and it powers navi-wired-restore.
MANIFEST_DIR="/var/lib/navi"
MANIFEST_FILE="$MANIFEST_DIR/deploy-manifest.tsv"
MANIFEST_TMP=""

# Every apt package, audited against Debian 13 "trixie" (2026-09-10).
# Corrections baked in: opendoas (doas is a transitional dummy), no slock
# (virtual, provided by suckless-tools), imagemagick-7.q16 (imagemagick is
# virtual). i3status + i3blocks were dropped: navi uses polybar, not i3bar,
# on the X11 session.
PKGS=(
  i3 i3lock-fancy nitrogen pamixer wget curl git htop opendoas lsd
  nsxiv pulseaudio-utils xcompmgr picom waybar alacritty fonts-inter xterm
  arandr nemo rofi xss-lock feh pandoc volumeicon-alsa polybar blueman dunst
  flameshot meteo-qt pasystray ffmpeg mpv kitty stterm surf conky-all suckless-tools zathura zathura-pdf-poppler
  lxpolkit lxappearance vim nnn cmus cava amfora sway swaylock
  swayidle swaybg grimshot xdg-desktop-portal-wlr qt5ct tty-clock wf-recorder
  brightnessctl sakura foot gsimplecal calcurse pavucontrol yaru-theme-gtk yaru-theme-icon
  # sddm-theme-maldives is installed alongside sddm on purpose: it satisfies
  # sddm's "sddm-theme" requirement with a 1.3 MB theme, so apt never reaches
  # for sddm-theme-debian-breeze — which would drag in plasma-workspace.
  glow pipx wl-clipboard wlr-randr jq imagemagick-7.q16 tmux shotman nwg-look fastfetch sddm-theme-maldives sddm qml6-module-qtmultimedia xwayland zenity
  # SDDM navi theme imports these QML modules explicitly (Main.qml lines 2-3);
  # minimal installs don't pull them in on their own, and without them the
  # greeter falls back to the default theme with a module-not-installed error.
  qml6-module-qtquick-controls qml6-module-qtquick-layouts
  fonts-jetbrains-mono fonts-firacode fonts-noto fonts-cascadia-code wdisplays papirus-icon-theme
  fonts-font-awesome fonts-material-design-icons-iconfont bibata-cursor-theme
  cmatrix lynx elinks w3m libnotify-bin flatpak gnome-software-plugin-flatpak dconf-cli
  chromium firefox-esr
  # zstd: the ollama installer needs it to unpack its payload.
  zstd
  # wifi firmware bundle: every common wireless chipset, so networking is
  # seamless on any machine. firmware blobs are inert without matching
  # hardware, so shipping them all is safe.
  firmware-realtek firmware-iwlwifi firmware-atheros firmware-brcm80211 firmware-mediatek
  # small system utilities we love (2026-09-13): archives, rainbow cat,
  # python3 dev conveniences, pandora radio, firewall (+ graphical frontend),
  # node runtime, graphical sftp/ssh file transfer.
  zip unzip bzip2 lolcat python-dev-is-python3 pianobar ufw gufw nodejs npm filezilla imv
)

# Commands deploy to /usr/bin (not /usr/local/bin) so every user on the
# machine gets them — navi is a multi-user system.

# Webapps installed out of the box at the end of installation, via
# `navi-webapp install`. Names must match .desktop filenames in
# wired/naviApps/webapps/ exactly. Tweak freely — users can always
# add/remove more from the naviApps store.
DEFAULT_WEBAPPS=(
  navi-radio neighborli glyyph telegram element pandora
  github youtube yomi twitch discord perplexity dropbox
)

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
EOF
  # the version line is spaced out for the aesthetic; wired/VERSION is the
  # single source of truth, so this never goes stale.
  printf '\n       n a v i   %s\n' "$(printf '%s' "$NAVI_VERSION" | sed 's/./& /g; s/ $//')"
  cat <<'EOF'

       ナビ — everybody has already entered the wired

EOF
}

step()  {
  # per-step stopwatch: how long the previous step took, so slow steps
  # (big downloads, long builds) are visible in the log, not mysterious.
  local now=$SECONDS
  if [ -n "${STEP_T0:-}" ]; then
    printf '    · previous step took %s\n' "$(fmt_dur $(( now - STEP_T0 )))"
  fi
  STEP_T0=$now
  echo; echo "──▶ $1"
}
ok()    { echo "    ✓ $1"; }
info()  { echo "    · $1"; }
warn()  { echo "    ! $1"; }

fmt_dur() { # <seconds> -> H:MM:SS (or M:SS under an hour)
  local s="$1" h m
  h=$(( s / 3600 )); m=$(( (s % 3600) / 60 )); s=$(( s % 60 ))
  if [ "$h" -gt 0 ]; then printf '%d:%02d:%02d' "$h" "$m" "$s"
  else printf '%d:%02d' "$m" "$s"; fi
}

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
  # ydotool's daemon only works for users in the input group
  # (see /usr/share/doc/ydotool/README.Debian). group membership takes
  # effect on the next login — i.e. the first SDDM login after install.
  $DOAS groupadd -f input
  $DOAS usermod -aG input "$USER"
  ok "$USER added to the input group for ydotool"
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
  # selection belongs to the navi installer, never to the fork. it must run
  # from the fork's repo root (it uses relative meson/ninja paths).
  ( cd "$src" && bash scripts/build_install.sh )
  ok "mpvpaper built and installed"
}

# ---------------------------------------------------------------- agents

# Agent-native from the first boot: ollama runtime, opencode, omp, goose.
# Binaries land in /usr/bin (or /usr/local/bin via npm) so every user on
# the machine gets them.
#
# Downloads go through fetch() (timeouts + retries) so a stalled host fails
# fast instead of hanging the install forever — learned the hard way on the
# x200, where a dead ollama.com connection sat silent for an hour. Each
# agent host is probed first; a host that's down (or a download that fails
# after retries) warns and moves on to the next agent instead of killing a
# two-hour provision. Anything skipped here can be picked up later with
# ./install.sh --yes once the network cooperates.
#
# The installer scripts themselves get the same treatment: every one runs
# under `timeout -k 30 300` with stdin from /dev/null, so a hung installer
# degrades to a warning instead of wedging the whole provision (seen on the
# Cloudbook with the omp and pi installers). Third-party install scripts
# additionally run under `setsid` (no controlling terminal): pi's official
# installer waits for a keypress on /dev/tty when a terminal is present,
# and detaching takes its designed non-interactive path instead.
fetch() { # fetch <url> [curl args...] — curl with sane timeouts and retries
  curl -fsSL --connect-timeout 20 --max-time 600 \
       --retry 3 --retry-all-errors "$@"
}

host_up() { # host_up <url> -> 0 if the host answers a quick probe
  curl -fsS --connect-timeout 10 --max-time 15 -o /dev/null "$1" 2>/dev/null
}

setup_agents() {
  step "agent runtime (ollama, opencode, omp, goose)"

  if command -v ollama >/dev/null 2>&1; then
    ok "ollama already installed"
  elif ! host_up https://ollama.com/install.sh; then
    warn "ollama.com unreachable — skipping ollama (re-run install.sh --yes later)"
  else
    info "installing ollama..."
    local tmp
    tmp="$(mktemp)"
    if fetch https://ollama.com/install.sh -o "$tmp" \
        && $DOAS setsid timeout -k 30 300 sh "$tmp" </dev/null; then
      # enable is pure symlink work (client-side), so it lands in the target
      # correctly even when provisioning inside the installer chroot; the
      # start is best-effort — in the chroot the system bus belongs to the
      # live ISO, so a failed start just means "starts on next boot".
      if $DOAS systemctl enable ollama 2>/dev/null; then
        ok "ollama installed and enabled on boot"
        if $DOAS systemctl start ollama 2>/dev/null; then
          ok "ollama service started"
        else
          info "ollama will start on next boot"
        fi
      else
        warn "ollama installed but the service did not enable — run: doas systemctl enable --now ollama"
      fi
    else
      warn "ollama download/install failed — skipping (re-run install.sh --yes later)"
    fi
    rm -f "$tmp"
  fi

  # the official installer drops the binary in ~/.opencode/bin; promote it
  # to /usr/bin so it's on every user's PATH.
  if [ -x /usr/bin/opencode ]; then
    ok "opencode already in /usr/bin"
  elif ! host_up https://opencode.ai/install; then
    warn "opencode.ai unreachable — skipping opencode (re-run install.sh --yes later)"
  else
    info "installing opencode..."
    local octmp
    octmp="$(mktemp)"
    if fetch https://opencode.ai/install -o "$octmp" \
        && setsid timeout -k 30 300 bash "$octmp" </dev/null \
        && [ -x "$HOME/.opencode/bin/opencode" ] \
        && $DOAS install -m 0755 "$HOME/.opencode/bin/opencode" /usr/bin/opencode; then
      ok "opencode -> /usr/bin/opencode"
    else
      warn "opencode install failed — skipping (re-run install.sh --yes later)"
    fi
    rm -f "$octmp"
  fi

  # the omp installer honors PI_INSTALL_DIR — straight into /usr/bin.
  if [ -x /usr/bin/omp ]; then
    ok "omp already in /usr/bin"
  elif ! host_up https://omp.sh/install; then
    warn "omp.sh unreachable — skipping omp (re-run install.sh --yes later)"
  else
    info "installing omp (oh-my-pi)..."
    local omptmp
    omptmp="$(mktemp)"
    if fetch https://omp.sh/install -o "$omptmp" \
        && $DOAS setsid timeout -k 30 300 env PI_INSTALL_DIR=/usr/bin sh "$omptmp" </dev/null; then
      ok "omp -> /usr/bin/omp"
    else
      warn "omp install failed — skipping (re-run install.sh --yes later)"
    fi
    rm -f "$omptmp"
  fi

  # goose (aaif-goose/goose — ex-Block, now the Linux Foundation's Agentic
  # AI Foundation): open-source AI agent, CLI + desktop app. Its install
  # script is designed for non-interactive use (automation/docker): both
  # interactive points are tty-guarded and degrade gracefully — the anti-pi.
  # GOOSE_BIN_DIR pins the install location like omp's PI_INSTALL_DIR;
  # GOOSE_VERSION pins the release (never float on the moving `stable` tag
  # in a shipped ISO); the musl variant is fully static (sidesteps libssl).
  # CONFIGURE=false is explicit: first-run `goose configure` happens at
  # user-invocation time, never during provisioning. The script unpacks a
  # .tar.bz2, so bzip2 must be in PKGS.
  if [ -x /usr/bin/goose ]; then
    ok "goose already in /usr/bin"
  elif ! host_up https://github.com/aaif-goose/goose/releases/download/v1.50.1/download_cli.sh; then
    warn "goose release unreachable — skipping goose (re-run install.sh --yes later)"
  else
    info "installing goose v1.50.1..."
    local goosetmp
    goosetmp="$(mktemp)"
    if fetch https://github.com/aaif-goose/goose/releases/download/v1.50.1/download_cli.sh -o "$goosetmp" \
        && $DOAS setsid timeout -k 30 300 env GOOSE_BIN_DIR=/usr/bin GOOSE_VERSION=v1.50.1 GOOSE_LINUX_VARIANT=musl CONFIGURE=false bash "$goosetmp" </dev/null \
        && [ -x /usr/bin/goose ]; then
      ok "goose v1.50.1 -> /usr/bin/goose"
    else
      warn "goose install failed — skipping (re-run install.sh --yes later)"
    fi
    rm -f "$goosetmp"
  fi

  # herdr (agent multiplexer): pinned release, one static binary, no
  # installer script. arch-aware — upstream ships x86_64 and aarch64.
  if [ -x /usr/bin/herdr ]; then
    ok "herdr already in /usr/bin"
  elif ! host_up https://github.com; then
    warn "github unreachable — skipping herdr (re-run install.sh --yes later)"
  else
    info "installing herdr v0.9.0..."
    local herdr_arch herdr_tmp
    case "$(uname -m)" in
      x86_64)  herdr_arch="x86_64" ;;
      aarch64) herdr_arch="aarch64" ;;
      *)       herdr_arch="" ;;
    esac
    herdr_tmp="$(mktemp)"
    if [ -n "$herdr_arch" ] \
        && fetch "https://github.com/ogulcancelik/herdr/releases/download/v0.9.0/herdr-linux-${herdr_arch}" -o "$herdr_tmp" \
        && $DOAS install -m 0755 "$herdr_tmp" /usr/bin/herdr; then
      ok "herdr v0.9.0 -> /usr/bin/herdr"
    else
      warn "herdr install failed — skipping (re-run install.sh --yes later)"
    fi
    rm -f "$herdr_tmp"
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

backup_file() {
  # backup_file <path> — stashes the original at <path>.navi-orig if it exists
  local f="$1"
  [ -e "$f" ] || return 0
  [ -e "$f.navi-orig" ] || $DOAS cp -a "$f" "$f.navi-orig"
}

# ---------------------------------------------------------------- deploy manifest
# /var/lib/navi/deploy-manifest.tsv — one tab-separated line per deployed file:
#   <sha256> <tab> <navi-version> <tab> <repo-relpath> <tab> <dest> <tab> <mode>
# accumulated in a temp file during the deploy phase, then written once
# (root-owned) by manifest_write.

manifest_begin() {
  MANIFEST_TMP="$(mktemp /tmp/navi-manifest.XXXXXX)"
  if [ "$NAVI_UPDATE_MODE" -eq 1 ] && [ -s "$MANIFEST_FILE" ]; then
    # update mode: seed from the previous deploy so files we don't touch
    # keep their entries (their baseline stays valid).
    cp -a "$MANIFEST_FILE" "$MANIFEST_TMP"
    info "manifest seeded from previous deploy ($(wc -l < "$MANIFEST_TMP") entries)"
  fi
}

manifest_lookup() {
  # manifest_lookup <dest> — prints the recorded sha256 for dest, or nothing
  [ -s "$MANIFEST_TMP" ] 2>/dev/null || return 0
  awk -F'\t' -v d="$1" '$4 == d {print $1}' "$MANIFEST_TMP" | tail -n 1
}

manifest_record() {
  # manifest_record <src> <relpath> <dest> <mode>
  local src="$1" rel="$2" dest="$3" mode="$4"
  local sha
  sha="$(sha256sum "$src" | cut -d' ' -f1)"
  # one line per dest: drop any stale entry first, then append
  if [ -s "$MANIFEST_TMP" ]; then
    awk -F'\t' -v d="$dest" '$4 != d' "$MANIFEST_TMP" > "$MANIFEST_TMP.new"
    mv "$MANIFEST_TMP.new" "$MANIFEST_TMP"
  fi
  printf '%s\t%s\t%s\t%s\t%s\n' "$sha" "$NAVI_VERSION" "$rel" "$dest" "$mode" >> "$MANIFEST_TMP"
}

manifest_write() {
  step "writing deploy manifest"
  $DOAS mkdir -p "$MANIFEST_DIR"
  $DOAS install -m 644 "$MANIFEST_TMP" "$MANIFEST_FILE"
  ok "manifest -> $MANIFEST_FILE ($(wc -l < "$MANIFEST_TMP") files)"
  rm -f "$MANIFEST_TMP"
  MANIFEST_TMP=""
}

deploy_config() {
  # deploy_config <repo-relpath> <dest> [mode]
  # destinations outside $HOME (e.g. /etc/lynx.cfg) are deployed with $DOAS,
  # the same elevation install_identity/setup_environment use for /etc writes.
  local rel="$1" dest="$2" mode="${3:-644}"
  local src="$WIRED_SHARE/$rel"
  if [ ! -e "$src" ]; then
    warn "missing in wired/: $rel — skipping"
    return 0
  fi
  local elevate=""
  case "$dest" in
    "$HOME"/*) ;;
    *) elevate="$DOAS" ;;
  esac
  if [ "$NAVI_UPDATE_MODE" -eq 1 ] && [ -e "$dest" ]; then
    # update mode: never overwrite a config the user has touched. the
    # manifest holds the checksum of the navi default we last deployed —
    # if the live file still matches it, the file is untouched and takes
    # the new default. if it differs, the user customized it: leave it
    # alone and say so.
    local baseline live_sha
    baseline="$(manifest_lookup "$dest")"
    if [ -n "$baseline" ]; then
      # a failed read compares unequal -> treated as modified -> kept.
      # failing toward keeping the user's file is the safe direction.
      live_sha="$($elevate sha256sum "$dest" 2>/dev/null | cut -d' ' -f1 || true)"
      if [ "$live_sha" != "$baseline" ]; then
        info "kept your modified ${dest#$HOME/} (new defaults: navi-wired-restore --diff)"
        return 0
      fi
    else
      # pre-manifest install: no baseline to compare against. the backup
      # below is the safety net — differing files are stashed, never lost.
      info "no manifest baseline for ${dest#$HOME/} — deploying (backup first)"
    fi
  fi
  $elevate mkdir -p "$(dirname "$dest")"
  backup_if_changed "$src" "$dest"
  $elevate install -m "$mode" "$src" "$dest"
  manifest_record "$src" "$rel" "$dest" "$mode"
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
  # cmus + cava: the nightshadeNeon music setup. the cmus rc selects the
  # theme; the theme and cava config ship the full aesthetic.
  deploy_config "cmus/rc"                    "$HOME/.config/cmus/rc"
  deploy_config "cmus/nightshadeNeon.theme"  "$HOME/.config/cmus/nightshadeNeon.theme"
  deploy_config "cava/config"                "$HOME/.config/cava/config"
  deploy_config "conky.conf"                  "$HOME/.config/conky/conky.conf"
  deploy_config "dunstrc"                     "$HOME/.config/dunst/dunstrc"
  # native GTK apps (e.g. gsimplecal) need a theme set explicitly —
  # nothing else in the installer does this (flatpak theming is separate).
  deploy_config "gtk-2.0/gtkrc"              "$HOME/.gtkrc-2.0"
  deploy_config "gtk-3.0/settings.ini"        "$HOME/.config/gtk-3.0/settings.ini"
  deploy_config "gtk-4.0/settings.ini"        "$HOME/.config/gtk-4.0/settings.ini"
  # tmux reads ~/.config/tmux/tmux.conf first (since 3.1) — the XDG path
  # is the real config. the old ~/.tmux.conf target is retired below so a
  # stale copy can never shadow it.
  deploy_config "tmux/tmux.conf"              "$HOME/.config/tmux/tmux.conf"
  if [ -e "$HOME/.tmux.conf" ]; then
    [ -e "$HOME/.tmux.conf.navi-orig" ] \
      || cp -a "$HOME/.tmux.conf" "$HOME/.tmux.conf.navi-orig"
    rm -f "$HOME/.tmux.conf"
    ok "retired stale ~/.tmux.conf (config now lives at ~/.config/tmux/tmux.conf)"
  fi
  deploy_config "vimrc"                       "$HOME/.vimrc"
  # terminal browsers (the learn manual renders in elinks/lynx):
  # both run in terminal-default/mono mode so the transparent nightshadeNeon
  # terminal shows through — no white bars, no color clashes.
  # elinks reads the XDG location on navi; lynx colors live system-wide
  # in /etc/lynx.cfg while ~/.lynxrc forces color mode off.
  deploy_config "elinks/elinks.conf"          "$HOME/.config/elinks/elinks.conf"
  deploy_config "lynx/lynx.cfg"               "/etc/lynx.cfg"
  deploy_config "lynx/lynxrc"                 "$HOME/.lynxrc"
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
  # the canonical sway config execs /usr/bin/navi-wallpaper, which paints the
  # animated wallpaper first (mpvpaper) and falls back to the static swaybg
  # image at /usr/share/navi/wired/wp/navi-lain-rgbsplit.jpg.
  local cfg="$HOME/.config/sway/config"
  if grep -q "navi-wallpaper" "$cfg" 2>/dev/null; then
    ok "sway config wires navi-wallpaper (mpvpaper + swaybg fallback)"
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
  install_bin "navi-fetch.sh"      "navi-fetch"
  install_bin "navi-logo.sh"       "navi-logo"
  install_bin "wired_power_menu.sh" "power_menu"
  install_bin "remoji.sh"           "remoji"
  install_bin "learn.sh"            "learn"
  install_bin "navi-Q.sh"           "navi-Q"
  install_bin "navi-Qx.sh"          "navi-Qx"
  install_bin "navi-webapp.sh"      "navi-webapp"
  install_bin "navi-notifs.sh"       "navi-notifs"

  # distro-level tools live in scripts/ (repo root), outside the /wired
  # desktop layer — same /usr/bin destination, same rofi visibility.
  for pair in "navi-update.sh:navi-update" "navi-wired-restore.sh:navi-wired-restore"; do
    local src="$REPO_DIR/scripts/${pair%%:*}" name="${pair##*:}"
    if [ -e "$src" ]; then
      $DOAS install -m 0755 "$src" "/usr/bin/$name"
      ok "$name"
    else
      warn "missing script: scripts/${pair%%:*} — skipping"
    fi
  done

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
  # Respect ANY existing permit rule for this user — e.g. the installer's
  # temporary 'permit nopass' during provisioning. Appending a second rule
  # flips doas to last-match-wins and would silently revoke the nopass.
  if [ -f /etc/doas.conf ] && grep -q "permit .*$USER as root" /etc/doas.conf; then
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
GTK_THEME=Yaru-magenta-dark
ICON_THEME=Papirus-Dark
EOF
    ok "flatpak theme overrides applied"
  else
    info "flatpak overrides already exist"
  fi
  # Let sandboxed apps actually see the host icon/cursor themes: without
  # this the ICON_THEME above is a dead letter, since /usr/share/icons is
  # not in the sandbox by default. Mirrors the old wiredWM override_fp
  # step (cursor consistency, e.g. firefox). Read-only, user-level.
  if ! grep -q '^\[Context\]' "$override_dir/global" 2>/dev/null; then
    printf '\n[Context]\nfilesystems=%s/.icons:ro;/usr/share/icons:ro;\n' "$HOME" >> "$override_dir/global"
    ok "flatpak icon-directory overrides applied"
  else
    info "flatpak icon overrides already present"
  fi
}

# Let the local user manage system flatpaks without a password prompt.
# Rationale: on navi the user already holds doas root (permit persist),
# so the polkit prompt on every flatpak install/update was pure friction,
# not a real authorization boundary — it also broke non-interactive
# `flatpak update` inside navi-update. Remote users are unaffected; this
# only applies to local, active sessions.
setup_flatpak_polkit() {
  step "flatpak authorization"
  local rule_file="/etc/polkit-1/rules.d/49-navi-flatpak.rules"
  if [ -f "$rule_file" ]; then
    info "flatpak polkit rule already present"
    return 0
  fi
  $DOAS mkdir -p /etc/polkit-1/rules.d
  $DOAS tee "$rule_file" >/dev/null <<'EOF'
// navi: the local, active user manages system flatpaks without auth.
// They already have doas root; the prompt was friction, not security.
polkit.addRule(function(action, subject) {
  if (action.id.indexOf("org.freedesktop.Flatpak.") === 0 &&
      subject.local && subject.active) {
    return polkit.Result.YES;
  }
});
EOF
  $DOAS chmod 644 "$rule_file"
  ok "flatpak polkit rule installed"
}

# ---------------------------------------------------------------- user dirs + app installers

setup_dirs() {
  step "user directories"
  mkdir -p "$HOME/Pictures/Screenshots"
  mkdir -p "$HOME/Documents" "$HOME/Downloads" "$HOME/Videos" "$HOME/Music"
  ok "user directories ready"
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

# ---------------------------------------------------------------- default webapps
# Installs the DEFAULT_WEBAPPS set for the current user via navi-webapp.
# Per-user by design: launchers go to ~/.local/share/applications and
# their icons to ~/.local/share/icons (handled inside install-webapp.sh),
# so every user keeps their own set. Idempotent: re-running never
# duplicates launchers or breaks existing ones.
setup_webapps() {
  step "default webapps (${#DEFAULT_WEBAPPS[@]} launchers)"
  if command -v navi-webapp >/dev/null 2>&1; then
    navi-webapp install "${DEFAULT_WEBAPPS[@]}"
    ok "default webapps installed for $USER"
  else
    warn "navi-webapp not on PATH — skipping default webapp install"
  fi
}

# ---------------------------------------------------------------- gtk theme cohesion
# settings.ini covers plain GTK apps, but GSettings-aware apps (nemo and
# friends) read org.gnome.desktop.interface — whose schema default is
# Adwaita, which is why installs kept falling back to it. seed system-wide
# dconf defaults so Yaru-magenta-dark wins from first boot. no locks: users
# can still override per-account with gsettings or a theme tool.
setup_gtk_theme() {
  step "gtk theme defaults (yaru-magenta-dark)"
  $DOAS install -d -m 755 /etc/dconf/db/local.d
  $DOAS tee /etc/dconf/db/local.d/00-navi-theme >/dev/null <<'EOF'
[org/gnome/desktop/interface]
gtk-theme='Yaru-magenta-dark'
icon-theme='Papirus-Dark'
color-scheme='prefer-dark'
EOF
  # The profile is the piece RC20.x was missing: without
  # /etc/dconf/profile/user, dconf falls back to its internal user-db-only
  # profile and never reads the compiled local db — fresh installs fell
  # back to Adwaita despite the keyfile above. Standard Debian profile.
  $DOAS install -d -m 755 /etc/dconf/profile
  printf '%s\n' 'user-db:user' 'system-db:local' 'system-db:site' 'system-db:distro' \
    | $DOAS tee /etc/dconf/profile/user >/dev/null
  $DOAS dconf update
  ok "yaru-magenta-dark seeded as the gtk default (user-overridable)"
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
  # the theme is dead on arrival without these — a missing Main.qml or
  # metadata.desktop (which pins the Qt6 greeter) is how a login silently
  # falls back to something bland.
  for f in Main.qml metadata.desktop theme.conf; do
    [ -f "/usr/share/sddm/themes/navi/$f" ] \
      || warn "sddm theme file missing after deploy: $f"
  done
  $DOAS install -m 644 "$WIRED_DIR/sessions/navi.desktop" \
    /usr/share/wayland-sessions/navi.desktop
  $DOAS install -m 644 "$WIRED_DIR/sessions/navi-x11.desktop" \
    /usr/share/xsessions/navi.desktop
  $DOAS systemctl enable sddm
  ok "sddm serves the navi login; pick Wayland or X11 at the prompt"
}

# ---------------------------------------------------------------- chromium

# Chromium ships navi-flavored: the nightshadeNeon theme (by rav3ndust, on
# the Chrome Web Store) installs itself via managed enterprise policy on
# first launch — no clicks, no profile surgery. Force-install means the
# theme stays put while the policy is in place; that's the price of a
# curated default. Dark mode is forced explicitly too: Chromium's
# system-theme auto-detection is unreliable on sway (it needs
# xdg-desktop-portal's Settings portal to see the prefer-dark dconf key,
# and silently falls back to light without it), so a tiny wrapper in
# /usr/local/bin injects --force-dark-mode on every launch. That covers
# the stock launcher, the webapp launchers, xdg-open, and the terminal —
# and an explicit `chromium --force-light-mode` still wins.
setup_chromium() {
  step "chromium (nightshadeNeon theme via managed policy)"
  $DOAS install -d -m 755 /etc/chromium/policies/managed
  $DOAS install -m 644 "$WIRED_DIR/chromium/policies/managed/navi.json" \
    /etc/chromium/policies/managed/navi.json
  ok "nightshadeNeon theme installs on first Chromium launch"
  if [[ -x /usr/bin/chromium ]]; then
    $DOAS install -m 755 "$WIRED_DIR/chromium/usr-local-bin/chromium" \
      /usr/local/bin/chromium
    ok "chromium wrapper forces dark mode on every launch"
    # Debian's chromium.desktop hardcodes Exec=/usr/bin/chromium, which
    # bypasses the wrapper (and its --force-dark-mode) for Rofi and
    # launcher invocations. Shadow it under /usr/local/share/applications
    # — which outranks /usr/share/applications — with the Exec line
    # rewritten to the wrapper. Generated from Debian's own file so the
    # override tracks upstream launcher changes; the packaged file is
    # never modified.
    if [ -f /usr/share/applications/chromium.desktop ]; then
      $DOAS install -d -m 755 /usr/local/share/applications
      sed 's|^Exec=/usr/bin/chromium|Exec=/usr/local/bin/chromium|' \
        /usr/share/applications/chromium.desktop \
        | $DOAS tee /usr/local/share/applications/chromium.desktop >/dev/null
      ok "chromium.desktop override routes Rofi through the dark-mode wrapper"
    else
      warn "Debian chromium.desktop not found — skipping launcher override"
    fi
  else
    warn "chromium not found at /usr/bin/chromium; skipping dark-mode wrapper"
  fi
}

# ---------------------------------------------------------------- fonts

# JetBrainsMono Nerd Font: Debian ships no nerd-fonts packages, so we
# download it from the upstream release. Provides the PUA glyphs Waybar
# and lsd need (brightness icons, devicons). Matches the already-shipped
# JetBrains Mono.
setup_fonts() {
  step "nerd fonts (JetBrainsMono Nerd Font)"
  local font_dir="/usr/share/fonts/truetype/jetbrainsmono-nerd"
  if [ -f "$font_dir/JetBrainsMonoNerdFont-Regular.ttf" ]; then
    ok "JetBrainsMono Nerd Font already installed"
    return 0
  fi
  $DOAS mkdir -p "$font_dir"
  local tmp_zip="/tmp/JetBrainsMonoNerdFont.zip"
  if wget -q -O "$tmp_zip" "https://github.com/ryanoasis/nerd-fonts/releases/latest/download/JetBrainsMono.zip"; then
    $DOAS unzip -o -q "$tmp_zip" -d "$font_dir"
    rm -f "$tmp_zip"
    $DOAS fc-cache -f >/dev/null 2>&1 || true
    ok "JetBrainsMono Nerd Font installed"
  else
    warn "could not download JetBrainsMono Nerd Font — skipping"
    rm -f "$tmp_zip"
  fi
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
  # stamp the human-facing version from wired/VERSION (the single source of
  # truth) so the identity files can never ship a stale release number
  # again — every deploy and every navi-update refreshes these.
  local iver iname
  iver="$(printf '%s' "$NAVI_VERSION" | awk '{print $1}')"
  iname="$(printf '%s' "$NAVI_VERSION" | awk '{print $2}' | tr -d '"')"
  $DOAS sed -i \
    -e "s/^PRETTY_NAME=.*/PRETTY_NAME=\"navi $iver ($iname)\"/" \
    -e "s/^VERSION_ID=.*/VERSION_ID=\"$iver\"/" \
    -e "s/^VERSION=.*/VERSION=\"$iver ($iname)\"/" \
    -e "s/^VERSION_CODENAME=.*/VERSION_CODENAME=$iname/" \
    /etc/os-release
  $DOAS sed -i \
    -e "s/^DISTRIB_RELEASE=.*/DISTRIB_RELEASE=$iver/" \
    -e "s/^DISTRIB_CODENAME=.*/DISTRIB_CODENAME=$iname/" \
    -e "s/^DISTRIB_DESCRIPTION=.*/DISTRIB_DESCRIPTION=\"navi $iver ($iname)\"/" \
    /etc/lsb-release
  $DOAS sed -i -e "s/navi [0-9][0-9.]* \"[^\"]*\"/navi $iver \"$iname\"/" \
    /etc/issue /etc/issue.net
  ok "/etc/os-release stamped navi $iver ($iname)"
  # release channel + version: navi-update reads these to decide what
  # "newer" means. never clobber an existing channel — the user may have
  # opted into a different one (e.g. eiri).
  $DOAS mkdir -p /etc/navi
  if [ ! -f /etc/navi/channel ]; then
    echo "stable" | $DOAS tee /etc/navi/channel >/dev/null
    ok "release channel: stable"
  fi
  $DOAS install -m 644 "$WIRED_DIR/VERSION" /etc/navi/version
  ok "/etc/navi/version stamped ($NAVI_VERSION)"
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
  printf '    total provision time: %s\n' "$(fmt_dur $(( SECONDS - INSTALL_T0 )))"
}

# ---------------------------------------------------------------- main

main() {
  for a in "$@"; do
    case "$a" in
      --yes)         ASSUME_YES=1 ;;
      --deploy-only) DEPLOY_ONLY=1 ;;
      --help) usage ;;
      *) echo "unknown option: $a (try --help)"; exit 1 ;;
    esac
  done

  # --deploy-only: refresh an installed machine to this repo's state.
  # structurally incapable of provisioning: no confirm prompt, no user
  # creation, no LUKS handling. navi-update shells out to this mode, and
  # every setup step below is idempotent (guard-before-write) by design.
  if [ "$DEPLOY_ONLY" -eq 1 ]; then
    NAVI_UPDATE_MODE=1
    preflight
    install_packages
    build_mpvpaper
    deploy_share
    manifest_begin
    deploy_configs
    install_identity
    manifest_write
    install_commands
    setup_doas
    setup_agents
    setup_environment
    setup_flatpak
    setup_flatpak_polkit
    setup_gtk_theme
    setup_dirs
    setup_sddm
    setup_chromium
    setup_fonts
    run_app_installers
    setup_webapps
    ok "deploy-only refresh complete (navi $NAVI_VERSION)"
    exit 0
  fi

  banner
  if ! confirm "install the navi wired desktop on this machine?"; then
    echo "    ok, maybe next time. the wired will wait."
    exit 0
  fi

  preflight
  install_packages
  build_mpvpaper
  deploy_share
  manifest_begin
  deploy_configs
  install_identity
  manifest_write
  install_commands
  setup_doas
  setup_agents
  setup_environment
  setup_flatpak
  setup_flatpak_polkit
  setup_gtk_theme
  setup_dirs
  setup_sddm
  setup_chromium
  setup_fonts
  run_app_installers
  setup_webapps
  done_banner
}

main "$@"
