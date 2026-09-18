#!/usr/bin/env python3
"""Persistent Piper TTS server: voice stays resident, no per-call reload.

GET  /health -> {"status": "ok"}
POST /speak  {"text": "..."} -> raw s16le mono bytes + X-Sample-Rate.
Localhost only. Run via bin/piper-serve.sh (port 8766).
"""
import io
import json
import wave
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path

HERE = Path(__file__).resolve().parent.parent
CFG = json.loads((HERE / "config.json").read_text())
MODEL = str(HERE / CFG.get("piper_voice", "voices/en_US-libritts_r-medium.onnx"))
PORT = int(__import__("os").environ.get("PIPER_SERVICE_PORT", 8766))

from piper import PiperVoice  # noqa: E402

VOICE = PiperVoice.load(MODEL)
SR = VOICE.config.sample_rate


class Handler(BaseHTTPRequestHandler):
    def _json(self, code, obj):
        body = json.dumps(obj).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path == "/health":
            self._json(200, {"status": "ok", "service": "piper",
                             "sample_rate": SR})
        else:
            self._json(404, {"error": "not found"})

    def do_POST(self):
        if self.path != "/speak":
            self._json(404, {"error": "not found"})
            return
        try:
            length = int(self.headers.get("Content-Length", "0"))
            if length <= 0 or length > 100_000:
                raise ValueError("bad body size")
            text = json.loads(self.rfile.read(length)).get("text", "")
            if not isinstance(text, str) or not text.strip():
                raise ValueError("text must be a non-empty string")
        except (ValueError, json.JSONDecodeError) as e:
            self._json(400, {"error": str(e)})
            return
        try:
            buf = io.BytesIO()
            with wave.open(buf, "wb") as w:
                w.setnchannels(1)
                w.setsampwidth(2)
                w.setframerate(SR)
                for chunk in VOICE.synthesize(text.strip()):
                    w.writeframes(chunk.audio_int16_bytes)
            # strip back to raw s16le for the pipe (keep header parse honest)
            buf.seek(0)
            with wave.open(buf, "rb") as w:
                raw = w.readframes(w.getnframes())
        except Exception as e:  # noqa: BLE001
            self._json(500, {"error": str(e)})
            return
        self.send_response(200)
        self.send_header("Content-Type", "application/octet-stream")
        self.send_header("Content-Length", str(len(raw)))
        self.send_header("X-Sample-Rate", str(SR))
        self.end_headers()
        self.wfile.write(raw)

    def log_message(self, *args):
        pass


if __name__ == "__main__":
    print(f"Piper TTS listening on http://127.0.0.1:{PORT}", flush=True)
    HTTPServer(("127.0.0.1", PORT), Handler).serve_forever()
