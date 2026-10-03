#!/usr/bin/env bash
#
# install.sh — the navi desktop installer
#
# Installs the navi "wired" desktop layer on Debian 13 (trixie):
#   packages -> /usr/share/navi/wired (the iron structure) -> ~/.config/*
#   commands -> /usr/bin + /usr/share/applications (rofi-visible)
#   agent runtime: ollama, opencode, omp, goose -> /usr/bin
#   editor: NaviVim (default terminal IDE) -> /usr/local + /etc/skel
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
# NAVI_CHANNEL: the release channel for this build, derived from
# wired/VERSION — e.g. 2.0 "eiri" -> eiri. the experimental build identity
# keys off this variable, never off which directories happen to exist.
NAVI_CHANNEL="$(printf '%s' "$NAVI_VERSION" | awk '{print $2}' | tr -d '"')"
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
  # sudo rides along for compatibility: third-party install scripts
  # (scripts/installers/*) and random upstream tooling expect it.
  # doas stays the navi-native way up; see setup_sudo.
  i3 i3lock-fancy nitrogen pamixer wget curl git htop opendoas sudo lsd
  nsxiv pulseaudio-utils xcompmgr picom waybar alacritty fonts-inter xterm
  # navi-bluetooth (eiri): BlueZ for bluetoothctl, upower for the headset
  # battery fallback when a device doesn't report Battery Percentage itself.
  bluez upower
  arandr nemo rofi xss-lock feh pandoc volumeicon-alsa polybar blueman dunst
  flameshot meteo-qt pasystray ffmpeg mpv kitty stterm surf conky-all suckless-tools zathura zathura-pdf-poppler maim xdotool xclip
  lxpolkit lxappearance vim nnn cmus cava amfora sway swaylock
  swayidle swaybg grimshot xdg-desktop-portal-wlr qt5ct tty-clock wf-recorder
  brightnessctl sakura foot gsimplecal calcurse pavucontrol playerctl yaru-theme-gtk yaru-theme-icon bibata-cursor-theme
  # sddm-theme-maldives is installed alongside sddm on purpose: it satisfies
  # sddm's "sddm-theme" requirement with a 1.3 MB theme, so apt never reaches
  # for sddm-theme-debian-breeze — which would drag in plasma-workspace.
  glow pipx wl-clipboard wlr-randr jq imagemagick-7.q16 tmux shotman fastfetch sddm-theme-maldives sddm qml6-module-qtmultimedia xwayland zenity
  # SDDM navi theme imports these QML modules explicitly (Main.qml lines 2-3);
  # minimal installs don't pull them in on their own, and without them the
  # greeter falls back to the default theme with a module-not-installed error.
  qml6-module-qtquick-controls qml6-module-qtquick-layouts
  fonts-jetbrains-mono fonts-firacode fonts-noto fonts-cascadia-code wdisplays papirus-icon-theme moka-icon-theme
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
  # Hey Lain voice assistant (eiri): venv tooling + audio capture libs.
  # The venv itself (faster-whisper, piper) is built by setup_heylain.
  python3-venv python3-pip pipewire-audio-client-libraries alsa-utils
  # libspa-0.2-bluetooth: BlueZ audio backend for PipeWire/WirePlumber.
  # WirePlumber builds its module graph at boot, so this must be installed
  # before first boot — installing it mid-session needs a wireplumber restart.
  libspa-0.2-bluetooth
  # NaviVim (default terminal IDE): telescope needs ripgrep + fd
  # (Debian calls it fd-find), treesitter parsers and fzf-native compile
  # at first launch (build-essential), shellcheck for config linting.
  # fzf also ships system-wide: ani-cli needs it, and it's handy
  # everywhere else too.
  ripgrep fd-find fzf shellcheck build-essential
  # eiri QoL prototypes: Alt+Tab switcher (screenshot/crop/thumbnail) and
  # the bottom-center volume OSD (GTK3 popup).
  grim python3-pil python3-gi gir1.2-gtk-3.0
  # zram-tools: compressed swap for low-memory machines (setup_zram
  # enables it only when RAM <= 2 GiB). the cloudbook bench proved big
  # deb unpacks OOM without it.
  zram-tools
  # navi-power mod (eiri): power-profiles-daemon provides
  # powerprofilesctl; upower gives honest time-to-empty/full estimates.
  power-profiles-daemon upower
  # gnome-keyring: secret store for Element, Signal, Chromium, etc.
  # libpam-gnome-keyring auto-unlocks the login keyring via SDDM.
  gnome-keyring libpam-gnome-keyring
  # wlsunset: night light for Wayland (used by navi-displays)
  wlsunset
)

# Commands deploy to /usr/bin (not /usr/local/bin) so every user on the
# machine gets them — navi is a multi-user system.

