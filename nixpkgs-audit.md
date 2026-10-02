# nixpkgs audit — navi Debian package list

First pass, 2026-10-02. Source: `PKGS` array in
`~/workspace/navi-restructure/install.sh` (143 unique Debian packages),
plus additional navi dependencies.

Legend: `→` means "install this nixpkgs attribute instead."

---

## Mapped (Debian name → nixpkgs attribute)

### Window managers / compositors / display
| Debian | nixpkgs |
|---|---|
| i3 | `i3` |
| i3lock-fancy | `i3lock-fancy` |
| nitrogen | `nitrogen` |
| xcompmgr | `xcompmgr` |
| picom | `picom` |
| sway | `sway` |
| swaylock | `swaylock` |
| swayidle | `swayidle` |
| swaybg | `swaybg` |
| xwayland | `xwayland` |
| wdisplays | `wdisplays` |
| arandr | `arandr` |
| sddm | `sddm` |

### Bars, launchers, notifications
| Debian | nixpkgs |
|---|---|
| waybar | `waybar` |
| polybar | `polybar` |
| conky-all | `conky` |
| rofi | `rofi` |
| dunst | `dunst` |

### Terminals
| Debian | nixpkgs |
|---|---|
| alacritty | `alacritty` |
| xterm | `xterm` |
| kitty | `kitty` |
| stterm | `st` |
| sakura | `sakura` |
| foot | `foot` |
| tmux | `tmux` |

### File managers
| Debian | nixpkgs |
|---|---|
| nemo | `nemo` |
| nnn | `nnn` |
| filezilla | `filezilla` |

### Audio
| Debian | nixpkgs |
|---|---|
| pulseaudio-utils | `pulseaudio` |
| pamixer | `pamixer` |
| pavucontrol | `pavucontrol` |
| playerctl | `playerctl` |
| volumeicon-alsa | `volumeicon` |
| pasystray | `pasystray` |
| alsa-utils | `alsa-utils` |
| cmus | `cmus` |
| cava | `cava` |
| pianobar | `pianobar` |

### Bluetooth / power
| Debian | nixpkgs |
|---|---|
| bluez | `bluez` |
| blueman | `blueman` |
| upower | `upower` |
| power-profiles-daemon | `power-profiles-daemon` |
| brightnessctl | `brightnessctl` |

### Screenshots / screen capture
| Debian | nixpkgs |
|---|---|
| flameshot | `flameshot` |
| maim | `maim` |
| shotman | `shotman` |
| wf-recorder | `wf-recorder` |
| xdotool | `xdotool` |
| xclip | `xclip` |
| wl-clipboard | `wl-clipboard` |
| grim | `grim` |

### Image / PDF viewers
| Debian | nixpkgs |
|---|---|
| nsxiv | `nsxiv` |
| feh | `feh` |
| imv | `imv` |
| zathura | `zathura` |

### Browsers / web
| Debian | nixpkgs |
|---|---|
| chromium | `chromium` |
| firefox-esr | `firefox-esr` |
| surf | `surf` |
| lynx | `lynx` |
| elinks | `elinks` |
| w3m | `w3m` |
| amfora | `amfora` |

### Video
| Debian | nixpkgs |
|---|---|
| ffmpeg | `ffmpeg` |
| mpv | `mpv` |
| mpvpaper | `mpvpaper` |

### Fonts / themes / icons
| Debian | nixpkgs |
|---|---|
| fonts-inter | `inter` |
| fonts-jetbrains-mono | `jetbrains-mono` |
| fonts-firacode | `fira-code` |
| fonts-noto | `noto-fonts` |
| fonts-cascadia-code | `cascadia-code` |
| fonts-font-awesome | `font-awesome` |
| fonts-material-design-icons-iconfont | `material-design-icons` |
| bibata-cursor-theme | `bibata-cursors` |
| papirus-icon-theme | `papirus-icon-theme` |
| moka-icon-theme | `moka-icon-theme` |
| yaru-theme-gtk | `yaru-theme` (covers gtk + icon) |
| yaru-theme-icon | `yaru-theme` |

### Dev tools / CLI
| Debian | nixpkgs |
|---|---|
| wget | `wget` |
| curl | `curl` |
| git | `git` |
| htop | `htop` |
| lsd | `lsd` |
| jq | `jq` |
| ripgrep | `ripgrep` |
| fd-find | `fd` |
| fzf | `fzf` |
| shellcheck | `shellcheck` |
| vim | `vim` |
| pandoc | `pandoc` |
| glow | `glow` |
| fastfetch | `fastfetch` |
| cmatrix | `cmatrix` |
| tty-clock | `tty-clock` |
| imagemagick-7.q16 | `imagemagick` |
| nodejs | `nodejs` |
| npm | *(bundled with nodejs)* |
| zip | `zip` |
| unzip | `unzip` |
| bzip2 | `bzip2` |
| zstd | `zstd` |
| lolcat | `lolcat` |
| dmenu | `dmenu` *(split out of suckless-tools)* |

### System / desktop integration
| Debian | nixpkgs |
|---|---|
| sudo | `sudo` |
| opendoas | `doas` |
| flatpak | `flatpak` |
| dconf-cli | `dconf` |
| libnotify-bin | `libnotify` |
| xdg-desktop-portal-wlr | `xdg-desktop-portal-wlr` |
| wlr-randr | `wlr-randr` |
| zenity | `zenity` |
| xss-lock | `xss-lock` |
| calcurse | `calcurse` |
| gsimplecal | `gsimplecal` |
| meteo-qt | `meteo-qt` |
| lxappearance | `lxappearance` |
| qt5ct | `libsForQt5.qt5ct` |
| gnome-keyring | `gnome-keyring` |
| libsecret | `libsecret` |
| wlsunset | `wlsunset` |

