#!/usr/bin/env bash
# Brain: local Ollama chat (same models opencode uses) — fast, offline, no auth.
# `opencode run` hangs/errors headless here (no default model/auth), and a cold
# opencode per utterance is too slow for a hotkey loop anyway.
# Upgrade path: point BRAIN_URL at `opencode serve` + session id later.
# Env overrides: BRAIN_MODEL (default gemma3:270m), BRAIN_URL, BRAIN_TIMEOUT.
set -uo pipefail
HERE="$(cd "$(dirname "$0")/.." && pwd)"
PROMPT_FILE="$HERE/SYSTEM_PROMPT.txt"

SYSTEM="You are Lain, a concise voice assistant on Navi Linux (Debian 13, sway-based wiered WM). Reply in 1-3 short spoken sentences, no markdown, no code blocks, plain speech. If the user asks to do a desktop action, start your reply with [ACTION: name args] on its own line, then the spoken reply. Supported actions: notify <msg> | open <app|url> | volume <up|down|mute> | lock | screenshot."

[ -f "$PROMPT_FILE" ] && SYSTEM="$(cat "$PROMPT_FILE")"
USER_TEXT="${*:-$(cat)}"
if [ -z "${USER_TEXT// }" ]; then
  echo "I didn't catch that. Could you say it again?"
  exit 0
fi

# Deterministic actions stay reliable, but their spoken acknowledgement can
# still feel alive. The utterance keeps the choices relevant while a small
# time component prevents identical requests from sounding mechanically fixed.
variant() {
  local kind="$1" a n
  shift
  local -a choices=("$@")
  n=$(printf '%s|%s|%s' "$USER_TEXT" "$kind" "$(date +%s%N)" | cksum | awk '{print $1}')
  a=$((n % ${#choices[@]}))
  printf '%s' "${choices[$a]}"
}

ack() {
  case "$1" in
    open) variant open "Opening $2." "I've got $2 open." "Launching $2 now." ;;
    search) variant search "I'll look that up." "Searching for that now." "On it - checking the web." ;;
    workspace) variant workspace "Switching to workspace $2." "Workspace $2 is coming up." "Moving over to workspace $2." ;;
    volume-up) variant volume-up "Turning it up." "Raising the volume." "Making it a little louder." ;;
    volume-down) variant volume-down "Turning it down." "Lowering the volume." "Making it a little quieter." ;;
    brightness-up) variant brightness-up "Brightening things up." "Raising the screen brightness." "Bringing up the brightness." ;;
    brightness-down) variant brightness-down "Dimming the screen." "Lowering the brightness." "Softening the display." ;;
    weather) variant weather "Checking the skies." "Let me look at the forecast." "I'm checking the weather now." ;;
    news) variant news "Pulling the headlines." "Let me gather the latest headlines." "Checking what's happening." ;;
    action) variant action "Done." "That’s taken care of." "Handled." ;;
    *) variant general "Done." "On it." "All set." ;;
  esac
}