# Webapps installed out of the box at the end of installation, via
# `navi-webapp install`. Names must match .desktop filenames in
# wired/naviApps/webapps/ exactly. Tweak freely — users can always
# add/remove more from the naviApps store.
# (telegram and element graduated to native apps — both install via
# setup_telegram/setup_element below.)
DEFAULT_WEBAPPS=(
  navi-radio neighborli glyyph pandora
  github youtube yomi twitch discord perplexity dropbox
  xbox-cloud geforce-now
  # Workspace Stack — installed out of the box on every fresh install
  # (Raven, 2026-09-30). Users can remove any of them from the naviApps store.
  gmail googledrive fizzy notion airtable
  # Full Google suite curated in naviApps (Raven, 2026-09-30)
  googlechat googledocs googlemessages googlephotos
  # Full Proton suite (Raven, 2026-09-30)
  protonmail protoncalendar protondrive protonmeet protonpass
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
  # eiri: the experimental channel stamp. wired/VERSION stays the single
  # source of truth — the stamp keys off NAVI_CHANNEL, not the version
  # number and not which directories happen to exist.
  if [ "$NAVI_CHANNEL" = "eiri" ]; then
    printf '\n       ✦  e x p e r i m e n t a l   e i r i   b u i l d  ✦\n'
  fi
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
  # NB: absolute /usr/sbin paths — doas keeps the caller's PATH, and
  # launchers (waybar/rofi) don't have sbin on it. bare `groupadd`
  # dies here with "doas: groupadd: command not found".
  $DOAS /usr/sbin/groupadd -f input
  $DOAS /usr/sbin/usermod -aG input "$USER"
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

# Agent-native from the first boot: ollama runtime, opencode, omp, goose, hermes.
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
  step "agent runtime (ollama, opencode, omp, goose, hermes)"

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

  # wisp (xvoidsx/wisp — navi's own coding agent, opencode fork with
  # nightshadeNeon theme, Ollama-first, herdr integration). Installs from
  # the GitHub releases via the install.sh script.
  if [ -x /usr/bin/wisp ]; then
    ok "wisp already in /usr/bin"
  elif ! host_up https://raw.githubusercontent.com/xvoidsx/wisp/dev/install.sh; then
    warn "wisp installer unreachable — skipping wisp (re-run install.sh --yes later)"
  else
    info "installing wisp..."
    local wisptmp
    wisptmp="$(mktemp)"
    if fetch https://raw.githubusercontent.com/xvoidsx/wisp/dev/install.sh -o "$wisptmp" \
        && $DOAS setsid timeout -k 30 300 bash "$wisptmp" </dev/null; then
      ok "wisp -> /usr/bin/wisp"
    else
      warn "wisp install failed — skipping (re-run install.sh --yes later)"
    fi
    rm -f "$wisptmp"
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

  # pi (the pi agent): official curl installer from pi.dev. Faster updates
  # than npm, and the designed non-interactive path under setsid.
  if command -v pi >/dev/null 2>&1; then
    ok "pi already installed"
  elif ! host_up https://pi.dev/install.sh; then
    warn "pi.dev unreachable — skipping pi (re-run install.sh --yes later)"
  else
    info "installing pi..."
    local pitmp
    pitmp="$(mktemp)"
    if fetch https://pi.dev/install.sh -o "$pitmp" \
        && $DOAS setsid timeout -k 30 300 sh "$pitmp" </dev/null; then
      ok "pi installed"
    else
      warn "pi install failed — skipping (re-run install.sh --yes later)"
    fi
    rm -f "$pitmp"
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

  # hermes (Nous Research's open-source agent — navi's default agent, "Lain"):
  # self-improving AI agent with persistent memory. MIT licensed.
  # Install script is fetched first (never pipe-to-bash), then run with
  # the standard timeout/setsid treatment. The Lain identity layer
  # (persona, theme) deploys from wired/lain/ to ~/.config/hermes/lain.
  if command -v hermes >/dev/null 2>&1; then
    ok "hermes already installed"
  elif ! host_up https://raw.githubusercontent.com/NousResearch/hermes-agent/main/scripts/install.sh; then
    warn "hermes installer unreachable — skipping hermes (re-run install.sh --yes later)"
  else
    info "installing hermes (Nous Research)..."
    local hermestmp
    hermestmp="$(mktemp)"
    if fetch https://raw.githubusercontent.com/NousResearch/hermes-agent/main/scripts/install.sh -o "$hermestmp" \
        && setsid timeout -k 30 300 bash "$hermestmp" --non-interactive </dev/null \
        && command -v hermes >/dev/null 2>&1; then
      ok "hermes -> $(command -v hermes)"
    else
      warn "hermes install failed — skipping (re-run install.sh --yes later)"
    fi
    rm -f "$hermestmp"
  fi

  # Lain identity layer: the persona, theme, and knowledge scaffolding
  # that makes Hermes "Lain" in navi. Hermes is the engine (Nous Research,
  # MIT — honestly attributed, never claimed as ours); Lain is the
  # navi-configured experience on top.
  #
  # TWO deployment targets:
  #   ~/.hermes/SOUL.md + ~/.hermes/memories/{USER,MEMORY}.md — Hermes
  #     NATIVELY injects these into every session (CLI, gateway, herdr,
  #     subagents). This is what makes *every* hermes invocation Lain,
  #     not just navi-lain.
  #   ~/.config/hermes/lain/ — navi's theme/banner assets for the
  #     navi-lain wrapper (ASCII art, THEME.md).
  # The OS suggests: we seed the persona on fresh installs but never
  # overwrite files the user has customized.
  if [ -d "$REPO_DIR/wired/lain" ]; then
    info "deploying lain identity layer..."
    # 1. Hermes-native persona — every hermes invocation picks this up.
    #    SOUL.md lives at ~/.hermes/; USER.md and MEMORY.md live at
    #    ~/.hermes/memories/ (per Hermes docs).
    #    On navi, Hermes IS Lain — so if the existing SOUL.md is the stock
    #    Hermes default (not Lain, not user-customized), we replace it.
    #    Genuine user customizations are backed up, never lost.
    mkdir -p "$HOME/.hermes" "$HOME/.hermes/memories"
    if [ -f "$REPO_DIR/wired/lain/SOUL.md" ]; then
      if [ ! -f "$HOME/.hermes/SOUL.md" ]; then
        cp "$REPO_DIR/wired/lain/SOUL.md" "$HOME/.hermes/SOUL.md"
        ok "Lain SOUL.md deployed"
      elif ! grep -q "Lain, the navi assistant" "$HOME/.hermes/SOUL.md"; then
        # Not Lain — either the Hermes default or a user customization.
        # Back it up, then deploy Lain. The OS suggests, the user decides:
        # they can restore the backup if they preferred the old one.
        cp "$HOME/.hermes/SOUL.md" "$HOME/.hermes/SOUL.md.pre-lain-backup"
        cp "$REPO_DIR/wired/lain/SOUL.md" "$HOME/.hermes/SOUL.md"
        ok "Lain SOUL.md deployed (previous saved to SOUL.md.pre-lain-backup)"
      fi
    fi
    for _f in USER.md MEMORY.md; do
      if [ -f "$REPO_DIR/wired/lain/$_f" ] && [ ! -f "$HOME/.hermes/memories/$_f" ]; then
        cp "$REPO_DIR/wired/lain/$_f" "$HOME/.hermes/memories/$_f"
      fi
    done
    unset _f
    # also seed /etc/skel so future users get it
    if [ -w /etc/skel ]; then
      mkdir -p /etc/skel/.hermes /etc/skel/.hermes/memories
      if [ -f "$REPO_DIR/wired/lain/SOUL.md" ] && [ ! -f "/etc/skel/.hermes/SOUL.md" ]; then
        cp "$REPO_DIR/wired/lain/SOUL.md" "/etc/skel/.hermes/SOUL.md"
      fi
      for _f in USER.md MEMORY.md; do
        if [ -f "$REPO_DIR/wired/lain/$_f" ] && [ ! -f "/etc/skel/.hermes/memories/$_f" ]; then
          cp "$REPO_DIR/wired/lain/$_f" "/etc/skel/.hermes/memories/$_f"
        fi
      done
      unset _f
    fi
    # 2. navi theme/banner assets for the wrapper
    mkdir -p "$HOME/.config/hermes/lain"
    cp -r "$REPO_DIR/wired/lain/." "$HOME/.config/hermes/lain/"
    if [ -w /etc/skel ]; then
      mkdir -p /etc/skel/.config/hermes/lain
      cp -r "$REPO_DIR/wired/lain/." /etc/skel/.config/hermes/lain/
    fi
    # the navi-lain launcher goes to /usr/bin for every user
    if [ -f "$REPO_DIR/wired/lain/navi-lain.sh" ]; then
      $DOAS install -m 0755 "$REPO_DIR/wired/lain/navi-lain.sh" /usr/bin/navi-lain
      ok "navi-lain -> /usr/bin/navi-lain"
    fi
    # nightshadeNeon skin for Hermes — the visual layer. Deployed to
    # ~/.hermes/skins/ where Hermes natively loads it. Activated via
    # display.skin in config.yaml (merged, not overwritten).
    if [ -f "$REPO_DIR/wired/lain/skins/nightshadeNeon.yaml" ]; then
      mkdir -p "$HOME/.hermes/skins"
      cp "$REPO_DIR/wired/lain/skins/nightshadeNeon.yaml" "$HOME/.hermes/skins/"
      # activate: merge display.skin into config.yaml without clobbering
      _cfg="$HOME/.hermes/config.yaml"
      if [ -f "$_cfg" ]; then
        if ! grep -qE "^[[:space:]]*skin:" "$_cfg"; then
          printf '
display:
  skin: nightshadeNeon
' >> "$_cfg"
        fi
      else
        printf 'display:
  skin: nightshadeNeon
' > "$_cfg"
      fi
      unset _cfg
      ok "nightshadeNeon skin deployed and activated"
    fi
    ok "lain identity layer deployed (~/.hermes/ + ~/.config/hermes/lain)"
  else
    warn "wired/lain missing from repo — skipping lain identity layer"
  fi

  # goose <-> herdr awareness: user-scope goose plugin that reports goose's
  # lifecycle state to the herdr pane hosting the session — herdr's official
  # "custom socket integration" path (pane.report_agent + HERDR_PANE_ID),
  # so no herdr fork is needed. The hook script no-ops unless HERDR_ENV=1
  # and HERDR_PANE_ID are set, so this is harmless on machines without
  # herdr. Goose auto-discovers plugins in ~/.agents/plugins/ at startup
  # (enabled by default when present). This is navi's own shipped plugin
  # (not user config), so deploy refreshes it wholesale like wired/.
  if [ -d "$REPO_DIR/scripts/goose-herdr" ]; then
    info "installing goose/herdr awareness plugin..."
    plugin_dest="$HOME/.agents/plugins/navi-goose-herdr"
    mkdir -p "$plugin_dest/hooks" "$plugin_dest/scripts"
    cp -a "$REPO_DIR/scripts/goose-herdr/plugin.json" "$plugin_dest/plugin.json"
    cp -a "$REPO_DIR/scripts/goose-herdr/hooks/hooks.json" "$plugin_dest/hooks/hooks.json"
    cp -a "$REPO_DIR/scripts/goose-herdr/scripts/report-state.sh" "$plugin_dest/scripts/report-state.sh"
    chmod 0755 "$plugin_dest/scripts/report-state.sh"
    ok "goose reports state to herdr panes (~/.agents/plugins/navi-goose-herdr)"
  else
    warn "scripts/goose-herdr missing from repo — skipping goose/herdr awareness"
  fi
}

# ---------------------------------------------------------------- NaviVim

# NaviVim is navi's default terminal IDE (xvoidsx/navivim): Neovim 0.11+
# from the upstream tarball (Debian stable's 0.10 is too old) plus Raven's
# hand-rolled config. Installed through NaviVim's own install.sh --system,
# which lands the binary in /usr/local, seeds /etc/skel for future users,
# and registers editor/vi alternatives + EDITOR/VISUAL in /etc/profile.d.
#
# Source resolution, in order:
#   1. $REPO_DIR/navivim — staged on the ISO by iso/stage.sh at a pinned
#      commit (== /opt/navi-iso/navivim on the live system).
#   2. /opt/navi-iso/navivim — the installer copies the ISO payload onto
#      the installed system, so navi-update --deploy-only finds it here.
#   3. fresh clone of xvoidsx/navivim at the pinned commit (network) —
#      never floats on navivim's main.
# The neovim release is pinned too: never float on the moving `stable`
# tag in a shipped ISO (same rule as goose's GOOSE_VERSION).
NAVIVIM_PIN="82440c2a21086c26c5e3b029acc52a95d9bd460d"
NAVIVIM_NVIM_VERSION="v0.12.5"

