# xvoidsx release signing key — ceremony

> **Status (2026-09-17): deferred to the eiri era.** Raven ran the
> ceremony and the key exists (offline master + signing subkey, backups
> kept), but xvoidsx is deliberately not committing to a signing scheme
> for the 1.x series — the ceremony has to fit how releases actually get
> cut (multi-machine remote workflow), and that needs exploration first
> (GPG vs SSH vs sigstore/keyless vs minisign vs something of our own —
> "navicheck"?). Until then, `navi-update` pulls the channel with a loud
> warning instead of verifying. The ceremony below is preserved for when
> the decision lands; provisioning the key file + fingerprint flips the
> updater back to fail-closed with no other code changes.

`navi-update` only installs navi's files from a release tag whose
signature verifies against the xvoidsx release key. No signature,
no update — fail closed. This document is the ceremony for creating
that key. It is the single most security-sensitive thing in navi's
update story, so read it fully before running any command.

## Principles

- **Raven generates the key, on hardware he controls.** Lain writes the
  ceremony and all the verification code, but never sees the private
  key. That is not modesty — it is the security model working as intended.
- **GPG, not SSH.** GitHub renders "Verified" for both, but GPG is the
  release-signing convention across the distro world (Debian, Arch,
  Fedora). If xvoidsx ever wants to look like a real distro to a
  hardware partner, GPG is the expected answer.
- **Master key + signing subkey.** The master (certify-only) lives
  offline; a signing subkey lives on the daily machine. If the subkey
  is ever compromised, revoke just the subkey and issue a new one —
  the trust root never changes and every navi machine keeps verifying
  without an update.
- **One key, two jobs.** It signs release tags (for `navi-update`) AND
  the SHA256SUMS files for ISO downloads (for the website). One ceremony.

## The ceremony

Do this on a trusted machine, ideally offline (an air-gapped live USB
is the gold standard; your daily driver with no network is fine for v1).

```bash
# 1. generate: RSA 4096, certify-only master. no expiry is a footgun —
#    set 2 years; you can extend it later with the master key.
gpg --full-generate-key
#   kind: (4) RSA (sign only)  -> then toggle to certify-only:
#   actually: choose (4) RSA (sign only), then after creation:
gpg --quick-set-expire <KEYID> 2y

# 2. add the signing subkey (this is what signs tags day-to-day)
gpg --quick-add-key <KEYID> rsa4096 sign 2y

# 3. identity: a ROLE, not a person
gpg --quick-add-uid <KEYID> "xvoidsx (navi release signing) <rav3nwing@proton.me>"
#   (the address can change when xvoidsx has its own MX — UIDs are cheap)

# 4. revocation certificate — generate NOW, while everything works.
#    without this, a compromised key cannot be revoked. ever.
gpg --output xvoidsx-release-revoke.asc --gen-revoke <KEYID>

# 5. export the PUBLIC key — this is the only thing that leaves the building
gpg --armor --export <KEYID> > release.asc
```

## What goes where

| Artifact | Destination | Notes |
|---|---|---|
| `release.asc` (public) | `wired/keys/release.asc` in this repo | ships in the ISO at `/usr/share/navi/wired/keys/release.asc` |
| fingerprint (40 hex chars) | `RELEASE_FINGERPRINT` in `scripts/navi-update.sh` | belt-and-suspenders: the script checks the pinned key's fingerprint matches, so even a swapped key file fails closed |
| `release.asc` (public) | the navi website, next to the ISO downloads | transparency; also sign SHA256SUMS with `gpg --armor --detach-sign` |
| master private key + revoke cert | encrypted offline backup (USB in a drawer) | never on a networked machine longer than the ceremony |
| signing subkey | Raven's daily machine (+ Ryoko's later, if wanted) | passphrase-protected, obviously |

Until `RELEASE_FINGERPRINT` is filled in and `wired/keys/release.asc`
exists, `navi-update`'s navi layer refuses to update. That is correct —
fail closed, not fail convenient.

## Rotation

Written down now so nobody improvises later:

1. Generate the new key (same ceremony).
2. Have the OLD key sign a transition statement; publish it.
3. Ship the new `release.asc` + new fingerprint in a normal navi update,
   signed by the old key. Machines pick it up through the existing trust.
4. After one full release cycle, retire the old key.

You hope to never use this. Write it down anyway.

## Later, maybe

- **Ryoko as second signer.** Not for v1 — one trust root is simpler to
  reason about. Revisit when the nonprofit exists; shared custody is a
  natural thing to formalize then.
- **A real apt repo with a versioned `navi-meta` .deb** (the eiri-era
  plan) will want its own Release-file signing — this same key does that job.
