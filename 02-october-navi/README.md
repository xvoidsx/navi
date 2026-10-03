# 02-october-navi — waybar performance, theme system, Bluetooth

Self-contained bundle of everything changed on **2026-10-02**, ready to land in
the navi repo and the next ISO.

**Read [`AGENT_HANDB.md`](AGENT_HANDB.md) before committing.** It records the
measurements, the platform gotchas that cost the most time, and seven specific
regressions not to reintroduce. This file only tells you what is in the box and
how to deploy it.

---

## Quick start

```bash
./apply.sh --dry-run      # show what would change, touch nothing
./apply.sh                # repo + this machine
./apply.sh --repo-only    # just the git repo
./apply.sh --system-only  # just this machine (needs root for /usr/share/navi)
```

Overridable: `NAVI_REPO=/path/to/navi ./apply.sh`

Everything overwritten is backed up first as `<file>.bak-<timestamp>`.

---

## Layout

```
02-october-navi/
├── AGENT_HANDB.md          the handoff — measurements, gotchas, open items
├── apply.sh                one-shot deployer (--dry-run / --repo-only / --system-only)
├── README.md               this file
└── payload/
    ├── repo/               final desired state, mirroring repo paths
    └── system/             same payload files, for direct deploy to /usr/share/navi
```

`payload/repo/` is a **copy, not a patch** — each file is the finished version.
`apply.sh` therefore works on a clean checkout; it does not re-apply diffs.

`payload/system/` holds the two `/usr/share/navi` scripts under flat names,
since their destination has no analogue in the repo layout.

---

## What lands where

| Bundle path | Lands at | Note |
|---|---|---|
| `payload/repo/wired/waybar/weather.sh` | `wired/waybar/weather.sh` | **fix** — strips `U+FE0F` |
| `payload/repo/wired/scripts/navi-wallpaper.sh` | `wired/scripts/navi-wallpaper.sh` | **fix** — `mpvpaper -p`, no `-a` |
| `payload/repo/configs/core/waybar/config.jsonc` | `configs/core/waybar/config.jsonc` | + `bluetooth` module |
| `payload/repo/configs/core/waybar/style.css` | `configs/core/waybar/style.css` | **is** the patchbay theme — this is the shipped default |
| `payload/repo/configs/core/waybar/themes/*/` | same | 5 themes, `theme.css` + `meta` each |
| `payload/repo/configs/core/rofi/navi-theme.rasi` | `configs/core/rofi/navi-theme.rasi` | new picker skin |
| `payload/repo/configs/core/sway/config` | `configs/core/sway/config` | keyring fix, blueman off, `Mod1+t` |
| `payload/repo/configs/core/systemd/waybar.service` | `configs/core/systemd/waybar.service` | **autostart fix** — replaces the packaged unit |
| `payload/repo/scripts/navi-theme` | `scripts/navi-theme` → `/usr/bin` | the picker tool |

Runtime installs performed by `apply.sh --system-only`:

| To | What |
|---|---|
| `~/.config/waybar/{config.jsonc,style.css}` | active config + default theme |
| `~/.config/rofi/navi-theme.rasi` | picker skin |
| `~/.config/sway/config` | bindings + keyring |
| `~/.local/bin/navi-theme` | the tool |
| `~/.local/share/navi/waybar/themes/` | all 5 themes |
| `~/.config/systemd/user/waybar.service` | **enabled + started** (see below) |
| user systemd | `blueman-applet.service` masked |

---

## patchbay is the default, and why it now actually persists

`configs/core/waybar/style.css` **is** the patchbay theme (byte-identical to
`themes/patchbay/theme.css`). That is the whole persistence mechanism: waybar has
no `"style"` key in `config.jsonc`, so it reads `~/.config/waybar/style.css`, and
a fresh install — or a re-install that overwrites the config — lands on patchbay.
`navi-theme` derives the active theme by comparing `style.css` against the theme
store, so there is no state file to drift and no login hook is needed.

**But the bar was not reliably starting at all.** The packaged
`waybar.service` carries `Requisite=graphical-session.target`, and that target is
never activated in a plain sddm session (`navi.desktop` just execs `sway`), so the
unit failed to start *silently* — confirmed by an empty `ExecMainStartTimestamp`,
meaning it had never once run. Hence the replacement unit in the payload: a full
user unit (not a drop-in — systemd can only *add* dependencies in drop-ins, never
remove them) that hooks into `default.target`, with `Restart=on-failure` so a bad
theme can't leave the user with no bar.

Verified: starts clean · repeated `start` is a singleton · auto-recovers from
`SIGKILL`.

The other four themes are opt-in via `navi-theme` / `Mod1+T`.

---

## Verifying after deploy

```bash
# 1. every theme must parse — GTK rejects comma keyframe selectors outright
for t in ~/.local/share/navi/waybar/themes/*/theme.css; do
  python3 - "$t" <<'EOF'
import sys, gi; gi.require_version("Gtk","3.0")
from gi.repository import Gtk
e=[]; p=Gtk.CssProvider()
p.connect("parsing-error", lambda a,b,c: e.append(c.message))
try: p.load_from_path(sys.argv[1])
except Exception as ex: e.append(str(ex))
print(("FAIL " if e else "  ok ")+sys.argv[1].split("/")[-2]+("" if not e else " "+str(e)))
EOF
done

# 2. waybar CPU as % of one core over 30s  (baseline was 87.7%, expect ~10-14%)
W=$(pgrep -x waybar | head -1)
a=$(awk '{print $14+$15}' /proc/$W/stat); sleep 30
b=$(awk '{print $14+$15}' /proc/$W/stat)
echo "scale=1; ($b-$a)/30" | bc

# 3. weather module emits no VS16  (want 0)
navi-weather 2>/dev/null || /usr/share/navi/wired/waybar/weather.sh | head -1 | grep -c fe0f

# 4. picker launches without theme errors  (want 0)
navi-theme list
rofi -theme ~/.config/rofi/navi-theme.rasi -dmenu -p t -kb-custom-1 exit </dev/null 2>&1 \
  | grep -c 'failed to parse'
```

**Expected:** themes `ok`, waybar **10–14%** (was 87.7%), VS16 count `0`, rofi
parse errors `0`.

---

## Three things that need a human

`apply.sh` deliberately does **not** do these; it prints them instead.

1. **`sudo apt install libspa-0.2-bluetooth` then
   `systemctl --user restart wireplumber`.** WirePlumber builds its module graph
   at boot, so installing the plugin mid-session does nothing until restart.
   Consider adding the package to navi's deps so users never hit this.
2. **`navi-theme shot`** — regenerates picker screenshots (switches themes,
   captures, restores).
3. **Next login** — the keyring's `ssh` component only appears then; gnome-keyring
   will not add a component to a running daemon. Then `swaymsg reload` to
   register `Mod1+t`.

---

## Also worth doing before merge

The waybar `bluetooth` module's `on-click` still points at `blueman-manager`, but
`mods/navi-bluetooth` already exists in the repo and is the intended replacement
(a Go TUI that replaces the applet). Repoint it once that mod ships to users.

---

## Not in this bundle

Working files and backups that exist only on this machine and should **not** be
committed — listed in `AGENT_HANDB.md` §12. Note that
`style.css.nightshade-v2` and `style.css.steps-themev2` in
`~/.config/waybar/` are byte-identical duplicates; keep at most one.