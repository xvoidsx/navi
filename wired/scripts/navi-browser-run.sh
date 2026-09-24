#!/usr/bin/env bash
# navi-browser-run — launch the user's chosen Chromium browser.
#
# Webapp .desktop files Exec this instead of hardcoding `chromium`, so the
# choice made in navi-browser (stored in ~/.config/navi/default-browser)
# applies to the whole app runtime at launch time: pick Brave and every
# webapp runs under brave-browser --app.
#
# Resolution lives in `navi-browser --print-binary` (configured default,
# then chromium, then the first installed Chromium-family browser). If the
# picker is missing or nothing is installed, fall back to bare `chromium`
# and let exec report the real error.
set -u

BIN="$(navi-browser --print-binary 2>/dev/null)"
rc=$?
if [[ $rc -ne 0 || -z "$BIN" ]]; then
  BIN="chromium"
fi
exec "$BIN" "$@"
