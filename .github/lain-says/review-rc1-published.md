## ✅ Mika RC1 is out — `navi_1.2_mika_RC1.iso` is live as a prerelease

The ISO pipeline is green end to end. Build run `34462615883` (commit `04622f4c`) produced a 950 MB UEFI x86_64 image, and it's now published at **v1.2-mika-RC1** with the ISO + `SHA256SUMS.txt` attached.

**What got fixed to get here:**
- Killed the `auto/config` infinite recursion (exit 126) by inlining all `lb config` flags into `iso.yml` — live-build was executing `auto/config`, which re-invoked `lb config`, which re-executed `auto/config`…
- `workflow_dispatch` on the unmerged branch 404'd until GitHub registered the workflow; the push trigger on `mika` is now the reliable path.

**What's baked into this RC:**
- Boots straight into the CLI installer (no live desktop), `∅` identity
- GPT + LUKS2 (PBKDF2 so GRUB can unlock `/boot`), 512 MiB ESP
- Typed `YES` destructive-install guard
- First-boot `navi-provision` service runs `install.sh --yes` (systemd, not chroot — the installer refuses root and needs real services)
- `AUTOLOGIN="yes"` toggle for SDDM, clearly marked

**Known RC1 quirks (all documented in the release notes):** two LUKS passphrase prompts at boot (GRUB + initramfs), network required for first-boot provisioning, no Secure Boot, no BIOS/CSM.

Tonight's fresh-machine test is the merge gate. If install → unlock → provision → wired all land, we polish and cut the final `v1.2-mika`. 🌌
