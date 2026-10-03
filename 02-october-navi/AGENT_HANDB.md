# AGENT HANDOFF — Waybar performance, theme system, and Bluetooth

**Session date:** 2026-10-02
**Machine:** ThinkPad T440p, Intel Haswell iGPU, 1366×768 @ 60Hz, 3.5 GB RAM, zram swap
**Stack:** waybar 0.12.0 · GTK 3.24.49 · sway 1.10.1 · Debian trixie · PipeWire/WirePlumber 0.5.8
**Repo:** `~/Documents/navi` (branch `eiri`, base commit `f57b234`)

**Starting premise was wrong.** The session began with "waybar is using a lot of
memory." It was not — 28 MB PSS, 9 MB private heap. The real defect was **CPU**
(87.7% of a core, continuously) plus collateral damage. Memory work below is
real but minor.

---

## TL;DR for the agent

1. Waybar burned **87.7% of a core** forever. Now **~10–14%**, depending on theme.
2. Five waybar themes built; a `navi-theme` picker lets users switch with live preview.
   **patchbay is the shipped default** (§5).
3. **waybar autostart was silently broken** — the packaged unit cannot start in a
   sddm session. Fixed with a full user unit (§4.1). Without this, "persist on
   login" is meaningless.
4. **weather.sh** emitted `U+FE0F`, which made GTK draw a white box instead of the glyph. Fixed.
5. **mpvpaper** now auto-pauses on true occlusion only, so gaps keep the gifpapers animating.
6. **sway keyring line was silently broken** — sway's lexer splits on `,`.
7. **blueman retired** (never worked on Wayland, held 77 MB), replaced by waybar's `bluetooth` module.
8. `libspa-0.2-bluetooth` needed a wireplumber restart to load.

---

## 1. The core finding — read this before touching waybar CSS

GTK3 does **no partial damage** on this setup. Any animated CSS property
repaints the entire bar — all Pango text re-rendered — at **~15 ms/frame**.
Cost is therefore priced per *visible change*, and is **independent** of
what is animated, how many widgets, or how complex the visuals.

Measured (waybar CPU as % of one core):

| Configuration | CPU |
|---|---|
| No animation at all | **0.2%** |
| steps, ~1 change/sec | ~18% |
| Any continuous 60 Hz animation | **~90%** |

Variants all measured ~85–90%, proving the cost is flat:
smooth+all-shadows 89.9 · smooth+window-static 89.3 · trivial 1-layer shadow
85.6 · only `border-color` animated 90.1 · only 3 modules animated 89.7 ·
**completely flat bar, solid bg, no shadows 86.9**.

The `steps(N, end)` cost curve is near-linear in steps-per-second:

| Timing | CPU |
|---|---|
| `steps(4)` over 7 s (0.57 chg/s) | 11.8% |
| `steps(8)` over 7 s (1.14) | 21.3% |
| `steps(16)` over 7 s (2.29) | 39.1% |
| `steps(32)` over 7 s (4.57) | 61.1% |

**Consequences, all already applied:**
- A *static* `box-shadow` still costs per-repaint if its widget repaints. Making
  the shadow static is worthless on its own — the widget must stop repainting.
- Colour steps must be **low amplitude**. Full neon→neon deltas make stepping
  read as a strobe; halving the swing makes the same timing read as a shimmer.
- Budget is best spent on **rare, deliberate events** (a packet crossing a trace
  every 16 s) over constant motion.

Frame clock was confirmed correctly vsync-locked (~60 Hz, verified by context
switch rate), so this is not a throttling bug. Waybar 0.12 is GTK3/cairo:
software rasterization, no GPU path.

---

## 2. Files to add to the repo (none of these exist yet)

| New file | Suggested repo path |
|---|---|
| `navi-theme` (tool, ~250 lines bash) | `scripts/navi-theme` → installed `/usr/bin` |
| `patchbay/theme.css` + `meta` | `configs/core/waybar/themes/patchbay/` |
| `neon-tube/` | `configs/core/waybar/themes/neon-tube/` |
| `crt-scope/` | `configs/core/waybar/themes/crt-scope/` |
| `jack-panel/` | `configs/core/waybar/themes/jack-panel/` |
| `refined-obsidian/` | `configs/core/waybar/themes/refined-obsidian/` |
| `navi-theme.rasi` (rofi picker skin) | `configs/core/rofi/navi-theme.rasi` |
| `waybar.service` (autostart replacement) | `configs/core/systemd/waybar.service` → `~/.config/systemd/user/` |

