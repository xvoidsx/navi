#!/usr/bin/env bash
# fetch-weather.sh <location words...> — prints speakable forecast: current
# conditions plus a 3-day outlook. Exit nonzero on failure (caller falls
# back to opening a browser search).
# Primary: open-meteo (structured JSON, free, no key). Fallback: wttr.in
# one-liner with STRICT validation (wttr returns chatty error pages like
# "location not found: upstream error: ..." that must never be spoken).
set -uo pipefail

LOC="${*:-}"
[ -z "${LOC// }" ] && exit 2
echo "fetch-weather: query=[$LOC]" >&2
Q=$(echo "$LOC" | sed -E 's/^ +//; s/ +$//; s/ +/+/g')

# --- primary: open-meteo ---
if OUT=$(python3 - "$LOC" <<'EOF' 2>/dev/null
import sys, json, urllib.request, urllib.parse
from datetime import date
loc = sys.argv[1]
def get(url):
    req = urllib.request.Request(url, headers={"User-Agent": "hey-lain"})
    return json.load(urllib.request.urlopen(req, timeout=10))
geo = get("https://geocoding-api.open-meteo.com/v1/search?name="
          + urllib.parse.quote(loc) + "&count=1&language=en&format=json")
res = (geo.get("results") or [None])[0]
if not res:
    sys.exit(2)
lat, lon = res["latitude"], res["longitude"]
name = res.get("name", loc)
place = name
if res.get("admin1") and res["admin1"] != name:
    place += ", " + res["admin1"]
fc = get("https://api.open-meteo.com/v1/forecast?latitude=%s&longitude=%s"
         "&current=temperature_2m,relative_humidity_2m,apparent_temperature,"
         "weather_code,wind_speed_10m&daily=temperature_2m_max,"
         "temperature_2m_min,weather_code&temperature_unit=fahrenheit"
         "&wind_speed_unit=mph&timezone=auto&forecast_days=4" % (lat, lon))
cur, day = fc["current"], fc["daily"]
def desc(code):
    if code == 0: return "clear skies"
    if code == 1: return "mostly clear skies"
    if code == 2: return "partly cloudy skies"
    if code == 3: return "overcast skies"
    if code in (45, 48): return "fog"
    if code in (51, 53, 55, 56, 57): return "drizzle"
    if code in (61, 63, 65, 66, 67, 80, 81, 82): return "rain"
    if code in (71, 73, 75, 77, 85, 86): return "snow"
    if code in (95, 96, 99): return "thunderstorms"
    return "changing skies"
t = round(cur["temperature_2m"]); f = round(cur["apparent_temperature"])
w = round(cur["wind_speed_10m"]); h = round(cur["relative_humidity_2m"])
parts = [f"In {place}: {desc(cur.get('weather_code', -1))}, {t} degrees, "
         f"feels like {f} degrees, wind {w} miles per hour, "
         f"humidity {h} percent."]
outlook = []
for i in (1, 2, 3):
    try:
        dname = date.fromisoformat(day["time"][i]).strftime("%A")
        hi = round(day["temperature_2m_max"][i])
        lo = round(day["temperature_2m_min"][i])
        outlook.append(f"{dname} high {hi} low {lo}")
    except (IndexError, ValueError, KeyError):
        pass
if outlook:
    parts.append("Coming up: " + ", ".join(outlook) + ".")
print(" ".join(parts))
EOF
); then
  echo "$OUT"
  exit 0
fi
echo "fetch-weather: open-meteo failed, trying wttr.in" >&2

# --- fallback: wttr.in one-liner, strictly validated ---
RAW=$(curl -s --max-time 12 "https://wttr.in/${Q}?format=%l:+%C,+temperature+%t,+feels+like+%f,+wind+%w,+humidity+%h." 2>/dev/null || true)
if [ -z "$RAW" ] || [[ "$RAW" =~ [Nn]ot\ found ]] || [[ "$RAW" =~ [Uu]nknown\ location ]] \
   || [[ "$RAW" =~ [Ee]rror ]] || ! [[ "$RAW" =~ [0-9]+°[FC] ]]; then
  echo "fetch-weather: wttr.in rejected: [${RAW:0:120}]" >&2
  exit 1
fi
echo "$RAW" | sed -E -e 's/°F/ degrees/g; s/°C/ degrees/g' \
  -e 's/[↓↑→←↔↗↘↙↖]/ /g; s/\+/ /g; s/%/ percent/g' \
  -e 's/mph/ miles per hour/g; s/ ,/,/g; s/  +/ /g; s/^ //; s/ $//'
