#!/usr/bin/env bash
# Allowlisted desktop actions for hey-lain. Input: full Lain reply on stdin or $*.
# Looks for a leading: [ACTION: name args...]
# Prints the speech-only text to stdout, executes the side effect.
# HEY_LAIN_DRY_RUN=1 prints what WOULD run without running it (for tests).
#
# CRITICAL: our stdout IS the spoken text (caller redirects it to speech.txt).
# Every side-effect command MUST send its own stdout to stderr (the debug
# log) — otherwise IPC replies like swaymsg's [{"success": true}] get
# SPOKEN ALOUD. Yes, that really happened.
set -uo pipefail
DIR="$(cd "$(dirname "$0")" && pwd)"
SWAY_CONTROL="${HEY_LAIN_SWAY_CONTROL:-$DIR/sway-control.py}"
REPLY_TEXT="${*:-$(cat)}"
ACTION=$(echo "$REPLY_TEXT" | grep -oE '^\[ACTION:[^]]+\]' | head -n 1 || true)
# default speech = reply minus the tag; individual actions may override it
# (e.g. app-not-found) AFTER attempting the side effect.
SPEECH=$(echo "$REPLY_TEXT" | sed -E 's/^\[ACTION:[^]]+\][[:space:]]*//')

# Execute several leading action tags in order, using the normal single-action
# path for each one. This deliberately avoids command substitution so spawned
# desktop processes survive.
ACTION_COUNT=$(printf '%s\n' "$REPLY_TEXT" | grep -cE '^\[ACTION:[^]]+\][[:space:]]*$' || true)
if [ "$ACTION_COUNT" -gt 1 ]; then
  while IFS= read -r TAG; do
    [ -z "$TAG" ] && continue
    "$0" <<<"$TAG" >/dev/null
  done < <(printf '%s\n' "$REPLY_TEXT" | grep -E '^\[ACTION:[^]]+\][[:space:]]*$')
  SPEECH=$(printf '%s\n' "$REPLY_TEXT" | sed -E ':a; s/^\[ACTION:[^]]+\][[:space:]]*//; ta')
  [ -n "${SPEECH// }" ] && echo "$SPEECH"
  exit 0
fi

if [ -z "$ACTION" ]; then echo "$SPEECH"; exit 0; fi
INNER=$(echo "$ACTION" | sed -E 's/^\[ACTION:[[:space:]]*//; s/\]$//')
NAME=$(echo "$INNER" | awk '{print $1}')
ARGS=$(echo "$INNER" | cut -d' ' -f2- -s || true)

if [ "${HEY_LAIN_DRY_RUN:-0}" = "1" ]; then
  echo "$SPEECH"
  echo "[dry-run] action=$NAME args=$ARGS" >&2
  exit 0
fi

# URL normalizer: bare domains become https:// URLs.
normalise_url() {
  local u="$1"
  [[ "$u" == *://* ]] || u="https://$u"
  echo "$u"
}

# The app isn't installed. A single word is an app name we don't have —
# say so honestly instead of guessing. A longer phrase is probably a
# search request, so that still opens a web search.
app_not_found() {
  local target="$1"
  if [[ "$target" =~ ^[A-Za-z0-9._-]+$ ]]; then
    SPEECH="I don't have an app called $target."
  else
    if swaymsg exec "chromium --app=https://duckduckgo.com/?q=$(urlencode "$target")" >&2; then
      SPEECH="I couldn't find $target installed, so I opened a web search."
    else
      SPEECH="I couldn't find $target, and I couldn't reach the desktop."
    fi
  fi
}

sway_control() {
  # Run the allow-listed sway controller. On failure the human-readable
  # reason lands in SWAY_ERR so the caller can speak it instead of
  # claiming success.
  SWAY_ERR=$(python3 "$SWAY_CONTROL" "$@" 2>&1 >/dev/null) || {
    # Strip the "sway-control: " prefix: SWAY_ERR is spoken aloud, and
    # the prefix is log furniture, not speech.
    SWAY_ERR="${SWAY_ERR#sway-control: }"
    printf 'sway-control: %s\n' "$SWAY_ERR" >&2
    return 1
  }
}

# con_id of the best running window for an app name, or nothing.
# Quiet by design: "not running" is the normal launch path, not an error.
sway_find() {
  python3 "$SWAY_CONTROL" find "$1" 2>/dev/null || true
}

# swaymsg with a hang guard: a wedged IPC socket must degrade to a spoken
# error, never wedge the voice loop holding the single-flight lock.
# (env(1) does the PATH lookup so this doesn't recurse into itself.)
swaymsg() {
  timeout 10 env swaymsg "$@"
}

# Is this app launchable? binary on PATH, .desktop file, or flatpak.
app_exists() {
  local q ql
  q=$(normalise_app "$1")
  ql=$(echo "$q" | tr '[:upper:]' '[:lower:]')
  command -v "$q" >/dev/null 2>&1 && return 0
  ls /usr/share/applications ~/.local/share/applications 2>/dev/null \
    | grep -qiE "^${ql}[^/]*\.desktop$" && return 0
  flatpak list --app --columns=application 2>/dev/null \
    | grep -qiE "${ql}" && return 0
  return 1
}

normalise_app() {
  local q
  q=$(echo "${1:-}" | sed -E 's/[[:space:]]+$//; s/^[[:space:]]+//')
  case "$(echo "$q" | tr '[:upper:]' '[:lower:]')" in
    neighborly|neighborli|neighbor-li|neighborly\ app|neighborli\ app|neighborli.xyz|neighborly.xyz) echo "neighborli" ;;
    fire\ fox|firefox\ browser) echo "firefox" ;;
    chrome|google\ chrome|chromium\ browser) echo "chromium" ;;
    code\ editor|visual\ studio\ code) echo "code" ;;
    *) echo "$q" ;;
  esac
}

