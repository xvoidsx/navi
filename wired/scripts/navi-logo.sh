#!/usr/bin/env bash
# navi-logo: the empty-set mark of navi, animated.
# usage: navi-logo [--static] [--loop]
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
case "${1:-}" in
  --static) draw "$STATIC"; exit 0 ;;
esac
tput civis 2>/dev/null || true
trap 'tput cnorm 2>/dev/null || true' EXIT
if [ "${1:-}" = "--loop" ]; then
  draw "${FRAMES[0]}"
  while true; do for f in "${FRAMES[@]}"; do up; draw "$f"; sleep 0.09; done; done
else
  draw "${FRAMES[0]}"
  for _ in 1 2; do for f in "${FRAMES[@]}"; do up; draw "$f"; sleep 0.09; done; done
  up; draw "$STATIC"
fi
tput cnorm 2>/dev/null || true
