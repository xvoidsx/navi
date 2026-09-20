#!/usr/bin/env python3
"""navi-switcher — Windows-style Alt+Tab grid for sway, with thumbnails.

Modes:
  --capture DIR   silently grim each output, crop per-window thumbnails with
                  PIL (rects from swaymsg -t get_tree, times output scale),
                  write DIR/manifest.json
  --show DIR      GTK grid over the manifest; single-flight via unix socket
                  ("next" advances the selection instead of a second instance)
  --next          tell a running switcher to advance one tile

Wayland hands clients no per-window pixels, so thumbnails are cropped out of
whole-output screenshots. Only *visible* windows get live thumbnails (tiled
crops are pixel-perfect; floating occluders may bleed through — accepted);
everything else gets icon + title + workspace tiles. /tmp dir is rm -rf'd
on dismiss/select.

Rect semantics (verified against sway's ipc-json.c): node "rect" is in
output-layout coordinates, so crop = (rect - output_rect) * output_scale.
"""
import json
import math
import os
import shutil
import socket
import subprocess
import sys

SOCK = os.path.join(os.environ.get("XDG_RUNTIME_DIR", "/tmp"),
                    "navi-switcher.sock")
APP_ID = "navi-switcher"
THUMB_MAX = 320  # longest thumbnail edge, px


def swaymsg(*args):
    p = subprocess.run(["swaymsg", "-t", *args], capture_output=True,
                       text=True, timeout=15)
    if p.returncode != 0:
        raise RuntimeError("swaymsg -t %s failed: %s"
                           % (" ".join(args), p.stderr.strip()))
    return json.loads(p.stdout)


def is_window(node):
    return bool(node.get("app_id") or node.get("window_properties"))


def app_of(node):
    return (node.get("app_id")
            or (node.get("window_properties") or {}).get("class")
            or "?")


def collect(node, ctx, windows, top_id=None):
    """Walk a workspace subtree; append one entry per window con."""
    for child in list(node.get("nodes", [])) + list(node.get("floating_nodes", [])):
        if child.get("type") != "con":
            continue
        tid = top_id if top_id is not None else child.get("id")
        if is_window(child):
            r = child.get("rect") or {}
            entry = {
                "id": child.get("id"),
                "title": child.get("name") or "?",
                "app": app_of(child),
                "workspace": ctx["ws_name"],
                "output": ctx["out_name"],
                "visible": ctx["visible"],
                "thumb": None,
                "_top": tid,
                "_mru": ctx["focus"].index(tid) if tid in ctx["focus"] else 999,
                "_focused": bool(child.get("focused")),
                "_rect": r,
            }
            shot = ctx["shots"].get(ctx["out_name"])
            if ctx["visible"] and shot is not None and r.get("width") and r.get("height"):
                scale = ctx["scale"]
                ox, oy = ctx["out_rect"].get("x", 0), ctx["out_rect"].get("y", 0)
                x = int((r["x"] - ox) * scale)
                y = int((r["y"] - oy) * scale)
                w = int(r["width"] * scale)
                h = int(r["height"] * scale)
                x, y = max(0, x), max(0, y)
                w, h = min(w, shot.width - x), min(h, shot.height - y)
                if w > 16 and h > 16:
                    entry["thumb"] = (x, y, w, h)  # resolved to a file below
            windows.append(entry)
        else:
            collect(child, ctx, windows, tid)


def build_manifest(tree, shots, outdir):
    """Pure-ish core: tree JSON + {output: PIL image|None} -> manifest dict.

    Separated from grim/swaymsg so the rect math is unit-testable.
    """
    from PIL import Image  # noqa: F401  (kept local: import cost only when used)
    windows = []
    for out in tree.get("nodes", []):
        if out.get("type") != "output" or out.get("name") == "__i3_scratch":
            continue
        out_name = out.get("name", "?")
        try:
            scale = float(out.get("scale") or 1)
        except (TypeError, ValueError):
            scale = 1
        ctx_base = {
            "out_name": out_name,
            "out_rect": out.get("rect", {}),
            "scale": scale,
            "shots": shots,
        }
        for ws in out.get("nodes", []):
            if ws.get("type") != "workspace":
                continue
            ctx = dict(ctx_base,
                       ws_name=ws.get("name", "?"),
                       visible=bool(ws.get("visible")),
                       focus=ws.get("focus", []) or [])
            collect(ws, ctx, windows)

    # Order: focused workspace first (its windows in MRU order), then the rest.
    focused_ws = next((w["workspace"] for w in windows if w["_focused"]), None)
    windows.sort(key=lambda w: (0 if w["workspace"] == focused_ws else 1,
                                w["_mru"]))

    from PIL import Image
    manifest_windows = []
    for w in windows:
        box = w.pop("thumb")
        w.pop("_top"); w.pop("_mru"); w.pop("_focused"); w.pop("_rect")
        w["thumb"] = None
        if isinstance(box, tuple) and w["visible"]:
            shot = shots[w["output"]]
            x, y, bw, bh = box
            thumb = shot.crop((x, y, x + bw, y + bh))
            thumb.thumbnail((THUMB_MAX, THUMB_MAX), Image.LANCZOS)
            fname = f"thumb-{w['id']}.png"
            thumb.save(os.path.join(outdir, fname))
            w["thumb"] = fname
        manifest_windows.append(w)
    manifest = {"windows": manifest_windows}
    with open(os.path.join(outdir, "manifest.json"), "w") as f:
        json.dump(manifest, f)
    return manifest


