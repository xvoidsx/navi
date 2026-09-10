## ∅ Review: Mika RC1 batch — agents, wallpapers, ISO scaffold

Since the last review, `mika` has grown up a lot:

- **Agent stack** — `install.sh` now provisions ollama, opencode, and omp, all landing in `/usr/bin` so every machine user gets them. `navi-Q` should light up on first boot.
- **mpvpaper, fixed properly** — the fork moved to `xvoidsx/mpvpaper`, the build script's package-list bug is fixed, and the installer runs the build from the repo root (relative `meson` paths need that).
- **Wallpaper system** — `navi-lain.gif` ships in the repo; first boot runs `navi-wallpaper`, which tries mpvpaper first, *verifies* it's alive, and only then falls back to static. The two never fight. `gifpaperslain` is in the rofi launcher, and `naviWalls` kills mpvpaper when you go static. Default static is now `SElain3.jpg` for both sway and SDDM.
- **Executable-bit hygiene** — all repo scripts are tracked `100755` in git, and the installer chmods every `*.sh` under `/usr/share/navi`, so scripts run no matter how the repo was fetched.
- **ISO scaffold** — `iso/` holds the live-build trixie config, `navi-install` (boot-to-installer: disk → type-YES → user/pass → LUKS → debootstrap → GRUB), and first-boot provisioning via a oneshot service (chroot was the wrong call — `install.sh` needs a real booted system). Workflow is manual-dispatch only. Tagged `v1.2-mika-RC1`.

**What's solid:** the wallpaper layering is genuinely robust now; `/usr/bin` everywhere; the system-installer vs provisioning split is the right architecture; nothing merged or released — RC tag only.

**Still unproven (the weekend test decides):**
1. Nothing has booted yet. First Actions build, SDDM QML theme, the LUKS double-prompt, first-boot provisioning — all untested on real hardware.
2. First-boot provisioning needs network; offline machines get a getty with manual instructions. Acceptable for RC1, but the installer should warn about it.
3. UEFI x86_64 only for RC1 — fine, just say so when it goes on the website.

**Verdict:** the RC1 ISO is the test. Boot it, install it, unlock it, land in the wired. If that works, this merges and we polish for release. Still draft until then.
