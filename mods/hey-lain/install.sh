#!/usr/bin/env bash
# Portable installer for Hey Lain. Safe to re-run; never overwrites user data.
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VENV="$HERE/venv"
SKIP_APT=0
PREFETCH=1

usage() { echo "Usage: $0 [--skip-apt] [--no-prefetch]"; }
for arg in "$@"; do
  case "$arg" in
    --skip-apt) SKIP_APT=1 ;;
    --no-prefetch) PREFETCH=0 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $arg" >&2; usage >&2; exit 2 ;;
  esac
done

if [ "$SKIP_APT" -eq 0 ]; then
  command -v sudo >/dev/null || { echo "sudo is required for system packages (or use --skip-apt)." >&2; exit 1; }
  sudo apt-get update
  sudo apt-get install -y python3 python3-venv python3-pip curl ca-certificates \
    pipewire-audio-client-libraries alsa-utils libnotify-bin grim swaylock \
    xdg-utils foot chromium playerctl brightnessctl
fi

python3 -m venv "$VENV"
"$VENV/bin/python" -m pip install --upgrade pip
"$VENV/bin/pip" install -r "$HERE/requirements.txt"
chmod +x "$HERE"/bin/*.sh "$HERE"/bin/*.py "$HERE/install.sh"

if [ "$PREFETCH" -eq 1 ]; then
  "$VENV/bin/python" -c "from faster_whisper import WhisperModel; WhisperModel('tiny.en', device='cpu', compute_type='int8')"
  # Piper TTS voice — the "voice model" tts-ready.sh checks for. Downloaded,
  # not bundled; warns (not aborts) when the network is unreachable.
  if ! "$HERE/bin/fetch-voice.sh"; then
    echo "warning: Piper voice not fetched — Hey Lain will say its voice model is not ready until the download succeeds." >&2
  fi
fi

mkdir -p "${XDG_RUNTIME_DIR:-/tmp}/hey-lain" "$HERE/log"
echo
echo "Hey Lain is installed at: $HERE"
echo "Run '$HERE/bin/warmup.sh' after login for faster first replies."
echo
echo "Add this line to your Sway config, then run 'swaymsg reload':"
echo "  bindsym --no-repeat Alt+v exec $HERE/bin/toggle.sh"
echo
echo "For a root-owned config, use sudoedit rather than letting this installer modify it."