setup_navivim() {
  step "NaviVim (default terminal IDE)"

  local nvim_dir="" cand
  for cand in "$REPO_DIR/navivim" "/opt/navi-iso/navivim"; do
    if [ -x "$cand/install.sh" ]; then nvim_dir="$cand"; break; fi
  done
  if [ -z "$nvim_dir" ]; then
    if ! host_up https://github.com; then
      warn "github unreachable and no staged NaviVim copy — skipping (re-run install.sh --yes later)"
      return 0
    fi
    nvim_dir="$(mktemp -d)/navivim"
    info "cloning NaviVim at pinned commit ${NAVIVIM_PIN:0:12}..."
    if ! git clone -q https://github.com/xvoidsx/navivim.git "$nvim_dir" 2>/dev/null \
        || ! git -C "$nvim_dir" checkout -q "$NAVIVIM_PIN" 2>/dev/null \
        || [ ! -x "$nvim_dir/install.sh" ]; then
      warn "NaviVim clone failed — skipping (re-run install.sh --yes later)"
      return 0
    fi
    ok "NaviVim ${NAVIVIM_PIN:0:12} cloned"
  else
    info "NaviVim source: $nvim_dir"
  fi

  # neovim binary: skip the (re)install when the pinned version is already
  # in place — makes navi-update cheap and offline-safe.
  local skip_nvim=0 tarball="" skip_plugins=0 have_ver tarball_arch=""
  if [ -x /usr/local/bin/nvim ]; then
    have_ver="$(/usr/local/bin/nvim --version 2>/dev/null | head -n 1 | grep -o 'v[0-9.]*' | head -n 1 || true)"
    if [ "$have_ver" = "$NAVIVIM_NVIM_VERSION" ]; then
      skip_nvim=1
      info "neovim $have_ver already installed — skipping binary install"
    fi
  fi
  # staged tarball (iso/stage.sh): arch-qualified, so a retained ISO
  # payload can never feed the wrong arch to a different machine.
  case "$(uname -m)" in
    x86_64)        tarball_arch="x86_64" ;;
    aarch64|arm64) tarball_arch="arm64" ;;
  esac
  if [ -n "$tarball_arch" ] && [ -f "$nvim_dir/nvim-linux-${tarball_arch}.tar.gz" ]; then
    tarball="$nvim_dir/nvim-linux-${tarball_arch}.tar.gz"
    info "using staged neovim tarball"
  fi
  if [ "$skip_nvim" -eq 0 ] && [ -z "$tarball" ] && ! host_up https://github.com/neovim/neovim; then
    warn "neovim release unreachable and no staged tarball — skipping NaviVim (re-run install.sh --yes later)"
    return 0
  fi
  if ! host_up https://github.com; then
    skip_plugins=1
    warn "offline: skipping the plugin smoke test — first nvim launch syncs plugins"
  fi

  # --system needs root; --no-apt because navi owns the package list
  # (ripgrep, fd-find, shellcheck, build-essential are in PKGS). env(1)
  # carries the knobs through doas/sudo, which would otherwise strip them.
  info "installing NaviVim (neovim $NAVIVIM_NVIM_VERSION)..."
  local env_args="NVIM_VERSION=$NAVIVIM_NVIM_VERSION NAVIVIM_SKIP_NVIM=$skip_nvim NAVIVIM_SKIP_PLUGINS=$skip_plugins"
  [ -n "$tarball" ] && env_args="$env_args NVIM_TARBALL=$tarball"
  # shellcheck disable=SC2086
  if $DOAS env $env_args \
      timeout -k 30 600 bash "$nvim_dir/install.sh" --system --no-apt </dev/null; then
    ok "NaviVim installed"
  else
    warn "NaviVim install failed — skipping (re-run install.sh --yes later)"
    return 0
  fi

  # the tarball ships its own nvim.desktop ("Neovim"); navi's launcher
  # entry is branded NaviVim (wired/applications/navi-nvim.desktop, shipped
  # below by install_commands) — drop the upstream duplicate so rofi shows
  # exactly one.
  if [ -f /usr/local/share/applications/nvim.desktop ]; then
    $DOAS rm -f /usr/local/share/applications/nvim.desktop
    info "upstream nvim.desktop removed (navi-nvim.desktop is the launcher entry)"
  fi

  # --system seeds /etc/skel (future users). seed the invoking user too —
  # but never touch an existing config; customized setups are sacred.
  if [ -e "$HOME/.config/nvim" ] || [ -L "$HOME/.config/nvim" ]; then
    info "~/.config/nvim already exists — left alone"
  elif [ -d /etc/skel/.config/nvim ]; then
    mkdir -p "$HOME/.config"
    cp -a /etc/skel/.config/nvim "$HOME/.config/nvim"
    ok "~/.config/nvim seeded from NaviVim"
  else
    warn "/etc/skel/.config/nvim missing — NaviVim system install did not seed it"
  fi
  if [ ! -f "$HOME/.config/omaterm/nvim.theme" ]; then
    mkdir -p "$HOME/.config/omaterm"
    echo "nightshadeNeon" > "$HOME/.config/omaterm/nvim.theme"
    ok "NaviVim theme default: nightshadeNeon"
  fi
}
setup_navicode() {
  step "naviCode (VScodium + nightshadeNeon + Lain)"

  # 1. VScodium apt repo (official). trixie uses the .sources format.
  local keyring="/usr/share/keyrings/vscodium-archive-keyring.gpg"
  if [ ! -f "$keyring" ]; then
    if ! host_up https://download.vscodium.com; then
      warn "vscodium repo unreachable — skipping naviCode (re-run install.sh --yes later)"
      return 0
    fi
    info "adding VScodium apt repo..."
    fetch https://gitlab.com/paulcarroty/vscodium-deb-rpm-repo/raw/master/pub.gpg -o /tmp/vscodium.gpg \
      && $DOAS gpg --dearmor -o "$keyring" /tmp/vscodium.gpg 2>/dev/null \
      && rm -f /tmp/vscodium.gpg \
      && printf 'Types: deb\nURIs: https://download.vscodium.com/debs\nSuites: vscodium\nComponents: main\nArchitectures: amd64 arm64\nSigned-by: %s\n' "$keyring" | $DOAS tee /etc/apt/sources.list.d/vscodium.sources >/dev/null \
      && $DOAS apt-get update -qq 2>/dev/null
    ok "VScodium repo added"
  fi

  # 2. Install codium (the package is called "codium", not "vscodium").
  if ! command -v codium >/dev/null 2>&1; then
    info "installing VScodium..."
    if ! $DOAS apt-get install -y -qq codium 2>/dev/null; then
      warn "codium install failed — skipping naviCode extensions/theme (re-run install.sh --yes later)"
      return 0
    fi
    ok "codium installed"
  else
    ok "codium already installed"
  fi

  # 3. nightshadeNeon theme — from the naviCode repo if available,
  #    else fetch the .vsix from GitHub.
  local vsix="" tmpdir=""
  for cand in "$REPO_DIR/../naviCode/nightshadeNeon-VSC/nightshade-neon-1.0.0.vsix" "/opt/navi-iso/naviCode.vsix"; do
    if [ -f "$cand" ]; then vsix="$cand"; break; fi
  done
  if [ -z "$vsix" ] && host_up https://github.com; then
    tmpdir="$(mktemp -d)"
    if fetch "https://github.com/xvoidsx/naviCode/raw/main/nightshadeNeon-VSC/nightshade-neon-1.0.0.vsix" -o "$tmpdir/theme.vsix" 2>/dev/null; then
      vsix="$tmpdir/theme.vsix"
    fi
  fi
  if [ -n "$vsix" ] && [ -f "$vsix" ]; then
    if ! codium --list-extensions 2>/dev/null | grep -qi "nightshade"; then
      codium --install-extension "$vsix" 2>/dev/null && ok "nightshadeNeon theme installed"
    else
      ok "nightshadeNeon theme already installed"
    fi
  else
    warn "nightshadeNeon .vsix not found — theme not installed (re-run install.sh --yes later)"
  fi
  [ -n "$tmpdir" ] && rm -rf "$tmpdir"

  # 4. Rina Hermes ACP — the in-editor Lain. From Open VSX (VScodium's
  #    native registry, no sideloading).
  #    NOTE: a bad extension ID installs nothing and raises no error —
  #    so we assert afterward. A miss must be loud, not silent.
  if ! codium --list-extensions 2>/dev/null | grep -qi "rina-hermes-acp"; then
    codium --install-extension "JoveRina.rina-hermes-acp" 2>/dev/null || true
    if codium --list-extensions 2>/dev/null | grep -qi "rina-hermes-acp"; then
      ok "Rina Hermes ACP installed (Lain in the editor)"
    else
      warn "Rina Hermes ACP install may have failed — check 'codium --list-extensions' (ID: JoveRina.rina-hermes-acp)"
    fi
  else
    ok "Rina Hermes ACP already installed"
  fi

  # 5. Default settings — nightshadeNeon theme, no auto-detect fighting it.
  #    The OS suggests: merge the theme key into existing settings.json
  #    without clobbering user customizations. Only touch the two keys
  #    we own; everything else stays as the user left it.
  local settings_dir="$HOME/.config/VSCodium/User"
  mkdir -p "$settings_dir"
  python3 - "$settings_dir/settings.json" << 'PYEOF_MERGE' 2>/dev/null || true
import json, sys
path = sys.argv[1]
try:
    with open(path) as f:
        data = json.load(f)
except (FileNotFoundError, json.JSONDecodeError):
    data = {}
changed = False
if data.get("workbench.colorTheme") != "nightshadeNeon":
    # Only set if the user hasn't chosen a different theme
    if "workbench.colorTheme" not in data:
        data["workbench.colorTheme"] = "nightshadeNeon"
        changed = True
if data.get("window.autoDetectColorScheme") is not False:
    if "window.autoDetectColorScheme" not in data:
        data["window.autoDetectColorScheme"] = False
        changed = True
if changed or not data:
    with open(path, "w") as f:
        json.dump(data, f, indent=2)
        f.write("\n")
    print("naviCode defaults merged")
PYEOF_MERGE
  ok "naviCode settings ensured (nightshadeNeon)"
  # /etc/skel so future users get it too
  if [ -w /etc/skel ]; then
    $DOAS mkdir -p /etc/skel/.config/VSCodium/User
    if [ ! -f /etc/skel/.config/VSCodium/User/settings.json ]; then
      printf '{\n  "workbench.colorTheme": "nightshadeNeon",\n  "window.autoDetectColorScheme": false\n}\n' | $DOAS tee /etc/skel/.config/VSCodium/User/settings.json >/dev/null
    fi
  fi
}


