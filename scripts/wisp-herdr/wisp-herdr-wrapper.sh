#!/usr/bin/env bash
# navi-wisp-herdr-wrapper — launch wisp with herdr self-reporting
# If running inside a herdr pane (HERDR_ENV=1), reports wisp to herdr
# so it appears in `herdr agent list` and navi's agent center.
# Otherwise, just launches wisp normally.
set -u

report() {
  local state="$1"
  [ "${HERDR_ENV:-}" = "1" ] || return 0
  local pane_id="${HERDR_PANE_ID:-}"
  [ -n "$pane_id" ] || return 0
  command -v herdr >/dev/null 2>&1 || return 0
  if [ "$state" = "release" ]; then
    herdr pane release-agent "$pane_id" \
      --source "navi:wisp-herdr" --agent "wisp" >/dev/null 2>&1 || true
  else
    herdr pane report-agent "$pane_id" \
      --source "navi:wisp-herdr" --agent "wisp" --state "$state" >/dev/null 2>&1 || true
  fi
}

# Report idle on startup (wisp is running but waiting for input)
report "idle"

# Ensure we release on exit
trap 'report "release"' EXIT INT TERM

# Find and exec wisp
if [ -x "$HOME/.wisp/bin/wisp" ]; then
  exec "$HOME/.wisp/bin/wisp" "$@"
elif command -v wisp >/dev/null 2>&1; then
  exec wisp "$@"
else
  echo "navi-wisp-herdr-wrapper: wisp not found" >&2
  exit 1
fi
