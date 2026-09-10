# wp/ — navi wallpapers

Static wallpapers live here. The animated collection lives in `gifpaperslain/`.

These are binary assets — they ship with the repo via git, not in this draft.
Source: `wiredWM` repo (`next` branch), `wp/` directory.

First boot: `sway/config` runs `/usr/bin/navi-wallpaper`, which tries the
animated default (`gifpaperslain/navi-lain.gif` via mpvpaper) first, verifies
it is actually running, and only falls back to the static `lain3wp.jpg` via
swaybg if mpvpaper fails. The two never run at once.
