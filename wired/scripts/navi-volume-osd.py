#!/usr/bin/env python3
"""navi-volume-osd — bottom-center volume meter for the wired.

Usage: navi-volume-osd.py --show <0-100> <0|1-muted>

Single-flight over a unix socket: if a meter is already up it is updated in
place (and its hide timer reset); otherwise a detached server is spawned.
The window floats itself via swaymsg (no shipped-config rule needed) and
lands bottom-center of the focused output.

The server is a persistent single-flight daemon, so replacing this file
(navi-update) must also retire the running server: every client first
checks the pidfile's code id against its own and SIGTERMs a stale
server (pre-pidfile servers are found via /proc), so the fresh code
always serves the next keypress. No manual pkill after updates.

Why not dunst: dunst rules cannot override `origin` (verified against
upstream docs), so a per-notification bottom-center meter is impossible with
stock dunst. This owns the 80 lines instead.
"""
import json
import math
import os
import signal
import socket
import subprocess
import sys
import time

SOCK = os.path.join(os.environ.get("XDG_RUNTIME_DIR", "/tmp"),
                    "navi-volume-osd.sock")
PIDFILE = os.path.join(os.environ.get("XDG_RUNTIME_DIR", "/tmp"),
                       "navi-volume-osd.pid")
APP_ID = "navi-volume-osd"
W, H = 380, 104
HIDE_MS = 1200


def on_osd_draw(win, cr):
    # Paint the window background ourselves: with set_app_paintable(True)
    # the CSS background-color never fills, so without this the OSD is
    # transparent and only the labels/bar show. Rounded near-black fill
    # with a 1px neon-pink border (grey while muted).
    w, h = win.get_allocated_width(), win.get_allocated_height()
    r = 14
    cr.new_sub_path()
    cr.arc(w - r, r, r - 1, -math.pi / 2, 0)
    cr.arc(w - r, h - r, r - 1, 0, math.pi / 2)
    cr.arc(r, h - r, r - 1, math.pi / 2, math.pi)
    cr.arc(r, r, r - 1, math.pi, 3 * math.pi / 2)
    cr.close_path()
    cr.set_source_rgba(0.047, 0.047, 0.067, 0.94)  # #0c0c11 @ 94%
    cr.fill_preserve()
    if win.get_style_context().has_class("muted"):
        cr.set_source_rgb(0x5b / 255, 0x5b / 255, 0x66 / 255)
    else:
        cr.set_source_rgb(1.0, 0x10 / 255, 0xf0 / 255)
    cr.set_line_width(1)
    cr.stroke()
    return False


def _code_id():
    # Identifies this script's revision. After navi-update replaces the
    # file, a server still holding the socket is stale when its id differs.
    try:
        return str(int(os.path.getmtime(__file__)))
    except OSError:
        return "0"


def _is_osd_cmdline(cmdline):
    # True when a /proc/<pid>/cmdline belongs to an OSD process: python
    # executing navi-volume-osd.py. An editor merely viewing the file does
    # not match (argv[0] wouldn't be python).
    parts = cmdline.split()
    return (len(parts) >= 2
            and "python" in os.path.basename(parts[0])
            and parts[1].endswith("navi-volume-osd.py"))


def _osd_pids(exclude):
    # Live PIDs currently executing this script (never the caller).
    found = []
    try:
        for pid in os.listdir("/proc"):
            if not pid.isdigit() or int(pid) == exclude:
                continue
            try:
                with open(f"/proc/{pid}/cmdline", "rb") as f:
                    cmd = f.read().replace(b"\0", b" ").decode(errors="replace")
            except OSError:
                continue
            if _is_osd_cmdline(cmd):
                found.append(int(pid))
    except OSError:
        pass
    return found


def ensure_fresh_server():
    """Retire a stale OSD server before talking to the socket.

    The OSD server persists across navi-update, so without this the old
    code keeps serving from the old socket forever. Cases:
      - pidfile code id matches ours and the pid is a live OSD: nothing.
      - pidfile code id differs (stale): SIGTERM that exact pid, but only
        after confirming via /proc it really is an OSD (pids get recycled).
      - no pidfile (pre-pidfile server, or a crashed one): scan /proc for
        OSD processes and retire those; never touch anything else.
    Afterwards any leftover socket/pidfile is unlinked so the fresh server
    can bind. A wedged server that ignores SIGTERM gets SIGKILL.
    """
    me = os.getpid()
    my_code = _code_id()
    live = set(_osd_pids(exclude=me))
    stale_pid = None
    try:
        with open(PIDFILE) as f:
            bits = f.read().strip().split()
        if len(bits) == 2:
            stale_pid = int(bits[0])
            if bits[1] == my_code and stale_pid in live:
                return  # current server confirmed alive; fast path
    except (OSError, ValueError):
        stale_pid = None
    targets = []
    if stale_pid is not None:
        if stale_pid in live:
            targets.append(stale_pid)  # stale code, confirmed OSD
        # else: dead or recycled pid — clean up files, never kill it
    else:
        targets.extend(live)  # no pidfile: pre-pidfile server, if any
    for p in targets:
        try:
            os.kill(p, signal.SIGTERM)
        except OSError:
            pass
    deadline = time.time() + 1.0
    while time.time() < deadline:
        if not os.path.exists(SOCK) and not any(
                str(p) in os.listdir("/proc") for p in targets):
            break
        time.sleep(0.1)
    else:
        for p in targets:  # wedged: ignored SIGTERM
            try:
                os.kill(p, signal.SIGKILL)
            except OSError:
                pass
        time.sleep(0.2)
    for p in (SOCK, PIDFILE):
        try:
            os.unlink(p)
        except OSError:
            pass


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
    try:
        srv.bind(SOCK)
    except OSError:
        # Lost a race with a concurrently spawning server: hand the
        # request to whoever holds the socket instead of dying.
        srv.close()
        send_to_server(vol, muted)
        return
    with open(PIDFILE, "w") as f:
        f.write(f"{os.getpid()} {_code_id()}\n")
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
    # The CSS background cannot paint: set_app_paintable makes the window
    # fully transparent and GTK CSS won't fill it. Request an RGBA visual
    # and paint the rounded near-black background + 1px border ourselves.
    rgba = Gdk.Screen.get_default().get_rgba_visual()
    if rgba is not None:
        win.set_visual(rgba)
    win.set_app_paintable(True)
    win.connect("draw", on_osd_draw)

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
        #navi-volume-osd label { color: #f2ecf5; font-family: "Noto Sans", sans-serif; }
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
        cmd = (f'[app_id="{APP_ID}"] floating enable, border none, '
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
        win.queue_draw()  # repaint the Cairo background (border color follows mute)
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
        for p in (SOCK, PIDFILE):
            try:
                os.unlink(p)
            except OSError:
                pass
        Gtk.main_quit()

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
    ensure_fresh_server()  # retire a stale daemon so the new code serves
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
