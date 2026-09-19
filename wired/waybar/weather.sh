#!/usr/bin/env bash
# waybar custom/weather module — one emoji, one temperature.
#
# wttr.in backend (no API key). Location and units come from
# ~/.config/navi-weather/config.json, shared with the navi-weather TUI so
# the bar and the TUI always agree ("" location = IP geolocation).
# Results are cached for an hour so the bar stays cheap; a stale cache is
# served when offline, and the module degrades to "?" with no cache.
#
# Emits waybar JSON: {"text": "…", "tooltip": "…", "class": "…"}.

set -uo pipefail

CACHE_DIR="$HOME/.cache/navi-weather"
CONFIG="$HOME/.config/navi-weather/config.json"
CACHE_TTL=3600 # 1 hour

json_escape() { python3 -c 'import json,sys; print(json.dumps(sys.stdin.read().strip()))' <<<"$1"; }

emit() { # <text> <tooltip> <class>
  printf '{"text": %s, "tooltip": %s, "class": %s}\n' \
    "$(json_escape "$1")" "$(json_escape "$2")" "$(json_escape "$3")"
}

# read location + units out of the shared config (defaults: auto, imperial)
read_config() {
  python3 -c '
import json, os
p = os.path.expanduser("~/.config/navi-weather/config.json")
loc = ""
units = "imperial"
try:
    d = json.load(open(p))
    loc = str(d.get("default_location") or "")
    if d.get("units") == "metric":
        units = "metric"
except Exception:
    pass
print(loc)
print(units)
'
}

build_url() { # <location> <units>
  python3 -c '
import sys, urllib.parse
loc = sys.argv[1]
units = sys.argv[2]
path = urllib.parse.quote(loc) if loc else ""
flag = "m" if units == "metric" else "u"
print("https://wttr.in/" + path + "?format=%c|%t|%l|%C&" + flag)
' "$1" "$2"
}

mkdir -p "$CACHE_DIR"

mapfile -t cfg < <(read_config)
LOC="${cfg[0]:-}"
UNITS="${cfg[1]:-imperial}"

CACHE_KEY="$(printf '%s|%s' "$LOC" "$UNITS" | md5sum | cut -d' ' -f1)"
CACHE="$CACHE_DIR/bar-$CACHE_KEY.cache"

now="$(date +%s)"
serve() { # <cache-file> <stale: 0|1>
  local line emoji temp locname cond tooltip
  line="$(cat "$1")"
  emoji="${line%%|*}"; line="${line#*|}"
  temp="${line%%|*}";  line="${line#*|}"
  locname="${line%%|*}"; cond="${line#*|}"
  tooltip="$locname: $temp, $cond"
  if [ "$2" -eq 1 ]; then
    tooltip="$tooltip (cached)"
    emit "$emoji $temp" "$tooltip" "weather-stale"
  else
    emit "$emoji $temp" "$tooltip" "weather"
  fi
}

if [ -f "$CACHE" ] && [ "$(( now - $(stat -c %Y "$CACHE" 2>/dev/null || echo 0) ))" -lt "$CACHE_TTL" ]; then
  serve "$CACHE" 0
  exit 0
fi

if fresh="$(curl -s --max-time 10 -A "navi-weather/1.0 (navi linux)" "$(build_url "$LOC" "$UNITS")" 2>/dev/null)" \
   && [ -n "$fresh" ] && [[ "$fresh" == *"|"* ]]; then
  fresh="$(printf '%s' "$fresh" | tr -d '\n')"
  # wttr.in prefixes positive temps with "+"; the bar reads cleaner without it
  fresh="$(printf '%s' "$fresh" | sed 's/|+/|/g')"
  printf '%s' "$fresh" > "$CACHE"
  serve "$CACHE" 0
  exit 0
fi

# offline: fall back to a stale cache rather than breaking the bar
if [ -f "$CACHE" ]; then
  serve "$CACHE" 1
  exit 0
fi

emit "☁ ?" "weather unavailable — offline?" "weather-offline"