# Resolve a spoken app name to a launch command. Three STRICT passes —
# exact .desktop id, exact Name=, executable basename of Exec= — and NO
# substring or full-Exec-line matching. The old code matched the Exec LINE,
# and every navi webapp's Exec starts with "chromium --app=…", so "open
# chromium" once launched Discord (field bug 2026-09-18: the webapp dir
# sorts before /usr/share/applications and discord.desktop's Exec contains
# "chromium"). HEY_LAIN_APP_DIRS overrides the scanned dirs (tests).
desktop_launcher() {
  local q="$1" ql file id pass idn name execbin first
  ql=$(echo "$q" | tr '[:upper:]' '[:lower:]' | tr -cd '[:alnum:]')
  [ -n "$ql" ] || return 1
  local dirs="${HEY_LAIN_APP_DIRS:-/usr/share/applications $HOME/.local/share/applications}"
  for pass in 1 2 3; do
    while IFS= read -r file; do
      id=$(basename "$file" .desktop)
      case "$pass" in
        1) idn=$(echo "$id" | tr '[:upper:]' '[:lower:]' | tr -cd '[:alnum:]')
           [ "$idn" = "$ql" ] || continue ;;
        2) name=$(grep -m1 -iE '^Name=' "$file" 2>/dev/null | cut -d= -f2- \
               | tr '[:upper:]' '[:lower:]' | tr -cd '[:alnum:]')
           [ -n "$name" ] && [ "$name" = "$ql" ] || continue ;;
        3) first=$(grep -m1 -iE '^Exec=' "$file" 2>/dev/null | cut -d= -f2- \
               | awk '{print $1}')
           execbin=$(basename "$first" 2>/dev/null \
               | tr '[:upper:]' '[:lower:]' | tr -cd '[:alnum:]')
           [ -n "$execbin" ] && [ "$execbin" = "$ql" ] || continue ;;
      esac
      printf 'gtk-launch %s' "$id"
      return 0
    done < <(for d in $dirs; do
               find "$d" -maxdepth 1 -type f -name '*.desktop' 2>/dev/null
             done | sort -u)
  done
  return 1
}

urlencode() { python3 -c "import urllib.parse,sys; print(urllib.parse.quote_plus(sys.argv[1]))" "$1"; }

