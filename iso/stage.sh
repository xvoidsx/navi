#!/bin/sh
# iso/stage.sh — stage the navi repo into a live-build tree.
#
# Copies the live-build configuration (iso/auto, iso/config) plus the navi
# payload the installer needs (install.sh, wired/, iso/installer) into a
# build directory, ready for `lb config && lb build`.
#
#   ./iso/stage.sh [build-dir]   # default: iso/build
#   cd iso/build && lb config && lb build   # needs root
#
# Nothing under iso/build is committed to git — it is assembled here at
# build time so the repo never carries duplicated binaries.

set -e

HERE="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
REPO="$(CDPATH= cd -- "$HERE/.." && pwd)"
BUILD="${1:-$HERE/build}"

for f in "$REPO/install.sh" "$REPO/wired" "$REPO/iso/installer" "$HERE/auto" "$HERE/config"; do
  if [ ! -e "$f" ]; then
    echo "stage.sh: required path missing: $f" >&2
    exit 1
  fi
done

rm -rf "$BUILD"
mkdir -p "$BUILD"
cp -a "$HERE/auto" "$BUILD/auto"
cp -a "$HERE/config" "$BUILD/config"

# the payload the ISO (and later the installed system) needs
mkdir -p "$BUILD/config/includes.chroot/opt/navi-iso"
cp -a "$REPO/install.sh" "$REPO/wired" "$REPO/iso/installer" \
  "$BUILD/config/includes.chroot/opt/navi-iso/"

# live-build only runs hooks / auto/config when they are executable —
# make that true no matter how the repo was fetched.
chmod +x "$BUILD/auto/config" "$BUILD/config/hooks/normal/"*.hook.chroot

echo "staged live-build tree at $BUILD"
echo "next: cd $BUILD && lb config && lb build"
