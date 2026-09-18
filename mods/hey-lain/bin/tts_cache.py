"""Shared raw-PCM sentence cache for hey-lain TTS (stdlib only).

Keyed by SHA1(voice|speed|variant|text); values are raw s16le mono PCM at
the engine's native rate (24000Hz for Kokoro — stored alongside as .sr).
Lives in tmpfs: fast, auto-cleared on reboot. Importable from any python.
"""
import hashlib
from pathlib import Path

CACHE_MAX_BYTES = 64 * 1024 * 1024


def cache_dir(runtime: str) -> Path:
    d = Path(runtime) / "tts-cache"
    d.mkdir(parents=True, exist_ok=True)
    return d


def key(text: str, voice: str, speed: float, variant: str) -> str:
    h = hashlib.sha1()
    h.update(f"{voice}|{speed}|{variant}|{text}".encode())
    return h.hexdigest()


def get(runtime: str, k: str):
    d = cache_dir(runtime)
    pcm, sr = d / f"{k}.pcm", d / f"{k}.sr"
    if pcm.is_file() and sr.is_file():
        try:
            pcm.touch()
            return pcm.read_bytes(), int(sr.read_text().strip())
        except (OSError, ValueError):
            pass
    return None


def put(runtime: str, k: str, pcm: bytes, sample_rate: int) -> None:
    d = cache_dir(runtime)
    try:
        (d / f"{k}.pcm").write_bytes(pcm)
        (d / f"{k}.sr").write_text(str(sample_rate))
    except OSError:
        return
    prune(runtime)


def prune(runtime: str) -> None:
    d = cache_dir(runtime)
    try:
        files = sorted(d.glob("*.pcm"), key=lambda p: p.stat().st_mtime)
        total = sum(p.stat().st_size for p in files)
        for p in files:
            if total <= CACHE_MAX_BYTES:
                break
            try:
                total -= p.stat().st_size
                p.unlink()
                (d / f"{p.stem}.sr").unlink(missing_ok=True)
            except OSError:
                pass
    except OSError:
        pass