One directory per theme (not a flat CSS file) **specifically so a theme can later
carry its own `config.json`** — see §7.

`meta` format is trivial key=value:
```
name=Patchbay
description=Signal trace and packet comet; sockets soldered onto a live wire.
```

**Install layout** the tool expects (user overrides system):
```
~/.local/share/navi/waybar/themes/<name>/theme.css   user  (wins)
/usr/share/navi/waybar/themes/<name>/theme.css      system (distro)
```

---

## 3. Two payload fixes — apply these to the repo

Neither is committed. Exact diffs were generated from the live box.

### `wired/waybar/weather.sh`

```diff
-json_escape() { python3 -c 'import json,sys; print(json.dumps(sys.stdin.read().strip()))' <<<"$1"; }
+# U+FE0F (VARIATION SELECTOR-16) forces emoji *presentation* on whatever follows
+# it. Pango honours that by demanding a real colour-emoji font, and the only one
+# installed here (Noto Color Emoji) is a fixed 109px strike that cannot be used at
+# bar size — so GTK painted the .notdef box instead of the glyph, and the weather
+# module showed a white square next to the temperature.
+#
+# The condition glyphs are fine on their own: every font on the box (Noto Sans,
+# Noto Sans Symbols2, DejaVu, the Nerd Fonts) carries U+1F32B and friends as
+# clean monochrome glyphs that read correctly at 10px. Dropping the selector
+# leaves each codepoint at its default text presentation, which is what we want
+# in a 10px bar anyway.
+#
+# Stripped in json_escape so the fix covers text, tooltip and every other
+# consumer of this module at once, present and future.
+json_escape() {
+  python3 -c 'import json,sys; print(json.dumps(sys.stdin.read().replace("️","").strip()))' <<<"$1"
+}
```

The `.replace()` argument is a literal U+FE0F. **Verify the byte is present in
the committed file** — it is invisible in most editors.

### `wired/scripts/navi-wallpaper.sh`

```diff
-  mpvpaper ALL -o "loop panscan=1" "$ANIMATED" >>"$LOG" 2>&1 &
+  # -p (--auto-pause) with NO -a/--auto-mode on purpose.
+  #
+  # mpvpaper has two different triggers and they are not interchangeable:
+  #   -p alone          fires on TRUE OCCLUSION (the layer-shell surface is
+  #                     genuinely covered)
+  #   -a full|max|active extends that to fire merely because a window is
+  #                     MAXIMIZED, whether or not it actually covers anything
+  # We run with gaps (inner 6 / outer 9), so a maximized tiled window leaves the
+  # wallpaper visible through the gaps and the surface is never occluded —
+  # `-p` therefore stays silent for maximized windows and the gifpapers keep
+  # animating, which is the whole point. It still engages for genuine fullscreen
+  # (an mpv video covers everything), where nobody can see the wallpaper.
+  # Passing -a max here would freeze every gifpaper behind any maximized window.
+  #
+  # no-audio is defensive only: the gifpapers carry no audio track and mpvpaper
+  # opens no /dev/snd or pulse fd. It documents intent and costs nothing.
+  #
+  # Deliberately NOT capping the frame rate: mpv's --fps is documented as a
+  # testing flag that overrides output rate rather than throttling it, and the
+  # gifpapers' frame timing is an intentional part of how they look.
+  mpvpaper ALL -p -o "loop panscan=1 no-audio" "$ANIMATED" >>"$LOG" 2>&1 &
```

**`-a` must never be used here.** It would freeze every gifpaper behind any
maximized window, because gaps mean the wallpaper is never truly occluded.

---

## 4. Config changes

### `configs/core/sway/config`

1. **Keyring** — was silently broken. Sway's config lexer treats `,` as a token
   separator, so `--components=pkcs11,secrets,ssh` split into three tokens and
   sway aborted the line with `Unknown/invalid command 'secrets'`. The daemon
   never launched, so `SSH_AUTH_SOCK`/`GNOME_KEYRING_CONTROL` were never exported.
   Fixed with quotes + `exec_always` so it also runs on reload:
   ```
   exec_always --no-startup-id sh -c "gnome-keyring-daemon --start --components=pkcs11,secrets,ssh"
   ```
2. **blueman autostart commented out** (line ~104) with rationale in comments.
3. **New binding:** `bindsym Mod1+t exec --no-startup-id "navi-theme"`
   (Mod1+t was verified free.)

### `configs/core/waybar/config.jsonc`

