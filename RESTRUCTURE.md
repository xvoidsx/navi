# navi repository restructure — draft (2026-09-10)

The "iron structure": `/wired` is the desktop layer only. Distro-level
concerns (base system, users, doas, services) live elsewhere in the repo.

## What this draft contains

- `install.sh` — the new idempotent desktop installer (welcome-to-navi
  experience). Deploys `wired/` -> `/usr/share/navi/wired`, user configs ->
  `~/.config/*` (with backups), commands -> `/usr/bin` + rofi `.desktop`
  launchers, doas, flathub, wallpapers.
- `wired/` — the complete desktop layer (44 files + wp/ placeholders).
- `src-navi/`, `src-wiredWM/` — local read-only source checkouts used to
  build this draft. Not part of the real repo.

## Migration map

From `xvoidsx/navi` (main):
`configs/core/sway/config` -> `wired/sway/config`
`configs/core/i3/config` -> `wired/i3/config`
`configs/core/waybar/{config.jsonc,style.css}` -> `wired/waybar/`
`configs/core/polybar/config.ini` -> `wired/polybar/config.ini`
`configs/core/rofi/config.rasi` -> `wired/rofi/config.rasi`
`configs/core/picom/picom.conf` -> `wired/picom/picom.conf`
`configs/alacritty.toml` -> `wired/terminals/alacritty.toml`
`configs/cliamp/config.toml` -> `wired/cliamp/config.toml`
`configs/herdr/config.toml` -> `wired/herdr/config.toml`
`configs/ai/opencode/tui.json` -> `wired/ai/opencode/tui.json`
`configs/ai/pi/agent/themes/nightshadeNeon.json` -> `wired/ai/pi/agent/themes/`
`configs/{conky.conf,dunstrc,vimrc,bashrc}` -> `wired/`
`configs/tmux/tmux.conf` -> `wired/tmux/tmux.conf`
`scripts/system/{nslock.sh,naviWalls.sh,navi-Q.sh,navi-Qx.sh,gifpaperslain.sh}`
-> `wired/scripts/`

From `rav3ndust/wiredWM` (next branch, scripts-config/):
`configs/chromium/theme.json` -> `wired/chromium/theme.json`
`configs/foot.ini` -> `wired/terminals/foot.ini`
`configs/i3status-config` -> `wired/i3status.conf`
`configs/i3blocks-config` -> `wired/i3blocks.conf`
`configs/environment` -> DROPPED (see below)
`learn.sh`, `learn.txt` -> `wired/scripts/`
`manual/*.html` (5 files) -> `wired/manual/`
`polybar-scripts/battery-combined-shell.sh` -> `wired/polybar/`
`remoji/remoji.sh`, `remoji/emojis.txt` -> `wired/scripts/`
`wired_power_menu.sh` -> `wired/scripts/`
`wp/` + `wp/gifpaperslain/` -> `wired/wp/` (binaries ship via git)

New: `wired/applications/*.desktop` (7 rofi launchers for the navi commands).

## Fixes applied during migration

- `learn.sh`: manual path `$HOME/wiredWM/scripts-config/manual/manual.html`
  -> `/usr/share/navi/wired/manual/manual.html`
- `remoji.sh`: `$emojidir=$HOME/wiredWM/scripts-config/remoji`
  -> `/usr/share/navi/wired/scripts`
- `naviWalls.sh`, `gifpaperslain.sh`: zenity default dirs pointed at
  `/usr/share/navi/wired/wp[/gifpaperslain]`
- `sway/config`: wallpaper block rewritten — swaybg paints
  `wp/lain3wp.jpg` (static fallback), then mpvpaper plays
  `wp/gifpaperslain/navi-lain.gif` on all outputs (animated default)
- `navi-Q.sh`, `navi-Qx.sh`: hardcoded `node-v22.23.1-linux-x64` path replaced
  with runtime resolution of the newest `node-v*-linux-x64` under
  `~/.local/share/pi-node/`
