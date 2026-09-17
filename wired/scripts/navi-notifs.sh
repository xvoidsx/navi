#!/usr/bin/env bash
#
# navi-notifs — a small terminal viewer for your dunst notifications.
#
# Lists recent notification history; lets you invoke an action, dismiss
# one, or clear them all. Open it from the waybar bell mod, or run it
# directly. q quits.
#
# v1 is deliberately plain bash. The eiri-era navi mods will replace it
# with a Go/bubbletea TUI.

set -uo pipefail

# neon palette — nightshadeNeon, terminal edition
N_PINK=$'\033[38;5;201m'
N_GREEN=$'\033[92m'
N_OFF=$'\033[0m'

HIST_JSON=""

fetch_history() {
  HIST_JSON="$(dunstctl history 2>/dev/null || echo '')"
}

list_notifs() {
  python3 -c '
import json, sys, textwrap
try:
    d = json.loads(sys.argv[1])
except Exception:
    print("  (could not read dunst history)")
    sys.exit()
data = d.get("data", [])
hist = data[0] if data else []
if not hist:
    print("  all quiet — no notifications in history.")
    sys.exit()
for n, h in enumerate(hist):
    app = h.get("appname", "?") or "?"
    summary = (h.get("summary", "") or "").strip().replace("\n", " ")
    body = (h.get("body", "") or "").strip().replace("\n", " ")
    actions = h.get("actions", {}) or {}
    acts = (", actions: " + ", ".join(actions.values())) if actions else ""
    # NOTE: no backslashes inside f-string expressions — python < 3.12
    # chokes on them (SyntaxError: unexpected character after line
    # continuation character). build the line with concatenation instead.
    line = "  [" + str(n) + "] " + app + ": " + summary
    if body:
        line = line + " — " + textwrap.shorten(body, width=72, placeholder="…")
    print(line + acts)
' "$HIST_JSON"
}

entry_id() { # <index> -> dunst id (or empty)
  python3 -c '
import json, sys
try:
    d = json.loads(sys.argv[1])
    hist = (d.get("data", []) or [[]])[0]
    print(hist[int(sys.argv[2])].get("id", ""))
except Exception:
    print("")
' "$HIST_JSON" "$1"
}

entry_actions() { # <index> -> "key:label ..." or empty
  python3 -c '
import json, sys
try:
    d = json.loads(sys.argv[1])
    hist = (d.get("data", []) or [[]])[0]
    acts = hist[int(sys.argv[2])].get("actions", {}) or {}
    print(" ".join(f"{k}:{v}" for k, v in acts.items()))
except Exception:
    print("")
' "$HIST_JSON" "$1"
}

do_action() { # <index> — invoke the default (first) action
  local id acts key
  id="$(entry_id "$1")"
  [ -n "$id" ] || { echo "  (no such notification)"; return; }
  acts="$(entry_actions "$1")"
  if [ -z "$acts" ]; then
    echo "  (that notification has no actions)"
    return
  fi
  key="${acts%%:*}"
  dunstctl action "$id" "$key" 2>/dev/null \
    && echo "  action invoked" \
    || echo "  (action failed — the notification may be gone)"
}

do_dismiss() { # <index>
  local id
  id="$(entry_id "$1")"
  [ -n "$id" ] || { echo "  (no such notification)"; return; }
  dunstctl history-rm "$id" 2>/dev/null \
    && echo "  dismissed" \
    || echo "  (dismiss failed)"
}

main() {
  command -v dunstctl >/dev/null 2>&1 \
    || { echo "dunstctl not found — is dunst running?"; exit 1; }

  while true; do
    fetch_history
    echo
    printf '  ── %snotifications%s ─────────────────────────────\n' "$N_PINK" "$N_OFF"
    list_notifs
    echo "  ─────────────────────────────────────────────"
    printf '  %s<number> open action · d<number> dismiss · c clear all · q quit%s\n' "$N_GREEN" "$N_OFF"
    read -r -p "  > " cmd
    case "$cmd" in
      q|Q) break ;;
      c|C)
        dunstctl history-clear 2>/dev/null && echo "  cleared" ;;
      d*)
        idx="${cmd#d}"
        [[ "$idx" =~ ^[0-9]+$ ]] && do_dismiss "$idx" || echo "  (usage: d3)" ;;
      *)
        if [[ "$cmd" =~ ^[0-9]+$ ]]; then do_action "$cmd"
        elif [ -n "$cmd" ]; then echo "  (?) try q to quit"; fi ;;
    esac
  done
  echo "  done."
}

main "$@"
