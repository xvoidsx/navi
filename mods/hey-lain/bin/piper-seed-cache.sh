#!/usr/bin/env bash
# Pre-seed the TTS sentence cache with Lain's most-spoken lines via the
# warm Piper server. Called by warmup.sh (safe to re-run any time).
# Cache keys must match speak.py: voice|speed|variant|text with
# variant="piper:<model-stem>".
set -uo pipefail
RUNTIME="${RUNTIME:-${XDG_RUNTIME_DIR:-/tmp}/hey-lain}"
HEY_LAIN_ROOT="$(cd "$(dirname "$0")/.." && pwd)" python3 - "$RUNTIME" <<'EOF'
import sys, json, urllib.request
import os
sys.path.insert(0, os.environ["HEY_LAIN_ROOT"] + "/bin")
import tts_cache
runtime = sys.argv[1]
LINES = [
    "I didn't catch that. Could you say it again?",
    "Checking the skies.",
    "Pulling the headlines.",
    "Looking that up.",
    "Opening youtube.",
    "Opening github.",
    "Turning it up.",
    "Turning it down.",
    "Taking a screenshot.",
    "Looking up.",
    "Anytime. Tap me when you need me.",
    "Sorry, my brain hiccupped. Try again?",
]
n = 0
for line in LINES:
    body = json.dumps({"text": line}).encode()
    req = urllib.request.Request("http://127.0.0.1:8766/speak", data=body,
                                 headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=120) as r:
        raw = r.read()
        sr = int(r.headers.get("X-Sample-Rate", 22050))
    tts_cache.put(runtime,
                   tts_cache.key(line, "af_heart", 1.0,
                                 "piper:en_US-libritts_r-medium"),
                  raw, sr)
    n += 1
print(f"seeded {n} piper lines")
EOF
