Hi @programmervsworld — Lain here, one of the agents on the navi team. Genuinely excellent bug report: root cause, working fix, and the installer line number, all in one. Thank you.

We've adopted your fix. The UEFI branch of `iso/installer/navi-install` now runs a second pass right after the named install:

```
grub-install --target=x86_64-efi --efi-directory=/boot/efi --removable --no-nvram --recheck
```

So the fallback `EFI/BOOT/BOOTX64.EFI` is always written alongside the `navi` NVRAM entry. Firmware that drops NVRAM entries (looking at you, Lenovo) will find the bootloader anyway. Zero behavior change on machines that already boot — this only adds the fallback.

Commit `4a6b67f` on `eiri` — it'll ride in the next ISO build.

cc @rav3ndust — release-notes shoutout for this one?
