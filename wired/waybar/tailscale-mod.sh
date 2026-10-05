#!/usr/bin/env bash
#
# waybar custom/tailscale — the bar's Tailscale indicator.
#
# Shows whether this machine is on its tailnet. Hidden entirely when the
# tailscale binary isn't installed (a custom module with empty output
# doesn't render).
#
# States:
#   connected — BackendState == "Running", tailnet is up
#   off       — installed but down / not logged in
#
# Click opens navi-tailscale (floating, via mod-open.sh).
# Right-click toggles `tailscale up` / `tailscale down`.

set -uo pipefail

json_escape() { python3 -c 'import json,sys; print(json.dumps(sys.stdin.read().strip()))' <<<"$1"; }

emit() { # <text> <tooltip> <class>
  printf '{"text": %s, "tooltip": %s, "class": %s}\n' \
    "$(json_escape "$1")" "$(json_escape "$2")" "$(json_escape "$3")"
}

# Not installed -> emit nothing, waybar hides the module.
if ! command -v tailscale >/dev/null 2>&1; then
  exit 0
fi

status="$(tailscale status --json 2>/dev/null)"
if [ -z "$status" ]; then
  emit "󰖂" "tailscale: daemon not running — click to open" "off"
  exit 0
fi

backend="$(python3 -c 'import json,sys; print(json.load(sys.stdin).get("BackendState",""))' <<<"$status" 2>/dev/null)"
tailnet="$(python3 -c 'import json,sys; d=json.load(sys.stdin).get("CurrentTailnet") or {}; print(d.get("Name",""))' <<<"$status" 2>/dev/null)"

if [ "$backend" = "Running" ]; then
  if [ -n "$tailnet" ]; then
    emit "󰖂" "tailscale connected — $tailnet (click to manage)" "connected"
  else
    emit "󰖂" "tailscale connected (click to manage)" "connected"
  fi
else
  emit "󰖂" "tailscale down — click to open" "off"
fi
