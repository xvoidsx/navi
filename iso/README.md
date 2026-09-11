# navi ISO pipeline — Mika RC11

Builds the installer ISO: a Debian 13 (trixie) live image that boots
**straight into the navi CLI system installer** — no desktop, no "try navi"
mode. The ISO is the clean-machine test vehicle for Mika RC11.

## Layout

```text
iso/
├── config/
│   ├── package-lists/navi-installer.list.chroot   # tools inside the ISO
│   └── hooks/normal/0100-navi-iso.hook.chroot     # installer + tty1 autologin
├── installer/
│   └── navi-install                   # the CLI system installer (runs on ISO)
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
   `.github/workflows/iso.yml`) produces `navi-1.2-mika-RC11-<arch>.hybrid.iso`.
3. The ISO boots: root autologin on tty1 → `/usr/local/bin/navi-install`.
4. `navi-install` (system layer): disk select → type `YES` → username +
   full name + passwords + hostname → LUKS passphrase → hybrid GPT
   (bios_grub + ESP + one LUKS2 root, so legacy BIOS and UEFI both boot) →
   debootstrap trixie → users, crypttab, GRUB (with cryptodisk), initramfs →
   copies the navi payload to `/opt/navi-iso` in the target → **provisions
   the wired right there in the chroot** (`install.sh --yes` as the
   installed user, doas temporarily nopass, locked down to `permit persist`
   after) → reboot.
5. First boot: LUKS passphrase prompt (twice — GRUB, then initramfs, since
   `/boot` lives inside the encrypted root) → SDDM password login → the
   wired desktop. No network needed at first boot: everything is already
   provisioned.

## Why provisioning runs in the installer chroot, not at first boot

(RC10 changed this — it used to be a first-boot oneshot service.)
First-boot provisioning bets the whole install on the new system's network
coming up by itself, on unknown hardware. Old wifi chips don't always
cooperate (seen on the x200 bench: the persisted profile never connected,
so provisioning couldn't fetch a single package). The installer's network
is proven — it just downloaded the base system — so provisioning runs
while it's up, inside the chroot.

`install.sh` refuses root, so the installer runs it as the installed user
via `runuser` with a clean environment, with a temporary `permit nopass`
doas rule that is replaced by `permit persist` when provisioning finishes.
`systemctl enable` calls in the chroot do their symlink work client-side
(into the target's `/etc`); the D-Bus daemon-reload lands on the
disposable live session — harmless, and nothing starts target services.
If provisioning fails, the installer dies loudly with the log path instead
of stranding a half-built system at first boot.

## Building

CI does this on every manual dispatch (`.github/workflows/iso.yml`).
Locally, on a Debian machine as root:

```sh
./iso/stage.sh
cd iso/build && lb config <flags> && lb build   # flags: see .github/workflows/iso.yml
```

Requirements: x86_64, network access during installation. The installed
system needs no network at first boot.

## Follow-ups (not in RC1 scaffolding)

- `system/` and `scripts/installers/` are not populated yet — `install.sh`
  already tolerates their absence (skips silently).
- Secure Boot signing is not set up (unsigned `grub-efi-amd64`).
