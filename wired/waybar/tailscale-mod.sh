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

info="$(python3 -c '
import json, sys
d = json.load(sys.stdin)
backend = d.get("BackendState", "")
tailnet = (d.get("CurrentTailnet") or {}).get("Name", "")
self = d.get("Self") or {}
ips = self.get("TailscaleIPs", [])
ip = ips[0] if ips else ""
peers = d.get("Peer", {})
online = sum(1 for p in peers.values() if p and p.get("Online"))
print(f"{backend}|{tailnet}|{ip}|{len(peers)}|{online}")
' <<<"$status" 2>/dev/null)"

backend="${info%%|*}"; rest="${info#*|}"
tailnet="${rest%%|*}"; rest="${rest#*|}"
ip="${rest%%|*}"; rest="${rest#*|}"
total="${rest%%|*}"; online="${rest##*|}"

if [ "$backend" = "Running" ]; then
  tip="tailscale connected"
  [ -n "$tailnet" ] && tip="$tip — $tailnet"
  [ -n "$ip" ] && tip="$tip"$'\n'"this device: $ip"
  tip="$tip"$'\n'"peers: $online/$total online"
  tip="$tip"$'\n'"click to manage"
  emit "󰖂" "$tip" "connected"
else
  emit "󰖂" "tailscale down — click to open" "off"
fi