# ---------------------------------------------------------------- system: navi mods (eiri)
# the Go/Bubble Tea panel mods — prebuilt binaries committed in the repo,
# so the installed system never needs a Go toolchain. deployed to /usr/bin
# like the other navi commands; waybar opens them floating via mod-open.sh.

setup_mods() {
  step "navi mods -> /usr/bin"

  install_mod() { # <mod-dir> <binary>
    local src="$REPO_DIR/mods/$1/$2"
    if [ ! -e "$src" ]; then
      warn "missing mod binary: mods/$1/$2 — skipping"
      return 0
    fi
    $DOAS install -m 0755 "$src" "/usr/bin/$2"
    ok "$2"
  }

  install_mod "navi-networking" "navi-networking"
  install_mod "navi-calendar"   "navi-calendar"
  install_mod "navi-audio"      "navi-audio"
  install_mod "navi-bluetooth"  "navi-bluetooth"
  install_mod "navi-lain-config" "navi-lain-config"
  install_mod "navi-agents-config" "navi-agents-config"
  install_mod "navi-weather"    "navi-weather"
  install_mod "navi-get"        "navi-get"
  install_mod "navi-browser"    "navi-browser"
  install_mod "navi-power"      "navi-power"
  install_mod "navi-display"    "navi-display"
  install_mod "navi-reminders"  "navi-reminders"
  install_mod "navi-reminders"  "navi-reminder-fire"
  install_mod "navi-notifs"     "navi-notifs"
  install_mod "navi-nowplaying" "navi-nowplaying"
  install_mod "wiredrop"        "wiredrop"
  install_mod "navi-tailscale"  "navi-tailscale"
  install_mod "navi-imprint"    "navi-imprint"

  # wiredrop: the receiver only receives while the daemon listens, so it
  # runs as a per-user systemd service. Installed to the user's own
  # systemd dir (no root needed — install.sh runs as the user); enabled
  # so it starts on login, started now. linger (enabled below for the
  # reminder timers) also keeps it alive from boot.
  setup_wiredrop_service() {
    local unit_dir="$HOME/.config/systemd/user"
    local src="$REPO_DIR/mods/wiredrop/wiredrop.service"
    [ -f "$src" ] || { warn "missing wiredrop.service — skipping daemon setup"; return 0; }
    mkdir -p "$unit_dir"
    install -m 0644 "$src" "$unit_dir/wiredrop.service"
    if ! command -v systemctl >/dev/null 2>&1; then
      warn "systemctl not found — enable the wiredrop daemon by hand: systemctl --user enable --now wiredrop"
      return 0
    fi
    # daemon-reload must not be fatal: in the ISO provisioner (running as
    # root, no user D-Bus session) `systemctl --user` fails, and with
    # `set -e` that would kill the entire install. the enable --now below
    # is already guarded and warns gracefully.
    systemctl --user daemon-reload >/dev/null 2>&1 || true
    if systemctl --user enable --now wiredrop.service >/dev/null 2>&1; then
      ok "wiredrop daemon enabled and started (systemctl --user status wiredrop)"
    else
      warn "wiredrop.service could not be enabled — run: systemctl --user enable --now wiredrop"
    fi
  }
  setup_wiredrop_service

  # navi-reminders runs on systemd user timers — they only fire from boot
  # (before first login) with lingering enabled for the installing user.
  if command -v loginctl >/dev/null 2>&1; then
    if $DOAS loginctl enable-linger "$USER" >/dev/null 2>&1; then
      ok "linger enabled for $USER (reminder timers run from boot)"
    else
      warn "could not enable linger for $USER — reminders only fire while logged in"
    fi
  fi
}

# Local brain model guard for Hey Lain. This runs on EVERY setup_heylain —
# deliberately above the venv fast path — so a failed first model pull (or
# a stopped ollama service, or a broken ollama install) is repaired or at
# least warned about on later updates instead of being skipped forever.
ensure_heylain_model() {
  # A pulled model is useless if the daemon is down: make sure the service
  # is actually running (field lesson 2026-09-20 — inactive service plus a
  # missing llama-server binary made every off-list utterance die the
  # same silent way).
  if command -v ollama >/dev/null 2>&1 && command -v systemctl >/dev/null 2>&1; then
    if ! systemctl is-active --quiet ollama 2>/dev/null; then
      if $DOAS systemctl enable --now ollama >/dev/null 2>&1; then
        ok "ollama service enabled and started"
      else
        warn "ollama service is not running and could not be started — Hey Lain's brain stays offline until you run: sudo systemctl enable --now ollama"
      fi
    fi
  fi
  # Local brain: gemma3:270m is the default voice model — tiny enough for
  # low-end hardware, no key or cloud needed. `ollama pull` talks to the
  # local daemon, so the model lands where the ollama service sees it.
  # Skipped (with a warning) when ollama or the network isn't there yet.
  if ! command -v ollama >/dev/null 2>&1; then
    warn "ollama not found — skipping gemma3:270m pull"
  elif ollama list 2>/dev/null | grep -q "^gemma3:270m"; then
    ok "ollama model gemma3:270m already present"
  elif ! host_up https://ollama.com; then
    warn "ollama.com unreachable — skipping gemma3:270m pull (run: ollama pull gemma3:270m)"
  else
    info "pulling gemma3:270m (local voice brain, one time)..."
    if ollama pull gemma3:270m >/dev/null 2>&1; then
      ok "gemma3:270m ready"
    else
      warn "could not pull gemma3:270m — Hey Lain falls back to its offline line until you run: ollama pull gemma3:270m"
    fi
  fi
}

