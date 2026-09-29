# Prints what a voice-over clip actually says, with segment times (sanity check before alignment).
# Usage: .venv/bin/python scripts/transcribe.py assets/audio/vo/<id>.wav
import sys
from faster_whisper import WhisperModel

m = WhisperModel("small.en", device="cpu", compute_type="int8")
for f in sys.argv[1:]:
    segs, info = m.transcribe(f, word_timestamps=True)
    print(f"== {f} ({info.duration:.2f} s)")
    for s in segs:
        print(f"{s.start:6.2f}-{s.end:6.2f} {s.text}")
