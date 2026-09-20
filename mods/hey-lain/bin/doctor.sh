#!/usr/bin/env bash
# hey-lain --doctor: self-diagnostics for the voice pipeline.
# Every check prints pass/fail plus a one-line remediation hint; exits 0
# only if all pass. Safe to run anywhere — it never starts servers, pulls
# models, synthesizes speech, or touches the desktop.
set -uo pipefail
HERE="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$HERE/bin"
PASS=0
FAIL=0

ok()  { PASS=$((PASS + 1)); printf '  [ok]   %s\n' "$1"; }
bad() { FAIL=$((FAIL + 1)); printf '  [FAIL] %s\n         -> %s\n' "$1" "$2"; }

printf 'hey-lain doctor — voice pipeline self-check\n'

printf '\n1. python venv (STT runtime)\n'
if [ -x "$HERE/venv/bin/python" ]; then
  ok "venv python present"
else
  bad "venv python missing ($HERE/venv/bin/python)" "re-run the module installer: $HERE/install.sh --skip-apt"
fi

printf '\n2. speech-to-text deps\n'
STT_PY="$HERE/venv/bin/python"
[ -x "$STT_PY" ] || STT_PY="$(command -v python3 || true)"
if [ -n "$STT_PY" ] && "$STT_PY" -c 'import faster_whisper' >/dev/null 2>&1; then
  ok "faster-whisper importable"
else
  bad "faster-whisper not importable" "re-run the module installer: $HERE/install.sh --skip-apt"
fi

printf '\n3. microphone capture\n'
if command -v pw-record >/dev/null 2>&1; then
  ok "pw-record present"
elif command -v arecord >/dev/null 2>&1; then
  ok "arecord present (fallback)"
else
  bad "no mic capture tool (pw-record/arecord)" "install pipewire (pw-record) or alsa-utils (arecord)"
fi

printf '\n4. text-to-speech (piper voice)\n'
CFG="$HERE/config.json"
VOICE="$HERE/$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("piper_voice", "voices/en_US-libritts_r-medium.onnx"))' "$CFG" 2>/dev/null || echo voices/en_US-libritts_r-medium.onnx)"
if [ -s "$VOICE" ]; then
  ok "piper voice model present ($(basename "$VOICE"))"
else
  bad "piper voice missing ($VOICE)" "run: $BIN/fetch-voice.sh"
fi
if [ -x "$HERE/venv/bin/python" ] && "$HERE/venv/bin/python" -c 'from piper import PiperVoice' >/dev/null 2>&1; then
  ok "piper-tts importable in venv"
else
  bad "piper-tts not importable in venv" "re-run the module installer: $HERE/install.sh --skip-apt"
fi

printf '\n5. ollama service\n'
if ! command -v systemctl >/dev/null 2>&1; then
  bad "systemctl not found" "start ollama manually: ollama serve"
elif ! systemctl is-active --quiet ollama 2>/dev/null; then
  bad "ollama service not active" "run: sudo systemctl enable --now ollama"
else
  ok "ollama service active"
  if systemctl is-enabled --quiet ollama 2>/dev/null; then
    ok "ollama service enabled at boot"
  else
    bad "ollama service not enabled at boot" "run: sudo systemctl enable ollama"
  fi
fi

printf '\n6. ollama api (127.0.0.1:11434)\n'
API_VER="$(curl -fsS --max-time 5 "http://127.0.0.1:11434/api/version" 2>/dev/null \
  | python3 -c 'import json,sys; print(json.load(sys.stdin).get("version","?"))' 2>/dev/null || true)"
if [ -n "$API_VER" ]; then
  ok "api responding (ollama $API_VER)"
else
  bad "api not responding on 127.0.0.1:11434" "check: sudo systemctl status ollama"
fi

printf '\n7. brain model\n'
BRAIN_CONF="${XDG_CONFIG_HOME:-$HOME/.config}/hey-lain/brain.json"
WANT_MODEL="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("model",""))' "$BRAIN_CONF" 2>/dev/null || true)"
[ -n "$WANT_MODEL" ] || WANT_MODEL="${BRAIN_MODEL:-gemma3:270m}"
HAVE_MODELS="$(curl -fsS --max-time 10 "http://127.0.0.1:11434/api/tags" 2>/dev/null \
  | python3 -c 'import json,sys
try:
    print("\n".join(m.get("name","") for m in json.load(sys.stdin).get("models",[])))
except Exception:
    pass' 2>/dev/null || true)"
if printf '%s\n' "$HAVE_MODELS" | grep -qx "$WANT_MODEL" \
  || printf '%s\n' "$HAVE_MODELS" | grep -qx "$WANT_MODEL:latest"; then
  ok "model present ($WANT_MODEL)"
else
  bad "model '$WANT_MODEL' not pulled" "run: ollama pull $WANT_MODEL"
fi

printf '\n8. inference smoke test ("say hi", 60s)\n'
SMOKE_ERR="$(mktemp)"
SMOKE_RESP="$(curl -sS --max-time 60 "http://127.0.0.1:11434/api/chat" \
  -H 'Content-Type: application/json' \
  -d "$(python3 -c 'import json,sys; print(json.dumps({"model": sys.argv[1], "stream": False, "messages": [{"role": "user", "content": "say hi"}]}))' "$WANT_MODEL")" \
  2>"$SMOKE_ERR" || true)"
SMOKE_SAID="$(printf '%s' "$SMOKE_RESP" \
  | python3 -c 'import json,sys; print(json.load(sys.stdin).get("message",{}).get("content","").strip())' 2>/dev/null || true)"
if [ -n "$SMOKE_SAID" ]; then
  ok "inference works (replied: $(printf '%s' "$SMOKE_SAID" | head -c 60))"
else
  SMOKE_API_ERR="$(printf '%s' "$SMOKE_RESP" \
    | python3 -c 'import json,sys; print(str(json.load(sys.stdin).get("error",""))[:200])' 2>/dev/null || true)"
  [ -n "$SMOKE_API_ERR" ] || SMOKE_API_ERR="$(head -c 200 "$SMOKE_ERR" 2>/dev/null)"
  bad "inference failed" "raw error: ${SMOKE_API_ERR:-<empty response>} — then: sudo systemctl status ollama"
fi
rm -f "$SMOKE_ERR"

printf '\n%s passed, %s failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'doctor: issues found — work through the hints above.\n'
  exit 1
fi
printf 'doctor: all green — hey lain should talk.\n'
exit 0
