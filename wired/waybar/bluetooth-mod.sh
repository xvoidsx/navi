#!/usr/bin/env bash
#
# waybar custom/bluetooth — the bar's Bluetooth indicator.
#
# Why a script and not waybar's built-in `bluetooth` module: the built-in
# renders nothing at all on waybar 0.12.0 on this box. It instantiates
# (the log shows label#bluetooth.module) but emits an empty label whether
# the format is a literal string, {status}, or {icon}, and with or without
# controller-alias. That is why the module was removed on 2026-10-02 with
# the note "on-click never worked" — there was nothing on screen to click.
# Do not switch back to it without verifying that it renders.
#
# The glyph is ᛒ (U+16D2, RUNIC LETTER BERKANAN BEORC BJARKAN B) — the bind-rune
# the Bluetooth logo is literally derived from, so it is the real mark and it
# fits navi's rune theme. Only Noto Sans Runic carries it; fontconfig falls
# back to it automatically, so there is no tofu. Followed by a state glyph:
#   󰀍 radio off · 󰁨 radio on, nothing paired · 󰃧 something connected
# Never a dot: a filled circle belongs to custom/bell.
#
# Colours come from style.css (#custom-bluetooth.off / .connected), the
# same rules #bluetooth always had. This module emits "off" and
# "connected"; the pre-existing ".connecting" rule has no emitter and is
# harmless.
#
# Click opens navi-bluetooth (floating, via mod-open.sh). Right-click is the
# power menu (bluetooth-menu.sh).

set -uo pipefail

json_escape() { python3 -c 'import json,sys; print(json.dumps(sys.stdin.read().strip()))' <<<"$1"; }

emit() { # <text> <tooltip> <class>
  printf '{"text": %s, "tooltip": %s, "class": %s}\n' \
    "$(json_escape "$1")" "$(json_escape "$2")" "$(json_escape "$3")"
}

# One bluetoothctl call for controller power, one for connected devices.
# Real bluetoothctl output is parsed defensively: a blank or non-numeric
# field must never render as a count.
if ! command -v bluetoothctl >/dev/null 2>&1; then
  emit "ᛒ" "bluetoothctl not installed" "off"
  exit 0
fi

show="$(bluetoothctl show 2>/dev/null)"
powered="$(awk -F': *' '/^[[:space:]]*Powered:/ {print tolower($2); exit}' <<<"$show")"
alias="$(awk -F': *' '/^[[:space:]]*Alias:/ {print $2; exit}' <<<"$show")"

connected="$(bluetoothctl devices Connected 2>/dev/null | grep -c '^Device ' || true)"
case "$connected" in ''|*[!0-9]*) connected=0 ;; esac

# bluetoothctl missing/empty output lands here too — treat as "unknown",
# which is the safe thing to show: radio icon, default colour.
if [ -z "$powered" ]; then
  emit "ᛒ" "bluetooth: adapter state unknown" ""
  exit 0
fi

if [ "$powered" != "yes" ]; then
  emit "󰊓 󰀍" "bluetooth is off — click to open, right-click to turn on" "off"
  exit 0
fi

if [ "$connected" -gt 0 ]; then
  emit "ᛒ" "bluetooth on — ${connected} device(s) connected" "connected"
else
  emit "ᛒ" "bluetooth on — nothing connected${alias:+ ($alias)}" ""
fi