#!/usr/bin/env bash
# Hey Lain! overlay visualizer — nightshadeNeon, rendered inside foot.
# Arguments: runtime directory.
# State is read from overlay-state. Live audio levels come from mic-level
# (while Raven speaks) and voice-level (while Lain speaks): both directions
# of the conversation get REAL waveforms. The states between them get
# synthetic motion in the same visual language:
#   LISTENING  incoming green waveform   (live mic)
#   HEARD      cyan static burst         (transition)
#   THINKING   pink pulse
#   ACTING     green/pink shimmer sweep
#   SPEAKING   outgoing pink waveform    (live voice)
#   WAITING    cyan breathing glow
# Deliberately ASCII-only: some terminal/font combinations render block and
# box-drawing Unicode as mojibake. These characters remain clean everywhere.
set -uo pipefail

RUNTIME_PATH="${1:?runtime directory required}"
P=$'\033[38;2;255;16;240m'   # neon pink
G=$'\033[38;2;57;255;20m'    # neon green
C=$'\033[38;2;0;255;255m'    # neon cyan
W=$'\033[97m'
D=$'\033[2m'
R=$'\033[0m'

COLS=44          # waveform columns; rows are 4 + COLS + 4 = 52 wide
INNER=52         # inner width between the border pipes
frame=0
last=""

declare -a hist_in=()
declare -a hist_out=()

line() { printf '\033[2K%s\n' "$1"; }

read_level() { # $1 = level file -> 0-100
  local v
  v=$(cat "$1" 2>/dev/null || echo 0)
  [[ "$v" =~ ^[0-9]+$ ]] || v=0
  [ "$v" -gt 100 ] && v=100
  printf '%s' "$v"
}

push_hist() { # $1 = array name, $2 = value (cap at COLS)
  local -n arr=$1
  arr+=("$2")
  while [ "${#arr[@]}" -gt "$COLS" ]; do arr=("${arr[@]:1}"); done
}

# Print 3 rows of '#' bars from a 0-100 history array, bottom-anchored.
wave_rows() { # $1 = array name, $2 = color
  local -n h=$1
  local color=$2 r c lvl hh row
  for r in 0 1 2; do
    row=""
    for ((c = 0; c < COLS; c++)); do
      lvl=${h[$c]:-0}
      hh=$(( lvl * 4 / 101 ))   # 0..3
      if [ "$hh" -gt 0 ] && [ "$hh" -ge $((3 - r)) ]; then row+="#"; else row+=" "; fi
    done
    line "  ${G}|${R}    ${color}${row}${R}    ${G}|${R}"
  done
}

# Print 3 rows of '#' bars at a uniform height (pulse / breathing).
flat_rows() { # $1 = color, $2 = height 0..3
  local color=$1 hh=$2 r c row
  for r in 0 1 2; do
    row=""
    for ((c = 0; c < COLS; c++)); do
      if [ "$hh" -gt 0 ] && [ "$hh" -ge $((3 - r)) ]; then row+="#"; else row+=" "; fi
    done
    line "  ${G}|${R}    ${color}${row}${R}    ${G}|${R}"
  done
}

# Shimmer band sweeping across a dotted field, alternating green/pink.
sweep_rows() {
  local r c row d pos color="$G"
  [ $(( (frame / 8) % 2 )) -eq 1 ] && color="$P"
  pos=$(( (frame * 3) % (COLS + 12) ))
  for r in 0 1 2; do
    row=""
    for ((c = 0; c < COLS; c++)); do
      d=$(( c - pos + 6 )); [ "$d" -lt 0 ] && d=$(( -d ))
      if [ "$d" -le 2 ]; then row+="#"
      elif [ "$d" -le 5 ]; then row+="+"
      else row+="."
      fi
    done
    line "  ${G}|${R}    ${color}${row}${R}    ${G}|${R}"
  done
}

