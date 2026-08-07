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
# capture desktop img 
grim "$tmp/screen.png"
# imagemagick stuff/fade bg
magick "$tmp/screen.png" \
  -evaluate multiply 0.75 \
  \( +clone -crop x12+0+0 -scale 103%x100% -geometry +10+0 \) -composite \
  \( +clone -crop x24+0+0 -scale 97%x100% -geometry -15+0 \) -composite \
  \( +clone -crop x8+0+0 -scale 105%x100% -geometry +25+0 \) -composite \
  -scale 50% -scale 200% \
  \( +clone -font "Noto-Sans-Bold" -pointsize 96 -fill "$GREEN" \
     -gravity North -annotate +0+80 "$(date +%H:%M)" \
     -font "Noto-Sans-Regular" -pointsize 20 -fill "$CYAN" \
     -gravity North -annotate +0+204 "$(date +%A\ %d\ %B\ %Y)" \
     -font "Noto-Sans-Bold" -pointsize 18 -fill "$GREEN" \
     -gravity Center -annotate +0-132 "type your password to return:" \
     -blur 0x12 \) -composite \
  -font "Noto-Sans-Bold" -pointsize 96 -fill "$PINK" \
  -gravity North -annotate +0+80 "$(date +%H:%M)" \
  -font "Noto-Sans-Regular" -pointsize 20 -fill "$CYAN" \
  -gravity North -annotate +0+204 "$(date +%A\ %d\ %B\ %Y)" \
  -font "Noto-Sans-Bold" -pointsize 18 -fill "$GREEN" \
  -gravity Center -annotate +0-132 "type your password to return:" \
  -font "Noto-Sans-Regular" -pointsize 22 -fill "rgba(255,255,255,0.65)" \
  -gravity South -annotate +0+170 "$QUOTE_WRAPPED" \
  -font "Noto-Sans-Italic" -pointsize 12 -fill "rgba(255,16,240,0.60)" \
  -gravity South -annotate +0+132 "- serial experiments lain" \
  -background black -vignette 0x30 \
  "$face"
# lock
exec swaylock \
  --image "$face" \
  --scaling stretch \
  --indicator-idle-visible \
  --indicator-radius 75 \
  --indicator-thickness 10 \
  --font "Noto Sans" \
  --font-size 22 \
  --ring-color "$PINK" \
  --ring-ver-color "$CYAN" \
  --ring-wrong-color "$RED" \
  --ring-clear-color "$GREEN" \
  --ring-caps-lock-color "$PINK" \
  --inside-color "#00000040" \
  --inside-ver-color "#00ffff22" \
  --inside-wrong-color "#ff313122" \
  --inside-clear-color "#39ff1422" \
  --line-color "#00000000" \
  --separator-color "#00000000" \
  --text-color "$WHITE" \
  --text-ver-color "$CYAN" \
  --text-wrong-color "$RED" \
  --text-clear-color "$GREEN" \
  --key-hl-color "$CYAN" \
  --bs-hl-color "$RED" \
  --caps-lock-key-hl-color "$PINK" \
  --daemonize
