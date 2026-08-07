#!/usr/bin/env bash
# nslock
#   - by rav3ndust (xvoidsx)
# like i3lock-fancy, but for wayland
# specifically written for naviOS
# requires: grim, imagemagick (magick), swaylock, fonts-noto
set -euo pipefail
###############################################################
PINK="#ff10f0"
GREEN="#39ff14"
CYAN="#00ffff"
RED="#ff3131"
WHITE="#ffffff"
quotes=(
  "Let's all love Lain."
  "Present day. Present time."
  "If you aren't remembered, then you never existed."
  "What isn't remembered never happened. Memory is merely a record. You need to re-write that record."
  "No matter where you go, everyone's connected."
  "When you're connected, you're never alone."
  "Existence and will. The rest is mere data."
  "God is here."
)
QUOTE="${quotes[$((RANDOM % ${#quotes[@]}))]}"
QUOTE_WRAPPED="$(printf '%s' "$QUOTE" | fold -s -w 46)"
tmp="$(mktemp -d /tmp/nslock2.XXXXXX)"
trap 'rm -rf "$tmp"' EXIT
face="$tmp/face.png"
# capture, then build the whole face in a single magick pass.
# the glow is blurred at half resolution and upscaled (blur = low-frequency,
# so it survives the upscale identically); text is redrawn crisp at full res.
# full-res coords: clock +0+80, date +0+204, label +0-132,
# quote +0+170, attribution +0+132.
grim "$tmp/screen.png"
magick "$tmp/screen.png" \
  -resize 50% -evaluate multiply 0.88 -blur 0x2 \
  -font "Noto-Sans-Bold" -pointsize 48 -fill "$GREEN" \
  -gravity North -annotate +0+40 "$(date +%H:%M)" \
  -region 340x80+%[fx:w/2-170]+0 -blur 0x5 +region \
  -resize 200% \
  -font "Noto-Sans-Bold" -pointsize 96 -fill "$PINK" \
  -gravity North -annotate +0+80 "$(date +%H:%M)" \
  -font "Noto-Sans-Regular" -pointsize 20 -fill "$CYAN" \
  -gravity North -annotate +0+204 "$(date +%A\ %d\ %B\ %Y)" \
  -font "Noto-Sans-Bold" -pointsize 18 -fill "$GREEN" \
  -gravity Center -annotate +0-132 "type your password to return:" \
  -font "Noto-Sans-Regular" -pointsize 22 -fill "$WHITE" \
  -gravity South -annotate +0+170 "$QUOTE_WRAPPED" \
  -font "Noto-Sans-Italic" -pointsize 12 -fill "$PINK" \
  -gravity South -annotate +0+132 "- serial experiments lain" \
  "$face"
# lock
exec swaylock \
  --image "$face" \
  --scaling stretch \
  --indicator-idle-visible \
  --indicator-radius 70 \
  --indicator-thickness 8 \
  --font "Noto Sans" \
  --font-size 20 \
  --ring-color "$PINK" \
  --ring-ver-color "$GREEN" \
  --ring-wrong-color "$RED" \
  --ring-clear-color "$CYAN" \
  --ring-caps-lock-color "$PINK" \
  --inside-color "#00000000" \
  --inside-ver-color "#39ff1418" \
  --inside-wrong-color "#ff313118" \
  --inside-clear-color "#00ffff18" \
  --line-color "#00000000" \
  --separator-color "#00000000" \
  --text-color "$WHITE" \
  --text-ver-color "$GREEN" \
  --text-wrong-color "$RED" \
  --key-hl-color "$GREEN" \
  --bs-hl-color "$RED" \
  --caps-lock-key-hl-color "$PINK" \
  --daemonize
