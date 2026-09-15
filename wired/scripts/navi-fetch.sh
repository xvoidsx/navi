#!/usr/bin/env bash
# navi-fetch — fastfetch with an animated shimmer ∅ emblem
#     by rav3ndust.xyz (xvoidsx)
#
# Sweeps a band of light across navi's empty-set emblem a couple of times,
# then settles on the static gradient. Everything except the logo comes from
# your own fastfetch config — only the emblem is overridden per frame.
# Falls back to plain fastfetch when output isn't a terminal.
set -u

if [ ! -t 1 ]; then
  exec fastfetch "$@"
fi
command -v fastfetch >/dev/null 2>&1 || { echo "navi-fetch: fastfetch not installed" >&2; exit 1; }

# color slots: $1 shimmer band, $2 ring, $3 slashes, $4 settle shading
# (slot colors are set with --logo-color-N below)
FRAMES=(
$'$1            ██████\n$1         ████    ████\n$2        ███      $3╱╱$2███\n$2        ██    $3╱╱$2    ██\n$2        ███$3╱╱$2      ███\n$2         ████    ████\n$2            ██████'
$'$2            ██████\n$1         ████    ████\n$1        ███      $3╱╱$1███\n$2        ██    $3╱╱$2    ██\n$2        ███$3╱╱$2      ███\n$2         ████    ████\n$2            ██████'
$'$2            ██████\n$2         ████    ████\n$1        ███      $3╱╱$1███\n$1        ██    $3╱╱$1    ██\n$2        ███$3╱╱$2      ███\n$2         ████    ████\n$2            ██████'
$'$2            ██████\n$2         ████    ████\n$2        ███      $3╱╱$2███\n$1        ██    $3╱╱$1    ██\n$1        ███$3╱╱$1      ███\n$2         ████    ████\n$2            ██████'
$'$2            ██████\n$2         ████    ████\n$2        ███      $3╱╱$2███\n$2        ██    $3╱╱$2    ██\n$1        ███$3╱╱$1      ███\n$1         ████    ████\n$2            ██████'
$'$2            ██████\n$2         ████    ████\n$2        ███      $3╱╱$2███\n$2        ██    $3╱╱$2    ██\n$2        ███$3╱╱$2      ███\n$1         ████    ████\n$1            ██████'
)
# the settle frame matches navi's default fastfetch emblem: magenta ring, cyan slashes
SETTLE=$'$2            ██████\n$2         ████    ████\n$2        ███      $3╱╱$2███\n$2        ██    $3╱╱$2    ██\n$2        ███$3╱╱$2      ███\n$2         ████    ████\n$2            ██████'

LOGO_ARGS=(--pipe false --logo-type data
  --logo-color-1 light_magenta --logo-color-2 magenta
  --logo-color-3 cyan)

cleanup() { printf '\e[?25h'; }
trap cleanup EXIT INT TERM

printf '\e[?25l'  # hide the cursor for the show
printf '\e[s'     # save cursor position

for ((sweep=0; sweep<2; sweep++)); do
  for frame in "${FRAMES[@]}"; do
    fastfetch "${LOGO_ARGS[@]}" --logo "$frame" "$@"
    sleep 0.09
    printf '\e[u'  # back to the top — next frame overwrites in place
  done
done
fastfetch "${LOGO_ARGS[@]}" --logo "$SETTLE" "$@"

printf '\e[?25h'  # cursor back on
trap - EXIT INT TERM