# Deterministic intents: instant + reliable, no LLM round-trip.
# (The 270m brain is great for chatter, unreliable at protocol tags.)
LOW=$(echo "$USER_TEXT" | tr '[:upper:]' '[:lower:]' | sed -E 's/[?.!]+$//; s/^hey lain[, ]*//; s/^lain[, ]*//; s/^please //; s/^(could|would) you //')
site_for() {
  case "$1" in
    youtube|"you tube") echo https://www.youtube.com ;;
    hacker\ news|hackernews|hn) echo https://news.ycombinator.com ;;
    product\ hunt|producthunt) echo https://www.producthunt.com ;;
    notion) echo https://www.notion.so ;;
    canva) echo https://www.canva.com ;;
    figma) echo https://www.figma.com ;;
    zoom) echo https://app.zoom.us/wc ;;
    teams|microsoft\ teams) echo https://teams.microsoft.com ;;
    slack) echo https://app.slack.com/client ;;
    linkedin) echo https://www.linkedin.com ;;
    instagram) echo https://www.instagram.com ;;
    facebook) echo https://www.facebook.com ;;
    messenger) echo https://www.messenger.com ;;
    calendar|google\ calendar) echo https://calendar.google.com ;;
    drive|google\ drive) echo https://drive.google.com ;;
    docs|google\ docs) echo https://docs.google.com ;;
    maps|google\ maps) echo https://maps.google.com ;;
    github) echo https://github.com ;;
    gmail|mail) echo https://mail.google.com ;;
    reddit) echo https://www.reddit.com ;;
    twitch) echo https://www.twitch.tv ;;
    wikipedia|wiki) echo https://www.wikipedia.org ;;
    twitter|x) echo https://x.com ;;
    discord) echo https://discord.com/app ;;
    spotify) echo https://open.spotify.com ;;
    stackoverflow|"stack overflow") echo https://stackoverflow.com ;;
    amazon) echo https://www.amazon.com ;;
    netflix) echo https://www.netflix.com ;;
    *.*) echo "$1" ;;
    *) echo "" ;;
  esac
}
urlencode() { python3 -c "import urllib.parse,sys; print(urllib.parse.quote_plus(sys.argv[1]))" "$1"; }
# Per-site search URLs so "open youtube and search for X" lands on results,
# not the homepage. Falls back to site-restricted DDG, then plain DDG.
site_search() {
  local site="$1" query="$2" qenc url host
  qenc=$(urlencode "$query")
  case "$site" in
    youtube|"you tube") echo "https://www.youtube.com/results?search_query=$qenc" ;;
    github) echo "https://github.com/search?q=$qenc&type=repositories" ;;
    reddit) echo "https://www.reddit.com/search/?q=$qenc" ;;
    wikipedia|wiki) echo "https://en.wikipedia.org/wiki/Special:Search?search=$qenc" ;;
    stackoverflow|"stack overflow") echo "https://stackoverflow.com/search?q=$qenc" ;;
    amazon) echo "https://www.amazon.com/s?k=$qenc" ;;
    twitch) echo "https://www.twitch.tv/search?term=$qenc" ;;
    spotify) echo "https://open.spotify.com/search/$qenc" ;;
    *) url=$(site_for "$site");
       if [ -n "$url" ]; then
         host=$(echo "$url" | sed -E 's#^https?://(www\.)?##; s#/.*##')
         echo "https://duckduckgo.com/?q=site%3A$host+$qenc"
       else
         echo "https://duckduckgo.com/?q=$qenc"
       fi ;;
  esac
}
# Compound FIRST: "open <site> and search|play <query>" (else the plain
# open rule would swallow the whole thing as an app name).
if [[ "$LOW" =~ ^(open|launch|start)\ (.+)\ and\ (search|play)(\ for)?\ (.+)$ ]]; then
  SITE="${BASH_REMATCH[2]}"; QUERY="${BASH_REMATCH[5]}"
  SITE=$(echo "$SITE" | sed -E 's/^(the|my) //; s/ (website|site|page|app)$//')
  printf '[ACTION: open %s]\n%s\n' "$(site_search "$SITE" "$QUERY")" "$(ack search)"
  exit 0
fi
# General compound intent: recursively plan both halves, then only combine
# them when each half produces a validated action. This keeps arbitrary app
# names out of the parser and lets any deterministic intent be chained.
if [[ "$LOW" =~ ^(.+)[[:space:]]+(and|then)[[:space:]]+(.+)$ ]]; then
  LEFT="${BASH_REMATCH[1]}"; RIGHT="${BASH_REMATCH[3]}"
  OPEN_VERB=$(echo "$LEFT" | awk '{print $1}')
  if [[ "$LEFT" =~ ^(open|launch|start|go\ to|take\ me\ to)[[:space:]]+.+$ ]] && \
     [[ ! "$RIGHT" =~ ^(open|launch|start|go\ to|take\ me\ to|switch|go|move|send|set|turn|increase|decrease|mute|pause|play|stop|lock|close|take|capture|search|look|check|show|tell|get|what|how)[[:space:]] ]]; then
    RIGHT="$OPEN_VERB $RIGHT"
  fi
  LEFT_REPLY=$("$0" "$LEFT" 2>/dev/null || true)
  RIGHT_REPLY=$("$0" "$RIGHT" 2>/dev/null || true)
  LEFT_ACTIONS=$(printf '%s\n' "$LEFT_REPLY" | grep -E '^\[ACTION:[^]]+\]$' || true)
  RIGHT_ACTIONS=$(printf '%s\n' "$RIGHT_REPLY" | grep -E '^\[ACTION:[^]]+\]$' || true)
  if [ -n "$LEFT_ACTIONS" ] && [ -n "$RIGHT_ACTIONS" ]; then
    printf '%s\n%s\n' "$LEFT_ACTIONS" "$RIGHT_ACTIONS"
    printf '%s\n' "$(ack action)"
    exit 0
  fi