4. `"bluetooth"` added to `modules-right` (between `network` and `battery`), plus
   a documented module block. See §6 for the waybar-0.12 crash workaround it must keep.

---

## 4.1 Waybar autostart — was silently broken

**This is the reason "make patchbay persist on login" needed real work.** The
packaged `/usr/lib/systemd/user/waybar.service` carries:

```
Requisite=graphical-session.target
PartOf=graphical-session.target
WantedBy=graphical-session.target
```

`graphical-session.target` is **never activated** in a plain sddm-launched
Wayland session — `navi.desktop` just execs `sway`, so nothing pulls that target
in. `Requisite=` therefore makes the unit fail to start **silently**: no error, no
bar. Confirmed empirically — the unit's `ExecMainStartTimestamp=` was empty, i.e.
it had **never once run** during the session, and waybar only ever appeared
because something else happened to launch it.

**Do not try to fix this with a drop-in.** A drop-in was written and tested
first; systemd **cannot remove dependencies** in a drop-in, only add them, so
`Requisite=` / `PartOf=` resets are silently ignored. This was verified with
`systemctl --user show`.

The fix is a **complete user unit** at `~/.config/systemd/user/waybar.service`,
which replaces the vendor unit outright (same-name user units win completely):

```ini
[Unit]
Description=Waybar (navi)
After=graphical-session.target

[Service]
Type=simple
ExecStart=/usr/bin/waybar -b bar-0
ExecReload=/bin/kill -SIGUSR2 $MAINPID
Restart=on-failure
RestartSec=2

[Install]
WantedBy=default.target
```

`After=` is kept so ordering still behaves if the target is ever activated.
`Restart=on-failure` is deliberate: a malformed theme can take the whole bar down,
and the user should not be left with no bar at all.

**Verified:** starts clean with no bar running · repeated `start` is a singleton,
no duplicate · **auto-recovers from `SIGKILL`**. Also remove any stale
`~/.config/systemd/user/graphical-session.target.wants/waybar.service` symlink an
earlier `enable` may have left behind.

**Do not launch waybar from the sway config instead.** That was tried and is a
trap: sway re-runs plain `exec` lines on `swaymsg reload` in this version, and it
produced a duplicate bar every reload — the `pgrep` guard did not prevent it. A
systemd unit is a proper singleton and gets restart-on-failure for free.

---

## 5. patchbay is the shipped default

`configs/core/waybar/style.css` **is** the patchbay theme, byte-identical to
`configs/core/waybar/themes/patchbay/theme.css`. That single fact is what makes
the default persist: waybar has no `"style"` key in `config.jsonc`, so it reads
`~/.config/waybar/style.css`, and a fresh install — or a re-install that
overwrites the config — lands on patchbay.

`navi-theme` derives the active theme by comparing `style.css` against the theme
store, so there is no separate state file to drift out of sync, and no login hook
is needed to "restore" anything: whatever is in `style.css` simply comes back.

The other four are opt-in via `navi-theme` / `Mod1+T`.

---

---

## 5.1 The five themes

Per-theme CPU measured live, 30 s samples, zero module errors each:

| Theme | CPU | Character |
|---|---|---|
| **patchbay** (default, active) | 9.8–13.8% | full-width signal trace, sockets on 1px solder stems, packet comet every 16 s, pure-black bottom |
| **jack-panel** | 10.9% | rack plate, patch cable, jack sockets with dark bores; state by ring colour |
| **crt-scope** | 11.7% | JetBrainsMono NF, phosphor green, static scanlines, block cursor blinking on the clock |
| **neon-tube** | 14.3% | bent glass, filament, rare failing-tube flicker (3 dropouts / 20 s) |
| **refined-obsidian** | 14.5% | the original obsidian glass, corrected — closest to previous identity |

All five share the same state-class vocabulary (`agent-asleep/ready/working/
listening/blocked`, `has-notifs`, `no-notifs`, `up-to-date`, `updates-available`,
`battery.critical/warning`, `bluetooth.off/connected/connecting`,
`custom-volume.muted`, `idle_inhibitor.activated`, `nowplaying.*`).

**No theme may animate `box-shadow`.** All five animate only colour, or a
1px `border-bottom`. Revisit §1 before adding any motion.

---

## 6. Platform gotchas discovered (do not regress these)

**GTK3 CSS**
- Comma selector lists inside `@keyframes` are **rejected outright**
  (`Expected closing bracket after keyframes block`). Write every percentage as
  its own block. This broke two themes during authoring.
- `steps(1, end)` skips intermediate keyframes entirely — a flicker built with it
  never fires. Use `steps(N)` where N is fine enough to land on the keyframe
  percentages you need.
