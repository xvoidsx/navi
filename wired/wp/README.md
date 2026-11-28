# wp/ — navi wallpapers

Static wallpapers live here. The animated collection lives in `gifpaperslain/`.

These are binary assets — they ship with the repo via git, not in this draft.
Source: `wiredWM` repo (`next` branch), `wp/` directory.

First boot: `sway/config` runs `/usr/bin/navi-wallpaper`, which tries the
animated default (`gifpaperslain/neon-lain.gif` via mpvpaper) first,
verifies it is actually running (watching the log for EGL/context failures),
and only falls back to the static `neon-lain.png` (the still the gif is
built from) via swaybg if mpvpaper fails. The two never run at once.