fi
# General multi-target open: keep this before the single-target rule so
# multiple named applications become separate actions.
if [[ "$LOW" =~ ^(open|launch|start|go\ to|take\ me\ to)\ (.+)\ and\ (.+)$ ]] || [[ "$LOW" =~ ^(open|launch|start|go\ to|take\ me\ to)\ (.+),\ (.+)$ ]]; then
  TARGETS_TEXT="${BASH_REMATCH[2]} and ${BASH_REMATCH[3]}"
  TARGETS_TEXT=$(echo "$TARGETS_TEXT" | sed -E 's/, and /, /; s/ and /, /g')
  IFS=',' read -ra TARGETS <<< "$TARGETS_TEXT"
  if [ "${#TARGETS[@]}" -gt 1 ]; then
    SUMMARY=""; COUNT=0
    for TARGET in "${TARGETS[@]}"; do
      TARGET=$(echo "$TARGET" | sed -E 's/^ +//; s/ +$//; s/^(the|my) //; s/ (website|site|page|app)$//')
      URL=$(site_for "$TARGET")
      [ -n "$URL" ] || URL="$TARGET"
      printf '[ACTION: open %s]\n' "$URL"
      [ -n "$SUMMARY" ] && SUMMARY="$SUMMARY and "
      SUMMARY="$SUMMARY$TARGET"
      COUNT=$((COUNT + 1))
    done
    printf '%s\n' "$(ack open "$SUMMARY")"
    exit 0
  fi
  unset TARGETS
  unset IFS
  unset TARGETS_TEXT
  exit 0
fi
if [[ "$LOW" =~ ^(open|launch|start|go\ to|take\ me\ to)\ (.+)$ ]]; then
  TARGET="${BASH_REMATCH[2]}"
  TARGET=$(echo "$TARGET" | sed -E 's/^(the|my) //; s/ (website|site|page|app)$//')
  case "$TARGET" in
    neighborly|neighborli|neighbor-li|neighborli.xyz|neighborly.xyz) URL="" ;;
    *) URL=$(site_for "$TARGET") ;;
  esac
  if [ -n "$URL" ]; then
    printf '[ACTION: open %s]\n%s\n' "$URL" "$(ack open "$TARGET")"
  else
    case "$TARGET" in
      neighborly|neighborli|neighbor-li|neighborli.xyz|neighborly.xyz)
        printf '[ACTION: open neighborli]\n%s\n' "$(ack open Neighborli)" ;;
      *) printf '[ACTION: open %s]\n%s\n' "$TARGET" "$(ack open "$TARGET")" ;;
    esac
  fi
  exit 0
fi
if [[ "$LOW" =~ ^(search|look\ up|google)( for)?\ (.+)$ ]]; then
  printf '[ACTION: search %s]\n%s\n' "${BASH_REMATCH[3]}" "$(ack search)"
  exit 0
