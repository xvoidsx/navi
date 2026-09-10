# the navi login screen

SDDM with the `navi` QML theme: nightshade styling, the ∅ mark, 24-hour
clock, `DD Month` dates, and a session picker offering **navi (Wayland)**
and **navi (X11)**.

## background

Static: `/usr/share/navi/wired/wp/lain3wp.jpg` (shipped with the repo).

Animated (optional): the theme plays
`/usr/share/navi/wired/wp/login-loop.mp4` on a loop over the static
image when the file exists. Generate it from the flagship gifpaper:

```sh
ffmpeg -i navi-lain.gif -movflags +faststart -pix_fmt yuv420p \
  /usr/share/navi/wired/wp/login-loop.mp4
```

Requires `qml6-module-qtmultimedia` (installed by `install.sh`).

## files

- `wired/sddm/navi.conf` → `/etc/sddm.conf.d/navi.conf`
- `wired/sddm/themes/navi/` → `/usr/share/sddm/themes/navi/`
- `wired/sessions/navi.desktop` → `/usr/share/wayland-sessions/navi.desktop`
- `wired/sessions/navi-x11.desktop` → `/usr/share/xsessions/navi.desktop`
