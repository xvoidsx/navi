#!/usr/bin/env python3
"""Live mic levels for the Hey Lain! overlay waveform.

Tails the in-progress recording wav (44-byte header + s16le mono, written
progressively by pw-record/arecord), computes the peak of the most recent
~50ms of audio every 100ms, and writes 0-100 to <level-file> on tmpfs.

Usage: mic-level.py <wav> <level-file>
Exits when the wav stops growing (recorder gone) or disappears.
Stdlib only.
"""
import os
import struct
import sys
import time

SAMPLE_BYTES = 2  # s16le mono
WINDOW_BYTES = 1600  # ~50ms at 16kHz


def data_start(path):
    """Byte offset where PCM begins (skip the wav header robustly)."""
    try:
        with open(path, "rb") as f:
            head = f.read(128)
    except OSError:
        return None
    idx = head.find(b"data")
    if idx != -1 and idx + 8 <= len(head):
        return idx + 8
    if len(head) >= 44:
        return 44
    return None


def peak_of(path, start, size):
    want = min(WINDOW_BYTES, size - start)
    if want < SAMPLE_BYTES:
        return 0
    try:
        with open(path, "rb") as f:
            f.seek(size - want)
            data = f.read(want)
    except OSError:
        return -1
    n = (len(data) // SAMPLE_BYTES) * SAMPLE_BYTES
    if n < SAMPLE_BYTES:
        return 0
    vals = struct.unpack("<%dh" % (n // SAMPLE_BYTES), data[:n])
    return max(abs(v) for v in vals)


def main():
    wav_path, level_path = sys.argv[1], sys.argv[2]
    last_size = -1
    still = 0
    while True:
        try:
            size = os.path.getsize(wav_path)
        except OSError:
            break
        if size == last_size:
            still += 1
            if still >= 20:  # 2s of no growth: the recorder is gone
                break
        else:
            still = 0
            last_size = size
        start = data_start(wav_path)
        if start is None:
            time.sleep(0.1)
            continue
        peak = peak_of(wav_path, start, size)
        if peak < 0:
            break
        level = min(100, int(peak / 327.67))
        try:
            with open(level_path, "w") as f:
                f.write(str(level))
        except OSError:
            break
        time.sleep(0.1)


if __name__ == "__main__":
    main()
