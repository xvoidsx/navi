#!/usr/bin/env bash
# Pre-seed the TTS sentence cache with Lain's most-spoken lines via the
# warm Kokoro service. Called by warmup.sh (and safe to re-run any time).
set -uo pipefail
HERE="$(cd "$(dirname "$0")/.." && pwd)"
RUNTIME="${RUNTIME:-${XDG_RUNTIME_DIR:-/tmp}/hey-lain}"
HEY_LAIN_ROOT="$HERE" python3 - "$RUNTIME" <<'EOF'
import sys, json, io, wave, urllib.request
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
    body = json.dumps({"input": line, "voice": "af_heart", "speed": 1.0,
                       "lang": "en-us", "response_format": "wav",
                       "play": False}).encode()
    req = urllib.request.Request("http://127.0.0.1:8765/v1/audio/speech",
                                 data=body,
                                 headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=300) as r:
        data = r.read()
    with wave.open(io.BytesIO(data), "rb") as w:
        pcm, sr = w.readframes(w.getnframes()), w.getframerate()
    tts_cache.put(runtime, tts_cache.key(line, "af_heart", 1.0, "full"),
                  pcm, sr)
    n += 1
print(f"seeded {n} lines")
EOF
