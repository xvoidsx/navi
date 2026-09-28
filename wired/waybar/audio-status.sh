#!/usr/bin/env python3
# audio-status.sh — waybar custom/volume module source for navi-audio.
#
# Named .sh so install.sh's `find ... -name '*.sh' -exec chmod 0755` picks
# it up on deploy; the content is python3, for the JSON parsing.
#
# Long-running: prints one waybar-JSON object per line — the default sink's
# volume. The device label lives in the tooltip only, keeping the module
# itself compact ("🔊 74%") so the bar never crowds the clock.
# Re-emits on `pactl subscribe` events; no polling.
#
# Short-label rule mirrors deviceShortName in mods/navi-audio/audio.go:
# the active port's human description when it has one, otherwise the
# device description trimmed to fit.

import json
import re
import shutil
import subprocess
import sys
import time

VOL_RE = re.compile(r"(\d+)%")


def run(*args):
    try:
        p = subprocess.run(list(args), capture_output=True, text=True, timeout=8)
    except Exception:
        return None
    if p.returncode != 0:
        return None
    return p.stdout


def short_label(sink):
    ports = sink.get("ports") or []
    active = sink.get("active_port") or ""
    for pt in ports:
        if pt.get("name") == active:
            d = (pt.get("description") or "").strip()
            if d and d != active:
                return d if len(d) <= 18 else d[:17] + "…"
    desc = (sink.get("description") or sink.get("name") or "?").strip()
    return desc if len(desc) <= 24 else desc[:23] + "…"


def read_state():
    """(vol_pct, muted, short, full_desc); None when the daemon is down."""
    default = run("pactl", "get-default-sink")
    if default is None:
        return None
    default = default.strip()
    vol_raw = run("pactl", "get-sink-volume", "@DEFAULT_SINK@") or ""
    mute_raw = run("pactl", "get-sink-mute", "@DEFAULT_SINK@") or ""
    sinks_raw = run("pactl", "-f", "json", "list", "sinks") or "[]"
    try:
        sinks = json.loads(sinks_raw)
    except Exception:
        sinks = []
    pcts = [int(x) for x in VOL_RE.findall(vol_raw)]
    vol = max(pcts) if pcts else -1
    muted = "yes" in mute_raw.lower()
    short, full = "?", ""
    for s in sinks:
        if s.get("name") == default:
            full = s.get("description") or ""
            short = short_label(s)
            break
    return {"vol": vol, "muted": muted, "short": short, "full": full}


def render(st):
    if st is None:
        return json.dumps({
            "text": "🔇 no audio",
            "class": "error",
            "tooltip": "audio daemon unreachable — is PulseAudio running?",
        })
    icon = "🔇" if st["muted"] else "🔊"
    if st["muted"]:
        text = icon + " mute"
    elif st["vol"] < 0:
        text = icon + " --"
    else:
        text = icon + " " + str(st["vol"]) + "%"
    # device identity lives in the tooltip, not the module text
    tip = st["short"]
    if st["full"] and st["full"] != st["short"]:
        tip = tip + "\n" + st["full"]
    if not st["muted"] and st["vol"] >= 0:
        tip = tip + "\nvolume " + str(st["vol"]) + "%"
    return json.dumps({
        "text": text,
        "class": "muted" if st["muted"] else "",
        "tooltip": tip,
    })


def main():
    if shutil.which("pactl") is None:
        sys.stdout.write(render(None) + "\n")
        sys.stdout.flush()
        return
    last = None

    def push():
        nonlocal last
        line = render(read_state())
        if line != last:
            sys.stdout.write(line + "\n")
            sys.stdout.flush()
            last = line

    push()
    while True:
        try:
            p = subprocess.Popen(["pactl", "subscribe"],
                                 stdout=subprocess.PIPE, text=True, bufsize=1)
        except Exception:
            time.sleep(5)
            continue
        try:
            for raw in p.stdout:
                if (" on sink" in raw or " on server" in raw
                        or " on card" in raw):
                    time.sleep(0.25)  # let the daemon settle
                    push()
        except Exception:
            pass
        try:
            p.wait(timeout=5)
        except Exception:
            try:
                p.kill()
            except Exception:
                pass
        time.sleep(2)
        push()  # re-list after a subscribe death, then reconnect


if __name__ == "__main__":
    main()
