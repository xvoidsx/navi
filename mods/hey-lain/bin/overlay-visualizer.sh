#!/usr/bin/env bash
# Minimal, focus-safe nightshadeNeon visualizer rendered inside foot.
# Arguments: runtime directory. State is read from overlay-state.
set -uo pipefail

RUNTIME_PATH="${1:?runtime directory required}"
P=$'\033[38;2;255;16;240m'   # neon pink
G=$'\033[38;2;57;255;20m'    # neon green
C=$'\033[38;2;0;255;255m'    # neon cyan
W=$'\033[97m'
D=$'\033[2m'
R=$'\033[0m'
# Deliberately ASCII-only: some terminal/font combinations render block and
# box-drawing Unicode as mojibake. These characters remain clean everywhere.
GLYPHS=" .:-=+*#%@"
SPIN="|/-\\"
frame=0
last=""

printf '\033[?1049h\033[?25l'
trap 'printf "\033[?1049l\033[?25h\033[0m"; exit' INT TERM EXIT

meter() {
  local mode="$1" k height color out="" phase
  for k in $(seq 0 31); do
    phase=$((frame + k * 3))
    case "$mode" in
      listen) height=$(( (phase * phase + k * 7) % 8 )); color="$G"; [ "$height" -ge 5 ] && color="$C"; [ "$height" -ge 7 ] && color="$P" ;;
      speak) height=$(( (phase * 5 + k * k + frame / 2) % 8 )); color="$P"; [ "$height" -ge 4 ] && color="$C" ;;
      think) height=$(( (phase + k) % 5 )); color="$P" ;;
      act) height=$(( (phase * 2 + k * 5) % 8 )); color="$G"; [ "$height" -ge 6 ] && color="$P" ;;
      calm) height=$(( (frame / 3 + k) % 3 + 1 )); color="$C" ;;
      *) height=1; color="$W" ;;
    esac
    out="${out}${color}${GLYPHS:$height:1}${R}"
  done
  printf '%s' "$out"
}

centerpiece() {
  local mode="$1" n
  case "$mode" in
    listen) printf '%s' "${G}[ o o o o o ]${R}" ;;
    speak) printf '%s' "${P}[${C}*${P}] [${C}*${P}] [${C}*${P}]${R}" ;;
    think) printf '%s' "${P}${SPIN:frame%10:1}${R}" ;;
    act) printf '%s' "${P}[${G}*${P}]${G} [! ] ${P}[${G}*${P}]${R}" ;;
    calm) n=$((frame / 4 % 3)); [ "$n" -eq 0 ] && printf '%s' "${C}[ . ]${R}" || printf '%s' "${C}[ o ]${R}" ;;
    *) printf '%s' "${W}[ . ]${R}" ;;
  esac
}

line() {
  printf '\033[2K%s\n' "$1"
}

while true; do
  state=$(cat "$RUNTIME_PATH/overlay-state" 2>/dev/null || echo LISTENING)
  case "$state" in
    LISTENING) title="Lain is listening..."; subtitle="Press Alt+V again to send"; mode=listen; accent="$G" ;;
    HEARD) title="Lain heard you..."; subtitle="turning your voice into intent"; mode=think; accent="$C" ;;
    THINKING) title="Lain is thinking..."; subtitle="finding the best next move"; mode=think; accent="$P" ;;
    ACTING) title="Lain is taking action!"; subtitle="working with your desktop"; mode=act; accent="$G" ;;
    SPEAKING) title="Lain says..."; subtitle="Press Alt+V to talk back"; mode=speak; accent="$P" ;;
    WAITING) title="Lain is waiting..."; subtitle="Press Alt+V to talk to me"; mode=calm; accent="$C" ;;
    *) title="Lain is ready..."; subtitle="Press Alt+V to begin"; mode=calm; accent="$C" ;;
  esac

  [ "$state" != "$last" ] && printf '\033[2J' && last="$state"
  printf '\033[H'
  line "${G}  +--------------------------------------------------+${R}"
  line "${G}  |${R}        ${accent}*  ${W}${title}${R}        ${accent}*${R}         ${G}|${R}"
  line "${G}  |${R}                                                    ${G}|${R}"
  line "${G}  |${R}        $(centerpiece "$mode")                        ${G}|${R}"
  line "${G}  |${R}   $(meter "$mode")   ${G}|${R}"
  line "${G}  |${R}   $(meter "$mode")   ${G}|${R}"
  line "${G}  |${R}   $(meter "$mode")   ${G}|${R}"
  line "${G}  |${R}                                                    ${G}|${R}"
  line "${G}  |${R}        ${D}${subtitle}${R}                         ${G}|${R}"
  line "${G}  |${R}                                                    ${G}|${R}"
  line "${G}  +--------------------------------------------------+${R}"
  frame=$((frame + 1))
  sleep 0.12
done
