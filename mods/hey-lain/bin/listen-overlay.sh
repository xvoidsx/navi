#!/usr/bin/env bash
# hey-lain overlay: floating nightshadeNeon status window (foot terminal).
# A state machine: LISTENING -> HEARD -> THINKING -> ACTING -> SPEAKING -> WAITING.
# Writers drop state/text files; the foot loop polls them without taking focus.
# Usage: listen-overlay.sh show | hide | state NAME [text]
set -uo pipefail
HERE="$(cd "$(dirname "$0")/.." && pwd)"
APP_ID="hey-lain-listening"
RUNTIME="${XDG_RUNTIME_DIR:-/tmp}/hey-lain"
PIDFILE="$RUNTIME/overlay.pid"
STATEFILE="$RUNTIME/overlay-state"
TEXTFILE="$RUNTIME/overlay-text"
TIMEFILE="$RUNTIME/overlay-state-time"
LOG="$HERE/log/hey-lain.log"
mkdir -p "$RUNTIME"

log() { mkdir -p "$(dirname "$LOG")" 2>/dev/null; echo "$(date '+%F %T') overlay: $* (pid $$)" >> "$LOG"; }

overlay_alive() {
  local pid
  [ -f "$PIDFILE" ] || return 1
  pid=$(cat "$PIDFILE" 2>/dev/null || true)
  [ -n "${pid:-}" ] && [ -f "/proc/$pid/cmdline" ] \
    && tr '\0' ' ' < "/proc/$pid/cmdline" 2>/dev/null | grep -q "app-id $APP_ID"
}

sanitise() {
  echo "${1:-}" | sed -E 's/[[:blank:]]+/ /g; s/^ //; s/ $//' \
    | tr -d '\000-\010\013\014\016-\037\177' | cut -c1-240
}

hide() {
  log "hide"
  if [ -f "$PIDFILE" ]; then
    kill "$(cat "$PIDFILE" 2>/dev/null)" 2>/dev/null || true
    rm -f "$PIDFILE"
  fi
  pkill -f "app-id $APP_ID" 2>/dev/null || true
  pkill -f "hey-lain-anim" 2>/dev/null || true
  swaymsg "[app_id=\"$APP_ID\"] kill" >/dev/null 2>&1 || true
  rm -f "$STATEFILE" "$TEXTFILE" "$TIMEFILE"
}

set_state() {
  log "state $1"
  echo "${1:-LISTENING}" > "$STATEFILE"
  sanitise "${2:-}" > "$TEXTFILE"
  date +%s%3N > "$TIMEFILE"
}

