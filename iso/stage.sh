#!/bin/sh
# iso/stage.sh — stage the navi repo into a live-build tree.
#
# Copies the live-build configuration (iso/config) plus the navi
# payload the installer needs (install.sh, wired/, scripts/,
# iso/installer) into a build directory, ready for `lb config <flags> && lb build`.
# (The flags live in .github/workflows/iso.yml — see the note there about
# why there is no auto/config script.)
#
# NaviVim (the default terminal IDE) is fetched here at the pinned commit
# from install.sh (NAVIVIM_PIN), together with the pinned neovim release
# tarball (NAVIVIM_NVIM_VERSION) — so fresh installs never depend on
# GitHub being reachable at install time.
#
#   ./iso/stage.sh [build-dir]   # default: iso/build
#
# Nothing under iso/build is committed to git — it is assembled here at
# build time so the repo never carries duplicated binaries.

set -e

HERE="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
REPO="$(CDPATH= cd -- "$HERE/.." && pwd)"
BUILD="${1:-$HERE/build}"

for f in "$REPO/install.sh" "$REPO/wired" "$REPO/scripts" "$REPO/mods" "$REPO/iso/installer" "$HERE/config"; do
  if [ ! -e "$f" ]; then
    echo "stage.sh: required path missing: $f" >&2
    exit 1
  fi
done

rm -rf "$BUILD"
mkdir -p "$BUILD"
cp -a "$HERE/config" "$BUILD/config"

# the payload the ISO (and later the installed system) needs.
# scripts/ holds the distro-level tools (navi-update, navi-wired-restore)
# and the agent/app installers — install.sh deploys them from $REPO_DIR,
# so leaving it out silently drops navi-update from fresh installs.
# mods/ holds the eiri panel mods (prebuilt Go binaries) — setup_mods
# deploys them from $REPO_DIR/mods, so leaving it out silently drops
# navi-networking, navi-calendar, and navi-audio from fresh installs.
mkdir -p "$BUILD/config/includes.chroot/opt/navi-iso"
cp -a "$REPO/install.sh" "$REPO/wired" "$REPO/scripts" "$REPO/mods" "$REPO/iso/installer" \
  "$BUILD/config/includes.chroot/opt/navi-iso/"
for m in navi-networking/navi-networking navi-calendar/navi-calendar navi-audio/navi-audio navi-weather/navi-weather; do
  # the payload must carry EXECUTABLE binaries. git doesn't always preserve
  # the +x bit (gh-push-mika stored these as 100644 once and red-lit an ISO
  # build), so enforce it here instead of trusting the checkout mode.
  [ -f "$BUILD/config/includes.chroot/opt/navi-iso/mods/$m" ] \
    || { echo "stage.sh: mod binary missing: mods/$m" >&2; exit 1; }
  chmod 0755 "$BUILD/config/includes.chroot/opt/navi-iso/mods/$m"
done

# NaviVim: pin the editor exactly like the ISO pins everything else. the
# pins live in install.sh next to the code that consumes them, so a
# version bump is one edit + restage.
NAVIVIM_PIN="$(sed -n 's/^NAVIVIM_PIN="\(.*\)"/\1/p' "$REPO/install.sh")"
NAVIVIM_NVIM_VERSION="$(sed -n 's/^NAVIVIM_NVIM_VERSION="\(.*\)"/\1/p' "$REPO/install.sh")"
[ -n "$NAVIVIM_PIN" ] || { echo "stage.sh: NAVIVIM_PIN not found in install.sh" >&2; exit 1; }
[ -n "$NAVIVIM_NVIM_VERSION" ] || { echo "stage.sh: NAVIVIM_NVIM_VERSION not found in install.sh" >&2; exit 1; }

NAVIVIM_STAGE="$(mktemp -d)"
trap 'rm -rf "$NAVIVIM_STAGE"' EXIT
git clone -q https://github.com/xvoidsx/navivim.git "$NAVIVIM_STAGE/navivim"
git -C "$NAVIVIM_STAGE/navivim" checkout -q "$NAVIVIM_PIN"
rm -rf "$NAVIVIM_STAGE/navivim/.git"
# the ISO is x86_64; the tarball is arch-qualified so install.sh only
# ever uses it on a matching machine (other arches download at install).
curl -fsSL -o "$NAVIVIM_STAGE/navivim/nvim-linux-x86_64.tar.gz" \
  "https://github.com/neovim/neovim/releases/download/${NAVIVIM_NVIM_VERSION}/nvim-linux-x86_64.tar.gz"
chmod +x "$NAVIVIM_STAGE/navivim/install.sh"
cp -a "$NAVIVIM_STAGE/navivim" "$BUILD/config/includes.chroot/opt/navi-iso/navivim"
[ -x "$BUILD/config/includes.chroot/opt/navi-iso/navivim/install.sh" ] \
  || { echo "stage.sh: navivim staging failed" >&2; exit 1; }
[ -f "$BUILD/config/includes.chroot/opt/navi-iso/navivim/nvim-linux-x86_64.tar.gz" ] \
  || { echo "stage.sh: neovim tarball staging failed" >&2; exit 1; }
echo "staged NaviVim $NAVIVIM_PIN + neovim $NAVIVIM_NVIM_VERSION (x86_64)"

# live-build only runs hooks when they are executable —
# make that true no matter how the repo was fetched.
chmod +x "$BUILD/config/hooks/normal/"*.hook.chroot

echo "staged live-build tree at $BUILD"
echo "next: cd $BUILD && lb config <flags> && lb build  (flags: see .github/workflows/iso.yml)"