case "$NAME" in
  notify) notify-send "Hey Lain!" "$ARGS" >&2 ;;
  open)
    case "$ARGS" in
      http*|*://*|*.*)
        swaymsg exec "chromium --app=$(normalise_url "$ARGS")" >&2 \
          || SPEECH="I couldn't open that." ;;
      *)
        APP_TARGET=$(normalise_app "$ARGS")
        if app_exists "$APP_TARGET"; then
          # Already running? Focus it instead of spawning a duplicate —
          # "open firefox" with firefox up just brings it forward.
          if FOCUS_ID=$(sway_find "$APP_TARGET") && [ -n "$FOCUS_ID" ]; then
            if swaymsg "[con_id=$FOCUS_ID]" focus >&2; then
              SPEECH="$APP_TARGET is already open."
            else
              SPEECH="I couldn't reach the desktop to focus $APP_TARGET."
            fi
          else
            LAUNCH=$(desktop_launcher "$APP_TARGET" || true)
            # Foreground on purpose: callers must never run us inside $(...),
            # where bash reaps backgrounded children on subshell exit.
            if swaymsg exec "${LAUNCH:-$APP_TARGET}" >&2; then
              # SPEECH usually carries the brain's "Opening …" ack; if the
              # reply was a bare tag, say something anyway.
              [ -n "${SPEECH// }" ] || SPEECH="Opening $APP_TARGET."
            else
              SPEECH="I couldn't reach the desktop to launch $APP_TARGET."
            fi
          fi
        else
          app_not_found "$ARGS"
        fi ;;
    esac ;;
  search)
    if swaymsg exec "chromium --app=https://duckduckgo.com/?q=$(urlencode "$ARGS")" >&2; then
      SPEECH="Looking up $ARGS."
    else
      SPEECH="I couldn't reach the desktop to search."
    fi ;;
  fetch-weather)
    if [ -z "${ARGS// }" ]; then
      SPEECH="For which place should I check the weather?"
    elif OUT=$("$DIR/fetch-weather.sh" "$ARGS"); then
      SPEECH="$OUT"
    elif swaymsg exec "chromium --app=https://duckduckgo.com/?q=$(urlencode "weather $ARGS")" >&2; then
      SPEECH="I couldn't reach the forecast. I opened it instead."
    else
      SPEECH="I couldn't reach the forecast."
    fi ;;
  fetch-news)
    if OUT=$("$DIR/fetch-news.sh" "$ARGS"); then
      SPEECH="$OUT"
    elif swaymsg exec "chromium --app=https://duckduckgo.com/?q=$(urlencode "news $ARGS")" >&2; then
      SPEECH="I couldn't pull the headlines. I opened them instead."
    else
      SPEECH="I couldn't pull the headlines."
    fi ;;
  volume)
    case "$ARGS" in
      up) pactl set-sink-volume @DEFAULT_SINK@ +5% >&2 || SPEECH="I couldn't change the volume." ;;
      down) pactl set-sink-volume @DEFAULT_SINK@ -5% >&2 || SPEECH="I couldn't change the volume." ;;
      mute) pactl set-sink-mute @DEFAULT_SINK@ toggle >&2 || SPEECH="I couldn't change the volume." ;;
    esac ;;
  media)
    case "$ARGS" in
      play-pause|play|pause) playerctl play-pause >&2 || SPEECH="Nothing seems to be playing." ;;
      next) playerctl next >&2 || SPEECH="Nothing seems to be playing." ;;
      prev|previous) playerctl previous >&2 || SPEECH="Nothing seems to be playing." ;;
    esac ;;
  brightness)
    case "$ARGS" in
      up) brightnessctl set +10% >&2 || SPEECH="I couldn't change the brightness." ;;
      down) brightnessctl set 10%- >&2 || SPEECH="I couldn't change the brightness." ;;
    esac ;;
  workspace)
    if sway_control workspace "$ARGS"; then SPEECH="Switching to workspace $ARGS."
    else SPEECH="I couldn't switch workspaces: $SWAY_ERR"; fi ;;
  sway-focus)
    if sway_control focus "$ARGS"; then SPEECH="Focusing $ARGS."
    else SPEECH="I couldn't focus it: $SWAY_ERR"; fi ;;
  sway-move)
    TARGET="${ARGS%%|*}"; DEST="${ARGS#*|}"
    if sway_control move "$TARGET" "$DEST"; then SPEECH="Moving $TARGET to workspace $DEST."
    else SPEECH="I couldn't move it: $SWAY_ERR"; fi ;;
  sway-float)
    if sway_control float "$ARGS"; then SPEECH="Updating the window's floating mode."
    else SPEECH="I couldn't do that: $SWAY_ERR"; fi ;;
  sway-fullscreen)
    if sway_control fullscreen "$ARGS"; then SPEECH="Updating fullscreen mode."
    else SPEECH="I couldn't do that: $SWAY_ERR"; fi ;;
  sway-layout)
    if sway_control layout "$ARGS"; then SPEECH="Changing the layout."
    else SPEECH="I couldn't do that: $SWAY_ERR"; fi ;;
  sway-scratchpad)
    if sway_control scratchpad "$ARGS"; then SPEECH="Updating the scratchpad."
    else SPEECH="I couldn't do that: $SWAY_ERR"; fi ;;
  sway-focus-direction)
    if sway_control focus-direction "$ARGS"; then SPEECH="Moving focus $ARGS."
    else SPEECH="I couldn't move focus: $SWAY_ERR"; fi ;;
  lock)
    if command -v swaylock >/dev/null 2>&1; then
      swaylock -f >&2 || SPEECH="The screen locker wouldn't start."
    else
      SPEECH="There's no screen locker installed."
    fi ;;
  screenshot)
    if command -v grim >/dev/null 2>&1; then
      grim ~/Pictures/lain-$(date +%Y%m%d-%H%M%S).png >&2 \
        || SPEECH="The screenshot failed."
    else
      SPEECH="I don't have a screenshot tool installed."
    fi ;;
  close)
    if [ -n "${ARGS// }" ]; then
      # "close firefox": close the named window, not whatever is focused.
      if sway_control close "$ARGS"; then SPEECH="Closed $ARGS."
      else SPEECH="I couldn't close it: $SWAY_ERR"; fi
    else
      swaymsg kill >&2 || SPEECH="I couldn't close that."
    fi ;;
  overlay-hide) "$DIR/listen-overlay.sh" hide >&2 ;;
  *) echo "unknown action: $NAME" >&2 ;;
esac
echo "$SPEECH"