show() {
  echo "LISTENING" > "$STATEFILE"
  : > "$TEXTFILE"
  date +%s%3N > "$TIMEFILE"
  if overlay_alive; then
    log "show: already up"
    return 0
  fi
  hide 2>/dev/null || true
  log "show: launching foot"
  # The visualizer reads only the state file. Text is retained in the runtime
  # protocol for compatibility, but deliberately is not rendered.
  ANIM='
    P="\033[38;2;255;16;240m"; G="\033[38;2;57;255;20m"; C="\033[38;2;0;255;255m"; W="\033[97m"; D="\033[2m"; R="\033[0m";
    SPIN="⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"; LVL="▁▂▃▄▅▆▇█"; i=0; last="";
    printf "\033[?1049h\033[?25l";
    trap "printf \"\033[?1049l\033[?25h\033[0m\"; exit" INT TERM EXIT;
    vu() { local k h c out="";
      for k in $(seq 1 14); do h=$(( (i * 3 + k * 5 + i / 4) % 8 )); c="$G";
        if [ $h -ge 5 ]; then c="$C"; fi; if [ $h -ge 7 ]; then c="$P"; fi;
        out="${out}${c}${LVL:$h:1}${R}"; done; printf "%s" "$out"; };
    wave() { local k idx c out="";
      for k in $(seq 0 21); do idx=$(( (i + k) % 8 )); c="$G";
        if [ $idx -ge 3 ]; then c="$C"; fi; if [ $idx -ge 6 ]; then c="$P"; fi;
        out="${out}${c}${LVL:$idx:1}${R}"; done; printf "%s" "$out"; };
    txlines() {
      local text words elapsed now started out j;
      text=$(cat "$RUNTIME_PATH/overlay-text" 2>/dev/null || true);
      if [ "$st" = "SPEAKING" ]; then
        now=$(date +%s%3N); started=$(cat "$RUNTIME_PATH/overlay-state-time" 2>/dev/null || echo "$now");
        elapsed=$((now - started)); words=$((elapsed / 320));
        [ "$words" -lt 1 ] && words=1;
        read -r -a WORDS <<< "$text";
        out="";
        for ((j=0; j<${#WORDS[@]} && j<words; j++)); do
          [ -n "$out" ] && out="$out "; out="$out${WORDS[$j]}";
        done;
        text="$out";
      fi;
      printf "%s" "$text" | fold -w 46 -s | tail -n 5;
    };
    dialogue() {
      local line color;
      while IFS= read -r line; do
        color="$W";
        case "$line" in You:*) color="$C";; Lain:*) color="$P";; esac;
        printf "${G}  │${R}   ${color}%s${R}\n" "$line";
      done < <(txlines);
    };
    while true; do
      st=$(cat "$RUNTIME_PATH/overlay-state" 2>/dev/null || echo LISTENING);
      f=${SPIN:i%10:1};
      # No full clear per frame (that flicker is the flicker): home cursor
      # only, and clear once on state transitions (geometry changes there).
      if [ "$st" != "$last" ]; then printf "\033[2J"; last="$st"; fi;
      printf "\033[H";
      printf "${G}  ╭──────────────────────────────────────────────────╮${R}\n";
      case "$st" in
        LISTENING)
          printf "${G}  │${R}  ${P}%s  ${W}Lain is listening...${R}                       ${G}│${R}\n" "$f";
          printf "${G}  │${R}                                                    ${G}│${R}\n";
          printf "${G}  │${R}   $(vu)   ${G}│${R}\n";
          printf "${G}  │${R}                                                    ${G}│${R}\n";
          printf "${G}  │${R}   ${D}tap alt+v again to send${R}                       ${G}│${R}\n";;
        HEARD)
          printf "${G}  │${R}  ${C}%s  ${W}Lain heard you...${R}                          ${G}│${R}\n" "$f";
          printf "${G}  │${R}                                                    ${G}│${R}\n";
          dialogue;
          dots=$((i % 4));
          printf "${G}  │${R}   ${C}transcribing%.*s${R}                              ${G}│${R}\n" "$dots" "...";;
        THINKING)
          printf "${G}  │${R}  ${P}%s  ${W}Lain is thinking...${R}                        ${G}│${R}\n" "$f";
          printf "${G}  │${R}                                                    ${G}│${R}\n";
          while IFS= read -r line; do printf "${G}  │${R}   ${D}%s${R}\n" "$line"; done < <(txlines);
          printf "${G}  │${R}   $(wave)   ${G}│${R}\n";;
        ACTING)
          if [ $((i % 6)) -lt 3 ]; then A="$G"; else A="$P"; fi;
          printf "${G}  │${R}                                                    ${G}│${R}\n";
          printf "${G}  │${R}   ${A}⚡ Lain is taking action!${R}                       ${G}│${R}\n";
          dialogue;
          printf "${G}  │${R}                                                    ${G}│${R}\n";;
        SPEAKING)
          dots=$((i % 4));
          printf "${G}  │${R}  ${P}%s  ${W}Lain says...%.*s${R}                           ${G}│${R}\n" "$f" "$dots" "...";
          printf "${G}  │${R}                                                    ${G}│${R}\n";
          dialogue;
          printf "${G}  │${R}                                                    ${G}│${R}\n";
          printf "${G}  │${R}   ${D}tap alt+v to talk back · words arrive live${R}    ${G}│${R}\n";;
        WAITING)
          printf "${G}  │${R}  ${C}%s  ${W}Lain is waiting...${R}                         ${G}│${R}\n" "$f";
          printf "${G}  │${R}                                                    ${G}│${R}\n";
          dialogue;
          printf "${G}  │${R}                                                    ${G}│${R}\n";
          printf "${G}  │${R}   ${D}tap alt+v to talk to me${R}                      ${G}│${R}\n";;
      esac;
      printf "${G}  ╰──────────────────────────────────────────────────╯${R}\n";
      i=$((i+1)); sleep 0.12;
    done
  '
  # setsid: survive whatever parent launched us (sway exec, shells, timeouts).
  setsid foot --app-id "$APP_ID" --title "hey-lain-listening" \
    -W 58x13 \
    -o colors.background=000000 \
    -o colors.foreground=ffffff \
    -o colors.regular0=000000 \
    -o colors.regular5=ff10f0 \
    -o colors.regular2=39ff14 \
    -o colors.regular6=00ffff \
    -o main.font=monospace:size=13 \
    -o main.pad=12x12 \
    -o main.initial-window-mode=windowed \
    "$HERE/bin/overlay-visualizer.sh" "$RUNTIME" &
  # setsid forks, so $! is a dead wrapper: track the real foot PID instead.
  sleep 0.4
  pgrep -f "app-id $APP_ID" 2>/dev/null | head -n 1 > "$PIDFILE"
  sleep 0.5
  swaymsg "[app_id=\"$APP_ID\"] floating enable, sticky enable, border pixel 2" >/dev/null 2>&1 || true
  swaymsg "[app_id=\"$APP_ID\"] resize set 600 330" >/dev/null 2>&1 || true
  swaymsg "[app_id=\"$APP_ID\"] move position 383 36" >/dev/null 2>&1 || true
  # NOTE: deliberately no `focus` — stealing focus breaks key events.
}

case "${1:-}" in
  show) show ;;
  hide) hide ;;
  state) set_state "${2:-LISTENING}" "${3:-}" ;;
  *) echo "usage: listen-overlay.sh show|hide|state NAME [text]" >&2; exit 2 ;;
esac
