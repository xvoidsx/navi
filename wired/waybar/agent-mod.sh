#!/usr/bin/env bash
#
# waybar custom/agent module — the little robot face for navi's agents.
#
# Polls three signals and reports the most urgent one as waybar JSON:
#
#   agent-blocked    - a herdr agent is waiting on the user (permission/question)
#   agent-listening  - Hey Lain's mic is hot, or she is thinking/speaking
#   agent-working    - a herdr agent is actively working
#   agent-ready      - agents around but idle, or the ollama backend is up
#   agent-asleep     - nothing: no backend, no agents, herdr not running
#
# Scope note: herdr only sees agents in its own panes. An agent running in a
# bare terminal is invisible to this module — the tooltip says "in herdr"
# so the face never overclaims.
#
# Needs: herdr (optional), curl (for the ollama probe), python3 (JSON).

set -uo pipefail

TEXT="󰚩"   # nf-md-robot

json_escape() { python3 -c 'import json,sys; print(json.dumps(sys.stdin.read().strip()))' <<<"$1"; }

emit() { # <text> <tooltip> <class>
  printf '{"text": %s, "tooltip": %s, "class": %s}\n' \
    "$(json_escape "$1")" "$(json_escape "$2")" "$(json_escape "$3")"
}

# --- Hey Lain ---------------------------------------------------------------
HL_RUNTIME="${XDG_RUNTIME_DIR:-/tmp}/hey-lain"

heylain_listening() {
  local pid
  [ -f "$HL_RUNTIME/rec.pid" ] || return 1
  pid="$(cat "$HL_RUNTIME/rec.pid" 2>/dev/null || true)"
  [ -n "${pid:-}" ] && [ -f "/proc/$pid/cmdline" ] \
    && tr '\0' ' ' < "/proc/$pid/cmdline" 2>/dev/null | grep -qE "pw-record|arecord"
}

heylain_processing() {
  local lock="$HL_RUNTIME/processing.lock"
  [ -f "$lock" ] && ! flock -n "$lock" true 2>/dev/null
}

# --- ollama backend ----------------------------------------------------------
ollama_up() {
  command -v curl >/dev/null 2>&1 \
    && curl -s --max-time 1 http://localhost:11434/api/tags -o /dev/null 2>/dev/null
}

# --- herdr -------------------------------------------------------------------
# Prints "agent status" lines, or nothing when herdr is absent/down.
herdr_agents() {
  command -v herdr >/dev/null 2>&1 || return 0
  herdr agent list 2>/dev/null | python3 -c "
import json, sys
try:
    d = json.load(sys.stdin)
except Exception:
    sys.exit(0)
if not isinstance(d, dict) or 'error' in d:
    sys.exit(0)
for a in d.get('result', {}).get('agents', []):
    print((a.get('agent') or '?'), (a.get('agent_status') or 'unknown'))
" 2>/dev/null
}

# --- decide -------------------------------------------------------------------
agents="$(herdr_agents)"
n=0; working=0; blocked=0; names=""
if [ -n "$agents" ]; then
  n="$(printf '%s\n' "$agents" | wc -l)"
  working="$(printf '%s\n' "$agents" | awk '$2=="working"' | wc -l)"
  blocked="$(printf '%s\n' "$agents" | awk '$2=="blocked"' | wc -l)"
  names="$(printf '%s\n' "$agents" | awk '{printf "%s%s (%s)", (NR>1?", ":""), $1, $2}')"
fi

if [ "$blocked" -gt 0 ]; then
  emit "$TEXT" "agent mod: $blocked of $n in herdr need$([ "$blocked" -eq 1 ] && echo "s" || echo "") you — $names" "agent-blocked"
  exit 0
fi

if heylain_listening; then
  emit "$TEXT" "agent mod: Hey Lain is listening — tap Alt+V again to send" "agent-listening"
  exit 0
fi

if heylain_processing; then
  emit "$TEXT" "agent mod: Hey Lain is thinking…" "agent-listening"
  exit 0
fi

if [ "$working" -gt 0 ]; then
  emit "$TEXT" "agent mod: $working of $n in herdr working — $names" "agent-working"
  exit 0
fi

if [ "$n" -gt 0 ]; then
  emit "$TEXT" "agent mod: $n in herdr, all idle — $names" "agent-ready"
  exit 0
fi

if ollama_up; then
  emit "$TEXT" "agent mod: backend ready — no agents active" "agent-ready"
  exit 0
fi

emit "$TEXT" "agent mod: no agents, no backend" "agent-asleep"
