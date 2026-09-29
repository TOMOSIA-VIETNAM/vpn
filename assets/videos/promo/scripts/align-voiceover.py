# Aligns every voice-over clip to its script locally (faster-whisper word timestamps)
# and writes the voice's vo-lines file: per scene, per line start/end and token times in clip-local seconds.
# Script tokens keep their spelling, anchor tags included ("ボトルネック{bottleneck}"), because the timeline
# looks anchors up on them; only their times come from the recognizer.
# - Space-separated languages (en) match recognized words to script words; words heard differently
#   ("Claude dot M D" -> "claw.md") get times interpolated between the nearest matched words.
# - Languages written without spaces (ja) match character by character, so a script phrase gets the times of
#   the recognized characters it covers, however the recognizer split them.
# Usage: .venv/bin/python scripts/align-voiceover.py [--voice=en|ja] [--force] [--only=id1,id2]
# --only aligns just those scenes and prints them without writing the vo-lines file (a check before a full run).
import difflib
import json
import re
import subprocess
import sys
import unicodedata
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
FORCE = "--force" in sys.argv
ONLY = next((a[7:].split(",") for a in sys.argv if a.startswith("--only=")), None)
VOICE = next((a[8:] for a in sys.argv if a.startswith("--voice=")), "en")
V = json.loads((ROOT / "data/voices.json").read_text())[VOICE]
BY_CHAR = V["language"] in ("ja", "zh")
NUMBERS = {"1": "one", "2": "two", "3": "three", "4": "four", "5": "five", "6": "six", "10": "ten", "30": "thirty"}
TAG = re.compile(r"\{[^}]+\}")


def norm(t):
    t = re.sub(r"[^\w']", "", TAG.sub("", t).lower())
    return NUMBERS.get(t, t)


def chars(t):
    """Comparable characters of a token: NFKC, lower case, letters and digits only."""
    return [c for c in unicodedata.normalize("NFKC", TAG.sub("", t)).lower() if c.isalnum()]


def transcribe(model, wav):
    segs, _ = model.transcribe(str(wav), word_timestamps=True, language=V["language"])
    return [{"t": w.word.strip(), "s": w.start, "e": w.end} for s in segs for w in s.words]


def fill(times, lo0, hi0):
    """Spread each run of unknown (None) entries evenly between its known neighbours."""
    i = 0
    while i < len(times):
        if times[i]:
            i += 1
            continue
        j = i
        while j < len(times) and not times[j]:
            j += 1
        lo = times[i - 1][1] if i else lo0
        hi = times[j][0] if j < len(times) else hi0
        step = (hi - lo) / (j - i)
        for k in range(i, j):
            times[k] = (lo + step * (k - i), lo + step * (k - i + 1))
        i = j
    return times


def align_words(tokens, heard):
    a = [norm(t) for t in tokens]
    b = [norm(w["t"]) for w in heard]
    times = [None] * len(tokens)
    for m in difflib.SequenceMatcher(None, a, b, autojunk=False).get_matching_blocks():
        for k in range(m.size):
            w = heard[m.b + k]
            times[m.a + k] = (w["s"], w["e"])
    matched = sum(1 for x in times if x)
    return fill(times, heard[0]["s"], heard[-1]["e"]), matched, len(tokens)


def align_chars(tokens, heard):
    # every recognized character gets an equal share of its word's time span
    hc, ht = [], []
    for w in heard:
        cs = chars(w["t"])
        for k, c in enumerate(cs):
            d = (w["e"] - w["s"]) / len(cs)
            hc.append(c)
            ht.append((w["s"] + d * k, w["s"] + d * (k + 1)))
    sc, owner = [], []
    for i, t in enumerate(tokens):
        for c in chars(t):
            sc.append(c)
            owner.append(i)
    ctimes = [None] * len(sc)
    for m in difflib.SequenceMatcher(None, sc, hc, autojunk=False).get_matching_blocks():
        for k in range(m.size):
            ctimes[m.a + k] = ht[m.b + k]
    matched = sum(1 for x in ctimes if x)
    ctimes = fill(ctimes, heard[0]["s"], heard[-1]["e"])
    times = [None] * len(tokens)
    for c, i in enumerate(owner):
        s, e = ctimes[c]
        times[i] = (min(times[i][0], s), max(times[i][1], e)) if times[i] else (s, e)
    return fill(times, heard[0]["s"], heard[-1]["e"]), matched, len(sc)


def silences(wav, db=-35, min_len=0.25):
    """Pauses in a clip, from ffmpeg silencedetect: [(start, end)] in seconds, touching pauses merged. A pause
    that runs to the end of the clip ends at infinity."""
    log = subprocess.run(["ffmpeg", "-hide_banner", "-nostats", "-i", str(wav), "-af", f"silencedetect=n={db}dB:d={min_len}", "-f", "null", "-"],
                         capture_output=True, text=True).stderr
    h, m, sec = re.search(r"Duration: (\d+):(\d+):([\d.]+)", log).groups()
    duration = int(h) * 3600 + int(m) * 60 + float(sec)
    starts = [float(x) for x in re.findall(r"silence_start: (-?[\d.]+)", log)]
    ends = [float(x) for x in re.findall(r"silence_end: ([\d.]+)", log)] + [duration] * len(starts)
    merged = []
    for a, b in zip(starts, ends):
        if merged and a - merged[-1][1] < 0.03:
            merged[-1][1] = b
        else:
            merged.append([a, b])
    return [(a, float("inf") if b >= duration - 0.03 else b) for a, b in merged]


