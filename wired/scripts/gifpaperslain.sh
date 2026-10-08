#!/usr/bin/env bash
# gifpaperslain
#     by rav3ndust.xyz (xvoidsx)
# A simple way to set animated wallpapers in navi
# Wayland backend: 'mpvpaper'. X11 backend: 'xwinwrap' + 'mpv'.
# Uses 'zenity' for the picker on both.
set -e
x="gifpaperslain"

# Session detection: X11 when XDG_SESSION_TYPE says so, or when there's a
# DISPLAY but no Wayland display to speak of.
is_x11() {
  [ "${XDG_SESSION_TYPE:-}" = "x11" ] || { [ -n "${DISPLAY:-}" ] && [ -z "${WAYLAND_DISPLAY:-}" ]; }
}

# force wayland env vars (Wayland sessions only)
if ! is_x11 && [ -z "$WAYLAND_DISPLAY" ]; then
	export WAYLAND_DISPLAY="wayland-0"
fi
notifier () {
	notify-send -u low -t 2000 --transient "$x" "Displaying available animated wallpaper selections."
}
kill_last_instance () {
	# kills the last gifpaper, whichever backend served it
	killall mpvpaper 2>/dev/null || true
	killall xwinwrap 2>/dev/null || true
}
main () {
	notifier
	gifpaper=$(zenity --file-selection --title "$x | Select an animated wallpaper:" --filename="$HOME/wiredWM/wp/gifpaperslain/")
	if [ -z "$gifpaper" ]; then
		exit 0
	fi
	kill_last_instance
	if is_x11; then
		# X11: xwinwrap pins mpv to the desktop layer, behind everything.
		# (-ov: override-redirect, -ni: no input, -nf: no focus,
		#  -b: below, -s: sticky, -st/-sp: skip taskbar/pager, -fs: fullscreen)
		xwinwrap -ov -ni -nf -b -s -st -sp -fs -- mpv -wid WID --loop --no-audio "$gifpaper" >/dev/null 2>&1 &
	else
		# Wayland: mpvpaper on the layer shell, all outputs.
		mpvpaper ALL -o "loop panscan=1" "$gifpaper"
	fi
}
main