def capture(outdir):
    """grim each output, then build the manifest. Returns manifest dict."""
    tree = swaymsg("get_tree")
    try:
        outputs_info = {o["name"]: o for o in swaymsg("get_outputs")}
    except RuntimeError:
        outputs_info = {}
    # backfill scale from get_outputs if the tree lacks it
    for out in tree.get("nodes", []):
        if out.get("type") == "output" and not out.get("scale"):
            out["scale"] = outputs_info.get(out.get("name"), {}).get("scale", 1)

    shots = {}
    for out in tree.get("nodes", []):
        if out.get("type") != "output" or out.get("name") == "__i3_scratch":
            continue
        name = out.get("name", "?")
        path = os.path.join(outdir, f"shot-{name}.png")
        try:
            subprocess.run(["grim", "-o", name, path], check=True,
                           capture_output=True, timeout=20)
            from PIL import Image
            shots[name] = Image.open(path)
        except Exception as e:
            print(f"navi-switcher: grim failed for {name}: {e}",
                  file=sys.stderr)
            shots[name] = None
    return build_manifest(tree, shots, outdir)


# ------------------------------------------------------------------ UI

def send_next(direction="next"):
    try:
        s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        s.settimeout(0.5)
        s.connect(SOCK)
        s.sendall((direction + "\n").encode())
        s.close()
        return True
    except OSError:
        return False