### Qt6 QML modules (for SDDM theme)
| Debian | nixpkgs |
|---|---|
| qml6-module-qtmultimedia | `qt6.qtmultimedia` |
| qml6-module-qtquick-controls | `qt6.qtquickcontrols2` |
| qml6-module-qtquick-layouts | *(part of `qt6.qtdeclarative`)* |

### Python (for mod/switcher scripts)
| Debian | nixpkgs |
|---|---|
| python3-pil | `python3Packages.pillow` |
| python3-gi | `python3Packages.pygobject3` |

### Additional navi dependencies (not in PKGS)
| Name | nixpkgs |
|---|---|
| ollama | `ollama` (+ `services.ollama.enable`) |
| tailscale | `tailscale` (+ `services.tailscale.enable`) |
| brave-browser | `brave` |
| codium (vscodium) | `vscodium` |
| element-desktop | `element-desktop` |
| signal-desktop | `signal-desktop` |
| telegram-desktop | `telegram-desktop` |

---

## NixOS modules instead of packages

These Debian packages have no nixpkgs equivalent because NixOS handles
them declaratively. Do not put them in `environment.systemPackages`.

| Debian | NixOS equivalent |
|---|---|
| pipewire-audio-client-libraries | `services.pipewire = { enable = true; alsa.enable = true; pulse.enable = true; }` |
| zram-tools | `zramSwap.enable = true;` |
| libpam-gnome-keyring | `security.pam.services.<name>.enableGnomeKeyring = true;` |
| firmware-realtek, firmware-iwlwifi, firmware-atheros, firmware-brcm80211, firmware-mediatek | `hardware.enableAllFirmware = true;` (or `hardware.firmware = [ pkgs.linux-firmware ... ];`) |
| sddm-theme-maldives | Not needed — set `services.displayManager.sddm.theme` declaratively. (The Debian package only existed to satisfy apt's theme dependency cheaply.) |
| python-dev-is-python3 | N/A — Debian metapackage, meaningless on NixOS. |
| build-essential | N/A as a package — add `gcc` (and `gnumake` if needed) to systemPackages for NaviVim's runtime treesitter compilation. |
| suckless-tools | Debian metapackage — install components individually (`dmenu`, etc.). |
| npm | Bundled with `nodejs`. |
| ufw | Use `networking.firewall` instead (see below). |

---

## Needs custom derivation (not in nixpkgs)

| Debian | Notes |
|---|---|
| lxpolkit | **Not found** in nixpkgs (checked by-name + likely old paths). Alternatives already in nixpkgs: `lxqt.lxqt-policykit`, `polkit_gnome`. Pick one — lxpolkit itself would need a trivial custom derivation. |
| ufw | **Not found** in nixpkgs. The NixOS-native answer is `networking.firewall` — declarative firewall rules. This is strictly better for tachibana; do not package ufw. |
| gufw | **Not found** in nixpkgs (Ubuntu-specific GTK frontend for ufw). Drop it — with `networking.firewall` there's nothing for it to manage. If a GUI firewall is truly wanted later, that's a separate project. |
| grimshot | **Not found** in checked nixpkgs locations. It's a small shell script (grim + slurp wrapper). Either write a trivial derivation or replace its usage with direct `grim`/`slurp` calls. Recommend the latter — one less thing to maintain. |
| gnome-software-plugin-flatpak | Almost certainly not in nixpkgs and not needed — navi doesn't use GNOME Software. **Drop it.** |

---

## Uncertain (couldn't verify — needs manual check)

| Debian | Notes |
|---|---|
| pipx | Could not confirm location in nixpkgs (by-name check 404'd, but it may live under a python-versioned path). Fairly confident it exists — verify with `nix search nixpkgs pipx` on a Nix machine. |
| zathura-pdf-poppler | `zathura` itself is confirmed. The poppler plugin's packaging in nixpkgs is unclear — may be bundled, may need `zathuraPkgs` or a wrapper. Verify. |
| gir1.2-gtk-3.0 | The GIR typelib needs `gobject-introspection` + wrapped gtk3. In Nix this works but requires the consuming Python env to be built with `wrapGAppsHook` or equivalent. Not a simple package mapping — needs care when packaging the switcher/OSD scripts' Python env. |
| python3-venv, python3-pip | No direct equivalent — Nix doesn't do venvs this way. Hey Lain's Python env needs a proper Nix Python derivation (`python3.withPackages` or mach-nix style). Architectural, not a package swap. |

---

## Summary counts

- **Mapped:** ~125 packages → direct nixpkgs attributes (plus 13 additional deps, all confirmed)
- **NixOS modules instead:** 10 (no package needed)
- **Needs custom derivation / drop:** 5 (lxpolkit, ufw, gufw, grimshot, gnome-software-plugin-flatpak) — of which 3 should simply be dropped in favor of NixOS-native approaches
- **Uncertain:** 4 (pipx, zathura-pdf-poppler, gir1.2-gtk-3.0, python venv/pip)

**Bottom line:** ~95% of navi's package list maps cleanly to nixpkgs. The gaps are small and mostly have better NixOS-native answers (firewall, zram, firmware, keyring PAM). The real work isn't missing packages — it's the FHS assumptions in the scripts that *use* these packages, plus the Python environments (Hey Lain, switcher/OSD).
