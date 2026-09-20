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
info() { printf '  [..]   %s\n' "$1"; }

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

printf '\n5. brain backend (configured)\n'
BRAIN_CONF="${XDG_CONFIG_HOME:-$HOME/.config}/hey-lain/brain.json"
_brain_get() { python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get(sys.argv[2],""))' "$BRAIN_CONF" "$1" 2>/dev/null || true; }
BRAIN_BACKEND="$(_brain_get backend)"
[ -n "$BRAIN_BACKEND" ] || BRAIN_BACKEND="local"
BRAIN_WANT_MODEL="$(_brain_get model)"
[ -n "$BRAIN_WANT_MODEL" ] || BRAIN_WANT_MODEL="${BRAIN_MODEL:-gemma3:270m}"
BRAIN_API_URL="$(_brain_get api_url)"
BRAIN_API_STYLE="$(_brain_get api_style)"
if [ -z "$BRAIN_API_STYLE" ]; then
  case "$BRAIN_BACKEND" in openai|openrouter) BRAIN_API_STYLE="openai";; *) BRAIN_API_STYLE="ollama";; esac
fi
BRAIN_KEY_SET="not set"
if [ -n "$(_brain_get api_key)" ] || [ -n "${HEY_LAIN_API_KEY:-}" ]; then
  BRAIN_KEY_SET="set"
fi
# The key itself is NEVER printed — only whether one is configured.
ok "provider: $BRAIN_BACKEND"
ok "model: $BRAIN_WANT_MODEL"
[ -n "$BRAIN_API_URL" ] && ok "api url: $BRAIN_API_URL"
ok "protocol: $BRAIN_API_STYLE"
case "$BRAIN_BACKEND" in
  openai|openrouter)
    if [ "$BRAIN_KEY_SET" = "set" ]; then
      ok "api key: set"
    else
      bad "api key: not set" "run navi-lain-config (app launcher) and paste a key"
    fi ;;
  custom) ok "api key: $BRAIN_KEY_SET (optional for custom endpoints)" ;;
  *)      ok "api key: $BRAIN_KEY_SET (not needed)" ;;
esac
# The local ollama daemon matters for local/ollama-cloud backends; for
# pure-cloud backends it is only the fallback path, so its checks below
# become informational instead of failures.
NEEDS_LOCAL=0
case "$BRAIN_BACKEND" in local|ollama-cloud) NEEDS_LOCAL=1 ;; esac
case "$BRAIN_API_URL" in *127.0.0.1*|*localhost*) NEEDS_LOCAL=1 ;; esac
# svc_ok/svc_bad: hard checks when the local daemon is the live path,
# informational notes when it is only the fallback.
svc_ok()  { if [ "$NEEDS_LOCAL" -eq 1 ]; then ok "$1"; else info "$1"; fi; }
svc_bad() { if [ "$NEEDS_LOCAL" -eq 1 ]; then bad "$1" "$2"; else info "$1 — $2 (local fallback)"; fi; }

printf '\n6. ollama service'
if [ "$NEEDS_LOCAL" -eq 1 ]; then printf '\n'; else printf ' (local fallback — informational)\n'; fi
if ! command -v systemctl >/dev/null 2>&1; then
  svc_bad "systemctl not found" "start ollama manually: ollama serve"
elif ! systemctl is-active --quiet ollama 2>/dev/null; then
  svc_bad "ollama service not active" "run: sudo systemctl enable --now ollama"
else
  svc_ok "ollama service active"
  if systemctl is-enabled --quiet ollama 2>/dev/null; then
    svc_ok "ollama service enabled at boot"
  else
    svc_bad "ollama service not enabled at boot" "run: sudo systemctl enable ollama"
  fi
fi

printf '\n7. ollama api (127.0.0.1:11434)'
if [ "$NEEDS_LOCAL" -eq 1 ]; then printf '\n'; else printf ' (local fallback — informational)\n'; fi
API_VER="$(curl -fsS --max-time 5 "http://127.0.0.1:11434/api/version" 2>/dev/null \
  | python3 -c 'import json,sys; print(json.load(sys.stdin).get("version","?"))' 2>/dev/null || true)"
if [ -n "$API_VER" ]; then
  svc_ok "api responding (ollama $API_VER)"
else
  svc_bad "api not responding on 127.0.0.1:11434" "check: sudo systemctl status ollama"
fi

printf '\n8. brain model\n'
if [ "$BRAIN_BACKEND" = "local" ]; then
  HAVE_MODELS="$(curl -fsS --max-time 10 "http://127.0.0.1:11434/api/tags" 2>/dev/null \
    | python3 -c 'import json,sys
try:
    print("\n".join(m.get("name","") for m in json.load(sys.stdin).get("models",[])))
except Exception:
    pass' 2>/dev/null || true)"
  if printf '%s\n' "$HAVE_MODELS" | grep -qx "$BRAIN_WANT_MODEL" \
    || printf '%s\n' "$HAVE_MODELS" | grep -qx "$BRAIN_WANT_MODEL:latest"; then
    ok "model present ($BRAIN_WANT_MODEL)"
  else
    bad "model '$BRAIN_WANT_MODEL' not pulled" "run: ollama pull $BRAIN_WANT_MODEL"
  fi
