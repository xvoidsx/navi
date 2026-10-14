#!/usr/bin/env bash
# navi-switcher.sh — Alt+Tab window switcher launcher (single-flight).
#
# First invocation captures output screenshots, crops per-window thumbnails,
# and opens the GTK grid. Alt+Tab pressed again while it is open just
# advances the selection (the grid's unix socket gets a "next").
set -u
HERE="$(cd "$(dirname "$0")" && pwd)"
PY="$HERE/navi-switcher.py"
RUNDIR="${XDG_RUNTIME_DIR:-/tmp}"
[ -d "$RUNDIR" ] || RUNDIR="/tmp"
SOCK="$RUNDIR/navi-switcher.sock"
DIRECTION="next"
[ "${1:-}" = "--prev" ] && DIRECTION="prev"

# Fast path: switcher already open -> move one tile in the given direction.
if [ -S "$SOCK" ]; then
  python3 "$PY" --next "$DIRECTION" && exit 0 || true
fi

# Serialize launches while a capture is in flight (Alt+Tab mashed quickly).
exec 9>"$SOCK.lock"
if ! flock -n 9; then
  for _ in $(seq 1 20); do
    if [ -S "$SOCK" ]; then python3 "$PY" --next "$DIRECTION"; exit 0; fi
    sleep 0.05
  done
  exit 0
fi

for cmd in swaymsg grim python3; do
  command -v "$cmd" >/dev/null 2>&1 \
    || { echo "navi-switcher: missing $cmd" >&2; exit 1; }
done
python3 -c "import PIL" 2>/dev/null \
  || { echo "navi-switcher: python3-pil is not installed" >&2; exit 1; }
python3 -c "import gi" 2>/dev/null \
  || { echo "navi-switcher: python3-gi is not installed" >&2; exit 1; }

DIR="$(mktemp -d /tmp/navi-switcher-XXXXXX)"
trap 'rm -rf "$DIR"' EXIT
python3 "$PY" --capture "$DIR" || exit 1
exec python3 "$PY" --show "$DIR"
