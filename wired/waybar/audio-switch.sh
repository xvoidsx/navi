#!/usr/bin/env bash
# audio-switch.sh — rofi output-device picker for waybar's custom/volume
# module (middle-click).
#
# Lists sinks with the current one marked ●, then runs the shared one-key
# switch path: navi-audio --switch-sink "<name>" (set-default-sink plus
# move every playback stream — the same helper the TUI's Enter key uses,
# so the two can never drift apart).
set -uo pipefail

command -v rofi >/dev/null 2>&1 || exit 0
command -v pactl >/dev/null 2>&1 || exit 0
NAVI_AUDIO=/usr/bin/navi-audio
[ -x "$NAVI_AUDIO" ] || exit 0

# read_sinks <labels|names>: one entry per line, same order both ways.
read_sinks() {
pactl -f json list sinks 2>/dev/null | python3 -c "
import json, subprocess, sys
mode = '$1'
try:
    sinks = json.load(sys.stdin)
except Exception:
    sys.exit(0)
try:
    default = subprocess.run(['pactl', 'get-default-sink'],
                             capture_output=True, text=True,
                             timeout=5).stdout.strip()
except Exception:
    default = ''
for s in sinks:
    name = s.get('name', '')
    if not name:
        continue
    if mode == 'labels':
        desc = s.get('description') or name
        mark = '●' if name == default else '○'
        sys.stdout.write(mark + ' ' + desc + chr(10))
    else:
        sys.stdout.write(name + chr(10))
" 2>/dev/null
}

mapfile -t labels < <(read_sinks labels)
mapfile -t names < <(read_sinks names)
[ "${#labels[@]}" -eq 0 ] && exit 0

sel="$(printf '%s\n' "${labels[@]}" | rofi -dmenu -p "output" -i 2>/dev/null || true)"
[ -z "$sel" ] && exit 0

for i in "${!labels[@]}"; do
	if [ "${labels[$i]}" = "$sel" ]; then
		"$NAVI_AUDIO" --switch-sink "${names[$i]}"
		exit $?
	fi
done
exit 0
