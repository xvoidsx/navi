#!/usr/bin/env python3
"""Transcribe a wav with faster-whisper tiny.en (CPU int8). Prints text to stdout.

Robustness: rejects sub-0.4s clips up front, retries without VAD if the
VAD pass comes back empty (short/quiet utterances), errors go to stderr
so the caller can log them instead of silently hearing 'didn't catch that'.
"""
import sys, json, wave
from pathlib import Path

MIN_SECONDS = 0.4

def duration(path):
    with wave.open(path, "rb") as w:
        return w.getnframes() / float(w.getframerate() or 1)

def main():
    if len(sys.argv) < 2:
        print("usage: listen.py <wav>", file=sys.stderr); sys.exit(2)
    wav = sys.argv[1]
    here = Path(__file__).resolve().parent.parent
    cfg = json.loads((here / "config.json").read_text())
    try:
        secs = duration(wav)
    except Exception as e:
        print(f"listen: unreadable wav {wav}: {e}", file=sys.stderr)
        sys.exit(3)
    if secs < MIN_SECONDS:
        print(f"listen: too short ({secs:.2f}s), skipping", file=sys.stderr)
        print("")
        return
    from faster_whisper import WhisperModel
    model = WhisperModel(cfg.get("stt_model", "tiny.en"),
                         device=cfg.get("stt_device", "cpu"),
                         compute_type=cfg.get("stt_compute", "int8"))

    def run(vad):
        segs, _ = model.transcribe(wav, language="en", beam_size=1,
                                   vad_filter=vad)
        segs = list(segs)
        text = " ".join(s.text.strip() for s in segs).strip()
        # tiny.en hallucinates ("Hello", "Thank you") on near-silence:
        # if every segment looks like non-speech and text is tiny, drop it.
        if segs and all(getattr(s, "no_speech_prob", 0) > 0.7 for s in segs) \
                and len(text) < 25:
            print(f"listen: only non-speech (vad={vad}), dropping {text!r}",
                  file=sys.stderr)
            return ""
        return text

    text = run(True)
    if not text:
        text = run(False)
    print(text)

if __name__ == "__main__":
    main()