fi
case "$LOW" in
  switch\ workspace\ *\ and\ open\ *|switch\ to\ workspace\ *\ and\ open\ *|go\ to\ workspace\ *\ and\ open\ *|workspace\ *\ and\ open\ *)
    Q=$(echo "$LOW" | sed -E 's/^(switch |switch to |go to )?workspace //; s/ and open /|/')
    DEST="${Q%%|*}"; APP="${Q#*|}"
    case "$DEST" in one) DEST=1;; two) DEST=2;; three) DEST=3;; four) DEST=4;; five) DEST=5;; six) DEST=6;; seven) DEST=7;; eight) DEST=8;; nine) DEST=9;; ten) DEST=10;; esac
    printf '[ACTION: workspace %s]\n[ACTION: open %s]\n%s\n' "$DEST" "$APP" "$(ack action)"; exit 0 ;;
  workspace\ *|switch\ workspace\ *|switch\ to\ workspace\ *|go\ to\ workspace\ *)
    Q=$(echo "$LOW" | sed -E 's/^(switch |switch to |go to )?workspace //')
    case "$Q" in one) Q=1;; two) Q=2;; three) Q=3;; four) Q=4;; five) Q=5;; six) Q=6;; seven) Q=7;; eight) Q=8;; nine) Q=9;; ten) Q=10;; esac
    printf '[ACTION: workspace %s]\n%s\n' "$Q" "$(ack workspace "$Q")"; exit 0 ;;
  move\ *\ to\ workspace\ *|send\ *\ to\ workspace\ *)
    Q=$(echo "$LOW" | sed -E 's/^(move|send) //; s/ to workspace /|/')
    TARGET="${Q%%|*}"; DEST="${Q#*|}"
    case "$DEST" in one) DEST=1;; two) DEST=2;; three) DEST=3;; four) DEST=4;; five) DEST=5;; six) DEST=6;; seven) DEST=7;; eight) DEST=8;; nine) DEST=9;; ten) DEST=10;; esac
    printf '[ACTION: sway-move %s]\nMoving %s to workspace %s.\n' "$TARGET|$DEST" "$TARGET" "$DEST"; exit 0 ;;
  *fullscreen*|*full\ screen*) printf '[ACTION: sway-fullscreen toggle]\nToggling fullscreen.\n'; exit 0 ;;
  *floating*|*float*) printf '[ACTION: sway-float toggle]\nToggling floating mode.\n'; exit 0 ;;
  *scratchpad*) printf '[ACTION: sway-scratchpad toggle]\nToggling the scratchpad.\n'; exit 0 ;;
  *focus\ left*|*focus\ right*|*focus\ up*|*focus\ down*|*next\ window*|*previous\ window*)
    DIR=$(echo "$LOW" | sed -E 's/.*focus (left|right|up|down).*/\1/; s/.*next window.*/next/; s/.*previous window.*/prev/')
    printf '[ACTION: sway-focus-direction %s]\nMoving focus %s.\n' "$DIR" "$DIR"; exit 0 ;;
  *dismiss*|*go\ away*|*hide\ yourself*|*close\ yourself*|*thank\ you*|*thanks*|*that\'s\ all*|*goodbye*|*good\ night*|*bye*) printf '[ACTION: overlay-hide]\nAnytime. Tap me when you need me.\n'; exit 0 ;;
  *volume\ up*|*turn\ it\ up*|*louder*|*raise\ the\ volume*|*increase\ the\ volume*) printf '[ACTION: volume up]\n%s\n' "$(ack volume-up)"; exit 0 ;;
  *volume\ down*|*turn\ it\ down*|*quieter*|*lower\ the\ volume*|*decrease\ the\ volume*) printf '[ACTION: volume down]\n%s\n' "$(ack volume-down)"; exit 0 ;;
  *mute*|*unmute*) printf '[ACTION: volume mute]\nToggling mute.\n'; exit 0 ;;
  *pause*|*resume*|*play\ music*|*stop\ music*|*stop\ the\ music*) printf '[ACTION: media play-pause]\nToggling playback.\n'; exit 0 ;;
  *next\ song*|*next\ track*|*skip\ this*|*skip\ it*) printf '[ACTION: media next]\nSkipping.\n'; exit 0 ;;
  *previous\ song*|*previous\ track*|*last\ song*|*go\ back\ a\ track*) printf '[ACTION: media prev]\nGoing back.\n'; exit 0 ;;
  *brighter*|*brighten*|*brightness\ up*) printf '[ACTION: brightness up]\n%s\n' "$(ack brightness-up)"; exit 0 ;;
  *dim*|*darker*|*brightness\ down*) printf '[ACTION: brightness down]\n%s\n' "$(ack brightness-down)"; exit 0 ;;
  *screenshot*|*screen\ shot*|*capture\ my\ screen*|*capture\ the\ screen*) printf '[ACTION: screenshot]\nTaking a screenshot.\n'; exit 0 ;;
  *weather*|*forecast*|*rain*|*temperature\ outside*|*humid*)
    Q=$(echo "$LOW" | sed -E -e 's/^(fetch|get|give|tell|show|check) me (the )?//' -e 's/^.*\b(weather|forecast|rain) (forecast )?(in|for|at|like in|like for) //' -e 's/ for (the )?next [a-z]+( [a-z]+)?$//' -e 's/ for (a |the )?(few|couple of )?(day|days)$//' -e 's/^(a|an|the) //' -e 's/\b(what is|whats|what is|how is|hows|tell me|the|a|an|current|currently|today|tonight|tomorrow|outside|right now|like|please|weather|forecast|rain|raining)\b//g' -e 's/[^a-z0-9 ]//g; s/ +/ /g; s/^ //; s/ $//')
    printf '[ACTION: fetch-weather %s]\n%s\n' "$Q" "$(ack weather)"; exit 0 ;;
  *news*|*headlines*)
    Q=$(echo "$LOW" | sed -E -e 's/\b(read|give|tell|show|bring|get|me|the|latest|top|breaking|morning|evening|todays|today|about|on|for|some|any|what|whats|what is|how|how is|are|is|news|headlines|headline)\b//g' -e 's/[^a-z0-9 ]//g; s/ +/ /g; s/^ //; s/ $//')
    printf '[ACTION: fetch-news %s]\n%s\n' "$Q" "$(ack news)"; exit 0 ;;
  *lock\ the\ screen*|*lock\ screen*|*lock\ up*) printf '[ACTION: lock]\nLocking up.\n'; exit 0 ;;
  *close\ this*|*close\ that\ window*|*close\ the\ window*|*close\ it*) printf '[ACTION: close]\nClosing it.\n'; exit 0 ;;
  *what\ time\ is\ it*|*tell\ me\ the\ time*|*current\ time*|*what\'s\ the\ time*)
    printf 'It is %s.\n' "$(date +'%-I:%M %p')"; exit 0 ;;
  *what\ day\ is\ it*|*what\'s\ the\ date*|*what\ is\ today\'s\ date*|*tell\ me\ today\'s\ date*|*what\'s\ today\'s\ date*)
    printf 'Today is %s.\n' "$(date +'%A, %-d %B')"; exit 0 ;;
  *battery*)
    _bat=""
    for _b in /sys/class/power_supply/BAT*; do [ -d "$_b" ] && { _bat="$_b"; break; }; done
    if [ -n "$_bat" ]; then
      _pct=$(cat "$_bat/capacity" 2>/dev/null || echo "?")
      _st=$(cat "$_bat/status" 2>/dev/null || echo unknown)
      printf 'The battery is at %s percent and %s.\n' "$_pct" "$(echo "$_st" | tr '[:upper:]' '[:lower:]')"
    else
      printf 'I do not see a battery on this machine.\n'
    fi
    exit 0 ;;
