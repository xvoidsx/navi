#!/usr/bin/env bash
# waybar-json.sh — polybar adapter for waybar JSON module scripts.
#
# Runs a waybar custom-module script (which emits {"text": ..., ...} JSON)
# and prints just its "text" field as plain text for polybar's custom/script
# modules. The module logic stays in one place (wired/waybar/*.sh); this is
# only the translation layer so both bars stay in parity.
set -uo pipefail
out="$("$@" 2>/dev/null)" || exit 0
[ -n "$out" ] || exit 0
python3 -c 'import json,sys
try:
    print(json.loads(sys.stdin.read()).get("text", ""))
except Exception:
    pass' <<<"$out"
