# RC2: wireless installer + tools audit

RC2 is built and published: `navi-1.2-mika-RC2.iso` (968 MiB) at
https://github.com/xvoidsx/navi/releases/tag/v1.2-mika-RC2

What changed since RC1:
- **WiFi in the installer.** New `setup_network` step right after preflight: if Ethernet is live it carries on silently, otherwise it offers WiFi — scans, numbered list, manual SSID for hidden networks, silent password prompt, 3 attempts. `persist_network` copies the NetworkManager profile into the LUKS-encrypted target so first boot is already online for provisioning.
- `network-manager` + `wpasupplicant` + `iw` added to the installer ISO package list; `wpasupplicant` added to the debootstrap include list so the installed system can do WiFi too.
- iso.yml now parameterizes the release name (RC_LABEL / ISO_BASENAME) instead of hardcoding it.
- Publishing moved to publish-rc2.yml: the Actions GITHUB_TOKEN 403s on release *creation* here, so the release shell is created ahead of time and the runner only uploads assets.

Tools audit (Raven asked): remoji, naviWalls, gifpaperslain, navi-wallpaper, power_menu, learn, navi-Q, navi-Qx, nslock are all installed to /usr/bin by install.sh with rofi .desktop registration. No gaps found.

Still to come: Raven's fresh-machine test, now with the WiFi path as well as Ethernet.