- `transparent` is `rgba(0,0,0,0)`. Fading a colour **to** it interpolates
  through black and leaves a dark fringe — this read as a "persistent black line"
  under the trace. Always fade to a **zero-alpha copy of the colour's own hue**.
- `repeating-linear-gradient` needs ≥2 colour stops.
- Comma-separated `animation:` on one element works.
- `background-position` (multi-layer) **is** animatable — the packet uses it.
  Background *layer colours* are not interpolable; that's why the trace is a real
  animated `border-bottom`.

**Fonts / emoji**
- `U+FE0F` forces emoji presentation and can produce tofu at small sizes (§3).
- `Noto Color Emoji` is a fixed **109 px strike** — never name it in a 10 px font
  stack; it downscales to mush or fails entirely.
- The themes list `"Noto Sans Symbols2"` for monochrome fallback; Noto Sans's
  `.notdef` for these codepoints is an empty box.

**rofi / rasi**
- `-theme <file>` **replaces** config.rasi entirely — it does not merge. Every
  widget the launcher styles must be styled in the picker theme too, or it renders
  in rofi's light default. **`mainbox` is the one that bites**: it wraps the input
  bar and the list, so omitting it paints a white panel behind every row.
- Theme variables go **inside a `* { }` block** as `name: value;`. A top-level
  `@name: value;` is a parse error, as is `@define-color`.
- `#` cannot begin a line — rofi reads it as a colour.
- `border:` takes a unitless width; colour goes in `border-color:`.
- `box-shadow` is unsupported.
- Alternates are `element alternate`, not `element-alternate`.
- `-theme-str` is all-or-nothing: one bad line and rofi rejects the whole flag
  (this is what made `navi-theme` look broken).
- **rofi has no `<img>` tag.** Image support is the *icon* system — thumbnails
  must go through the NUL-separated icon field: `label\0/icon/path.png` with `-i`.

**waybar 0.12 Bluetooth crash**
`{device_count}`, `{device_enumerate}`, `{device_alias}`, `{device_address}` and
`{status_alias}` all **hard-crash the entire bar** ("argument not found") when no
device is paired. `{icon}` and `{status}` are safe. State is expressed with
literal per-state format strings instead. Revisit once a device is paired or
waybar moves past 0.12.0.

**waybar SIGUSR2** reload is the correct live-reload mechanism (`ExecReload`).
But a bad theme can kill the bar outright — the `navi-theme` tool therefore
validates CSS with a Gtk.CssProvider before installing, and can restore via
`systemctl --user start waybar`. Note that unit requires `graphical-session.target`,
which is **inactive** in a plain sddm-launched session, so it will not start;
launch manually with `WAYLAND_DISPLAY`/`DISPLAY`/`SWAYSOCK`/`XDG_RUNTIME_DIR`
exported and `setsid`.

---

## 7. Open question deliberately deferred

**One `config.jsonc` shared by all five themes, for now.** The agent should think
about per-theme JSON, but note the trap: modules are not cosmetic. Every custom
mod drives state classes, so a theme that drops or renames a module silently
loses its CSS. Worth a checklist rather than five divergent JSON files.

Also noted: `mods/navi-bluetooth` **already exists** — a Go TUI that replaces
blueman-applet, and it imports `github.com/rav3ndust/navi-theme`. The waybar
`bluetooth` module is the *status indicator*; `navi-bluetooth` is the *manager*.
The module's `on-click` currently points at `blueman-manager` and **should be
repointed at `navi-bluetooth`** once that mod ships to users. It is not yet
referenced from `configs/core/waybar/config.jsonc`.

---

## 8. Bluetooth

- `libspa-0.2-bluetooth` installed. **It does not take effect until wireplumber
  restarts** — wireplumber builds its module graph at boot, when the plugin was
  still missing. Symptom before restart:
  `s-monitors: PipeWire's BlueZ MIDI SPA missing or broken. Bluetooth not supported.`
  After `systemctl --user restart wireplumber` the BlueZ error is gone.
- **Consider adding `libspa-0.2-bluetooth` to navi's package deps** so users
  don't hit this.
- blueman retired: `systemctl --user mask blueman-applet.service`. Commenting the
  sway exec is **not** enough — the unit is `static`, i.e. D-Bus activatable, and
  it resurrected on demand anyway.
- No device paired yet, so no `bluez_card` appears and audio is untested.

---

## 9. Outstanding, not done