esac

# Brain config: ~/.config/hey-lain/brain.json, written by navi-lain-config.
# {backend: local|ollama-cloud|openai|openrouter, model, api_key, api_url}
# Env overrides (BRAIN_MODEL, BRAIN_URL, BRAIN_TIMEOUT, HEY_LAIN_API_KEY)
# always win over the file.
BRAIN_CONF="${XDG_CONFIG_HOME:-$HOME/.config}/hey-lain/brain.json"
BACKEND="local"
MODEL="gemma3:270m"
API_KEY=""
API_URL="http://127.0.0.1:11434/api/chat"
if [ -f "$BRAIN_CONF" ]; then
  eval "$(python3 - "$BRAIN_CONF" <<'EOF'
import json, shlex, sys
try:
    c = json.load(open(sys.argv[1]))
except Exception:
    sys.exit(0)
for k in ("backend", "model", "api_key", "api_url"):
    v = c.get(k, "")
    if v:
        print(k.upper() + "=" + shlex.quote(str(v)))
EOF
)"
fi
MODEL="${BRAIN_MODEL:-$MODEL}"
API_KEY="${HEY_LAIN_API_KEY:-$API_KEY}"
URL="${BRAIN_URL:-$API_URL}"
TIMEOUT="${BRAIN_TIMEOUT:-30}"
LOCAL_FALLBACK="gemma3:270m"
LOCAL_URL="http://127.0.0.1:11434/api/chat"

