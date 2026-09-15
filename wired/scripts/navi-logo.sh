#!/usr/bin/env bash
# navi-logo: the empty-set mark of navi.
# usage: navi-logo [--animate] [--loop]   (default: static mark)
set -u
LINES=7
FRAMES=(
  $'            ██████\n         ████    ████\n        ███      ╱╱███\n        ██    ╱╱    ██\n        ███╱╱      ███\n         ████    ████\n            ██████'
  $'            ██████\n         ████    ╱╱██\n        ███     ╱╱ ███\n        ██    ╱╱    ██\n        ███ ╱╱     ███\n         ██╱╱    ████\n            ██████'
  $'            ██████\n         ████   ╱╱███\n        ███    ╱╱  ███\n        ██    ╱╱    ██\n        ███  ╱╱    ███\n         ███╱╱   ████\n            ██████'
  $'            ██████\n         ████  ╱ ████\n        ███    ╱   ███\n        ██    ╱╱    ██\n        ███   ╱    ███\n         ████ ╱  ████\n            ██████'
  $'            ██████\n         ████ ╱  ████\n        ███   ╱    ███\n        ██    ╱╱    ██\n        ███    ╱   ███\n         ████  ╱ ████\n            ██████'
  $'            ██████\n         ███╱╱   ████\n        ███  ╱╱    ███\n        ██    ╱╱    ██\n        ███    ╱╱  ███\n         ████   ╱╱███\n            ██████'
  $'            ██████\n         ██╱╱    ████\n        ███ ╱╱     ███\n        ██    ╱╱    ██\n        ███     ╱╱ ███\n         ████    ╱╱██\n            ██████'
  $'            ██████\n         ████    ████\n        ███╱╱      ███\n        ██    ╱╱    ██\n        ███      ╱╱███\n         ████    ████\n            ██████'
  $'            ██████\n         ████    ████\n        ██╱╱       ███\n        ██   ╱╱╱╱   ██\n        ███       ╱╱██\n         ████    ████\n            ██████'
  $'            ██████\n         ████    ████\n        ███        ███\n        █╱╱╱╱╱╱╱╱╱╱╱╱█\n        ███        ███\n         ████    ████\n            ██████'
  $'            ██████\n         ████    ████\n        ███        ███\n        ██╱╱╱╱╱╱╱╱╱╱██\n        ███        ███\n         ████    ████\n            ██████'
  $'            ██████\n         ████    ████\n        ███       ╱╱██\n        ██   ╱╱╱╱   ██\n        ██╱╱       ███\n         ████    ████\n            ██████'
)
STATIC=$'            ██████\n         ████    ████\n        ███      ╱╱███\n        ██    ╱╱    ██\n        ███╱╱      ███\n         ████    ████\n            ██████'
draw() { printf '%s\n' "$1"; }
up() { tput cuu "$LINES" 2>/dev/null || true; }
animate() {
  tput civis 2>/dev/null || true
  trap 'tput cnorm 2>/dev/null || true' EXIT
  draw "${FRAMES[0]}"
  for _ in 1 2; do for f in "${FRAMES[@]}"; do up; draw "$f"; sleep 0.09; done; done
  up; draw "$STATIC"
  tput cnorm 2>/dev/null || true
}
loop_animate() {
  tput civis 2>/dev/null || true
  trap 'tput cnorm 2>/dev/null || true' EXIT
  draw "${FRAMES[0]}"
  while true; do for f in "${FRAMES[@]}"; do up; draw "$f"; sleep 0.09; done; done
}
case "${1:-}" in
  # static is the mika default; the animation comes back in eiri
  --animate) animate ;;
  --loop) loop_animate ;;
  *) draw "$STATIC" ;;
esac
