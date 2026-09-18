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

web_fallback() {
  local target="$1"
  swaymsg exec "chromium --app=https://duckduckgo.com/?q=$(urlencode "$target")" >&2
  SPEECH="I couldn't find $target installed, so I opened its website search."
}

sway_control() {
  python3 "$SWAY_CONTROL" "$@" >&2
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

desktop_launcher() {
  local q="$1" ql file id
  ql=$(echo "$q" | tr '[:upper:]' '[:lower:]' | tr -cd '[:alnum:]')
  [ -n "$ql" ] || return 1
  while IFS= read -r file; do
    id=$(basename "$file" .desktop)
    if echo "$id" | tr '[:upper:]' '[:lower:]' | tr -cd '[:alnum:]' | grep -q "$ql"; then
      printf 'gtk-launch %s' "$id"
      return 0
    fi
    if grep -qiE "^(Name|Exec)=.*${q}" "$file" 2>/dev/null; then
      printf 'gtk-launch %s' "$id"
      return 0
    fi
  done < <(find /usr/share/applications "$HOME/.local/share/applications" -maxdepth 1 -type f -name '*.desktop' 2>/dev/null | sort)
  return 1
}

urlencode() { python3 -c "import urllib.parse,sys; print(urllib.parse.quote_plus(sys.argv[1]))" "$1"; }

case "$NAME" in
  notify) notify-send "Hey Lain!" "$ARGS" >&2 ;;
  open)
    case "$ARGS" in
      http*|*://*|*.*) swaymsg exec "chromium --app=$(normalise_url "$ARGS")" >&2 ;;
      *)
        APP_TARGET=$(normalise_app "$ARGS")
        if app_exists "$APP_TARGET"; then
          LAUNCH=$(desktop_launcher "$APP_TARGET" || true)
          # Foreground on purpose: callers must never run us inside $(...),
          # where bash reaps backgrounded children on subshell exit.
          swaymsg exec "${LAUNCH:-$APP_TARGET}" >&2
        else
          web_fallback "$ARGS"
        fi ;;
    esac ;;
  search)
    swaymsg exec "chromium --app=https://duckduckgo.com/?q=$(urlencode "$ARGS")" >&2
    SPEECH="Looking up $ARGS." ;;
  fetch-weather)
    if [ -z "${ARGS// }" ]; then
      SPEECH="For which place should I check the weather?"
    elif OUT=$("$DIR/fetch-weather.sh" "$ARGS"); then
      SPEECH="$OUT"
    else
      swaymsg exec "chromium --app=https://duckduckgo.com/?q=$(urlencode "weather $ARGS")" >&2
      SPEECH="I couldn't reach the forecast. I opened it instead."
    fi ;;
  fetch-news)
    if OUT=$("$DIR/fetch-news.sh" "$ARGS"); then
      SPEECH="$OUT"
    else
      swaymsg exec "chromium --app=https://duckduckgo.com/?q=$(urlencode "news $ARGS")" >&2
      SPEECH="I couldn't pull the headlines. I opened them instead."
    fi ;;
  volume)
    case "$ARGS" in
      up) pactl set-sink-volume @DEFAULT_SINK@ +5% >&2 ;;
      down) pactl set-sink-volume @DEFAULT_SINK@ -5% >&2 ;;
      mute) pactl set-sink-mute @DEFAULT_SINK@ toggle >&2 ;;
    esac ;;
  media)
    case "$ARGS" in
      play-pause|play|pause) playerctl play-pause >&2 ;;
      next) playerctl next >&2 ;;
      prev|previous) playerctl previous >&2 ;;
    esac ;;
  brightness)
    case "$ARGS" in
      up) brightnessctl set +10% >&2 ;;
      down) brightnessctl set 10%- >&2 ;;
    esac ;;
  workspace) sway_control workspace "$ARGS"; SPEECH="Switching to workspace $ARGS." ;;
  sway-focus) sway_control focus "$ARGS"; SPEECH="Focusing $ARGS." ;;
  sway-move)
    TARGET="${ARGS%%|*}"; DEST="${ARGS#*|}"
    sway_control move "$TARGET" "$DEST"; SPEECH="Moving $TARGET to workspace $DEST." ;;
  sway-float) sway_control float "$ARGS"; SPEECH="Updating the window's floating mode." ;;
  sway-fullscreen) sway_control fullscreen "$ARGS"; SPEECH="Updating fullscreen mode." ;;
  sway-layout) sway_control layout "$ARGS"; SPEECH="Changing the layout." ;;
  sway-scratchpad) sway_control scratchpad "$ARGS"; SPEECH="Updating the scratchpad." ;;
  sway-focus-direction) sway_control focus-direction "$ARGS"; SPEECH="Moving focus $ARGS." ;;
  lock) swaylock -f >&2 ;;
  screenshot) grim ~/Pictures/lain-$(date +%Y%m%d-%H%M%S).png >&2 ;;
  close) swaymsg kill >&2 ;;
  overlay-hide) "$DIR/listen-overlay.sh" hide >&2 ;;
  *) echo "unknown action: $NAME" >&2 ;;
esac
echo "$SPEECH"
