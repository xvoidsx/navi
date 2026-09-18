#!/usr/bin/env python3
"""
Streaming Kokoro TTS with notify/voice sync fix.

Old behavior: notify-send fired BEFORE 10s+ of model-load+synth ->
  notification long before voice.
New: synthesize sentence 1 first, THEN fire notify concurrently
  with opening the audio pipe, then stream sentence-by-sentence
  into ONE aplay process (no gaps, no .wav files).

Usage: speak.py "text..." [-v voice] | echo text | speak.py
"""
import json, re, subprocess, sys, shutil, time, os, io, wave
import urllib.request, urllib.error
from pathlib import Path

HERE = Path(__file__).resolve().parent.parent
CFG = json.loads((HERE / "config.json").read_text())
DEFAULT_VOICE = CFG.get("voice", "af_heart")
TITLE = CFG.get("notify_title", "Hey Lain!")
SR = int(CFG.get("sample_rate_tts", 24000))
# full (fp32) measured ~2x FASTER than int8 on pre-VNNI Intel (Haswell).
VARIANT = os.environ.get("KOKORO_VARIANT", CFG.get("tts_variant", "full"))
SPEED = float(CFG.get("speed", 1.0))
SERVICE_URL = os.environ.get("KOKORO_SERVICE_URL", "http://127.0.0.1:8765")
# TTS engine: "piper" (fast, default) or "kokoro" (prettier, slower).
ENGINE = os.environ.get("TTS_ENGINE", CFG.get("tts_engine", "piper"))
PIPER_MODEL = str(HERE / CFG.get("piper_voice", "voices/en_US-libritts_r-medium.onnx"))
PIPER_URL = os.environ.get("PIPER_SERVICE_URL", "http://127.0.0.1:8766")
PIPER_BIN = os.environ.get("PIPER_BIN", shutil.which("piper") or str(HERE / "venv" / "bin" / "piper"))
PIPER_SR = int(CFG.get("piper_sample_rate", 22050))
RUNTIME = os.path.join(os.environ.get("XDG_RUNTIME_DIR", "/tmp"), "hey-lain")
sys.path.insert(0, str(Path(__file__).resolve().parent))
import tts_cache
LOGFILE = os.environ.get("HEY_LAIN_LOG")

def log(msg):
    if LOGFILE:
        with open(LOGFILE, "a") as f:
            f.write(f"speak: {msg}\n")
    else:
        print(f"speak: {msg}", file=sys.stderr)

def split_sentences(text: str):
    parts = re.split(r'(?<=[.!?])\s+', text.strip())
    buf, out = "", []
    for p in parts:
        buf = (buf + " " + p).strip()
        if len(buf) >= 120 or p is parts[-1]:
            if buf: out.append(buf)
            buf = ""
    if buf: out.append(buf)
    return [s for s in out if s] or ([text.strip()] if text.strip() else [])