- `i3blocks.conf`: `thunar` -> `nemo`; dead `kitty ~/Projects/i3-r3/logout.sh`
  -> `i3-msg exit`; dead `kitty ~/Projects/i3-r3/shutdown.sh` -> `power_menu`;
  dmenu launcher block -> `rofi -show drun`
- `install.sh` package list: audited against Debian 13 trixie (2026-09-10).
  `opendoas` (not transitional `doas`), no `slock` (virtual), real
  `imagemagick-7.q16`, added `i3status` + `i3blocks` (old installer deployed
  their configs without installing them).

## Intentionally dropped

- `i3status` (package + `wired/i3status.conf`) and `i3blocks` (package +
  `wired/i3blocks.conf`): the X11 session runs polybar, not i3bar, so both
  had nothing to feed. Dropped 2026-09-10 per Raven.

- `environment` — it's just Debian's default PATH; overwriting
  `/etc/environment` with it buys nothing. Dropped, not migrated.
- `arch-install-demo/`, `arch-installer/`, `nix-configs/`, `config-wayland-old`,
  `config.bak`, alternate Waybar/Polybar styles, `nightshade-glass.ini`,
  `nightshade-tmux.md` — per Raven's decisions.

## Still open (Raven's calls)

- **ydotool**: backports-only in trixie. Enable `trixie-backports` or drop it?
  (install.sh currently prompts.)
- **mpvpaper fork** (`scripts/build_install.sh`): remove the auto wallpaper
  setter from `main()`, fix the `"$pkgs"` quoting bug on the apt line, repoint
  `gifpaper_setter.sh` at the new wallpaper location.
- `wired/wp/` binaries: copy from wiredWM `next` branch `wp/` + add
  `navi-lain.gif` to `gifpaperslain/`.
- The manual "needs work" (Raven) — migrated as-is; `nightshade-tmux.md`
  already dropped.

## Identity set (2026-09-10)

- `wired/identity/os-release` → `/etc/os-release` (ID=navi, ID_LIKE=debian,
  VERSION_ID=1.2, codename mika) — fastfetch and friends pick up "navi"
- `wired/identity/lsb-release` → `/etc/lsb-release`
- `wired/identity/issue`, `issue.net` → `/etc/issue{,.net}` — TTY login banner
- `wired/VERSION` → `/usr/share/navi/VERSION` — the release stamp
- `wired/fastfetch/config.jsonc` → `~/.config/fastfetch/config.jsonc` —
  fastfetch renders the empty-set logo
- `wired/identity/navi-logo.txt` — the static ∅ mark + wordmark
- `wired/scripts/navi-logo.sh` → installed as the `navi-logo` command —
  the ∅ mark animated (slash sweeps 180° and loops); `--static` for still,
  `--loop` for infinite
- The installer banner now opens with the ∅ mark; preflight accepts
  ID=navi so reruns keep working after branding.

## Versioning

- Current release: **navi 1.2 "mika"**. Tag `v1.2-mika` goes on the shared
  repo when the draft lands, so future work can split by version.

## Login screen (2026-09-10)

- SDDM (Raven's old Arch installer used sddm too) with a custom `navi` QML
  theme: nightshade styling, the ∅ mark, 24-hour clock, `DD Month` dates,
  user + session pickers, reboot/shutdown buttons.
- Session picker offers **navi (Wayland)** (`wired/sessions/navi.desktop`
  → `/usr/share/wayland-sessions/`) and **navi (X11)**
  (`wired/sessions/navi-x11.desktop` → `/usr/share/xsessions/`).
- Background: static `wp/lain3wp.jpg`; if
  `wp/login-loop.mp4` exists the theme plays it on a muted loop over the
  static image (see `wired/sddm/README.md` for the ffmpeg one-liner).
  `qml6-module-qtmultimedia` is installed for this.
- `setup_sddm()` in install.sh deploys the theme, the session files, and
  enables the sddm service.

## Landing this

Review this draft, then replicate into the real repo: move the files per the
map above, add `install.sh` at the repo root, commit. No public changes were
made from here.
