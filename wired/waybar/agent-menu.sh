#!/usr/bin/env bash
#
# agent-menu.sh — right-click menu for waybar's custom/agent module.
#
# A rofi dispatcher, state-aware: when a herdr agent is blocked waiting on
# the user, jumping straight to it leads the menu. Everything else is the
# small set of things you actually want from the little robot face.
#
#   open herdr    — attach to the herdr session (floating, via mod-open.sh)
#   Hey Lain!     — tap the voice toggle (same as Alt+V)
#   agent config  — navi-agents-config: backends, keys, connection tests

set -uo pipefail

WB="/usr/share/navi/wired/waybar"
HL_TOGGLE=""
for cand in /usr/share/navi/hey-lain/bin/toggle.sh "$HOME/hey-lain/bin/toggle.sh"; do
  [ -x "$cand" ] && HL_TOGGLE="$cand" && break
done

blocked_names() {
  command -v herdr >/dev/null 2>&1 || return 0
  herdr agent list 2>/dev/null | python3 -c "
import json, sys
try:
    d = json.load(sys.stdin)
except Exception:
    sys.exit(0)
if not isinstance(d, dict) or 'error' in d:
    sys.exit(0)
out = [a.get('agent') or 'agent' for a in d.get('result', {}).get('agents', [])
       if a.get('agent_status') == 'blocked']
print('\n'.join(out))
" 2>/dev/null
}

items=()
while IFS= read -r b; do
  [ -n "$b" ] && items+=("⚠ $b needs you — jump to herdr")
done < <(blocked_names)

items+=("open herdr")
[ -n "$HL_TOGGLE" ] && items+=("Hey Lain!")
items+=("agent config")

choice="$(printf '%s\n' "${items[@]}" | rofi -dmenu -p "agent" -i 2>/dev/null || true)"
[ -z "$choice" ] && exit 0

case "$choice" in
  "⚠ "*)
    exec "$WB/mod-open.sh" "herdr" herdr
    ;;
  "open herdr")
    exec "$WB/mod-open.sh" "herdr" herdr
    ;;
  "Hey Lain!")
    exec "$HL_TOGGLE"
    ;;
  "agent config")
    if command -v navi-agents-config >/dev/null 2>&1; then
      exec "$WB/mod-open.sh" "navi-agents-config" navi-agents-config
    else
      notify-send "agent mod" "navi-agents-config is not installed" -t 2000
    fi
    ;;
esac