# Hey Lain voice assistant (eiri): deploys mods/hey-lain to
# /usr/share/navi/hey-lain and builds its STT/TTS venv in place. The venv
# build (faster-whisper + piper + whisper model, a few hundred MB) is the
# slow part, so it runs once: a .venv-ready marker skips rebuilds on
# later deploys, and a dead PyPI degrades to a warning instead of wedging
# the install. Logs go to the user's state dir (HEY_LAIN_LOG_DIR) because
# the deploy tree is root-owned.
setup_heylain() {
  step "hey lain -> $SHARE_DIR/hey-lain"
  local src="$REPO_DIR/mods/hey-lain" dest="$SHARE_DIR/hey-lain"
  if [ ! -d "$src" ]; then
    warn "mods/hey-lain missing — skipping voice assistant"
    return 0
  fi
  # Brain config lives in the user's config dir (0600 — it can hold an API
  # key). Written exactly once: an existing config (hand-made or via
  # navi-lain-config) is never overwritten by installs or updates.
  # install.sh refuses root, so $HOME is the user's home here.
  local bl_dir="${XDG_CONFIG_HOME:-$HOME/.config}/hey-lain"
  local bl_conf="$bl_dir/brain.json"
  mkdir -p "$bl_dir" 2>/dev/null && chmod 0700 "$bl_dir" 2>/dev/null
  if [ ! -f "$bl_conf" ]; then
    if printf '{\n  "backend": "local",\n  "model": "gemma3:270m",\n  "api_url": "http://127.0.0.1:11434/api/chat"\n}\n' \
        >"$bl_conf" 2>/dev/null && chmod 0600 "$bl_conf" 2>/dev/null; then
      ok "hey-lain brain config initialized (local gemma3:270m)"
    else
      warn "could not write $bl_conf"
    fi
  fi
  # The venv is the slow part (faster-whisper + piper + whisper model), so
  # it survives redeploys: the marker stores a hash of requirements.txt,
  # and a matching hash means "scripts refresh, venv stays". A mismatch
  # (or no marker) rebuilds from scratch. The Piper voice (~60MB, downloaded
  # not bundled) is preserved the same way so redeploys don't re-fetch it.
  local req_hash="" venv_ok=0 voice_ok=0
  req_hash="$(sha256sum "$src/requirements.txt" 2>/dev/null | cut -d' ' -f1)"
  if [ -n "$req_hash" ] && [ -f "$dest/.venv-ready" ] \
      && [ "$(cat "$dest/.venv-ready" 2>/dev/null)" = "$req_hash" ] \
      && [ -d "$dest/venv" ]; then
    venv_ok=1
    $DOAS rm -rf "$dest.venv-keep"
    $DOAS mv "$dest/venv" "$dest.venv-keep"
  fi
  if ls "$dest/voices"/*.onnx >/dev/null 2>&1; then
    voice_ok=1
    $DOAS rm -rf "$dest.voices-keep"
    $DOAS mv "$dest/voices" "$dest.voices-keep"
  fi
  $DOAS rm -rf "$dest"
  $DOAS cp -a "$src" "$dest"
  $DOAS find "$dest" -name '*.sh' -exec chmod 0755 {} +
  ok "hey-lain deployed"
  if [ "$voice_ok" -eq 1 ]; then
    $DOAS rm -rf "$dest/voices"
    $DOAS mv "$dest.voices-keep" "$dest/voices"
    ok "hey-lain voice kept (not re-downloaded)"
  else
    $DOAS rm -rf "$dest.voices-keep" 2>/dev/null || true
  fi
  # Brain health runs on every deploy, even when the venv is kept as-is:
  # a failed first model pull must be repaired by later updates, not
  # skipped forever behind the fast path below.
  ensure_heylain_model
  if [ "$venv_ok" -eq 1 ]; then
    $DOAS mv "$dest.venv-keep" "$dest/venv"
    echo "$req_hash" | $DOAS tee "$dest/.venv-ready" >/dev/null
    ok "hey-lain venv kept (requirements unchanged)"
    # Voice predates the venv-keep path on early installs: fetch it now so
    # tts-ready.sh stops reporting "voice model is not ready".
    if [ "$voice_ok" -eq 0 ]; then
      if host_up https://huggingface.co; then
        if $DOAS "$dest/bin/fetch-voice.sh" </dev/null; then
          ok "hey-lain voice fetched"
        else
          warn "hey-lain voice download failed — Alt+V will warn until it succeeds (re-run install.sh)"
        fi
      else
        warn "huggingface.co unreachable — hey-lain voice skipped (re-run install.sh later)"
      fi
    fi
    return 0
  fi
  rm -rf "$dest.venv-keep" 2>/dev/null
  if ! host_up https://pypi.org/simple/; then
    warn "pypi unreachable — hey-lain voice backend skipped (re-run install.sh --yes later)"
    return 0
  fi
  info "building hey-lain venv (faster-whisper + piper — a few minutes, one time)..."
  if $DOAS "$dest/install.sh" --skip-apt </dev/null; then
    echo "$req_hash" | $DOAS tee "$dest/.venv-ready" >/dev/null
    ok "hey-lain ready — tap Alt+V to talk"
  else
    warn "hey-lain venv build failed — Alt+V will not work until install.sh is re-run"
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

deploy_waybar_themes() {
  # deploy_waybar_themes — the waybar theme store.
  # system: /usr/share/navi/waybar/themes/<name>/{theme.css,meta}
  # user:   ~/.local/share/navi/waybar/themes/<name>/{theme.css,meta} (wins)
  # navi-theme resolves user-first, so a user theme of the same name shadows
  # the distro one. in update mode, user themes are only overwritten if
  # untouched (same manifest logic as deploy_config).
  local src_dir="$WIRED_SHARE/waybar/themes"
  [ -d "$src_dir" ] || { warn "missing waybar themes in wired/ — skipping"; return 0; }
  local sys_dir="$SHARE_DIR/waybar/themes"
  $DOAS mkdir -p "$sys_dir"
  $DOAS cp -a "$src_dir/." "$sys_dir/"
  $DOAS chmod -R a+rX "$sys_dir"
  ok "waybar themes -> $sys_dir"

  local user_dir="$HOME/.local/share/navi/waybar/themes"
  mkdir -p "$user_dir"
  local theme
  for theme in "$src_dir"/*/; do
    theme="$(basename "$theme")"
    local dest="$user_dir/$theme"
    if [ "$NAVI_UPDATE_MODE" -eq 1 ] && [ -d "$dest" ]; then
      # update mode: only overwrite untouched user themes
      local baseline live_sha
      baseline="$(manifest_lookup "$dest/theme.css")"
      if [ -n "$baseline" ]; then
        live_sha="$(sha256sum "$dest/theme.css" 2>/dev/null | cut -d' ' -f1 || true)"
        if [ "$live_sha" != "$baseline" ]; then
          info "kept your modified waybar theme: $theme"
          continue
        fi
      fi
    fi
    backup_if_changed "$src_dir/$theme/theme.css" "$dest/theme.css"
    mkdir -p "$dest"
    cp -a "$src_dir/$theme/." "$dest/"
    manifest_record "$src_dir/$theme/theme.css" "waybar/themes/$theme/theme.css" "$dest/theme.css" "644"
  done
  ok "waybar themes -> ~/.local/share/navi/waybar/themes"
}

deploy_waybar_service() {
  # deploy_waybar_service — the waybar systemd user unit.
  # replaces the packaged /usr/lib/systemd/user/waybar.service outright:
  # the packaged unit carries Requisite=graphical-session.target, which is
  # never activated in a plain sddm-launched Wayland session, so it fails
  # silently. a drop-in cannot remove dependencies, only add them — hence
  # a full replacement unit hooked into default.target.
  local src="$WIRED_SHARE/systemd/waybar.service"
  [ -f "$src" ] || { warn "missing waybar.service — skipping autostart setup"; return 0; }
  local unit_dir="$HOME/.config/systemd/user"
  mkdir -p "$unit_dir"
  backup_if_changed "$src" "$unit_dir/waybar.service"
  install -m 0644 "$src" "$unit_dir/waybar.service"
  manifest_record "$src" "systemd/waybar.service" "$unit_dir/waybar.service" "644"
  ok "waybar.service -> ~/.config/systemd/user/"
  if ! command -v systemctl >/dev/null 2>&1; then
    warn "systemctl not found — enable waybar by hand: systemctl --user enable --now waybar"
    return 0
  fi
  systemctl --user daemon-reload >/dev/null 2>&1 || true
  if systemctl --user enable --now waybar.service >/dev/null 2>&1; then
    ok "waybar enabled and started (systemctl --user status waybar)"
  else
    warn "waybar.service could not be enabled — run: systemctl --user enable --now waybar"
  fi
}