def snap_to_pauses(times, tokens, pauses, reach=0.8):
    """The recognizer often stretches a word across the pause after it. Each measured pause moves the nearest
    word boundary onto it: the word after starts where the pause ends, and the words heard inside the pause are
    squeezed, by length, into the speech before it."""
    times = [list(t) for t in times]
    weight = [max(1, len(chars(t))) for t in tokens]

    def squeeze(lo, hi, t0, t1):  # words lo..hi-1 share [t0, t1]
        total = sum(weight[lo:hi])
        for k in range(lo, hi):
            times[k][0] = t0
            t0 += (t1 - t0) * weight[k] / total if total else 0
            total -= weight[k]
            times[k][1] = t0

    for a, b in pauses:
        if a <= 0.02:  # leading silence: speech starts after it
            times[0][0] = max(times[0][0], b)
            continue
        if b == float("inf"):  # trailing silence: squeeze words heard in it back before it
            m = max([k for k in range(len(times)) if times[k][0] < a] or [0])
            squeeze(m, len(times), times[m][0], a)
            continue
        # distance of each boundary to the pause; a word heard straddling the pause counts as on it, and a
        # boundary after punctuation is preferred, since the narrator pauses at commas and full stops
        def dist(j):
            d = abs(times[j][0] - b)
            if times[j][0] < a and times[j][1] > b:
                d = min(d, 0.1)
            return d - (0.4 if re.search(r"[.,:;?!]$", TAG.sub("", tokens[j - 1])) else 0)

        cand = [j for j in range(1, len(times)) if dist(j) < reach]
        if not cand:
            continue
        j = min(cand, key=dist)
        m = max([k for k in range(j) if times[k][0] < a] or [0])
        squeeze(m, j, times[m][0], a)
        times[j][0] = b
        times[j][1] = max(times[j][1], b + 0.12)
    for j in range(1, len(times)):  # keep words ordered after the moves
        times[j][0] = max(times[j][0], times[j - 1][1])
        times[j][1] = max(times[j][1], times[j][0] + 0.05)
    return [tuple(t) for t in times]


def main():
    from faster_whisper import WhisperModel

    script = json.loads((ROOT / "data/script.json").read_text())
    cache = ROOT / V["align"]
    cache.mkdir(parents=True, exist_ok=True)
    model = None
    result = {}
    for s in [x for x in script["scenes"] if not ONLY or x["id"] in ONLY]:
        wav = ROOT / V["vo"] / f"{s['id']}.wav"
        raw = cache / f"{s['id']}.json"
        if FORCE or not raw.exists() or raw.stat().st_mtime < wav.stat().st_mtime:
            model = model or WhisperModel(V["whisper"], device="cpu", compute_type="int8")
            raw.write_text(json.dumps(transcribe(model, wav), ensure_ascii=False))
        heard = json.loads(raw.read_text())
        # the display text (with its anchor tags) is aligned by default: the recognizer writes "VPN", not the
        # phonetic respelling in the say field; a voice sets "alignSay": true to align against the say field
        spoken = lambda l: (V.get("alignSay") and l.get(V["say"])) or l[V["text"]]
        per_line = [[t for t in spoken(l).split() if chars(t)] for l in s["lines"]]
        tokens = [t for line in per_line for t in line]
        times, matched, total = (align_chars if BY_CHAR else align_words)(tokens, heard)
        times = snap_to_pauses(times, tokens, silences(wav))
        unit = "characters" if BY_CHAR else "words"
        if matched < 0.8 * total:
            print(f"WARN {s['id']}: only {matched}/{total} script {unit} heard; check the clip", file=sys.stderr)
        lines, k = [], 0
        for line in per_line:
            words = [{"t": t, "s": round(times[k + i][0], 3), "e": round(times[k + i][1], 3)} for i, t in enumerate(line)]
            k += len(line)
            lines.append({"start": words[0]["s"], "end": words[-1]["e"], "words": words})
        result[s["id"]] = {"speechStart": lines[0]["start"], "speechEnd": lines[-1]["end"], "lines": lines}
        print(s["id"], f"{matched}/{total}", " | ".join(f"{l['start']:.2f}-{l['end']:.2f}" for l in lines))
    if ONLY:
        for sid, r in result.items():
            for l in r["lines"]:
                print(" ", " ".join(f"{w['t']}@{w['s']:.2f}" for w in l["words"]))
        return
    (ROOT / V["lines"]).write_text(json.dumps(result, indent=1, ensure_ascii=False))


main()
