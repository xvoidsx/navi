#!/usr/bin/env python3
"""navi-volume-osd — bottom-center volume meter for the wired.

Usage: navi-volume-osd.py --show <0-100> <0|1-muted>

Single-flight over a unix socket: if a meter is already up it is updated in
place (and its hide timer reset); otherwise a detached server is spawned.
The window floats itself via swaymsg (no shipped-config rule needed) and
lands bottom-center of the focused output.

Why not dunst: dunst rules cannot override `origin` (verified against
upstream docs), so a per-notification bottom-center meter is impossible with
stock dunst. This owns the 80 lines instead.
"""
import json
import os
import socket
import subprocess
import sys

SOCK = os.path.join(os.environ.get("XDG_RUNTIME_DIR", "/tmp"),
                    "navi-volume-osd.sock")
APP_ID = "navi-volume-osd"
W, H = 380, 104
HIDE_MS = 1200


def send_to_server(vol, muted):
    try:
        s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        s.settimeout(0.5)
        s.connect(SOCK)
        s.sendall(f"{vol} {muted}\n".encode())
        s.close()
        return True
    except OSError:
        return False


def run_server(vol, muted):
    import gi
    gi.require_version("Gtk", "3.0")
    gi.require_version("Gdk", "3.0")
    from gi.repository import Gtk, Gdk, GLib
    GLib.set_prgname(APP_ID)

    try:
        os.unlink(SOCK)
    except OSError:
        pass
    srv = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    srv.bind(SOCK)
    srv.listen(5)
    srv.setblocking(False)

    win = Gtk.Window()
    win.set_name("navi-volume-osd")
    win.set_decorated(False)
    win.set_resizable(False)
    win.set_skip_taskbar_hint(True)
    win.set_skip_pager_hint(True)
    win.set_keep_above(True)
    win.set_type_hint(Gdk.WindowTypeHint.NOTIFICATION)
    win.set_app_paintable(True)

    box = Gtk.Box(orientation=Gtk.Orientation.HORIZONTAL, spacing=14)
    box.set_border_width(16)
    icon = Gtk.Label()
    icon.set_name("osd-icon")
    right = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=8)
    title = Gtk.Box(orientation=Gtk.Orientation.HORIZONTAL, spacing=8)
    name = Gtk.Label(label="Volume")
    name.set_name("osd-name")
    pct = Gtk.Label(label="--%")
    pct.set_name("osd-pct")
    pct.set_halign(Gtk.Align.END)
    pct.set_hexpand(True)
    title.pack_start(name, False, False, 0)
    title.pack_start(pct, True, True, 0)
    bar = Gtk.ProgressBar()
    bar.set_show_text(False)
    bar.set_name("osd-bar")
    right.pack_start(title, False, False, 0)
    right.pack_start(bar, False, False, 0)
    box.pack_start(icon, False, False, 0)
    box.pack_start(right, True, True, 0)
    win.add(box)

    css = Gtk.CssProvider()
    css.load_from_data(b"""
        #navi-volume-osd {
            background-color: rgba(12, 12, 17, 0.94);
            border: 1px solid #ff10f0;
            border-radius: 14px;
        }
        #navi-volume-osd.muted { border-color: #5b5b66; }
        #navi-volume-osd label { color: #f2ecf5; font-family: "Noto Sans"; }
        #osd-icon { font-size: 30px; }
        #osd-name { font-size: 13px; font-weight: bold; }
        #osd-pct { font-size: 13px; color: #39ff14; }
        #navi-volume-osd.muted #osd-pct { color: #8a8a96; }
        #navi-volume-osd progressbar trough {
            min-height: 16px; border-radius: 8px;
            background-color: #23232c; border: none;
        }
        #navi-volume-osd progressbar progress {
            min-height: 16px; border-radius: 8px;
            background-color: #ff10f0; border: none;
        }
        #navi-volume-osd.muted progressbar progress { background-color: #5b5b66; }
    """)
    Gtk.StyleContext.add_provider_for_screen(
        Gdk.Screen.get_default(), css,
        Gtk.STYLE_PROVIDER_PRIORITY_APPLICATION)

    def place(attempts=12):
        # Float + size + bottom-center, retried until the window maps.
        try:
            outs = json.loads(subprocess.run(
                ["swaymsg", "-t", "get_outputs"],
                capture_output=True, text=True, timeout=5).stdout)
            fo = next((o for o in outs if o.get("focused")), outs[0])
            r = fo.get("rect", {})
            ow, oh = r.get("width", 1920), r.get("height", 1080)
        except Exception:
            ow, oh = 1920, 1080
        x, y = (ow - W) // 2, oh - H - 64
        cmd = (f'[app_id="{APP_ID}"] floating enable, '
               f"resize set {W} {H}, move position {x} {y}")
        try:
            p = subprocess.run(["swaymsg", cmd], capture_output=True,
                               text=True, timeout=5)
            ok = p.returncode == 0 and '"success":true' in p.stdout.replace(" ", "")
        except Exception:
            ok = False
        if not ok and attempts > 1:
            GLib.timeout_add(80, place, attempts - 1)
        return False

    hide_id = []

    def hide():
        win.hide()
        hide_id.clear()
        return False

    def update(vol, is_muted):
        try:
            v = max(0, min(100, int(vol)))
        except (TypeError, ValueError):
            v = 0
        bar.set_fraction(v / 100.0)
        pct.set_text(f"{v}%")
        icon.set_text("\U0001f507" if is_muted else
                      ("\U0001f508" if v < 40 else "\U0001f50a"))
        style = win.get_style_context()
        if is_muted:
            style.add_class("muted")
        else:
            style.remove_class("muted")
        if not win.get_visible():
            win.show_all()
        win.present()
        for hid in hide_id:
            GLib.source_remove(hid)
        hide_id.clear()
        hide_id.append(GLib.timeout_add(HIDE_MS, hide))

    def on_msg(fd, _cond):
        try:
            conn, _ = srv.accept()
            parts = conn.recv(32).decode().strip().split()
            conn.close()
            if len(parts) >= 2:
                update(parts[0], parts[1] == "1")
        except OSError:
            pass
        return True

    GLib.io_add_watch(srv.fileno(), GLib.IO_IN, on_msg)

    def cleanup(*_a):
        try:
            os.unlink(SOCK)
        except OSError:
            pass
        Gtk.main_quit()

    import signal
    signal.signal(signal.SIGTERM, cleanup)
    win.connect("destroy", cleanup)

    GLib.timeout_add(60, place)
    update(vol, muted == "1")
    Gtk.main()


def main(argv):
    if len(argv) != 4 or argv[1] != "--show":
        print("usage: navi-volume-osd.py --show <0-100> <0|1>",
              file=sys.stderr)
        return 2
    vol, muted = argv[2], argv[3]
    if send_to_server(vol, muted):
        return 0
    try:
        import gi  # noqa: F401
    except ImportError:
        print("navi-volume-osd: python3-gi is not installed", file=sys.stderr)
        return 1
    # No server: detach and become it. The caller never blocks on us.
    if os.fork() > 0:
        return 0
    os.setsid()
    try:
        run_server(vol, muted)
    finally:
        try:
            os.unlink(SOCK)
        except OSError:
            pass
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
