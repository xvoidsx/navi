#!/usr/bin/env bash
# One-time setup: venv + faster-whisper (CPU int8) + model prefetch + exec bits
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
sudo apt-get update && sudo apt-get install -y pipewire-audio-client-libraries alsa-utils grim swaylock xdg-utils 2>/dev/null || \
  sudo apt install -y alsa-utils grim 2>/dev/null || true
python3 -m venv "$HERE/venv"
"$HERE/venv/bin/pip" install -U pip
"$HERE/venv/bin/pip" install -r "$HERE/requirements.txt"
"$HERE/venv/bin/python" -c "from faster_whisper import WhisperModel; WhisperModel('tiny.en', device='cpu', compute_type='int8')"
chmod +x "$HERE"/bin/*.sh "$HERE"/bin/*.py
echo "OK. Next: grep -q hey-lain ~/.config/sway/config || echo 'include ~/hey-lain/sway-bindings.conf' >> ~/.config/sway/config"
echo "Then: swaymsg reload"