def main():
    voice = DEFAULT_VOICE
    args = []
    for a in sys.argv[1:]:
        if a in ("-v", "--voice"):
            continue
        if sys.argv[sys.argv.index(a)-1] in ("-v", "--voice"):
            voice = a; continue
        args.append(a)
    if args:
        text = " ".join(args)
    else:
        text = sys.stdin.read()
    text = text.strip()
    if not text:
        print("speak.py: empty text", file=sys.stderr); sys.exit(2)

    import numpy as np
    runtime = RUNTIME
    level_file = os.path.join(runtime, "voice-level")

    def service_healthy() -> bool:
        try:
            with urllib.request.urlopen(SERVICE_URL + "/health",
                                        timeout=0.75) as r:
                import json as _j
                return _j.load(r).get("status") == "ok"
        except Exception:
            return False

    def synth_service(text: str):
        body = json.dumps({"input": text, "voice": voice, "speed": SPEED,
                           "lang": "en-us", "response_format": "wav",
                           "play": False}).encode()
        req = urllib.request.Request(SERVICE_URL + "/v1/audio/speech",
                                     data=body,
                                     headers={"Content-Type": "application/json"})
        with urllib.request.urlopen(req, timeout=300) as r:
            data = r.read()
        with wave.open(io.BytesIO(data), "rb") as w:
            assert w.getsampwidth() == 2 and w.getnchannels() == 1
            return w.readframes(w.getnframes()), w.getframerate()

    _engine = None
    def synth_local(text: str):
        nonlocal _engine
        if _engine is None:
            from kokoro_cli.engine import SpeechEngine
            t = time.monotonic()
            _engine = SpeechEngine(VARIANT)
            log(f"local engine ({VARIANT}) ready in {time.monotonic()-t:.1f}s")
        speech = _engine.synthesize(text, voice=voice, speed=SPEED)
        pcm = (np.clip(np.asarray(speech.samples, dtype=np.float32),
                       -1, 1) * 32767).astype('<i2').tobytes()
        return pcm, speech.sample_rate

    def piper_healthy() -> bool:
        try:
            with urllib.request.urlopen(PIPER_URL + "/health",
                                        timeout=0.75) as r:
                import json as _j
                return _j.load(r).get("status") == "ok"
        except Exception:
            return False

    def synth_piper_service(text: str):
        body = json.dumps({"text": text}).encode()
        req = urllib.request.Request(PIPER_URL + "/speak", data=body,
                                     headers={"Content-Type": "application/json"})
        with urllib.request.urlopen(req, timeout=120) as r:
            return r.read(), int(r.headers.get("X-Sample-Rate", PIPER_SR))

    def synth_piper_cli(text: str):
        # One-shot fallback: ~4s model load, still faster than kokoro cold.
        if not os.path.isfile(PIPER_BIN):
            raise RuntimeError(f"piper CLI not found: {PIPER_BIN}")
        p = subprocess.run([PIPER_BIN, "-m", PIPER_MODEL, "--output-raw"],
                           input=text.encode(), capture_output=True, timeout=120)
        if p.returncode != 0 or not p.stdout:
            raise RuntimeError(f"piper cli failed: {p.stderr.decode()[:200]}")
        return p.stdout, PIPER_SR

    use_service = service_healthy()
    use_piper = False
    cache_variant = f"kokoro:{VARIANT}"
    if ENGINE == "piper":
        use_piper = piper_healthy()
        cache_variant = f"piper:{Path(PIPER_MODEL).stem}"
        if use_piper:
            log("backend: piper service")
        else:
            log("backend: piper cli (server down — starting it for next time)")
            subprocess.Popen([str(HERE / "bin" / "piper-serve.sh"), "start"],
                             stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    elif use_service:
        log("backend: warm service")
    else:
        log("backend: local (service down — starting it for next time)")
        subprocess.Popen([str(HERE / "bin" / "kokoro-serve.sh"), "start"],
                         stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

    def resolve(text: str, n: int, total: int):
        k = tts_cache.key(text, voice, SPEED, cache_variant)
        hit = tts_cache.get(runtime, k)
        if hit is not None:
            log(f"chunk {n}/{total}: cache HIT ({len(hit[0])//2/hit[1]:.1f}s audio)")
            return hit
        t1 = time.monotonic()
        nonlocal use_service, use_piper
        if ENGINE == "piper":
            if use_piper:
                try:
                    pcm, sr = synth_piper_service(text)
                    log(f"chunk {n}/{total}: piper {time.monotonic()-t1:.1f}s")
                    tts_cache.put(runtime, k, pcm, sr)
                    return pcm, sr
                except Exception as e:
                    log(f"chunk {n}/{total}: piper service failed ({e}), cli fallback")
                    use_piper = False
            try:
                pcm, sr = synth_piper_cli(text)
                log(f"chunk {n}/{total}: piper cli {time.monotonic()-t1:.1f}s")
                tts_cache.put(runtime, k, pcm, sr)
                return pcm, sr
            except Exception as e:
                log(f"chunk {n}/{total}: piper cli failed ({e}), kokoro fallback")
        if use_service:
            try:
                pcm, sr = synth_service(text)
                log(f"chunk {n}/{total}: service {time.monotonic()-t1:.1f}s")
                tts_cache.put(runtime, k, pcm, sr)
                return pcm, sr
            except Exception as e:
                log(f"chunk {n}/{total}: service failed ({e}), local fallback")
                use_service = False
        try:
            pcm, sr = synth_local(text)
        except Exception as e:
            log(f"all TTS backends failed: {e}")
            raise RuntimeError("no working voice backend") from e
        log(f"chunk {n}/{total}: local synth {time.monotonic()-t1:.1f}s ({len(pcm)//2/sr:.1f}s audio)")
        tts_cache.put(runtime, k, pcm, sr)
        return pcm, sr

    sentences = split_sentences(text)
    log(f"{len(text)} chars in {len(sentences)} chunk(s), voice={voice}")

    aplay = shutil.which("aplay")
    if not aplay:
        print("speak.py: aplay not found", file=sys.stderr); sys.exit(3)

    def start_player(rate: int = SR):
        # aplay stderr goes to the debug log (was DEVNULL: silent deaths
        # caused mystery mid-speech cutoffs with zero evidence).
        err = open(LOGFILE, "a") if LOGFILE else subprocess.DEVNULL
        return subprocess.Popen([aplay, "-r", str(rate), "-f", "S16_LE",
                                 "-t", "raw", "-c", "1"],
                                stdin=subprocess.PIPE, stderr=err)

    def emit_level(frame):
        # Publish live voice energy for the overlay waveform (tmpfs, ~10Hz).
        try:
            a = np.frombuffer(frame, dtype="<i2")
            peak = float(np.max(np.abs(a))) if a.size else 0.0
            with open(level_file, "w") as f:
                f.write(str(min(100, int(peak / 327.67))))
        except Exception:
            pass

    def write_frames(player, pcm, sr):
        # Slice into ~100ms frames so the overlay waveform tracks the voice
        # in realtime; pipe backpressure paces the level emits to the audio.
        step = max(2, (sr // 10) * 2)
        assert player.stdin is not None
        for i in range(0, len(pcm), step):
            frame = pcm[i:i+step]
            emit_level(frame)
            player.stdin.write(frame)

    def feed(player, pcm, n, sr):
        # If aplay died mid-stream (device hiccup), restart it and keep
        # going instead of silently truncating the speech.
        if player.poll() is not None:
            log(f"chunk {n}: aplay died (rc={player.returncode}), restarting")
            try: player.stdin.close()
            except Exception: pass
            player = start_player()
        try:
            write_frames(player, pcm, sr)
        except (BrokenPipeError, ValueError):
            log(f"chunk {n}: pipe broke, restarting aplay once")
            try: player.stdin.close()
            except Exception: pass
            player = start_player()
            write_frames(player, pcm, sr)
        return player

    # Pre-resolve first chunk so notify ~= first audible audio.
    t0 = time.monotonic()
    first_pcm, first_sr = resolve(sentences[0], 1, len(sentences))
    player = start_player(rate=first_sr)
    # Fire notification AT first audio, not before synth
    subprocess.Popen(["notify-send", TITLE, text])
    player = feed(player, first_pcm, 1, first_sr)

    for n, s in enumerate(sentences[1:], 2):
        pcm, sr = resolve(s, n, len(sentences))
        player = feed(player, pcm, n, sr)
    try:
        player.stdin.close()
    except Exception:
        pass
    rc = player.wait()
    try:
        os.remove(level_file)
    except OSError:
        pass
    log(f"done rc={rc} total={time.monotonic()-t0:.1f}s")

if __name__ == "__main__":
    main()
