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
N_DIM=$'\033[2m'
N_BOLD=$'\033[1m'
N_OFF=$'\033[0m'

HIST_JSON=""

fetch_history() {
  HIST_JSON="$(dunstctl history 2>/dev/null || echo '')"
}

list_notifs() {
  python3 -c '
import json, sys, textwrap
PINK = "\033[38;5;201m"
GREEN = "\033[92m"
DIM = "\033[2m"
BOLD = "\033[1m"
OFF = "\033[0m"
def raw(d, k, default=None):
    # dunstctl prints busctl JSON: every D-Bus variant arrives wrapped as
    # {"type": "<sig>", "data": <value>}. unwrap that; leave anything else
    # (including a plain value, if dunst ever changes shape) alone.
    x = d.get(k, default)
    if isinstance(x, dict) and "data" in x and set(x.keys()) <= {"type", "data"}:
        return x["data"]
    return x
def s(d, k):
    x = raw(d, k, "")
    return x if isinstance(x, str) else ("" if x is None else str(x))
try:
    d = json.loads(sys.argv[1])
except Exception:
    print("  (could not read dunst history)")
    sys.exit()
data = raw(d, "data", []) or []
hist = data[0] if data else []
if not hist:
    print("  all quiet — no notifications in history.")
    sys.exit()
print("  " + DIM + str(len(hist)) + " in history" + OFF)
print("")
shown = 0
for n, h in enumerate(hist):
    if not isinstance(h, dict):
        continue
    if shown:
        print("  " + DIM + ("-" * 46) + OFF)
        print("")
    shown += 1
    app = s(h, "appname") or "?"
    summary = " ".join(s(h, "summary").split())
    body = " ".join(s(h, "body").split())
    actions = raw(h, "actions", {}) or {}
    if not isinstance(actions, dict):
        actions = {}
    # NOTE: no backslashes inside f-string expressions — python < 3.12
    # chokes on them (SyntaxError: unexpected character after line
    # continuation character). concatenation only, below.
    print("  " + PINK + "[" + str(n) + "]" + OFF + "  " + DIM + app + OFF)
    print("")
    if summary:
        print("  " + BOLD + summary + OFF)
        print("")
    if body:
        for wline in textwrap.wrap(body, width=56):
            print("  " + DIM + wline + OFF)
        print("")
    if actions:
        labels = ", ".join(str(v) for v in actions.values())
        print("  " + GREEN + "> " + labels + OFF)
        print("")
' "$HIST_JSON"
}

entry_id() { # <index> -> dunst id (or empty)
  python3 -c '
import json, sys
def raw(d, k, default=None):
    x = d.get(k, default)
    if isinstance(x, dict) and "data" in x and set(x.keys()) <= {"type", "data"}:
        return x["data"]
    return x
try:
    d = json.loads(sys.argv[1])
    data = raw(d, "data", []) or []
    hist = data[0] if data else []
    print(raw(hist[int(sys.argv[2])], "id", ""))
except Exception:
    print("")
' "$HIST_JSON" "$1"
}

entry_actions() { # <index> -> "key:label ..." or empty
  python3 -c '
import json, sys
def raw(d, k, default=None):
    x = d.get(k, default)
    if isinstance(x, dict) and "data" in x and set(x.keys()) <= {"type", "data"}:
        return x["data"]
    return x
try:
    d = json.loads(sys.argv[1])
    data = raw(d, "data", []) or []
    hist = data[0] if data else []
    acts = raw(hist[int(sys.argv[2])], "actions", {}) or {}
    if not isinstance(acts, dict):
        acts = {}
    # NOTE: no backslashes inside f-string expressions — build with
    # concatenation instead (python < 3.12 SyntaxError otherwise).
    print(" ".join(str(k) + ":" + str(v) for k, v in acts.items()))
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
    printf '  %s∅ notifications%s\n' "$N_PINK" "$N_OFF"
    list_notifs
    printf '  %s<number> action · d<number> dismiss · c clear all · q quit%s\n' "$N_DIM" "$N_OFF"
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