else
  info "remote model: $BRAIN_WANT_MODEL (liveness is verified by the smoke test below)"
fi

printf '\n9. inference smoke test ("say hi" via %s)\n' "$BRAIN_BACKEND"
# Smoke-test the CONFIGURED provider, not just the local daemon. The key
# is read for the request but never printed anywhere below.
SMOKE_KEY="$(_brain_get api_key)"
[ -n "${HEY_LAIN_API_KEY:-}" ] && SMOKE_KEY="$HEY_LAIN_API_KEY"
SMOKE_URL="$BRAIN_API_URL"
[ -n "$SMOKE_URL" ] || SMOKE_URL="http://127.0.0.1:11434/api/chat"
SMOKE_TIMEOUT=60
case "$BRAIN_BACKEND" in local|ollama-cloud) SMOKE_URL="http://127.0.0.1:11434/api/chat" ;; esac
SMOKE_ERR="$(mktemp)"
SMOKE_T0=$SECONDS
SMOKE_SKIP=0
if [ "$BRAIN_API_STYLE" = "openai" ]; then
  if [ -z "$SMOKE_KEY" ]; then
    bad "inference skipped — no API key configured" "run navi-lain-config (app launcher) and paste a key"
    SMOKE_SKIP=1
  else
    SMOKE_RESP_CODE="$(curl -sS --max-time "$SMOKE_TIMEOUT" -w '\n%{http_code}' "$SMOKE_URL" \
      -H "Authorization: Bearer $SMOKE_KEY" \
      -H 'Content-Type: application/json' \
      -d "$(python3 -c 'import json,sys; print(json.dumps({"model": sys.argv[1], "max_tokens": 16, "messages": [{"role": "user", "content": "say hi"}]}))' "$BRAIN_WANT_MODEL")" \
      2>"$SMOKE_ERR" || true)"
    SMOKE_CODE="$(printf '%s' "$SMOKE_RESP_CODE" | tail -n 1)"
    SMOKE_RESP="$(printf '%s' "$SMOKE_RESP_CODE" | sed '$d')"
    SMOKE_SAID="$(printf '%s' "$SMOKE_RESP" \
      | python3 -c 'import json,sys; print(json.load(sys.stdin)["choices"][0]["message"]["content"].strip())' 2>/dev/null || true)"
  fi
else
  if [ -n "$SMOKE_KEY" ]; then SMOKE_AUTH=(-H "Authorization: Bearer $SMOKE_KEY"); else SMOKE_AUTH=(); fi
  SMOKE_RESP_CODE="$(curl -sS --max-time "$SMOKE_TIMEOUT" -w '\n%{http_code}' "$SMOKE_URL" \
    -H 'Content-Type: application/json' "${SMOKE_AUTH[@]}" \
    -d "$(python3 -c 'import json,sys; print(json.dumps({"model": sys.argv[1], "stream": False, "messages": [{"role": "user", "content": "say hi"}]}))' "$BRAIN_WANT_MODEL")" \
    2>"$SMOKE_ERR" || true)"
  SMOKE_CODE="$(printf '%s' "$SMOKE_RESP_CODE" | tail -n 1)"
  SMOKE_RESP="$(printf '%s' "$SMOKE_RESP_CODE" | sed '$d')"
  SMOKE_SAID="$(printf '%s' "$SMOKE_RESP" \
    | python3 -c 'import json,sys; print(json.load(sys.stdin).get("message",{}).get("content","").strip())' 2>/dev/null || true)"
fi
SMOKE_DT=$((SECONDS - SMOKE_T0))
if [ "$SMOKE_SKIP" -eq 1 ]; then
  : # already reported above
elif [ -n "${SMOKE_SAID// }" ]; then
  ok "inference works via $BRAIN_BACKEND (${SMOKE_DT}s — replied: $(printf '%s' "$SMOKE_SAID" | head -c 60))"
else
  if [ "$SMOKE_CODE" = "401" ] || [ "$SMOKE_CODE" = "403" ]; then
    bad "API key rejected by $BRAIN_BACKEND (HTTP $SMOKE_CODE)" "check it in navi-lain-config (app launcher)"
  else
    SMOKE_API_ERR="$(printf '%s' "$SMOKE_RESP" \
      | python3 -c 'import json,sys; print(str(json.load(sys.stdin).get("error",""))[:200])' 2>/dev/null || true)"
    [ -n "$SMOKE_API_ERR" ] || SMOKE_API_ERR="$(head -c 200 "$SMOKE_ERR" 2>/dev/null)"
    bad "inference via $BRAIN_BACKEND failed" "raw error: ${SMOKE_API_ERR:-<empty response>}"
  fi
fi
rm -f "$SMOKE_ERR"

printf '\n%s passed, %s failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'doctor: issues found — work through the hints above.\n'
  exit 1
fi
printf 'doctor: all green — hey lain should talk.\n'
exit 0
