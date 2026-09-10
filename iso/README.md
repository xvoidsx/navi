# navi ISO pipeline — Mika RC1

Builds the installer ISO: a Debian 13 (trixie) live image that boots
**straight into the navi CLI system installer** — no desktop, no "try navi"
mode. The ISO is the clean-machine test vehicle for Mika RC1.

## Layout

```text
iso/
├── config/
│   ├── package-lists/navi-installer.list.chroot   # tools inside the ISO
│   └── hooks/normal/0100-navi-iso.hook.chroot     # installer + tty1 autologin
├── installer/
│   ├── navi-install                   # the CLI system installer (runs on ISO)
│   ├── navi-provision                 # first-boot provisioning wrapper
│   └── navi-provision.service         # …as a oneshot systemd unit
├── stage.sh                           # assemble a live-build tree (not committed)
└── build/                             # staged tree — gitignored, built by CI
```

(There is deliberately no `auto/config`: live-build auto-executes it, and
ours must call `lb config` itself — that recursed forever in the first
build. The flags live inline in `.github/workflows/iso.yml` instead.)

## How it works

1. `iso/stage.sh` copies `iso/config` and the navi payload
   (`install.sh`, `wired/`, `iso/installer`) into `iso/build/`, with the
   payload landing at `config/includes.chroot/opt/navi-iso/`.
2. `lb config <flags> && lb build` (as root; flags are in
   `.github/workflows/iso.yml`) produces `navi_1.2_mika_RC1-<arch>.hybrid.iso`.
3. The ISO boots: root autologin on tty1 → `/usr/local/bin/navi-install`.
4. `navi-install` (system layer only): disk select → type `YES` → user +
   passwords → LUKS passphrase → GPT (EFI + one LUKS2 root, PBKDF2 so GRUB
   can unlock `/boot`) → debootstrap trixie → users, crypttab, GRUB,
   initramfs → copies the navi payload to `/opt/navi-iso` in the target →
   installs + enables `navi-provision.service` → reboot.
5. First boot: LUKS passphrase prompt → `navi-provision.service` runs
   `install.sh --yes` as the installed user (provisioning layer: packages,
   `/wired`, commands, identity, agents, wallpapers, flatpak, SDDM) →
   writes the SDDM autologin drop-in (if `AUTOLOGIN=yes`) → locks doas down
   to `permit persist` → disables itself → starts SDDM → wired desktop.

## Why provisioning runs at first boot, not in chroot

`install.sh` must run as a **normal user** (it refuses root), uses
`doas`/`sudo` for escalation, enables real systemd services (`sddm`,
`ollama`), and touches per-user state (`~/.config`, flatpak user
overrides). None of that works faithfully inside a debootstrap chroot.
First-boot provisioning runs on the real system with networking up, so
every step behaves exactly as it will for users. The installer pre-seeds
a temporary `permit nopass` doas rule so provisioning is non-interactive;
`navi-provision` replaces it with `permit persist` when done.

If provisioning ever fails, boot continues to a getty: log in as the
installed user and run `/opt/navi-iso/install.sh --yes` by hand.

## Building

CI does this on every manual dispatch (`.github/workflows/iso.yml`).
Locally, on a Debian machine as root:

```sh
./iso/stage.sh
cd iso/build && lb config <flags> && lb build   # flags: see .github/workflows/iso.yml
```

Requirements: UEFI boot for the target machine (the installer aborts on
BIOS boot), x86_64, network access at first boot.

## Follow-ups (not in RC1 scaffolding)

- `system/` and `scripts/installers/` are not populated yet — `install.sh`
  already tolerates their absence (skips silently).
- Secure Boot signing is not set up (unsigned `grub-efi-amd64`).
- BIOS/CSM target installs are not supported (UEFI-only for RC1).