ask() {
  local model="$1" payload resp out
  # gpt-oss reasons: roomy cap or replies truncate. Others stop at EOS anyway.
  local cap=150
  case "$model" in *gpt-oss*) cap=400 ;; esac
  if [ "$BACKEND" = "openai" ] || [ "$BACKEND" = "openrouter" ]; then
    # OpenAI-compatible chat completions (OpenAI, OpenRouter).
    payload=$(python3 - "$SYSTEM" "$USER_TEXT" "$model" "$cap" <<'EOF'
import json, sys
system, user, model, cap = sys.argv[1], sys.argv[2], sys.argv[3], int(sys.argv[4])
print(json.dumps({
    "model": model,
    "max_tokens": cap,
    "messages": [
        {"role": "system", "content": system},
        {"role": "user", "content": user},
    ],
}))
EOF
)
    resp=$(curl -s --max-time "$TIMEOUT" "$URL" \
      -H "Authorization: Bearer $API_KEY" \
      -H "Content-Type: application/json" \
      -H "HTTP-Referer: https://navi.xvoidsx.org" \
      -H "X-Title: Hey Lain" \
      -d "$payload" 2>/dev/null || true)
    out=$(python3 -c "
import json,sys
d=json.load(sys.stdin)
print(d['choices'][0]['message']['content'].strip())" <<<"$resp" 2>/dev/null || true)
  else
    # Ollama-native /api/chat (local models AND ollama-cloud *-cloud names
    # proxied through the local daemon).
    payload=$(python3 - "$SYSTEM" "$USER_TEXT" "$model" "$cap" <<'EOF'
import json, sys
system, user, model, cap = sys.argv[1], sys.argv[2], sys.argv[3], int(sys.argv[4])
print(json.dumps({
    "model": model,
    "stream": False,
    "keep_alive": "30m",
    "options": {"num_predict": cap},
    "messages": [
        {"role": "system", "content": system},
        {"role": "user", "content": user},
    ],
}))
EOF
)
    resp=$(curl -s --max-time "$TIMEOUT" "$URL" -d "$payload" 2>/dev/null || true)
    out=$(python3 -c "import json,sys; print(json.load(sys.stdin).get('message',{}).get('content','').strip())" <<<"$resp" 2>/dev/null || true)
  fi
  echo "$out"
}

OUT=$(ask "$MODEL")
if [ -z "${OUT// }" ] && { [ "$BACKEND" != "local" ] || [ "$MODEL" != "$LOCAL_FALLBACK" ]; }; then
  # Cloud backends (and a non-default local model) fall back to the tiny
  # local brain before giving up to the offline line.
  echo "brain: primary backend failed, falling back to $LOCAL_FALLBACK" >&2
  BACKEND="local"
  URL="$LOCAL_URL"
  OUT=$(ask "$LOCAL_FALLBACK")
fi
if [ -z "${OUT// }" ]; then
  echo "You said: $USER_TEXT. My brain is offline right now, but my ears and voice work."
  exit 0
fi
echo "$OUT" | sed -e 's/\*\*//g' -e 's/`//g' | head -n 20
