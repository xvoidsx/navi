#!/usr/bin/env python3
"""Small, allow-listed Sway IPC controller used by Hey Lain.

The voice brain may choose an operation, but it never gets to construct a
shell command. This module resolves human-friendly targets from get_tree and
builds the corresponding swaymsg invocation itself.
"""
from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
from typing import Any, Iterable


def sway(*args: str, dry_run: bool = False) -> str:
    cmd = [os.environ.get("SWAYMSG", "swaymsg"), *args]
    if dry_run:
        print("[dry-run] " + " ".join(cmd), file=sys.stderr)
        return ""
    try:
        proc = subprocess.run(cmd, text=True, capture_output=True, timeout=10)
    except FileNotFoundError:
        raise RuntimeError("swaymsg not found — is sway running?")
    except subprocess.TimeoutExpired:
        raise RuntimeError("swaymsg timed out — is sway still responding?")
    if proc.returncode:
        raise RuntimeError(proc.stderr.strip() or "swaymsg failed")
    try:
        result = json.loads(proc.stdout)
        if isinstance(result, list):
            failures = [x.get("error") for x in result if isinstance(x, dict) and x.get("success") is False]
            if failures:
                raise RuntimeError("; ".join(str(x) for x in failures))
    except json.JSONDecodeError:
        pass
    return proc.stdout.strip()


def children(node: dict[str, Any]) -> Iterable[dict[str, Any]]:
    yield node
    for key in ("nodes", "floating_nodes"):
        for child in node.get(key, []) or []:
            yield from children(child)


def text_values(node: dict[str, Any]) -> list[str]:
    props = node.get("window_properties") or {}
    return [str(node.get("app_id") or ""), str(props.get("class") or ""),
            str(props.get("instance") or ""), str(node.get("name") or "")]


def resolve_target(target: str, dry_run: bool = False) -> int:
    if dry_run:
        return 0
    if target in ("focused", "current", "this", "it"):
        tree = json.loads(sway("-t", "get_tree", dry_run=dry_run) or "{}")
        for node in children(tree):
            if node.get("focused") and node.get("id"):
                return int(node["id"])
        raise RuntimeError("there is no focused window")

    tree = json.loads(sway("-t", "get_tree", dry_run=dry_run) or "{}")
    wanted = target.casefold()
    matches = []
    for node in children(tree):
        if not node.get("id"):
            continue
        values = [v.casefold() for v in text_values(node)]
        if any(wanted == v or wanted in v for v in values if v):
            matches.append(node)
    if not matches:
        raise RuntimeError(f"could not find a window matching {target}")
    if len(matches) > 1:
        focused = [n for n in matches if n.get("focused")]
        if focused:
            return int(focused[0]["id"])
        names = ", ".join(text_values(n)[-1] for n in matches[:3])
        raise RuntimeError(f"more than one window matches {target}: {names}")
    return int(matches[0]["id"])


def find_best(target: str) -> int | None:
    """con_id of the best window to focus for a spoken app name, or None.

    Used by the `open` action: an already-running app gets focused instead
    of spawning a duplicate. Exact app_id/class matches outrank substring
    ones, a matching window title outranks a non-matching one (so
    "chromium" prefers the browser over a chromium --app webapp), and the
    focused window wins ties.
    """
    tree = json.loads(sway("-t", "get_tree") or "{}")
    wanted = target.casefold()

    def rank(node: dict[str, Any]) -> tuple[int, int, int]:
        values = [v.casefold() for v in text_values(node) if v]
        exact = any(wanted == v for v in values[:3])
        titled = len(values) > 3 and wanted in values[3]
        return (0 if exact else 1, 0 if titled else 1,
                0 if node.get("focused") else 1)

    matches = [n for n in children(tree) if n.get("id") and any(
        wanted == v or wanted in v
        for v in (x.casefold() for x in text_values(n)) if v)]
    if not matches:
        return None
    return int(sorted(matches, key=rank)[0]["id"])


def main() -> int:
    parser = argparse.ArgumentParser(description="safe Hey Lain Sway controller")
    parser.add_argument("--dry-run", action="store_true")
    sub = parser.add_subparsers(dest="op", required=True)
    sub.add_parser("tree")
    sub.add_parser("find").add_argument("target")
    p = sub.add_parser("workspace"); p.add_argument("name")
    p = sub.add_parser("focus"); p.add_argument("target")
    p = sub.add_parser("move"); p.add_argument("target"); p.add_argument("workspace")
    p = sub.add_parser("float"); p.add_argument("mode", choices=("enable", "disable", "toggle"))
    p = sub.add_parser("fullscreen"); p.add_argument("mode", choices=("enable", "disable", "toggle"))
    p = sub.add_parser("layout"); p.add_argument("layout", choices=("splith", "splitv", "stacking", "tabbed", "toggle split"))
    p = sub.add_parser("scratchpad"); p.add_argument("mode", choices=("show", "hide", "toggle"))
    p = sub.add_parser("close"); p.add_argument("target", nargs="?", default="focused")
    p = sub.add_parser("focus-direction"); p.add_argument("direction", choices=("left", "right", "up", "down", "next", "prev"))
    args = parser.parse_args()
    try:
        if args.op == "tree":
            out = sway("-t", "get_tree", dry_run=args.dry_run)
            if out:
                print(out)
        elif args.op == "find":
            # Quiet by design: "not running" is the normal launch path for
            # the caller, not an error worth logging every time.
            if args.dry_run:
                return 1
            cid = find_best(args.target)
            if cid is None:
                return 1
            print(cid)
        elif args.op == "workspace":
            sway("workspace", args.name, dry_run=args.dry_run)
        elif args.op == "focus-direction":
            sway("focus", args.direction, dry_run=args.dry_run)
        elif args.op == "layout":
            sway("layout", *args.layout.split(), dry_run=args.dry_run)
        elif args.op == "scratchpad":
            sway("scratchpad", args.mode, dry_run=args.dry_run)
        else:
            con_id = resolve_target(getattr(args, "target", "focused"), args.dry_run) if args.op in {"focus", "move", "close"} else None
            if args.op == "focus":
                sway(f"[con_id={con_id}]", "focus", dry_run=args.dry_run)
            elif args.op == "move":
                sway(f"[con_id={con_id}]", "move", "container", "to", "workspace", args.workspace, dry_run=args.dry_run)
            elif args.op == "close":
                sway(f"[con_id={con_id}]", "kill", dry_run=args.dry_run)
            elif args.op == "float":
                sway("floating", args.mode, dry_run=args.dry_run)
            elif args.op == "fullscreen":
                sway("fullscreen", args.mode, dry_run=args.dry_run)
    except (RuntimeError, json.JSONDecodeError, OSError) as exc:
        print(f"sway-control: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