def show(outdir):
    import gi
    gi.require_version("Gtk", "3.0")
    gi.require_version("Gdk", "3.0")
    gi.require_version("GdkPixbuf", "2.0")
    gi.require_version("Pango", "1.0")
    from gi.repository import Gtk, Gdk, GdkPixbuf, GLib, Pango
    GLib.set_prgname(APP_ID)

    with open(os.path.join(outdir, "manifest.json")) as f:
        windows = json.load(f)["windows"]
    if not windows:
        shutil.rmtree(outdir, ignore_errors=True)
        return 0

    try:
        os.unlink(SOCK)
    except OSError:
        pass
    srv = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    srv.bind(SOCK)
    srv.listen(5)
    srv.setblocking(False)

    n = len(windows)
    cols = min(n, 4)
    rows = math.ceil(n / cols)
    sel = [1 if n >= 2 else 0]  # MRU: index 1 is the previously focused window

    win = Gtk.Window()
    win.set_name("navi-switcher")
    win.set_title("switch")
    win.set_decorated(False)
    win.set_resizable(False)
    win.set_skip_taskbar_hint(True)
    win.set_skip_pager_hint(True)
    win.set_keep_above(True)
    win.set_type_hint(Gdk.WindowTypeHint.DIALOG)

    css = Gtk.CssProvider()
    css.load_from_data(b"""
        #navi-switcher { background-color: rgba(11, 11, 14, 0.96);
                         border: 1px solid #ff10f0; border-radius: 14px; }
        #navi-switcher .tile { background-color: #141419;
                               border: 1px solid #2a2a33; border-radius: 10px;
                               padding: 8px; }
        #navi-switcher .tile.selected { border-color: #ff10f0;
                                        background-color: #1e1224; }
        #navi-switcher .tile-title { color: #f2ecf5; font: 12px "Noto Sans", sans-serif; }
        #navi-switcher .tile-ws { color: #8a8a96; font: 10px "Noto Sans", sans-serif; }
        #navi-switcher .tile-app { color: #39ff14; font: 10px "Noto Sans", sans-serif; }
    """)
    Gtk.StyleContext.add_provider_for_screen(
        Gdk.Screen.get_default(), css,
        Gtk.STYLE_PROVIDER_PRIORITY_APPLICATION)

    grid = Gtk.Grid()
    grid.set_name("switch-grid")
    grid.set_row_spacing(10)
    grid.set_column_spacing(10)
    grid.set_border_width(16)
    grid.set_column_homogeneous(True)

    icon_theme = Gtk.IconTheme.get_default()
    tiles = []

    def tile_icon(app):
        for cand in (app, app.lower(), (app.split(".") or [app])[-1].lower()):
            pb = None
            try:
                info = icon_theme.lookup_icon(cand, 64, 0)
                if info:
                    pb = info.load_icon()
            except Exception:
                pb = None
            if pb:
                return pb
        return None

    for i, w in enumerate(windows):
        btn = Gtk.Button()
        btn.set_relief(Gtk.ReliefStyle.NONE)
        btn.get_style_context().add_class("tile")
        vb = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=6)
        if w.get("thumb"):
            try:
                pb = GdkPixbuf.Pixbuf.new_from_file_at_size(
                    os.path.join(outdir, w["thumb"]), 300, 170)
                img = Gtk.Image.new_from_pixbuf(pb)
            except Exception:
                img = Gtk.Label(label="\u25a6")
        else:
            pb = tile_icon(w["app"])
            img = (Gtk.Image.new_from_pixbuf(pb) if pb
                   else Gtk.Label(label="\u25a6"))
            try:
                img.set_pixel_size(64)
            except AttributeError:
                pass
        vb.pack_start(img, False, False, 0)
        t = Gtk.Label(label=w["title"][:42])
        t.get_style_context().add_class("tile-title")
        t.set_ellipsize(Pango.EllipsizeMode.END)
        t.set_max_width_chars(28)
        vb.pack_start(t, False, False, 0)
        sub = Gtk.Label(label=f"{w['app']}  ·  ws {w['workspace']}")
        sub.get_style_context().add_class("tile-ws")
        sub.set_ellipsize(Pango.EllipsizeMode.END)
        sub.set_max_width_chars(30)
        vb.pack_start(sub, False, False, 0)
        btn.add(vb)
        btn.connect("clicked", lambda _b, idx=i: activate(idx))
        grid.attach(btn, i % cols, i // cols, 1, 1)
        tiles.append(btn)

    win.add(grid)

    def mark():
        for i, t in enumerate(tiles):
            style = t.get_style_context()
            if i == sel[0]:
                style.add_class("selected")
            else:
                style.remove_class("selected")

    def advance(d):
        sel[0] = (sel[0] + d) % len(tiles)
        mark()

    def cleanup():
        try:
            os.unlink(SOCK)
        except OSError:
            pass
        shutil.rmtree(outdir, ignore_errors=True)
        Gtk.main_quit()

    def activate(i=None):
        w = windows[sel[0] if i is None else i]
        subprocess.run(["swaymsg", f"[con_id={w['id']}] focus"],
                       capture_output=True, timeout=5)
        cleanup()

    def on_key_press(_w, ev):
        k = ev.keyval
        if k in (Gdk.KEY_Tab, Gdk.KEY_Right, Gdk.KEY_Down):
            advance(1)
            return True
        if k in (Gdk.KEY_ISO_Left_Tab, Gdk.KEY_Left, Gdk.KEY_Up):
            advance(-1)
            return True
        if k in (Gdk.KEY_Return, Gdk.KEY_KP_Enter):
            activate()
            return True
        if k == Gdk.KEY_Escape:
            cleanup()
            return True
        return False

    def on_key_release(_w, ev):
        # Windows-style: releasing Alt confirms the selection.
        if ev.keyval in (Gdk.KEY_Alt_L, Gdk.KEY_Alt_R):
            activate()
            return True
        return False

    def on_focus_out(*_a):
        cleanup()
        return False

    def on_sock(_fd, _cond):
        try:
            conn, _ = srv.accept()
            data = conn.recv(16)
            conn.close()
            if b"prev" in data:
                advance(-1)
            elif b"next" in data:
                advance(1)
        except OSError:
            pass
        return True

    def float_self(attempts=10):
        # Own the float: runtime rule, no shipped-config reliance.
        # border none: the compositor must not draw its own frame around the
        # switcher's CSS border. The geometry is a tight upper bound on the
        # natural content size — oversizing leaves a dead band inside the
        # border that reads as a huge frame.
        wpx, wpx_h = 330 * cols + 30, 245 * rows + 30
        cmd = (f'[app_id="{APP_ID}"] floating enable, border none, '
               f"resize set {wpx} {wpx_h}, move position center")
        try:
            p = subprocess.run(["swaymsg", cmd], capture_output=True,
                               text=True, timeout=5)
            ok = p.returncode == 0 and '"success":true' in p.stdout.replace(" ", "")
        except Exception:
            ok = False
        if not ok and attempts > 1:
            GLib.timeout_add(90, float_self, attempts - 1)
        return False

    win.connect("key-press-event", on_key_press)
    win.connect("key-release-event", on_key_release)
    win.connect("focus-out-event", on_focus_out)
    win.connect("destroy", lambda *_a: cleanup())
    GLib.io_add_watch(srv.fileno(), GLib.IO_IN, on_sock)

    mark()
    win.show_all()
    win.present()
    GLib.timeout_add(80, float_self)
    Gtk.main()
    return 0


def main(argv):
    if len(argv) >= 2 and argv[1] == "--next":
        direction = argv[2] if len(argv) >= 3 and argv[2] == "prev" else "next"
        return 0 if send_next(direction) else 1
    if len(argv) == 3 and argv[1] == "--capture":
        manifest = capture(argv[2])
        print(f"navi-switcher: captured {len(manifest['windows'])} windows")
        return 0
    if len(argv) == 3 and argv[1] == "--show":
        return show(argv[2])
    print("usage: navi-switcher.py --capture DIR | --show DIR | --next",
          file=sys.stderr)
    return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv))