# Full static burst.
noise_rows() { # $1 = color
  local color=$1 r c row hh
  for r in 0 1 2; do
    row=""
    for ((c = 0; c < COLS; c++)); do
      hh=$(( RANDOM % 4 ))
      if [ "$hh" -gt 0 ] && [ "$hh" -ge $((3 - r)) ]; then row+="#"; else row+=" "; fi
    done
    line "  ${G}|${R}    ${color}${row}${R}    ${G}|${R}"
  done
}

title_line() { # $title, $accent in scope
  local n pad core="*  ${title}  *"
  n=${#core}
  pad=$(( INNER - 8 - n ))
  [ "$pad" -lt 0 ] && pad=0
  line "  ${G}|${R}        ${accent}${core}${R}$(printf '%*s' "$pad" '')${G}|${R}"
}

sub_line() { # $subtitle in scope
  local pad
  pad=$(( INNER - 8 - ${#subtitle} ))
  [ "$pad" -lt 0 ] && pad=0
  line "  ${G}|${R}        ${D}${subtitle}${R}$(printf '%*s' "$pad" '')${G}|${R}"
}

blank_line() {
  line "  ${G}|${R}$(printf '%*s' "$INNER" '')${G}|${R}"
}

BAR="  ${G}+--------------------------------------------------+${R}"

printf '\033[?1049h\033[?25l'
trap 'printf "\033[?1049l\033[?25h\033[0m"; exit' INT TERM EXIT

while true; do
  state=$(cat "$RUNTIME_PATH/overlay-state" 2>/dev/null || echo LISTENING)
  case "$state" in
    LISTENING) title="Lain is listening..."; subtitle="Press Alt+V again to send"; mode=listen; accent="$G" ;;
    HEARD) title="Lain heard you..."; subtitle="turning your voice into intent"; mode=heard; accent="$C" ;;
    THINKING) title="Lain is thinking..."; subtitle="finding the best next move"; mode=think; accent="$P" ;;
    ACTING) title="Lain is taking action!"; subtitle="working with your desktop"; mode=act; accent="$G" ;;
    SPEAKING) title="Lain says..."; subtitle="Press Alt+V to talk back"; mode=speak; accent="$P" ;;
    WAITING) title="Lain is waiting..."; subtitle="Press Alt+V to talk to me"; mode=calm; accent="$C" ;;
    *) title="Lain is ready..."; subtitle="Press Alt+V to begin"; mode=calm; accent="$C" ;;
  esac

  # Fresh waveforms per state; one full clear on transitions (geometry
  # changes there), cursor-home redraws otherwise (no flicker).
  if [ "$state" != "$last" ]; then
    printf '\033[2J'; last="$state"; hist_in=(); hist_out=()
  fi

  case "$mode" in
    listen) push_hist hist_in "$(read_level "$RUNTIME_PATH/mic-level")" ;;
    speak) push_hist hist_out "$(read_level "$RUNTIME_PATH/voice-level")" ;;
  esac

  printf '\033[H'
  line "$BAR"
  title_line
  blank_line
  case "$mode" in
    listen) wave_rows hist_in "$G" ;;
    speak) wave_rows hist_out "$P" ;;
    heard) noise_rows "$C" ;;
    think) # pink pulse: triangle 0..3 over 16 frames
      ph=$(( frame % 16 )); [ "$ph" -gt 8 ] && ph=$(( 16 - ph ))
      flat_rows "$P" $(( ph * 3 / 8 )) ;;
    act) sweep_rows ;;
    calm) # cyan breathing: slow triangle 0..2 over 32 frames
      ph=$(( frame % 32 )); [ "$ph" -gt 16 ] && ph=$(( 32 - ph ))
      flat_rows "$C" $(( ph * 2 / 16 )) ;;
  esac
  blank_line
  sub_line
  blank_line
  line "$BAR"
  frame=$((frame + 1))
  sleep 0.12
done
