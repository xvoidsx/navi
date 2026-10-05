#!/usr/bin/env bash
# navi-fetch — fastfetch with a living, sparkling ∅ emblem
#     by rav3ndust.xyz (xvoidsx)
#
# The emblem shimmers and sparkles continuously until you press a key.
# Everything except the logo comes from your own fastfetch config.
# Falls back to plain fastfetch when output isn't a terminal.
set -u

if [ ! -t 1 ]; then
  exec fastfetch "$@"
fi
command -v fastfetch >/dev/null 2>&1 || { echo "navi-fetch: fastfetch not installed" >&2; exit 1; }

# color slots: $1 shimmer band, $2 ring, $3 slashes, $4 sparkles
LOGO_ARGS=(
  --logo-color-1 light_magenta --logo-color-2 magenta
  --logo-color-3 cyan --logo-color-4 yellow)

FRAMES=(
$'$1            ██████              \n$1         ████    ████           \n$2        ███      $3╱╱$2███          \n$2        ██    $3╱╱$2    ██          \n$2        ███$3╱╱$2      ███          \n$2         ████    ████           \n$2            ██████              '
$'$2            ██████              \n$1         ████    ████           \n$1        ███      $3╱╱$1███          \n$2        ██    $3╱╱$2    ██          \n$2        ███$3╱╱$2      ███          \n$2         ████    ████           \n$2            ██████              '
$'$2            ██████              \n$2         ████    ████           \n$1        ███      $3╱╱$1███          \n$1        ██    $3╱╱$1    ██          \n$2        ███$3╱╱$2      ███          \n$2         ████    ████           \n$2            ██████              '
$'$2            ██████              \n$2         ████    ████           \n$2        ███      $3╱╱$2███          \n$1        ██    $3╱╱$1    ██          \n$1        ███$3╱╱$1      ███          \n$2         ████    ████           \n$2            ██████              '
$'$2            ██████              \n$2         ████    ████           \n$2        ███      $3╱╱$2███          \n$2        ██    $3╱╱$2    ██          \n$1        ███$3╱╱$1      ███          \n$1         ████    ████           \n$2            ██████              '
$'$2            ██████              \n$2         ████    ████           \n$2        ███      $3╱╱$2███          \n$2        ██    $3╱╱$2    ██          \n$2        ███$3╱╱$2      ███          \n$1         ████    ████           \n$1            ██████              '
$'$1  $4✦$1         ██████              \n$1         ████    ████           \n$2        ███      $3╱╱$2███          \n$2        ██    $3╱╱$2    ██          \n$2        ███$3╱╱$2      ███          \n$2         ████    ████       $4⋆$2   \n$2            ██████              '
$'$2            ██████              \n$1         ████    ████         $4✧$1 \n$1        ███      $3╱╱$1███          \n$2        ██    $3╱╱$2    ██          \n$2        ███$3╱╱$2      ███          \n$2         ████    ████           \n$2 $4✦$2          ██████              '
$'$2            ██████              \n$2         ████    ████           \n$1$4⋆$1       ███      $3╱╱$1███          \n$1        ██    $3╱╱$1    ██          \n$2        ███$3╱╱$2      ███       $4✦$2  \n$2         ████    ████           \n$2            ██████              '
$'$2            ██████        $4⋆$2     \n$2         ████    ████           \n$2        ███      $3╱╱$2███          \n$1        ██    $3╱╱$1    ██         $4✦$1\n$1        ███$3╱╱$1      ███          \n$2         ████    ████           \n$2            ██████              '
$'$2            ██████              \n$2 $4✧$2       ████    ████           \n$2        ███      $3╱╱$2███          \n$2        ██    $3╱╱$2    ██          \n$1        ███$3╱╱$1      ███          \n$1$4✦$1        ████    ████           \n$2            ██████              '
$'$2            ██████              \n$2         ████    ████           \n$2        ███      $3╱╱$2███         $4✦$2\n$2        ██    $3╱╱$2    ██          \n$2        ███$3╱╱$2      ███          \n$1         ████    ████           \n$1            ██████           $4⋆$1  '
)

SETTLE=$'$2            ██████\n$2         ████    ████\n$2        ███      $3╱╱$2███\n$2        ██    $3╱╱$2    ██\n$2        ███$3╱╱$2      ███\n$2         ████    ████\n$2            ██████'

cleanup() { printf '\e[?25h\n'; }
trap cleanup EXIT INT TERM

printf '\e[?25l'  # hide the cursor for the show
printf '\e[s'     # save cursor position

# sparkle forever until a keypress — read -t doubles as the frame timer
while true; do
  for frame in "${FRAMES[@]}"; do
    fastfetch "${LOGO_ARGS[@]}" --logo "$frame" "$@"
    if read -t 0.12 -rsn1 _key 2>/dev/null; then
      printf '\e[u'  # back to the top for the settle frame
      fastfetch "${LOGO_ARGS[@]}" --logo "$SETTLE" "$@"
      printf '\n'
      exit 0
    fi
    printf '\e[u'  # back to the top — next frame overwrites in place
  done
done
