#!/usr/bin/env bash
#
# waybar custom/bell module — a little bell for your notifications.
#
# Shows ● with a count when dunst has unseen history, ○ when quiet.
# Click opens navi-notifs, a small terminal viewer over dunstctl history
# (view, invoke actions, dismiss, clear).
#
# Note: dunst keeps history in memory, so this is recent notifications,
# not a full log — it clears when dunst restarts. The eiri-era navi mods
# will replace this with a Go/bubbletea TUI.

set -uo pipefail

json_escape() { python3 -c 'import json,sys; print(json.dumps(sys.stdin.read().strip()))' <<<"$1"; }

emit() { # <text> <tooltip> <class>
  printf '{"text": %s, "tooltip": %s, "class": %s}\n' \
    "$(json_escape "$1")" "$(json_escape "$2")" "$(json_escape "$3")"
}

count=0
if command -v dunstctl >/dev/null 2>&1; then
  count="$(dunstctl history 2>/dev/null | python3 -c '
import json, sys
try:
    d = json.load(sys.stdin)
    data = d.get("data", [])
    hist = data[0] if data else []
    print(len(hist))
except Exception:
    print(0)
' 2>/dev/null || echo 0)"
fi
# guard against non-numeric output
case "$count" in ''|*[!0-9]*) count=0 ;; esac

if [ "$count" -eq 0 ]; then
  emit "○" "no notifications" "no-notifs"
else
  emit "● $count" "$count notification(s) — click to view" "has-notifs"
fi
