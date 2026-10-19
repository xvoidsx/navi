#!/usr/bin/env python3
"""navi walls — coverflow wallpaper picker.

Scans the shipped wallpapers (static + gifpapers), serves a coverflow UI
on localhost, and writes the user's pick to a file for the wrapper script.

Usage: navi-wallpaper-picker.py
Writes pick to: /tmp/navi-wallpaper-picked (or exits 1 on cancel)
"""
import http.server
import json
import os
import socketserver
import subprocess
import sys
import threading
import urllib.parse

SHARE = "/usr/share/navi/wired"
STATIC_DIR = os.path.join(SHARE, "wp")
GIF_DIR = os.path.join(SHARE, "wp", "gifpaperslain")
HERE = os.path.dirname(os.path.abspath(__file__))
PICK_FILE = "/tmp/navi-wallpaper-picked"
PORT = 0  # let the OS pick

STATIC_EXTS = {".jpg", ".jpeg", ".png", ".webp"}


def scan():
    walls = []
    # static wallpapers (top level of wp/, not the gifpaperslain subdir)
    if os.path.isdir(STATIC_DIR):
        for fn in sorted(os.listdir(STATIC_DIR)):
            p = os.path.join(STATIC_DIR, fn)
            if os.path.isfile(p) and os.path.splitext(fn)[1].lower() in STATIC_EXTS:
                walls.append({"path": "/w/static/" + fn, "name": fn,
                              "type": "static", "real": p, "dims": ""})
    # animated gifpapers
    if os.path.isdir(GIF_DIR):
        for fn in sorted(os.listdir(GIF_DIR)):
            if fn.lower().endswith(".gif"):
                p = os.path.join(GIF_DIR, fn)
                walls.append({"path": "/w/gif/" + fn, "name": fn,
                              "type": "animated", "real": p, "dims": ""})
    # fill in dimensions via `identify` if available (best effort)
    for w in walls:
        try:
            out = subprocess.run(["identify", "-format", "%wx%h", w["real"]],
                                 capture_output=True, text=True, timeout=5)
            if out.returncode == 0 and "x" in out.stdout:
                w["dims"] = out.stdout.strip()
        except Exception:
            pass
        if not w["dims"]:
            w["dims"] = "?"
    return walls


WALLS = scan()
BY_URL = {w["path"]: w["real"] for w in WALLS}
PICKED = []


class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def do_GET(self):
        parsed = urllib.parse.urlparse(self.path)
        if parsed.path == "/":
            with open(os.path.join(HERE, "index.html")) as f:
                html = f.read()
            # inject the wallpaper list
            pub = [{k: w[k] for k in ("path", "name", "type", "dims")} for w in WALLS]
            html = html.replace("var WALLS = [];",
                                "var WALLS = " + json.dumps(pub) + ";")
            data = html.encode()
            self.send_response(200)
            self.send_header("Content-Type", "text/html")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)
        elif parsed.path.startswith("/w/"):
            real = BY_URL.get(parsed.path)
            if real and os.path.isfile(real):
                with open(real, "rb") as f:
                    data = f.read()
                ctype = "image/gif" if real.endswith(".gif") else "image/jpeg"
                if real.endswith(".png"):
                    ctype = "image/png"
                elif real.endswith(".webp"):
                    ctype = "image/webp"
                self.send_response(200)
                self.send_header("Content-Type", ctype)
                self.send_header("Content-Length", str(len(data)))
                self.end_headers()
                self.wfile.write(data)
            else:
                self.send_error(404)
        elif parsed.path == "/select":
            qs = urllib.parse.parse_qs(parsed.query)
            upath = qs.get("path", [""])[0]
            real = BY_URL.get(upath)
            if real:
                PICKED.append(real)
                with open(PICK_FILE, "w") as f:
                    f.write(real + "\n")
            self.send_response(200)
            self.send_header("Content-Type", "text/plain")
            self.end_headers()
            self.wfile.write(b"ok")
            threading.Thread(target=self.server.shutdown, daemon=True).start()
        else:
            self.send_error(404)


def main():
    if not WALLS:
        print("no wallpapers found", file=sys.stderr)
        sys.exit(1)
    # clear any stale pick
    try:
        os.unlink(PICK_FILE)
    except OSError:
        pass
    with socketserver.TCPServer(("127.0.0.1", PORT), Handler) as httpd:
        port = httpd.server_address[1]
        url = f"http://127.0.0.1:{port}/"
        # launch chromeless window
        browser = None
        for b in ("chromium", "google-chrome", "brave-browser"):
            try:
                browser = subprocess.run(["which", b], capture_output=True,
                                         text=True).stdout.strip()
                if browser:
                    break
            except Exception:
                pass
        if not browser:
            print("no browser found", file=sys.stderr)
            sys.exit(1)
        proc = subprocess.Popen(
            [browser, "--app=" + url,
             "--window-size=1200,760",
             "--force-dark-mode"],
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        try:
            httpd.serve_forever()
        finally:
            proc.terminate()
    sys.exit(0 if PICKED else 1)


if __name__ == "__main__":
    main()