| Item | Status |
|---|---|
| Keyring `ssh` component | Config is correct, but the running daemon already holds `pkcs11,secrets` and won't add `ssh` retroactively. **Lands on next login.** Verify `SSH_AUTH_SOCK` afterwards. |
| DRM page-flip failures | **~19/min, unfixed.** `Atomic commit failed: Device or resource busy` on eDP-1. Independent of waybar (unchanged after a 7× CPU cut and with mpvpaper stopped). Dominated by mpvpaper vs. the wallpaper plane. Only known lever is capping mpvpaper's frame rate, which was rejected as it alters gifpaper timing. |
| Bluetooth audio | Untestable until a device is paired. |
| `/etc/dconf/db/site` + `distro` missing | Two-line warning per GTK app launch. `dconf` package is `un`. Cosmetic. |
| `xdg-desktop-portal-gtk` | Fails once at session handover (old Xwayland dies before new one exists), `Restart=no`, so it latches. ~0 ongoing cost. Sway needs `xdg-desktop-portal-wlr`. Ignore. |
| PipeWire ALSA "Broken pipe" | Historical. 6 lines in a 105 s window, **zero since**. Log noise only. |
| `swayfx` | Rejected for now. Not in Debian; depends on `scenefx` tracking wlroots; conflicts with sway; needs GLES2 or you get no desktop. Worth tracking: `layer_effects "waybar"` would give real GPU blur behind the bar and retire the fake glass. The CSS work here is correct either way. |

---

## 10. Verification commands

```bash
# waybar CPU as % of a core over N seconds
W=$(pgrep -x waybar | head -1)
a=$(awk '{print $14+$15}' /proc/$W/stat); sleep 30
b=$(awk '{print $14+$15}' /proc/$W/stat)
echo "scale=1; ($b-$a)/30" | bc

# validate a theme before shipping it (catches comma keyframe selectors)
python3 - <<'EOF'
import sys, gi; gi.require_version("Gtk","3.0")
from gi.repository import Gtk
errs=[]; p=Gtk.CssProvider()
p.connect("parsing-error", lambda a,b,c: errs.append(c.message))
try: p.load_from_path(sys.argv[1])
except Exception as e: errs.append(str(e))
print("\n".join(errs) if errs else "OK")
EOF

# confirm the weather module emits no VS16
/usr/share/navi/wired/waybar/weather.sh | head -1 | grep -c fe0f   # want 0

# page-flip failure rate
grep -c 'Atomic commit failed' ~/.local/share/sddm/wayland-session.log
```

---

## 11. Do not reintroduce these regressions

1. **A blanket `transition` on the universal `*` selector.** `transition: 240ms ease`
   expands to `transition-property: all`, so GTK builds a transition for every
   property change on every widget *and fights the animations on the same nodes*.
2. **`text-shadow: currentColor` on anything with an animated `color`.** GTK
   re-blurs every glyph shadow every frame, and the halo shifts hue with the text.
   Use a static near-white.
3. **Animating `box-shadow`.** Full-surface Gaussian blur, every frame, forever.
4. **`mpvpaper -a`.** Freezes the gifpapers behind any maximized window (§3).
5. **`sed -i 's/"x": true/"x": false/'` on a whole config file.** A global
   substitution during this session stripped `tooltip` from four unrelated waybar
   modules and injected a stray `tooltip-format` into modules whose resolver has
   no such placeholder, which broke four custom mods at once. Scope edits to one
   block and re-validate.
6. **Global `~/.config/mpv/mpv.conf`.** `no-audio` would kill audio in every
   video; `fps=30` would silently cap playback framerate. Scope mpv options to
   mpvpaper's `-o`.
7. **A rofi picker theme that doesn't style `mainbox`.** Renders white.

---

## 12. Cleanup before committing

These exist only on the live box and are **not** part of the handoff:

```
~/.config/waybar/config-{patchbay,neon-tube,crt-scope,jack-panel,refined-obsidian}   superseded by the theme store
~/.config/waybar/style.css.{bak,nightshade-v2,steps-themev2,broken,pre-serve}
~/.config/waybar/style-wire.css        early patchbay, pre black-line fix
~/.config/waybar/config.jsonc.bak / .nightshade-v2 / .broken
~/.config/sway/config.bak-opencode
```

`style.css.nightshade-v2` and `style.css.steps-themev2` are byte-identical
duplicates (17907 B each) — keep at most one.

Backups that are worth keeping on the box, not the repo:
`style.css.nightshade-v2` is the last state before the redesign.

---

*Delete this file once the changes are landed.*