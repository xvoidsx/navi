#!/usr/bin/env python3
"""build-themes.py — generate the 10 polybar themes from the waybar palettes.

Each theme is a full polybar config.ini stamped from the base config with a
themed [colors] section (+ minor structural tweaks for special themes like
ghost). Output: wired/polybar/themes/<name>/{config.ini,meta}.

Re-run after changing the base config.ini or any palette below.
"""
import configparser
import os
import shutil
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
BASE = os.path.join(HERE, "config.ini")
WAYBAR_THEMES = os.path.join(HERE, "..", "waybar", "themes")
OUT = os.path.join(HERE, "themes")

# theme -> dict of [colors] values (polybar #AARRGGBB)
PALETTES = {
    "afterglow": dict(
        background="#FF0D0714", foreground="#FFFFE6FB",
        primary="#FFFF10F0", secondary="#FF39FF14", cyan="#FF00FFFF",
        alert="#FFFF3131", disabled="#FF6B5B6E", border="#FFFF10F0",
    ),
    "crt-scope": dict(
        background="#FF020A02", foreground="#FFE6FFE6",
        primary="#FF39FF14", secondary="#FFFF10F0", cyan="#FF00FFFF",
        alert="#FFFF3131", disabled="#FF2E5B2E", border="#FF39FF14",
    ),
    "ghost": dict(
        background="#00000000", foreground="#FFF2F0EA",
        primary="#FFFF10F0", secondary="#FF00FFFF", cyan="#FF00FFFF",
        alert="#FFFF3131", disabled="#73726F68", border="#00000000",
    ),
    "glass": dict(
        background="#6B10141C", foreground="#FFE1EBF5",
        primary="#FFFF10F0", secondary="#FF39FF14", cyan="#FF00FFFF",
        alert="#FFFF3131", disabled="#99C9D4DE", border="#61FFFFFF",
    ),
    "jack-panel": dict(
        background="#FF0B0710", foreground="#FFFFFFFF",
        primary="#FFFF10F0", secondary="#FF39FF14", cyan="#FF00FFFF",
        alert="#FFFF3131", disabled="#FF505050", border="#FFFF10F0",
    ),
    "monolith": dict(
        background="#FF0A0A0A", foreground="#FFFFFFFF",
        primary="#FFFF10F0", secondary="#FF9A9A9A", cyan="#FF9A9A9A",
        alert="#FFFF3131", disabled="#FF505050", border="#FF2E2E2E",
    ),
    "neon-tube": dict(
        background="#E6040106", foreground="#FFFFFFFF",
        primary="#FFFF10F0", secondary="#FF39FF14", cyan="#FF00FFFF",
        alert="#FFFF3131", disabled="#FF505050", border="#FFFF10F0",
    ),
    "patchbay": dict(
        background="#FF000000", foreground="#FFFFFFFF",
        primary="#FFFF10F0", secondary="#FF39FF14", cyan="#FF00FFFF",
        alert="#FFFF3131", disabled="#FF505050", border="#FFFF10F0",
    ),
    "refined-obsidian": dict(
        background="#FF0A050B", foreground="#FFFFFFFF",
        primary="#FFFF10F0", secondary="#FF39FF14", cyan="#FF00FFFF",
        alert="#FFFF3131", disabled="#FF505050", border="#FFFF10F0",
    ),
    "voids": dict(
        background="#FF030305", foreground="#FFF0EBF5",
        primary="#FFFF10F0", secondary="#FF39FF14", cyan="#FF00FFFF",
        alert="#FFFF3131", disabled="#80B8AEBE", border="#FFFF10F0",
    ),
}

# Structural tweaks per theme: (section, key, value)
TWEAKS = {
    # ghost: no chrome at all — kill the borders polybar would draw
    "ghost": [
        ("bar/main", "border-size", "0"),
        ("bar/main", "border-top-size", "0"),
        ("bar/main", "border-bottom-size", "0"),
        ("bar/main", "line-size", "0"),
    ],
    # monolith: hairline rules, flat
    "monolith": [
        ("bar/main", "border-size", "1pt"),
        ("bar/main", "border-bottom-size", "1pt"),
        ("bar/main", "line-size", "1pt"),
    ],
}


def load_base():
    cfg = configparser.ConfigParser(interpolation=None)
    cfg.optionxform = str  # keep key case
    cfg.read(BASE)
    return cfg


def main():
    for name, palette in sorted(PALETTES.items()):
        cfg = load_base()  # fresh per theme — no state leaks across themes
        for key, val in palette.items():
            cfg["colors"][key] = val
        for section, key, val in TWEAKS.get(name, []):
            cfg[section][key] = val

        outdir = os.path.join(OUT, name)
        os.makedirs(outdir, exist_ok=True)
        with open(os.path.join(outdir, "config.ini"), "w") as f:
            cfg.write(f)

        # meta rides along from the waybar theme (same name, same story)
        src_meta = os.path.join(WAYBAR_THEMES, name, "meta")
        if os.path.exists(src_meta):
            shutil.copy(src_meta, os.path.join(outdir, "meta"))
        print(f"  {name}")

    print(f"{len(PALETTES)} polybar themes in {OUT}")


if __name__ == "__main__":
    sys.exit(main())