deploy_configs() {
  step "deploying user configs (backups -> $BACKUP_DIR)"

  deploy_config "sway/config"                 "$HOME/.config/sway/config"
  deploy_config "i3/config"                   "$HOME/.config/i3/config"
  deploy_config "waybar/config.jsonc"         "$HOME/.config/waybar/config.jsonc"
  deploy_config "waybar/style.css"            "$HOME/.config/waybar/style.css"
  # waybar theme store (eiri): five themes, one dir each so a theme can grow
  # its own config.json later. system copy lands under /usr/share/navi (what
  # navi-theme reads as SYS_THEMES); user copy goes to ~/.local/share/navi
  # (USER_THEMES, wins). style.css IS the patchbay theme, byte-identical —
  # that's the whole default-persistence mechanism (no state file to drift).
  deploy_waybar_themes
  deploy_config "rofi/navi-theme.rasi"       "$HOME/.config/rofi/navi-theme.rasi"
  deploy_waybar_service
  deploy_config "polybar/config.ini"          "$HOME/.config/polybar/config.ini"
  deploy_config "polybar/battery-combined-shell.sh" "$HOME/.config/polybar/battery-combined-shell.sh" 755
  deploy_config "rofi/config.rasi"            "$HOME/.config/rofi/config.rasi"
  deploy_config "picom/picom.conf"            "$HOME/.config/picom/picom.conf"
  deploy_config "terminals/alacritty.toml"    "$HOME/.config/alacritty/alacritty.toml"
  deploy_config "terminals/foot.ini"          "$HOME/.config/foot/foot.ini"
  deploy_config "terminals/kitty.conf"        "$HOME/.config/kitty/kitty.conf"
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
  # default cursor theme, file-based so it needs no xrdb: Xcursor falls back
  # to ~/.icons/default/index.theme, covering the X11 session's root cursor
  # and any client that doesn't read the GTK/Wayland settings.
  mkdir -p "$HOME/.icons/default"
  printf '[Icon Theme]\nInherits=Bibata-Modern-Ice\n' > "$HOME/.icons/default/index.theme"
  ok "default cursor theme: Bibata-Modern-Ice"
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

  # waybar hot-reloads style.css on its own, but a new config.jsonc (new
  # on-clicks, new modules) needs an explicit nudge — otherwise the
  # running bar keeps yesterday's config and new launchers never take
  # effect on a live system. SIGUSR2 is waybar's documented config-reload
  # signal. fresh installs have no bar running yet; the pgrep guard skips.
  if pgrep -x waybar >/dev/null 2>&1; then
    if pkill -USR2 -x waybar 2>/dev/null; then
      ok "waybar reloaded (new config active)"
    else
      warn "waybar is running but the reload signal failed — restart it or re-login"
    fi
  fi
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
  install_bin "navi-browser-run.sh" "navi-browser-run"
  install_bin "navi-notifs.sh"       "navi-notifs"
  install_bin "navishot.sh"          "navishot"

  # distro-level tools live in scripts/ (repo root), outside the /wired
  # desktop layer — same /usr/bin destination, same rofi visibility.
  for pair in "navi-update.sh:navi-update" "navi-wired-restore.sh:navi-wired-restore" "navi-wired-adopt.sh:navi-wired-adopt" "navi-extras.sh:navi-extras" "navi-theme:navi-theme" "navi-terminal:navi-terminal"; do
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

setup_sudo() {
  step "sudo (compatibility)"
  # sudo ships so third-party install scripts (scripts/installers/*) and
  # upstream tooling that expects it keep working. doas stays the
  # navi-native privilege tool. deliberately NOT an alias: a sudo->doas
  # alias would shadow the real sudo and break sudo -u/-i/-E, and aliases
  # don't expand in non-interactive scripts anyway — which is exactly
  # where the installers need sudo.
  if ! command -v sudo >/dev/null 2>&1; then
    # install_packages (which runs in both install and update mode) owns
    # the sudo package; this is just a safety net for odd states.
    info "sudo not present — installing"
    $DOAS apt-get install -y -qq sudo \
      || { warn "could not install sudo; installers expecting sudo will fail"; return 0; }
    ok "sudo installed"
  fi
  # debian-native steady state: the user sits in the sudo group, so sudo
  # asks for the login password — same feel as doas 'permit persist'.
  # (group membership takes effect on next login, like the input group
  # for ydotool.)
  if id -nG "$USER" | tr ' ' '\n' | grep -qx sudo; then
    ok "$USER already in the sudo group"
  else
    $DOAS /usr/sbin/usermod -aG sudo "$USER" \
      && ok "$USER added to the sudo group (next login)" \
      || warn "could not add $USER to the sudo group"
  fi
  # immediate steady state: an explicit sudoers.d rule for the user takes
  # effect on the very next sudo invocation — no re-login needed (unlike
  # group membership, which is still added above as the debian-native
  # belt and suspenders). password-required, same feel as doas
  # 'permit persist'. this is what makes scripts/installers/* work right
  # after navi-update, in the same session.
  printf '%s ALL=(ALL:ALL) ALL\n' "$USER" | $DOAS tee /etc/sudoers.d/10-navi-user >/dev/null
  $DOAS chmod 440 /etc/sudoers.d/10-navi-user
  if $DOAS /usr/sbin/visudo -c -q 2>/dev/null; then
    ok "sudoers rule in place for $USER (effective immediately)"
  else
    warn "/etc/sudoers.d/10-navi-user failed visudo check — removing it"
    $DOAS rm -f /etc/sudoers.d/10-navi-user
  fi
  if [ "$NAVI_UPDATE_MODE" -ne 1 ]; then
    # fresh-install provisioning is non-interactive (no tty for a password
    # prompt), so sudo stays passwordless until tighten_sudo() runs at the
    # end of the install. mirrors the installer's temporary doas
    # 'permit nopass' that gets locked down after provisioning. (this file
    # sorts after 10-navi-user, so its NOPASSWD wins while it exists.)
    printf '%s ALL=(ALL) NOPASSWD:ALL\n' "$USER" | $DOAS tee /etc/sudoers.d/navi-provision >/dev/null
    $DOAS chmod 440 /etc/sudoers.d/navi-provision
    $DOAS /usr/sbin/visudo -c -q 2>/dev/null \
      || warn "/etc/sudoers.d/navi-provision failed visudo check"
    ok "sudo passwordless during provisioning (tightened at end of install)"
  fi
}

tighten_sudo() {
  # end of a fresh install: drop the provisioning NOPASSWD so sudo goes
  # back to asking for the login password. never silently leave the
  # passwordless rule in place. the permanent /etc/sudoers.d/10-navi-user
  # rule (password-required, written by setup_sudo) stays — that is the
  # steady state.
  step "sudo lockdown"
  $DOAS rm -f /etc/sudoers.d/navi-provision
  ok "provisioning NOPASSWD removed; sudo now asks for the login password"
}

setup_zram() {
  step "zram (compressed swap)"
  # shellcheck disable=SC1091
  source "$REPO_DIR/scripts/lib/hardware.sh"
  if ! navi_is_lowmem; then
    ok "plenty of RAM — zram left alone"
    return 0
  fi
  # low-memory machine (the cloudbook bench, old thinkpads, little pis):
  # a compressed swap device in RAM, so big deb unpacks and heavy
  # browser tabs stop getting OOM-killed. permanent and system-wide —
  # this is why the per-installer memory warnings are gone.
  if [ ! -f /etc/default/zramswap ] \
    || ! grep -q "^ALGO=zstd" /etc/default/zramswap 2>/dev/null; then
    $DOAS tee /etc/default/zramswap >/dev/null <<'EOF'
# managed by navi (setup_zram) — compressed swap for low-memory machines
ALGO=zstd
PERCENT=50
PRIORITY=100
EOF
    ok "zram configured (zstd, 50% of RAM)"
  else
    ok "zram already configured"
  fi
  if ! $DOAS systemctl is-enabled -q zramswap.service 2>/dev/null; then
    $DOAS systemctl enable zramswap.service >/dev/null \
      || warn "could not enable zramswap.service"
  fi
  if navi_zram_active; then
    ok "zram swap already active"
  elif $DOAS systemctl start zramswap.service 2>/dev/null; then
    ok "zram swap active"
  else
    warn "zram configured but could not be started — it will come up on next boot"
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
GTK_THEME=nightshadeNeon
ICON_THEME=Moka
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

# ---------------------------------------------------------------- optional extras
# The third-party installers (scripts/installers/*) used to run with prompts
# at the tail of the installer — one failure there aborted the whole install
# (set -e). Now they ship deployed to /usr/share/navi/installers and the
# user picks them from the navi-extras menu whenever they want them.
deploy_installers() {
  step "optional extras -> $SHARE_DIR/installers"
  local dir="$REPO_DIR/scripts/installers"
  if [ ! -d "$dir" ]; then
    info "no scripts/installers/ — extras menu will be empty"
    return 0
  fi
  $DOAS rm -rf "$SHARE_DIR/installers"
  $DOAS mkdir -p "$SHARE_DIR/installers"
  $DOAS cp -a "$dir"/. "$SHARE_DIR/installers"/
  $DOAS find "$SHARE_DIR/installers" -name '*.sh' -exec chmod 0755 {} +
  ok "installers staged for navi-extras"
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
    # refresh launchers + icons for webapps the user installed themselves
    # (icons/launchers that shipped after first install otherwise never land)
    navi-webapp repair
    ok "default webapps installed for $USER"
  else
    warn "navi-webapp not on PATH — skipping default webapp install"
  fi
}

# ---------------------------------------------------------------- telegram (native)
# Telegram ships as a native app out of the box — its webapp left the
# catalog, so this runs scripts/installers/telegram-installer.sh once per
# machine. Afterwards the official binary self-updates, so re-runs are a
# cheap no-op (the guard below). Non-fatal by design: a failed download
# must never wedge an install or an update.
setup_telegram() {
  step "telegram (native)"
  if command -v telegram >/dev/null 2>&1; then
    ok "telegram already installed — skipping"
    return 0
  fi
  local installer="$REPO_DIR/scripts/installers/telegram-installer.sh"
  [ -x "$installer" ] || installer="/usr/share/navi/installers/telegram-installer.sh"
  if [ ! -x "$installer" ]; then
    warn "telegram installer not found — skipping (later: navi-extras --install telegram)"
    return 0
  fi
  if bash "$installer"; then
    ok "telegram installed natively"
  else
    warn "telegram installer failed — retry later with: navi-extras --install telegram"
  fi
}

setup_element() {
  step "element (native)"
  if command -v element-desktop >/dev/null 2>&1; then
    ok "element already installed — skipping"
    return 0
  fi
  local installer="$REPO_DIR/scripts/installers/element-installer.sh"
  [ -x "$installer" ] || installer="/usr/share/navi/installers/element-installer.sh"
  if [ ! -x "$installer" ]; then
    warn "element installer not found — skipping (later: navi-extras --install element)"
    return 0
  fi
  if bash "$installer"; then
    ok "element installed natively"
    # Electron can't auto-detect the secret store on sway/wayland — force
    # gnome-libsecret so safeStorage uses the keyring instead of falling
    # back to plaintext (which users correctly reject).
    for d in /usr/share/applications ~/.local/share/applications; do
      if [ -f "$d/element-desktop.desktop" ]; then
        $DOAS sed -i 's|^Exec=element-desktop|Exec=element-desktop --password-store=gnome-libsecret|' "$d/element-desktop.desktop" 2>/dev/null ||           sed -i 's|^Exec=element-desktop|Exec=element-desktop --password-store=gnome-libsecret|' "$d/element-desktop.desktop"
      fi
    done
  else
    warn "element installer failed — retry later with: navi-extras --install element"
  fi
}

setup_tailscale() {
  step "tailscale (mesh VPN)"
  if command -v tailscale >/dev/null 2>&1; then
    ok "tailscale already installed — skipping"
    return 0
  fi
  local installer="$REPO_DIR/scripts/installers/tailscale-installer.sh"
  [ -x "$installer" ] || installer="/usr/share/navi/installers/tailscale-installer.sh"
  if [ ! -x "$installer" ]; then
    warn "tailscale installer not found — skipping (later: navi-extras --install tailscale)"
    return 0
  fi
  if bash "$installer"; then
    ok "tailscale installed — run 'tailscale up' to join your tailnet"
  else
    warn "tailscale installer failed — retry later with: navi-extras --install tailscale"
  fi
}

# ---------------------------------------------------------------- gtk theme cohesion
# settings.ini covers plain GTK apps, but GSettings-aware apps (nemo and
# friends) read org.gnome.desktop.interface — whose schema default is
# Adwaita, which is why installs kept falling back to it. seed system-wide
# dconf defaults so nightshadeNeon wins from first boot. no locks: users
# can still override per-account with gsettings or a theme tool.
setup_gtk_theme() {
  step "gtk theme defaults (nightshadeNeon + moka)"
  # the house theme rides in wired/themes/, so it lands in the iron
  # structure on every install and update; copy it into the system
  # dir GTK actually reads.
  local theme_src="$WIRED_SHARE/themes/nightshadeNeon"
  [ -d "$theme_src" ] || theme_src="$WIRED_DIR/themes/nightshadeNeon"
  if [ -d "$theme_src" ]; then
    $DOAS mkdir -p /usr/share/themes
    $DOAS rm -rf /usr/share/themes/nightshadeNeon
    $DOAS cp -a "$theme_src" /usr/share/themes/nightshadeNeon
    ok "nightshadeNeon installed to /usr/share/themes"
  else
    warn "wired/themes/nightshadeNeon not found — skipping theme install"
  fi
  $DOAS install -d -m 755 /etc/dconf/db/local.d
  $DOAS tee /etc/dconf/db/local.d/00-navi-theme >/dev/null <<'EOF'
[org/gnome/desktop/interface]
gtk-theme='nightshadeNeon'
icon-theme='Moka'
cursor-theme='Bibata-Modern-Ice'
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
  ok "nightshadeNeon seeded as the gtk default (user-overridable)"
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
  # stamp the session names from wired/VERSION (single source of truth) so
  # SDDM shows the channel — "navi 2.0 "eiri"" vs "navi 1.6 "mika"" — instead
  # of a stale hardcoded release. every deploy and every navi-update
  # refreshes these, so mika updates re-stamp automatically.
  local sver sname
  sver="$(printf '%s' "$NAVI_VERSION" | awk '{print $1}')"
  sname="$(printf '%s' "$NAVI_VERSION" | awk '{print $2}' | tr -d '"')"
  $DOAS sed -i -e "s/^Name=.*/Name=navi $sver \"$sname\" (Wayland)/" \
    /usr/share/wayland-sessions/navi.desktop
  $DOAS sed -i -e "s/^Name=.*/Name=navi $sver \"$sname\" (X11)/" \
    /usr/share/xsessions/navi.desktop
  # auto-unlock the login keyring on SDDM login so Element, Signal,
  # Chromium etc. get encryption without a separate keyring password.
  # libpam-gnome-keyring must be installed (see PKGS above).
  if [ -f /etc/pam.d/sddm ]; then
    grep -q "pam_gnome_keyring.so" /etc/pam.d/sddm 2>/dev/null || {
      $DOAS sh -c 'printf "%s\n" "auth optional pam_gnome_keyring.so" >> /etc/pam.d/sddm'
      $DOAS sh -c 'printf "%s\n" "session optional pam_gnome_keyring.so auto_start" >> /etc/pam.d/sddm'
      ok "sddm unlocks the login keyring on sign-in"
    }
  fi
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

# ---------------------------------------------------------------- brave

# Brave is navi's default browser (Raven's call, 2026-09-29): the most
# private Chromium out of the box, with Leo AI, Brave Wallet, sync, and
# Shields built in. Stock Chromium stays installed as the vanilla
# alternative; the user can switch the default browser + webapp runtime
# anytime with navi-browser. We experiment with this setup on bench
# installs first — reverting is one commit.
#
# navi's touch is deliberately light: Brave's own ultradark look is
# beautiful as-is, so we seed two config files with user-overridable
# defaults and stop there. No managed policy (no "Managed by your
# organization" banner), no force-installed extensions, no imposed
# password manager — Shields covers ad/tracker blocking, so blackice
# stays Chromium-only. The OS suggests, the user decides.
#
# Seeded keys (verified against brave-core source, 2026-09-30):
#   Default/Preferences: browser.theme.color_scheme2=2 (dark),
#     brave.darker_mode=true (ultradark), brave.location_bar_is_wide=true
#   Local State: brave.tabs.compact_horizontal_tabs=true
#     (compact tabs is a Local State pref — seeding it in Preferences
#     silently does nothing)
# Both land in /etc/skel (future users) and the invoking user's $HOME
# (this install). Existing files are never overwritten, and Brave must
# not be running while seeding — it rewrites Preferences on exit.
setup_brave() {
  step "brave (navi's default browser)"
  # Brave's official apt repo — same keyring/sources pattern as
  # scripts/installers/brave-origin-installer.sh.
  if [ ! -f /etc/apt/sources.list.d/brave-browser-release.sources ]; then
    $DOAS curl -fsSLo /usr/share/keyrings/brave-browser-archive-keyring.gpg \
      https://brave-browser-apt-release.s3.brave.com/brave-browser-archive-keyring.gpg
    $DOAS curl -fsSLo /etc/apt/sources.list.d/brave-browser-release.sources \
      https://brave-browser-apt-release.s3.brave.com/brave-browser.sources
    $DOAS apt-get update
    ok "Brave apt repository added"
  else
    info "Brave apt repository already present"
  fi
  if ! command -v brave-browser >/dev/null 2>&1; then
    $DOAS apt-get install -y brave-browser
    ok "brave-browser installed"
  else
    info "brave-browser already installed"
  fi

  seed_brave_profile /etc/skel doas
  seed_brave_profile "$HOME"

  # navi's browser runtime defaults to Brave: ~/.config/navi/default-browser
  # drives navi-browser-run (webapps). Only write when absent or still
  # holding the old chromium default — an explicit user choice is sacred.
  seed_default_browser /etc/skel doas
  seed_default_browser "$HOME"

  # system default browser (xdg): Brave. Same rule — only flip when unset
  # or still on the old chromium default. Runs as the user; harmless if it
  # fails (e.g. no session during install).
  local xdg_cur=""
  xdg_cur="$(xdg-settings get default-web-browser 2>/dev/null || true)"
  if [ -z "$xdg_cur" ] || [ "$xdg_cur" = "chromium.desktop" ]; then
    if xdg-settings set default-web-browser brave-browser.desktop 2>/dev/null; then
      ok "system default browser: Brave"
    else
      warn "could not set xdg default browser (no session?)"
    fi
  else
    info "xdg default browser already '$xdg_cur' — left alone"
  fi
}

# seed_brave_profile <base> [doas] — write Brave's navi defaults under
# <base>/.config/BraveSoftware/Brave-Browser/. Existing files are left
# alone. Pass "doas" to elevate (for /etc/skel).
seed_brave_profile() {
  local base="$1" priv="${2:-}"
  local dir="$base/.config/BraveSoftware/Brave-Browser"
  if pgrep -x brave >/dev/null 2>&1 || pgrep -x brave-browser >/dev/null 2>&1; then
    warn "Brave is running — skipping config seed (it rewrites Preferences on exit)"
    return 0
  fi
  if [ -n "$priv" ]; then
    $DOAS install -d -m 755 "$dir/Default"
  else
    install -d -m 755 "$dir/Default"
  fi
  if [ ! -f "$dir/Default/Preferences" ]; then
    if [ -n "$priv" ]; then
      $DOAS tee "$dir/Default/Preferences" >/dev/null <<'EOF'
{
  "browser": {
    "theme": {
      "color_scheme2": 2
    }
  },
  "brave": {
    "darker_mode": true,
    "location_bar_is_wide": true
  }
}
EOF
    else
      tee "$dir/Default/Preferences" >/dev/null <<'EOF'
{
  "browser": {
    "theme": {
      "color_scheme2": 2
    }
  },
  "brave": {
    "darker_mode": true,
    "location_bar_is_wide": true
  }
}
EOF
    fi
    ok "Brave Preferences seeded ($dir/Default/Preferences)"
  else
    info "Brave Preferences already exists — left alone"
  fi
  if [ ! -f "$dir/Local State" ]; then
    if [ -n "$priv" ]; then
      $DOAS tee "$dir/Local State" >/dev/null <<'EOF'
{
  "brave": {
    "tabs": {
      "compact_horizontal_tabs": true
    }
  }
}
EOF
    else
      tee "$dir/Local State" >/dev/null <<'EOF'
{
  "brave": {
    "tabs": {
      "compact_horizontal_tabs": true
    }
  }
}
EOF
    fi
    ok "Brave Local State seeded ($dir/Local State)"
  else
    info "Brave Local State already exists — left alone"
  fi
}

# seed_default_browser <base> [doas] — default the navi browser runtime to
# Brave. Writes only when absent or still on the old chromium default.
seed_default_browser() {
  local base="$1" priv="${2:-}"
  local cfg="$base/.config/navi/default-browser"
  local cur=""
  if [ -f "$cfg" ]; then
    cur="$(cat "$cfg" 2>/dev/null || true)"
  fi
  if [ -z "$cur" ] || [ "$cur" = "chromium" ]; then
    if [ -n "$priv" ]; then
      $DOAS install -d -m 755 "$base/.config/navi"
      printf 'brave\n' | $DOAS tee "$cfg" >/dev/null
    else
      install -d -m 755 "$base/.config/navi"
      printf 'brave\n' >"$cfg"
    fi
    ok "navi browser runtime default: brave ($cfg)"
  else
    info "navi browser runtime already '$cur' — left alone"
  fi
}

# ---------------------------------------------------------------- etcher

# balenaEtcher (Raven's call, 2026-09-30): flash OS images to SD cards and
# USB drives, safely and easily. Ships out of the box so nobody needs to
# reach for GNOME Disks to flash an ISO. Long-term, Raven wants a native
# nightshadeNeon navi-disks for this; Etcher holds the fort until then.
#
# Distribution: balena's official Cloudsmith apt repo (the old Bintray
# deb.etcher.io is long dead). We replicate their setup.deb.sh manually —
# keyring + sources file, no piped-to-bash.
setup_etcher() {
  step "balenaEtcher (USB/SD image flasher)"
  if [ ! -f /etc/apt/sources.list.d/balena-etcher.list ]; then
    $DOAS curl -fsSL https://dl.cloudsmith.io/public/balena/etcher/gpg.70528471AFF9A051.key \
      | $DOAS gpg --dearmor -o /usr/share/keyrings/balena-etcher-archive-keyring.gpg
    $DOAS chmod 644 /usr/share/keyrings/balena-etcher-archive-keyring.gpg
    printf '%s\n' "deb [signed-by=/usr/share/keyrings/balena-etcher-archive-keyring.gpg] https://dl.cloudsmith.io/public/balena/etcher/deb/debian trixie main" \
      | $DOAS tee /etc/apt/sources.list.d/balena-etcher.list >/dev/null
    $DOAS apt-get update
    ok "balenaEtcher apt repository added"
  else
    info "balenaEtcher apt repository already present"
  fi
  if ! dpkg -l balena-etcher-electron 2>/dev/null | grep -q "^ii"; then
    $DOAS apt-get install -y balena-etcher-electron
    ok "balena-etcher-electron installed"
  else
    info "balena-etcher-electron already installed"
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
  # release channel: navi-update reads this to decide what "newer" means.
  # eiri builds follow eiri, everything else follows stable. never clobber
  # an existing channel — the user may have opted into a different one.
  $DOAS mkdir -p /etc/navi
  if [ ! -f /etc/navi/channel ]; then
    if [ "$NAVI_CHANNEL" = "eiri" ]; then
      echo "eiri" | $DOAS tee /etc/navi/channel >/dev/null
    else
      echo "stable" | $DOAS tee /etc/navi/channel >/dev/null
    fi
    ok "release channel: $(cat /etc/navi/channel 2>/dev/null || echo "$NAVI_CHANNEL")"
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
  ║   optional extras live in the navi-extras menu:              ║
  ║   Super+D -> navi-extras: nightly browsers, ani-cli, charm   ║
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
    deploy_installers
    manifest_begin
    deploy_configs
    install_identity
    manifest_write
    install_commands
    setup_doas
    setup_sudo
    setup_zram
    setup_agents
    setup_navivim
    setup_navicode
    setup_mods
    setup_heylain
    setup_environment
    setup_flatpak
    setup_flatpak_polkit
    setup_gtk_theme
    setup_dirs
    setup_sddm
    setup_chromium
    setup_brave
    # setup_etcher — DISABLED 2026-10-01: balena-etcher-electron hard-depends
    # on gconf2, which was removed from Debian trixie entirely. apt refuses
    # to install it. Re-enable if balena fixes their packaging, or retire
    # when navi-imprint (native flasher) lands.
    setup_fonts
    setup_webapps
    setup_telegram
    setup_element
    setup_tailscale
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
  deploy_installers
  manifest_begin
  deploy_configs
  install_identity
  manifest_write
  install_commands
  setup_doas
  setup_sudo
  setup_zram
  setup_agents
  setup_navivim
  setup_navicode
  setup_mods
  setup_heylain
  setup_environment
  setup_flatpak
  setup_flatpak_polkit
  setup_gtk_theme
  setup_dirs
  setup_sddm
  setup_chromium
  setup_brave
  # setup_etcher — DISABLED 2026-10-01: gconf2 gone from trixie (see above)
  setup_fonts
  setup_webapps
  setup_telegram
  setup_element
  setup_tailscale
  tighten_sudo
  done_banner
}

main "$@"

