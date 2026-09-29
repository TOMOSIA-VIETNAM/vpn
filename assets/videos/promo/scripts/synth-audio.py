"""Synthesize the background music, the outro bed and the sound effects offline.

Usage: .venv/bin/python scripts/synth-audio.py [music|outro|sfx|all] [--arrangement=data/music-arrangement.json]

  music  assets/audio/music/bgm.flac      48 kHz stereo 24-bit, built on the beat grid
                                          of data/music-arrangement.json, then verified
  outro  assets/audio/music/outro-raw.mp3 calm piano/pad bed, ~26 s
  sfx    assets/audio/sfx/<id>.mp3        one short effect per id in SFX
  all    everything above, then an ffprobe summary of every output

Only numpy, scipy and the ffmpeg binary are used. Every random source has a fixed seed,
so two runs produce identical files. Quarter note n of the main track starts exactly at
beat0 + n * 60 / bpm seconds (rounded to the nearest sample): kicks and snares are placed
on that grid without any timing offset, because the video cuts on it.
"""
import functools
import json
import re
import subprocess
import sys
from pathlib import Path

import numpy as np
from scipy.ndimage import minimum_filter1d, uniform_filter1d
from scipy.signal import butter, hilbert, oaconvolve, resample_poly, sosfilt, sosfiltfilt

ROOT = Path(__file__).resolve().parent.parent
SR = 48000
# --arrangement=<file> picks another arrangement (one per narrator voice); outputs come from it
ARR_FILE = next((a.split("=", 1)[1] for a in sys.argv if a.startswith("--arrangement=")), "data/music-arrangement.json")
ARR = json.loads((ROOT / ARR_FILE).read_text())
BPM = float(ARR["bpm"])
BEAT = 60.0 / BPM
BEAT0 = float(ARR["beat0"])
SECTIONS = ARR["sections"]
TOTAL_BEATS = SECTIONS[-1]["to"]

CEILING_DB = -1.3        # true-peak ceiling of the limiter (leaves margin under -1 dBFS)
MUSIC_LUFS = -14.0
OUTRO_LUFS = -18.0
SFX_LUFS = -16.0


# ---------------------------------------------------------------- grid + basic helpers

def beat_sample(beat):
    """Sample index of a (possibly fractional) quarter-note position on the grid."""
    return int(round((BEAT0 + beat * BEAT) * SR))


def hz(midi):
    return 440.0 * 2.0 ** ((midi - 69) / 12.0)


def tvec(n):
    return np.arange(n) / SR


def db(x):
    return 20.0 * np.log10(max(float(x), 1e-12))


def lowpass(x, fc, order=2):
    return sosfilt(butter(order, min(fc, 0.45 * SR), "low", fs=SR, output="sos"), x)


def highpass(x, fc, order=2):
    return sosfilt(butter(order, fc, "high", fs=SR, output="sos"), x)


def bandpass(x, lo, hi, order=2):
    return sosfilt(butter(order, [lo, min(hi, 0.45 * SR)], "band", fs=SR, output="sos"), x)


def tv_filter(x, fc, btype="low", ratio=1.6, block=128):
    """Time-varying 2nd-order filter: coefficients follow the cutoff array fc per block,
    filter state is carried across blocks. For btype 'band', the pass band is
    [fc / ratio, fc * ratio]."""
    y = np.empty_like(x)
    zi = np.zeros((1, 2))
    for i in range(0, len(x), block):
        f = float(np.clip(fc[min(i, len(fc) - 1)], 25.0, 0.44 * SR))
        if btype == "band":
            sos = butter(1, [f / ratio, min(f * ratio, 0.45 * SR)], "band", fs=SR, output="sos")
        else:
            sos = butter(2, f, btype, fs=SR, output="sos")
        y[i:i + block], zi = sosfilt(sos, x[i:i + block], zi=zi)
    return y


def saw(freq, n, phase0=0.0):
    """Band-limited sawtooth (polyBLEP); freq may be a scalar or a per-sample array."""
    dt = np.broadcast_to(np.asarray(freq, dtype=float) / SR, (n,))
    ph = (phase0 + np.cumsum(dt) - dt) % 1.0
    y = 2.0 * ph - 1.0
    m = ph < dt
    t = ph[m] / dt[m]
    y[m] -= t + t - t * t - 1.0
    m = ph > 1.0 - dt
    t = (ph[m] - 1.0) / dt[m]
    y[m] -= t * t + t + t + 1.0
    return y


def square(freq, n, phase0=0.0):
    return 0.5 * (saw(freq, n, phase0) - saw(freq, n, phase0 + 0.5))


def sine_sweep(freq, n, phase0=0.0):
    """Sine whose instantaneous frequency follows freq; the first sample has phase phase0."""
    f = np.broadcast_to(np.asarray(freq, dtype=float), (n,))
    ph = 2.0 * np.pi * (np.cumsum(f) - f[0]) / SR + phase0
    return np.sin(ph)


def adsr(n_hold, a, d, s, r):
    """Attack/decay/sustain for n_hold samples, then a release of r seconds."""
    na, nd, nr = int(a * SR), int(d * SR), int(r * SR)
    env = np.full(n_hold + nr, s, dtype=float)
    na = min(na, n_hold)
    if na:
        env[:na] = np.linspace(0.0, 1.0, na, endpoint=False)
    nd = max(0, min(nd, n_hold - na))
    if nd:
        env[na:na + nd] = 1.0 - (1.0 - s) * np.linspace(0.0, 1.0, nd, endpoint=False)
    level = env[n_hold - 1] if n_hold else s
    if nr:
        env[n_hold:] = level * np.linspace(1.0, 0.0, nr) ** 2
    return env


def fade_edges(x, fade_in=0.0, fade_out=0.004):
    x = x.copy()
    ni, no = int(fade_in * SR), int(fade_out * SR)
    if ni:
        x[..., :ni] *= np.linspace(0.0, 1.0, ni)
    if no:
        x[..., -no:] *= np.linspace(1.0, 0.0, no)
    return x


def pan_gains(p):
    """Equal-power pan, p in [-1, 1]."""
    a = (p + 1.0) * np.pi / 4.0
    return np.cos(a), np.sin(a)


def stereo(x, p=0.0):
    gl, gr = pan_gains(p)
    return np.vstack([x * gl, x * gr])


def rng_for(*key):
    """Deterministic RNG derived from a sound's parameters."""
    return np.random.default_rng(_stable_seed(key))


def _stable_seed(key):
    """FNV-1a hash of the key's repr (Python's hash() is salted per process)."""
    h = 2166136261
    for ch in repr(key).encode():
        h = ((h ^ ch) * 16777619) % (2 ** 32)
    return h


# ---------------------------------------------------------------- reverb

@functools.lru_cache(maxsize=None)
def reverb_ir(seconds, rt60, seed, lp=7000.0, predelay=0.012):
    """Stereo impulse response: decorrelated noise with an exponential decay (-60 dB at
    rt60), low-passed so the tail darkens, normalised to unit energy per channel."""
    rng = np.random.default_rng(seed)
    n = int(seconds * SR)
    t = tvec(n)
    env = 10.0 ** (-3.0 * t / rt60)
    ir = rng.standard_normal((2, n)) * env
    ir = sosfilt(butter(2, lp, "low", fs=SR, output="sos"), ir, axis=-1)
    ir /= np.sqrt(np.sum(ir ** 2, axis=-1, keepdims=True))
    pad = np.zeros((2, int(predelay * SR)))
    return np.concatenate([pad, ir], axis=-1)


def apply_reverb(send, ir):
    n = send.shape[-1]
    return np.vstack([oaconvolve(send[c], ir[c])[:n] for c in range(2)])


# ---------------------------------------------------------------- loudness + limiting

_K_SOS = np.array([[1.53512485958697, -2.69169618940638, 1.19839281085285,
                 1.0, -1.69065929318241, 0.73248077421585],
                [1.0, -2.0, 1.0, 1.0, -1.99004745483398, 0.99007225036621]])


def k_weight(x):
    """ITU-R BS.1770 K-weighting at 48 kHz."""
    return sosfilt(_K_SOS, x, axis=-1)


def block_power(x, win, hop):
    y = k_weight(np.atleast_2d(x)) ** 2
    c = np.concatenate([np.zeros((y.shape[0], 1)), np.cumsum(y, axis=-1)], axis=-1)
    starts = np.arange(0, max(1, y.shape[-1] - win + 1), hop)
    return ((c[:, starts + win] - c[:, starts]) / win).sum(axis=0) if y.shape[-1] >= win else \
        np.array([y.sum() / win])


def integrated_lufs(x):
    """Gated integrated loudness (BS.1770-4): 400 ms blocks, 75 % overlap."""
    z = block_power(x, int(0.4 * SR), int(0.1 * SR))
    lk = -0.691 + 10.0 * np.log10(np.maximum(z, 1e-20))
    z = z[lk > -70.0]
    if not len(z):
        return -np.inf
    rel = -0.691 + 10.0 * np.log10(z.mean()) - 10.0
    z = z[(-0.691 + 10.0 * np.log10(z)) > rel]
    return -0.691 + 10.0 * np.log10(z.mean())


def max_momentary_lufs(x):
    """Loudest 400 ms window; shorter sounds are measured over a zero-padded 400 ms."""
    win = int(0.4 * SR)
    if x.shape[-1] < win:
        x = np.concatenate([x, np.zeros((x.shape[0], win - x.shape[-1]))], axis=-1)
    z = block_power(x, win, int(0.01 * SR))
    return -0.691 + 10.0 * np.log10(max(z.max(), 1e-20))


def true_peak_env(x):
    """Per-sample true-peak estimate (4x oversampled), max over channels."""
    up = resample_poly(x, 4, 1, axis=-1)
    n = x.shape[-1]
    return np.abs(up[:, :4 * n]).reshape(x.shape[0], n, 4).max(axis=-1).max(axis=0)


def true_peak(x):
    return float(true_peak_env(x).max())


def limit(x, ceiling_db=CEILING_DB, hold=0.008):
    """Offline look-ahead peak limiter: the gain curve is min-filtered then box-smoothed
    around every over-ceiling peak, so gain falls before the peak without delaying audio."""
    ceil = 10.0 ** (ceiling_db / 20.0)
    for _ in range(3):
        g = np.minimum(1.0, ceil / np.maximum(true_peak_env(x), 1e-9))
        if g.min() >= 1.0:
            break
        r = max(1, int(hold * SR))
        g = uniform_filter1d(minimum_filter1d(g, 2 * r + 1), r + 1)
        x = x * g
    tp = true_peak(x)
    if tp > ceil:
        x = x * (ceil / tp)
    return x


def soft_clip(x, drive=1.0):
    """Smooth saturation that is transparent at low level (unity small-signal gain)."""
    return np.tanh(drive * x) / drive


# ---------------------------------------------------------------- output

def encode(x, path, codec):
    """Pipe float32 stereo PCM into ffmpeg; codec 'flac24' or 'mp3'."""
    path.parent.mkdir(parents=True, exist_ok=True)
    pcm = np.ascontiguousarray(x.T.astype(np.float32)).tobytes()
    out = ["-c:a", "flac", "-sample_fmt", "s32", "-bits_per_raw_sample", "24"] if codec == "flac24" \
        else ["-c:a", "libmp3lame", "-b:a", "192k"]
    subprocess.run(["ffmpeg", "-v", "error", "-y", "-f", "f32le", "-ar", str(SR), "-ac", "2",
                    "-i", "-", *out, str(path)], input=pcm, check=True)


def decode(path, mono=False):
    raw = subprocess.run(["ffmpeg", "-v", "error", "-i", str(path), "-ac", "1" if mono else "2",
                          "-ar", str(SR), "-f", "f32le", "-"], capture_output=True, check=True).stdout
    a = np.frombuffer(raw, dtype=np.float32).astype(float)
    return a if mono else a.reshape(-1, 2).T


def ebur128(path):
    """Integrated loudness and true peak as measured by ffmpeg's ebur128 filter."""
    err = subprocess.run(["ffmpeg", "-nostats", "-i", str(path), "-af", "ebur128=peak=true",
                          "-f", "null", "-"], capture_output=True, text=True).stderr
    summary = err[err.rfind("Summary:"):]
    i = float(re.search(r"I:\s+(-?[\d.]+|-inf) LUFS", summary).group(1))
    p = float(re.search(r"Peak:\s+(-?[\d.]+|-inf) dBFS", summary).group(1))
    return i, p


def ffprobe(path):
    out = subprocess.run(["ffprobe", "-v", "error", "-select_streams", "a:0", "-show_entries",
                          "stream=sample_rate,channels,codec_name:format=duration", "-of", "json",
                          str(path)], capture_output=True, text=True, check=True).stdout
    j = json.loads(out)
    s = j["streams"][0]
    return s["codec_name"], int(s["sample_rate"]), int(s["channels"]), float(j["format"]["duration"])


# ================================================================= instruments
# Every generator returns a fresh-looking but cached array; callers never mutate it.

@functools.lru_cache(maxsize=None)
def kick(f_hi=150.0, f_lo=45.0, decay=0.32, click=0.5, drive=1.6):
    """Boom-bap kick: sine with a fast pitch drop f_hi -> f_lo plus a noise click.
    The sine starts at phase 0 and the click at sample 0, so the transient is sample 0."""
    n = int(0.6 * SR)
    t = tvec(n)
    f = f_lo + (f_hi - f_lo) * np.exp(-t / 0.032)
    body = sine_sweep(f, n) * np.exp(-t / decay)
    rng = rng_for("kick-click")
    ck = highpass(rng.standard_normal(n), 1200.0) * np.exp(-t / 0.0035) * click
    ck += np.sin(2 * np.pi * 2200 * t) * np.exp(-t / 0.002) * click * 0.6
    y = np.tanh(drive * (body + ck)) / np.tanh(drive)
    return fade_edges(y, 0.0, 0.02)


@functools.lru_cache(maxsize=None)
def snare(tone=190.0, decay=0.16, clap=0.0, bright=1.0, seed=0):
    """Noise + tone-body snare; optional hand-clap layer whose first burst is at sample 0."""
    n = int((decay * 5 + 0.05) * SR)
    t = tvec(n)
    rng = rng_for("snare", seed)
    noise = bandpass(rng.standard_normal(n), 1400.0, 7000.0 * bright + 1500) * np.exp(-t / decay)
    f = tone * (1.0 + 0.35 * np.exp(-t / 0.012))
    body = sine_sweep(f, n) * np.exp(-t / 0.07)
    body += 0.4 * sine_sweep(f * 1.52, n) * np.exp(-t / 0.04)
    y = 0.8 * noise + 0.9 * body
    if clap:
        c = np.zeros(n)
        cn = bandpass(rng.standard_normal(n), 900.0, 5000.0)
        for off in (0.0, 0.009, 0.019):
            k = int(off * SR)
            c[k:] += cn[:n - k] * np.exp(-t[:n - k] / 0.006)
        c += cn * np.exp(-t / 0.11) * 0.5
        y += clap * c
    y = np.tanh(1.3 * y)
    return fade_edges(y, 0.0, 0.01)


_HAT_PARTIALS = (205.3, 304.4, 369.6, 522.7, 540.0, 800.0)


@functools.lru_cache(maxsize=None)
def hat(decay=0.035, seed=0):
    """Closed/open hat: metallic square cluster + noise, high-passed."""
    n = int((decay * 6 + 0.01) * SR)
    t = tvec(n)
    rng = rng_for("hat", seed)
    metal = sum(square(f * 3.1, n, rng.random()) for f in _HAT_PARTIALS) / len(_HAT_PARTIALS)
    y = 0.6 * rng.standard_normal(n) + 0.8 * metal
    y = highpass(y, 7200.0, order=4) * np.exp(-t / decay)
    return fade_edges(y, 0.0, 0.004)


@functools.lru_cache(maxsize=None)
def bass808(midi, n_hold, drive=2.2, detune_cents=0.0):
    """Sustained 808: sine with a short pitch drop from above, saturated for harmonics."""
    f0 = hz(midi) * 2.0 ** (detune_cents / 1200.0)
    env = adsr(n_hold, 0.004, 0.25, 0.75, 0.07)
    n = len(env)
    t = tvec(n)
    f = f0 * (1.0 + 0.18 * np.exp(-t / 0.018))
    env *= np.exp(-t / 2.5)
    y = np.tanh(drive * sine_sweep(f, n) * env) / np.tanh(drive)
    return y


def _detuned_saws(notes, n, detune_cents, voices, seed):
    rng = rng_for("saws", notes, seed)
    left, right = np.zeros(n), np.zeros(n)
    for m in notes:
        for v in range(voices):
            c = detune_cents * (v - (voices - 1) / 2.0) / max(1, (voices - 1) / 2.0)
            s = saw(hz(m) * 2.0 ** (c / 1200.0), n, rng.random())
            p = -0.6 + 1.2 * v / max(1, voices - 1)
            gl, gr = pan_gains(p)
            left += s * gl
            right += s * gr
    k = 1.0 / (len(notes) * voices) ** 0.5
    return np.vstack([left, right]) * k


@functools.lru_cache(maxsize=None)
def stab(notes, n_hold, hi=3200.0, lo=800.0, detune=9.0, attack=0.008, release=0.12, seed=0):
    """Brass-like chord stab: detuned saws, filter envelope crossfading bright -> dark."""
    env = adsr(n_hold, attack, 0.18, 0.55, release)
    n = len(env)
    t = tvec(n)
    x = _detuned_saws(notes, n, detune, 3, seed)
    bright = sosfilt(butter(2, hi, fs=SR, output="sos"), x, axis=-1)
    dark = sosfilt(butter(2, lo, fs=SR, output="sos"), x, axis=-1)
    fenv = np.exp(-t / 0.09)
    return (bright * fenv + dark * (1.0 - fenv)) * env


@functools.lru_cache(maxsize=None)
def pad(notes, n_hold, cutoff=900.0, attack=0.35, release=0.6, seed=0):
    """Soft sustained pad: wide detuned saws through a static low-pass."""
    env = adsr(n_hold, attack, 0.5, 0.8, release)
    x = _detuned_saws(notes, len(env), 14.0, 4, seed)
    x = sosfilt(butter(2, cutoff, fs=SR, output="sos"), x, axis=-1)
    return x * env


@functools.lru_cache(maxsize=None)
def power_stab(root, n_hold, seed=0):
    """Distorted power chord (root, fifth, octave) saw stab for the drop hook."""
    notes = (root, root + 7, root + 12)
    env = adsr(n_hold, 0.003, 0.12, 0.6, 0.08)
    n = len(env)
    x = _detuned_saws(notes, n, 12.0, 2, seed)
    x = np.tanh(3.5 * x) / np.tanh(3.5)
    x = sosfilt(butter(2, 3800.0, fs=SR, output="sos"), x, axis=-1)
    return x * env


@functools.lru_cache(maxsize=None)
def pluck(midi, seed=0):
    """16th-note arp pluck: square + saw with a decaying filter."""
    n = int(0.28 * SR)
    t = tvec(n)
    f = hz(midi)
    rng = rng_for("pluck", seed)
    x = 0.6 * square(f, n, rng.random()) + 0.4 * saw(f * 1.003, n, rng.random())
    x = tv_filter(x, 600.0 + 3400.0 * np.exp(-t / 0.05))
    return x * np.exp(-t / 0.09) * np.minimum(1.0, t / 0.002 + 1e-9)


@functools.lru_cache(maxsize=None)
def impact(seconds=2.2, seed=0):
    """Trailer impact: sub boom with a falling pitch, low thump, bright crash noise."""
    n = int(seconds * SR)
    t = tvec(n)
    rng = rng_for("impact", seed)
    sub = sine_sweep(32.0 + 60.0 * np.exp(-t / 0.22), n) * np.exp(-t / 0.9)
    thump = lowpass(rng.standard_normal(n), 220.0, 4) * np.exp(-t / 0.08) * 3.0
    crash = highpass(rng.standard_normal((2, n)), 2500.0) * np.exp(-t / 0.7) * 0.35
    mono = np.tanh(1.8 * (sub + thump)) / np.tanh(1.8)
    return fade_edges(np.vstack([mono, mono]) + crash, 0.0, 0.05)


@functools.lru_cache(maxsize=None)
def scratch_source():
    """One second of a vowel-like 'ahh' used as the record under the needle."""
    n = SR
    rng = rng_for("scratch-src")
    x = saw(170.0, n) + 0.3 * rng.standard_normal(n)
    return bandpass(x, 500.0, 900.0) + 0.7 * bandpass(x, 1100.0, 1600.0) + 0.3 * bandpass(x, 2400.0, 3200.0)


def scratch(moves, gain=1.0):
    """Turntable scratch: plays scratch_source at a varying rate. moves is a sequence of
    (duration_s, peak_rate); positive rate = forward, negative = backward. Amplitude
    follows the hand speed, so each move is a pitch-swept chirp."""
    src = scratch_source()
    out, pos = [], 0.3 * SR
    for dur, peak in moves:
        n = int(dur * SR)
        u = np.linspace(0.0, 1.0, n, endpoint=False)
        rate = peak * np.sin(np.pi * u) ** 0.6
        p = pos + np.cumsum(rate)
        pos = p[-1]
        y = np.interp(np.clip(p, 0, len(src) - 1), np.arange(len(src)), src)
        amp = (np.abs(rate) / abs(peak)) ** 0.8
        out.append(y * amp)
    y = np.concatenate(out)
    return y / (np.abs(y).max() + 1e-9) * gain


@functools.lru_cache(maxsize=None)
def riser(n, f_start=300.0, f_end=10000.0, tone_from=53, tone_to=77, seed=0):
    """Rising noise riser with an opening filter and a rising saw tone, n samples long,
    loudest at the very end."""
    t = tvec(n)
    u = t / t[-1]
    rng = rng_for("riser", seed)
    fc = f_start * (f_end / f_start) ** u
    noise = np.vstack([tv_filter(rng.standard_normal(n), fc), tv_filter(rng.standard_normal(n), fc)])
    tone_f = hz(tone_from) * (hz(tone_to) / hz(tone_from)) ** u
    tone = saw(tone_f, n) + saw(tone_f * 1.006, n, 0.3)
    tone = tv_filter(tone, 400.0 + 5000.0 * u ** 2) * 0.35
    amp = u ** 2.2
    return fade_edges((noise + tone) * amp, 0.0, 0.004)


def vinyl(n, seed, hiss=0.012, crackle_rate=14.0):
    """Vinyl surface: band-limited hiss plus sparse random crackles (stereo)."""
    rng = np.random.default_rng(seed)
    out = bandpass(rng.standard_normal((2, n)), 400.0, 5000.0, 1) * hiss
    k = rng.poisson(crackle_rate * n / SR)
    pos = rng.integers(0, max(1, n - 200), k)
    amp = rng.random(k) ** 3 * 0.25
    ch = rng.integers(0, 2, k)
    imp = np.zeros((2, n))
    imp[ch, pos] = amp * rng.choice([-1, 1], k)
    imp = highpass(imp, 1500.0)
    return out + imp


# ================================================================= arrangement

# Two-beat-per-chord is too fast for this tempo, so the i-VI-III-VII progression
# (Fm - Db - Ab - Eb) moves one chord per bar and repeats every four bars.
PROG = [
    dict(name="Fm", bass=29, stab=(53, 56, 60), power=53, arp=(65, 68, 72, 77)),
    dict(name="Db", bass=37, stab=(53, 56, 61), power=49, arp=(65, 68, 73, 77)),
    dict(name="Ab", bass=32, stab=(51, 56, 60), power=56, arp=(63, 68, 72, 75)),
    dict(name="Eb", bass=39, stab=(51, 55, 58), power=51, arp=(63, 67, 70, 75)),
]
STEP = BEAT / 4.0
HAT_SWING = 0.009   # seconds; off-beat 16th hats lean back slightly


def steps(k):
    return int(round(k * STEP * SR))


CHOKE_FADE = int(0.0015 * SR)


class Mix:
    """Named stereo buses; a sound is added at a sample position with gain, pan and a
    reverb send. A choke bus is monophonic like a drum-machine voice: a new sound fades
    out whatever the bus still holds over the 1.5 ms before its start and replaces it
    (sounds on a choke bus must be added in time order)."""

    CHOKE = ("kick", "lofi_kick", "bass", "lofi_bass")

    def __init__(self, n):
        self.n = n
        self.buses = {}
        self.kicks = []

    def bus(self, name):
        if name not in self.buses:
            self.buses[name] = np.zeros((2, self.n))
        return self.buses[name]

    def add(self, name, sig, start, gain=1.0, pan=0.0, send=0.0):
        if start >= self.n:
            return
        s = stereo(sig, pan) if sig.ndim == 1 else sig
        m = min(s.shape[-1], self.n - start)
        bus = self.bus(name)
        if name in self.CHOKE:
            a = max(0, start - CHOKE_FADE)
            bus[:, a:start] *= np.linspace(1.0, 0.0, start - a, endpoint=False)
            bus[:, start:] = 0.0
        bus[:, start:start + m] += s[:, :m] * gain
        if send:
            self.bus("send")[:, start:start + m] += s[:, :m] * gain * send


def chord_for(bar):
    return PROG[bar % 4]


def render_intro(mix, sec, first):
    """No kick: filtered pad + stabs + vinyl; the first intro opens with scratches, the
    second adds closed hats and sustained 808 notes."""
    b0, b1 = sec["from"], sec["to"]
    for bar_beat in range(b0, b1, 4):
        bar = bar_beat // 4
        c = chord_for(bar)
        s = beat_sample(bar_beat)
        hold = min(4, b1 - bar_beat)
        cutoff = 500.0 + 900.0 * (bar_beat / 20.0)
        mix.add("music", pad(c["stab"] + (c["stab"][0] - 12,), steps(4 * hold), cutoff), s, 0.30, send=0.35)
        mix.add("music", stab(c["stab"], steps(2), hi=1400.0, lo=500.0), s, 0.35, send=0.4)
        if not first:
            mix.add("bass", bass808(c["bass"], steps(4 * hold) - int(0.07 * SR)), s, 0.40)
    if first:
        mix.add("fx", impact(1.6, seed=1), beat_sample(b0), 0.55, send=0.4)
        gestures = [(0.0, [(0.11, 2.2), (0.13, -2.6)]), (1.5, [(0.08, 3.0), (0.09, -3.0), (0.07, 2.4)]),
                    (3.5, [(0.12, 2.0), (0.14, -2.4)])]
        for i, (bt, moves) in enumerate(gestures):
            mix.add("fx", scratch(tuple(moves)), beat_sample(b0 + bt), 0.35, pan=(-0.3 if i % 2 else 0.3), send=0.2)
    else:
        for q in range(b0 * 4, b1 * 4, 2):
            s = beat_sample(q / 4.0)
            accent = 1.0 if q % 4 == 2 else 0.7
            mix.add("drums", hat(0.03, seed=q % 7), s, 0.10 * accent, pan=0.25)
    n0, n1 = beat_sample(b0), beat_sample(b1)
    mix.add("fx", vinyl(n1 - n0, seed=10 + b0), n0, 1.0)


def render_groove(mix, sec, mode, thin=None):
    """Full beat. mode: 'groove', 'lofi' (dull generic universe), 'drop' (max energy).
    thin(bar_index) -> level 0..2 removes layers progressively (used by the tail)."""
    lofi, drop = mode == "lofi", mode == "drop"
    dr = "lofi_drums" if lofi else "drums"
    kb = "lofi_kick" if lofi else "kick"
    tb = "lofi_bass" if lofi else "bass"
    tm = "lofi_tonal" if lofi else "music"
    detune = -22.0 if lofi else 0.0
    q0, q1 = sec["from"] * 4, sec["to"] * 4
    loud = 1.12 if drop else 1.0
    for q in range(q0, q1):
        rel = q - q0
        bar, st = rel // 16, rel % 16
        c = chord_for(bar)
        s = beat_sample(q / 4.0)
        th = thin(bar) if thin else 0
        odd = bar % 2 == 1
        # kick
        kick_steps = (0, 7, 10) if odd else (0, 10)
        if drop:
            kick_steps = (0, 7, 10, 14) if bar % 4 == 3 else ((0, 7, 10) if odd else (0, 3, 10))
        if th >= 2:
            kick_steps = (0,)
        if st in kick_steps:
            k = kick(decay=0.36, click=0.6, drive=2.0) if drop else kick()
            mix.add(kb, k, s, (1.0 if st == 0 else 0.85) * loud)
            mix.kicks.append(s)
        # snare / clap
        if st in (4, 12) and th < 2:
            sn = snare(clap=0.9, bright=1.3, seed=st) if drop else snare(clap=0.45, seed=st)
            mix.add(dr, sn, s, 0.80 * loud, pan=0.05, send=0.18)
        if st == 15 and bar % 4 == 3 and th < 1:
            mix.add(dr, snare(tone=210.0, decay=0.08, seed=3), s, 0.18, pan=-0.1)
        # hats
        hat_on = (st % 2 == 0) or drop or (odd and st in (13, 15))
        if th >= 1:
            hat_on = st % 4 == 2
        if hat_on:
            open_hat = drop and odd and st == 14
            h = hat(0.22 if open_hat else 0.032, seed=st % 5)
            g = (0.21 if st % 4 == 2 else 0.14) * (0.45 if lofi else 1.0) * (1.15 if drop else 1.0)
            off = int(HAT_SWING * SR) if st % 2 == 1 else 0
            mix.add(dr, h, s + off, g, pan=0.25 - 0.1 * (st % 3))
        # 808
        if st == 0:
            mix.add(tb, bass808(c["bass"], steps(10) - int(0.07 * SR), drive=2.8 if drop else 2.2,
                                detune_cents=detune), s, 0.40)
        if st == 10 and th < 2:
            m = c["bass"] + (12 if (drop and odd) else 0)
            mix.add(tb, bass808(m, steps(6) - int(0.07 * SR), detune_cents=detune), s, 0.36)
        # chord stabs + pad
        if th < 1:
            if st == 0:
                mix.add(tm, pad(c["stab"], steps(16), 1500.0 if drop else (700.0 if lofi else 1000.0)),
                        s, 0.15 if drop else 0.13, send=0.3)
            hi, lo = (4200.0, 1200.0) if drop else ((1000.0, 500.0) if lofi else (3000.0, 800.0))
            hits = {0: 3, 3: 3} if not odd else {0: 6, 14: 2}
            if lofi:
                hits = {0: 3, 3: 3} if not odd else {0: 6}
            if st in hits:
                notes = tuple(n for n in c["stab"])
                mix.add(tm, stab(notes, steps(hits[st]), hi=hi, lo=lo, detune=18.0 if lofi else 9.0,
                                 seed=bar % 4), s, (0.48 if drop else 0.44) * (0.8 if lofi else 1.0),
                        pan=0.0, send=0.25)
        if drop:
            hook = (0, 3, 6, 10, 12) if bar % 4 == 3 else (0, 3, 6)
            if st in hook:
                mix.add("music", power_stab(c["power"], steps(2 if st else 3), seed=st), s, 0.32, send=0.2)
            arp = c["arp"]
            order = (0, 1, 2, 3, 2, 1)
            mix.add("music", pluck(arp[order[rel % 6]] + (12 if st >= 8 else 0), seed=st % 4), s, 0.09,
                    pan=0.45 if st % 2 else -0.45, send=0.3)
    if drop:
        mix.add("fx", impact(), beat_sample(sec["from"]), 0.9, send=0.5)
    if lofi:
        n0, n1 = beat_sample(sec["from"]), beat_sample(sec["to"])
        mix.add("fx", vinyl(n1 - n0, seed=20, hiss=0.02, crackle_rate=22.0), n0, 1.0)


def render_build(mix, sec):
    """No kick: snare roll accelerating quarters -> 8ths -> 16ths -> 32nds, rising in
    pitch and level, over a riser that ends exactly at the section end."""
    b0, b1 = sec["from"], sec["to"]
    length = b1 - b0
    bounds = [round(length * k / 4) for k in range(5)]
    for k, rate in enumerate((1, 2, 4, 8)):
        n_hits = (bounds[k + 1] - bounds[k]) * rate
        for i in range(n_hits):
            bt = b0 + bounds[k] + i / rate
            p = (bt - b0) / length
            tone = round(180.0 + 150.0 * p, 0)
            dec = round(0.14 - 0.08 * p, 3)
            mix.add("drums", snare(tone=tone, decay=dec, clap=0.3, seed=k), beat_sample(bt),
                    0.22 + 0.5 * p ** 1.3, pan=0.0, send=0.25)
    n0, n1 = beat_sample(b0), beat_sample(b1)
    mix.add("fx", riser(n1 - n0), n0, 0.5, send=0.2)
    mix.add("music", pad(PROG[3]["stab"], n1 - n0 - int(0.3 * SR), 1200.0, attack=0.8, release=0.3),
            n0, 0.10, send=0.3)


def build_music():
    n = beat_sample(TOTAL_BEATS)
    mix = Mix(n)
    intro_seen = 0
    for sec in SECTIONS:
        role = sec["role"]
        if role == "intro":
            render_intro(mix, sec, first=intro_seen == 0)
            intro_seen += 1
        elif role == "groove":
            render_groove(mix, sec, "lofi" if sec.get("lofi") else "groove")
        elif role == "drop":
            render_groove(mix, sec, "drop")
        elif role == "build":
            render_build(mix, sec)
        elif role == "tail":
            render_groove(mix, sec, "groove", thin=lambda bar: min(bar, 2))
        elif role != "stop":
            raise SystemExit(f"unknown section role {role!r}")

    # Kick sidechain: bass and chords duck under each kick for the pumping feel.
    duck_len = int(0.3 * SR)
    t = tvec(duck_len)
    duck_b, duck_m = np.ones(n), np.ones(n)
    shape_b = 1.0 - 0.65 * np.exp(-t / 0.09)
    shape_m = 1.0 - 0.30 * np.exp(-t / 0.10)
    for k in mix.kicks:
        m = min(duck_len, n - k)
        duck_b[k:k + m] = np.minimum(duck_b[k:k + m], shape_b[:m])
        duck_m[k:k + m] = np.minimum(duck_m[k:k + m], shape_m[:m])

    b = mix.buses
    zero = np.zeros((2, n))
    master = b.get("kick", zero) + b.get("drums", zero) + b.get("fx", zero)
    master = master + b.get("bass", zero) * duck_b + b.get("music", zero) * duck_m
    master = master + lofi_process(b.get("lofi_kick", zero) + b.get("lofi_drums", zero), tonal=False)
    master = master + lofi_process(b.get("lofi_bass", zero), tonal=True) * duck_b
    master = master + lofi_process(b.get("lofi_tonal", zero), tonal=True) * duck_m
    master = master + apply_reverb(b["send"], reverb_ir(1.6, 1.2, seed=5)) * 0.35

    master = master * master_envelope(n)
    for name, bus in sorted(b.items()):
        print(f"  bus {name:11s} rms {db(np.sqrt(np.mean(bus ** 2))):6.1f} dBFS")
    return master


def lofi_process(x, tonal):
    """Deliberately dull 'generic universe' sound: tape-like pitch wobble (tonal parts
    only, so drum transients stay on the grid), zero-phase 1.2 kHz low-pass, light
    bit/sample-rate crush."""
    n = x.shape[-1]
    if tonal:
        t = tvec(n)
        d = SR * (0.0025 + 0.0018 * np.sin(2 * np.pi * 0.55 * t) + 0.0005 * np.sin(2 * np.pi * 3.1 * t))
        idx = np.arange(n) - d
        x = np.vstack([np.interp(idx, np.arange(n), x[c]) for c in range(2)])
    x = sosfiltfilt(butter(4, 1200.0, fs=SR, output="sos"), x, axis=-1)
    x = np.round(x * 96.0) / 96.0
    x = np.repeat(x[:, ::3], 3, axis=-1)[:, :n]
    x = sosfiltfilt(butter(2, 2600.0, fs=SR, output="sos"), x, axis=-1)
    return x * 0.9


def master_envelope(n):
    """Gain curve applied after the mix: silence in 'stop' sections (the previous sound
    is faded within 25 ms of the stop start) and the fade-out (up to `fadeOut.resume` when set)."""
    env = np.ones(n)
    fade = int(0.025 * SR)
    for sec in SECTIONS:
        if sec["role"] == "stop":
            a, b = beat_sample(sec["from"]), beat_sample(sec["to"])
            env[a:a + fade] = np.cos(np.linspace(0, np.pi / 2, fade)) ** 2
            env[a + fade:b] = 0.0
    fo = ARR["fadeOut"]
    a = beat_sample(fo["beat"])
    m = int(fo["seconds"] * SR)
    m = min(m, n - a)
    env[a:a + m] *= np.cos(np.linspace(0, np.pi / 2, m)) ** 2
    # silent after the fade; with `resume`, the sections from that beat on play again (post-credits)
    end = beat_sample(fo["resume"]) if "resume" in fo else n
    env[a + m:end] = 0.0
    return env


def master_bus(x, target_lufs, ceiling_db=CEILING_DB, drive=0.9):
    """Gain -> soft clip -> look-ahead limiter, iterating the input gain until the
    integrated loudness lands on target."""
    g = 10.0 ** ((target_lufs - integrated_lufs(x)) / 20.0)
    for _ in range(6):
        y = limit(soft_clip(x * g, drive), ceiling_db)
        err = target_lufs - integrated_lufs(y)
        if abs(err) < 0.1:
            break
        g *= 10.0 ** (err / 20.0)
    return y


# ================================================================= outro bed

@functools.lru_cache(maxsize=None)
def piano(midi, hold, vel=1.0):
    """Piano-ish note: slightly inharmonic sine partials, higher partials decay faster,
    soft hammer noise, damper release after hold seconds."""
    f = hz(midi)
    n = int((hold + 0.6) * SR)
    t = tvec(n)
    y = np.zeros(n)
    rng = rng_for("piano", midi)
    for k in range(1, 11):
        fk = k * f * np.sqrt(1.0 + 0.0004 * k * k)
        if fk > 0.4 * SR:
            break
        tau = 3.2 / (1.0 + 0.7 * (k - 1)) * (220.0 / f) ** 0.3
        amp = vel / k ** 1.4 * (1.0 if k > 1 else 1.2)
        y += amp * np.sin(2 * np.pi * fk * t + rng.random() * 0.3) * (0.7 * np.exp(-t / tau) + 0.3 * np.exp(-t / (tau * 4)))
    y += lowpass(rng.standard_normal(n), 1800.0) * np.exp(-t / 0.01) * 0.05 * vel
    y *= np.minimum(1.0, t / 0.004)
    rel = int(hold * SR)
    y[rel:] *= np.linspace(1.0, 0.0, n - rel) ** 2
    return y


def build_outro(seconds=26.0):
    n = int(seconds * SR)
    mix = Mix(n)
    # (start s, bass + chord notes); F minor colour moving to a hopeful Ab major.
    chords = [
        (0.0, (41, 53, 56, 60, 67)),     # Fm9
        (3.0, (37, 49, 53, 56, 60)),     # Dbmaj7
        (6.0, (34, 46, 53, 56, 61)),     # Bbm7
        (9.0, (39, 51, 55, 58, 63)),     # Eb
        (12.0, (44, 51, 56, 60, 63)),    # Ab
        (15.0, (41, 53, 56, 60, 63)),    # Db/F
        (18.0, (43, 51, 55, 58, 63)),    # Eb/G
        (21.0, (32, 44, 51, 56, 60, 63, 70)),  # Ab add9, held to the end
    ]
    for i, (t0, notes) in enumerate(chords):
        last = i == len(chords) - 1
        hold = (seconds - t0 - 0.6) if last else 3.2
        for j, m in enumerate(notes):
            vel = 0.55 if j == 0 else 0.42
            mix.add("piano", piano(m, round(hold, 2), vel), int((t0 + 0.028 * j) * SR), 1.0,
                    pan=-0.35 + 0.7 * j / max(1, len(notes) - 1), send=0.5)
        pad_notes = tuple(m + 12 for m in notes[1:4])
        mix.add("pad", pad(pad_notes, int((hold + 0.2) * SR), 1300.0, attack=1.2, release=1.6, seed=i),
                int(t0 * SR), 0.12, send=0.6)
    melody = [(1.5, 72), (4.5, 72), (5.25, 75), (7.5, 73), (10.5, 70), (13.5, 72), (14.25, 75),
              (16.5, 77), (19.5, 75), (21.4, 80)]
    for t0, m in melody:
        mix.add("piano", piano(m, 2.2 if t0 < 21 else 4.0, 0.32), int(t0 * SR), 1.0, pan=0.15, send=0.55)
    rng = np.random.default_rng(31)
    air = highpass(rng.standard_normal((2, n)), 3000.0) * 0.004
    mix.add("pad", air, 0, 1.0)
    mix.add("fx", vinyl(n, seed=32, hiss=0.004, crackle_rate=9.0), 0, 0.6)
    b = mix.buses
    x = lowpass(b["piano"], 4200.0) + b["pad"] + b["fx"]
    x = x + apply_reverb(b["send"], reverb_ir(3.5, 2.8, seed=33, lp=5000.0, predelay=0.02)) * 0.45
    env = np.ones(n)
    a = int(21.5 * SR)
    env[a:] = np.cos(np.linspace(0, np.pi / 2, n - a)) ** 2
    return fade_edges(x * env, 0.02, 0.01)


# ================================================================= sound effects

def sfx_swing(n, t, rng):
    u = t / t[-1]
    fc = 500.0 * np.exp(np.log(8.0) * np.sin(np.pi * np.minimum(1.0, u / 0.7)) ** 0.8)
    x = tv_filter(rng.standard_normal(n), fc, "band", ratio=1.5)
    amp = np.minimum(1.0, t / 0.015) * np.exp(-((u - 0.18) / 0.22) ** 2)
    x *= amp
    p = -0.8 + 1.6 * np.minimum(1.0, u / 0.5)
    gl, gr = pan_gains(p)
    return np.vstack([x * gl, x * gr])


def sfx_glitch(n, t, rng):
    f = 1400.0 * (60.0 / 1400.0) ** (t / t[-1])
    tone = square(f, n) * 0.5 + 0.4 * rng.standard_normal(n) * (rng.random(n // 480 + 1).repeat(480)[:n] > 0.5)
    gate = ((t // 0.028) % 3 != 2).astype(float)
    tone = np.round(tone * 8) / 8
    tone = np.repeat(tone[::6], 6)[:n] * gate * np.exp(-t / 0.45)
    thump = sine_sweep(40.0 + 60.0 * np.exp(-t / 0.05), n) * np.exp(-t / 0.2) * 1.3
    y = np.tanh(1.5 * (0.6 * tone + thump))
    return np.vstack([y, np.roll(y, int(0.004 * SR))])


def sfx_punch(n, t, rng):
    snap = bandpass(rng.standard_normal(n), 900.0, 5000.0) * np.exp(-t / 0.018) * 1.4
    body = sine_sweep(60.0 + 160.0 * np.exp(-t / 0.03), n) * np.exp(-t / 0.12)
    sub = np.sin(2 * np.pi * 52.0 * t) * np.exp(-t / 0.18) * 0.6
    y = np.tanh(2.2 * (snap + body + sub))
    return stereo(y)


def sfx_boom(n, t, rng):
    x = impact(n / SR, seed=7)[:, :n]
    tail = apply_reverb(x * 0.6, reverb_ir(1.5, 1.1, seed=8))
    return x + tail * 0.5


def sfx_scratch(n, t, rng):
    y = scratch(((0.13, 2.6), (0.12, -2.8), (0.02, 0.2), (0.14, 3.0), (0.15, -2.6)))
    return stereo(np.pad(y, (0, max(0, n - len(y))))[:n])


def sfx_page(n, t, rng):
    u = t / t[-1]
    flutter = 0.5 + 0.5 * np.abs(np.sin(2 * np.pi * (35.0 + 25.0 * u) * t + rng.random()))
    fc = 1800.0 + 3500.0 * np.sin(np.pi * np.minimum(1, u / 0.8))
    x = tv_filter(rng.standard_normal(n), fc, "band", ratio=1.8) * flutter
    x += lowpass(rng.standard_normal(n), 600.0) * 0.6
    amp = np.minimum(1.0, t / 0.01) * np.exp(-((u - 0.25) / 0.3) ** 2)
    x *= amp
    return np.vstack([x, np.roll(x, 90)])


def sfx_stamp(n, t, rng):
    thud = sine_sweep(48.0 + 70.0 * np.exp(-t / 0.025), n) * np.exp(-t / 0.11)
    knock = lowpass(rng.standard_normal(n), 900.0, 4) * np.exp(-t / 0.03) * 2.0
    slap = bandpass(rng.standard_normal(n), 1500.0, 4000.0) * np.exp(-t / 0.008) * 0.6
    return stereo(np.tanh(2.0 * (thud + knock + slap)))


def sfx_pop(n, t, rng):
    f = 350.0 + 1300.0 * (1.0 - np.exp(-t / 0.012))
    y = sine_sweep(f, n) * np.exp(-t / 0.045)
    y += highpass(rng.standard_normal(n), 3000.0) * np.exp(-t / 0.003) * 0.5
    return stereo(y)


def sfx_zap(n, t, rng):
    jitter = np.repeat(rng.random(n // 240 + 1), 240)[:n]
    f = (900.0 + 2600.0 * jitter) * np.exp(-t / 0.35)
    y = saw(f, n) * 0.6 + square(f * 0.5, n) * 0.3
    bursts = (np.repeat(rng.random(n // 120 + 1), 120)[:n] > 0.45)
    y = y * (0.4 + 0.6 * bursts) + highpass(rng.standard_normal(n), 4000.0) * bursts * 0.5
    y = highpass(y, 300.0) * np.exp(-t / 0.14)
    return np.vstack([y, np.roll(y, 60)])


def sfx_click(n, t, rng):
    y = np.sin(2 * np.pi * 1700.0 * t) * np.exp(-t / 0.008)
    y += bandpass(rng.standard_normal(n), 2000.0, 6000.0) * np.exp(-t / 0.002) * 0.6
    y += np.sin(2 * np.pi * 420.0 * t) * np.exp(-t / 0.012) * 0.5
    return stereo(y)


def sfx_ding(n, t, rng):
    f0 = 1318.5
    y = np.zeros(n)
    for ratio, amp, tau in ((1.0, 1.0, 0.45), (2.0, 0.45, 0.3), (2.76, 0.35, 0.2), (5.4, 0.18, 0.1), (8.93, 0.1, 0.06)):
        y += amp * np.sin(2 * np.pi * f0 * ratio * t) * np.exp(-t / tau)
    y *= np.minimum(1.0, t / 0.0015)
    return np.vstack([y, np.roll(y, 45)])


def sfx_buzz(n, t, rng):
    y = square(110.0, n) + square(116.5, n, 0.3) + 0.5 * saw(55.0, n)
    y = lowpass(np.tanh(1.5 * y), 2200.0) * np.minimum(1.0, t / 0.004)
    return stereo(y)


def sfx_typing(n, t, rng):
    y = np.zeros(n)
    pos = 0
    while pos < n - int(0.03 * SR):
        m = min(int(0.05 * SR), n - pos)
        tt = tvec(m)
        v = 0.6 + 0.4 * rng.random()
        c = bandpass(rng.standard_normal(m), 1800.0 + 1500 * rng.random(), 6500.0) * np.exp(-tt / 0.004)
        c += np.sin(2 * np.pi * (260.0 + 80 * rng.random()) * tt) * np.exp(-tt / 0.01) * 0.6
        y[pos:pos + m] += c * v
        pos += int((0.055 + 0.06 * rng.random()) * SR)
    return np.vstack([y, np.roll(y, 30)])


def sfx_marker(n, t, rng):
    strokes = [(0.0, 0.14), (0.16, 0.13), (0.31, 0.16)]
    y = np.zeros(n)
    for i, (a, d) in enumerate(strokes):
        s, m = int(a * SR), int(d * SR)
        tt = tvec(m)
        f = 2300.0 + 500.0 * np.sin(2 * np.pi * (7.0 + i) * tt) + 200 * i
        squeak = sine_sweep(f, m) * 0.5 + 0.2 * sine_sweep(f * 2.01, m)
        felt = bandpass(rng.standard_normal(m), 3000.0, 9000.0) * 0.5
        env = np.sin(np.pi * tt / tt[-1]) ** 0.4
        y[s:s + m] += (squeak + felt) * env
    return stereo(y, 0.1)


def sfx_riser(n, t, rng):
    return riser(n, 250.0, 12000.0, 48, 84, seed=9)


def sfx_spray(n, t, rng):
    x = highpass(rng.standard_normal((2, n)), 3200.0, 4)
    x = x + bandpass(rng.standard_normal((2, n)), 6000.0, 10000.0) * 0.6
    flutter = 0.85 + 0.15 * np.sin(2 * np.pi * 23.0 * t)
    env = np.minimum(1.0, t / 0.008) * np.where(t < 0.5, 1.0, np.exp(-(t - 0.5) / 0.06))
    return x * flutter * env


SFX = {
    "swing": (0.6, sfx_swing), "glitch": (0.9, sfx_glitch), "punch": (0.5, sfx_punch),
    "boom": (1.8, sfx_boom), "scratch": (0.6, sfx_scratch), "page": (0.5, sfx_page),
    "stamp": (0.45, sfx_stamp), "pop": (0.3, sfx_pop), "zap": (0.5, sfx_zap),
    "click": (0.12, sfx_click), "ding": (0.9, sfx_ding), "buzz": (0.5, sfx_buzz),
    "typing": (1.0, sfx_typing), "marker": (0.5, sfx_marker), "riser": (2.0, sfx_riser),
    "spray": (0.7, sfx_spray),
}
MAX_SFX_LIMIT_DB = 4.0   # at most this much peak limiting to reach the loudness target


def render_sfx(sid):
    seconds, fn = SFX[sid]
    n = int(round(seconds * SR))
    x = fn(n, tvec(n), np.random.default_rng(_stable_seed(("sfx", sid))))[:, :n]
    x = fade_edges(x, 0.0, min(0.01, seconds * 0.05))
    x = x / (np.abs(x).max() + 1e-12)
    g = 10.0 ** ((SFX_LUFS - max_momentary_lufs(x)) / 20.0)
    ceil = 10.0 ** (CEILING_DB / 20.0)
    tp = true_peak(x * g)
    if tp > ceil * 10.0 ** (MAX_SFX_LIMIT_DB / 20.0):
        g *= ceil * 10.0 ** (MAX_SFX_LIMIT_DB / 20.0) / tp
    return limit(x * g, CEILING_DB, hold=0.002)


# ================================================================= verification

def kick_onset(x, s0, search=int(0.015 * SR), margin=2048):
    """Onset of the low-passed signal near s0: the sample where its analytic envelope
    rises most steeply, searched within +-15 ms of s0. A steepest-rise detector is not
    biased by the level of the previous note's tail, unlike a fixed threshold."""
    a = max(0, s0 - search - margin)
    seg = x[a:s0 + search + margin]
    env = np.abs(hilbert(seg))
    lo = s0 - search - a
    rise = np.diff(env[lo:lo + 2 * search + 1])
    return s0 - search + int(np.argmax(rise))


def verify_music(path):
    x = decode(path)
    n = x.shape[-1]
    expect = TOTAL_BEATS * BEAT
    i_lufs, tp = ebur128(path)
    print(f"bgm.flac duration {n / SR:.4f} s (expected {expect:.4f}, diff {1000 * (n / SR - expect):+.3f} ms)")
    print(f"bgm.flac ffmpeg ebur128: integrated {i_lufs:.1f} LUFS, true peak {tp:.2f} dBFS, "
          f"sample peak {db(np.abs(x).max()):.2f} dBFS")
    for role in ("groove", "drop"):
        segs = [x[:, beat_sample(s["from"]):beat_sample(s["to"])] for s in SECTIONS
                if s["role"] == role and not s.get("lofi")]
        print(f"  {role:6s} sections integrated {integrated_lufs(np.concatenate(segs, axis=-1)):.1f} LUFS")
    lofi = [x[:, beat_sample(s["from"]):beat_sample(s["to"])] for s in SECTIONS if s.get("lofi")]
    if lofi:
        print(f"  lofi   section  integrated {integrated_lufs(np.concatenate(lofi, axis=-1)):.1f} LUFS")

    lp = sosfiltfilt(butter(4, 200.0, fs=SR, output="sos"), x.mean(axis=0))
    # Detector latency from an isolated kick at a known position, same filter/detector.
    probe = np.zeros(SR)
    probe[SR // 2:SR // 2 + len(kick())] += kick()[:SR // 2]
    lat = kick_onset(sosfiltfilt(butter(4, 200.0, fs=SR, output="sos"), probe), SR // 2) - SR // 2
    downbeats = [b for s in SECTIONS if s["role"] in ("groove", "drop")
                 for b in range(s["from"], s["to"]) if (b - s["from"]) % 4 == 0]
    pick = np.random.default_rng(7).choice(downbeats, size=8, replace=False)
    devs = []
    for b in sorted(pick):
        s0 = beat_sample(int(b))
        d = (kick_onset(lp, s0) - s0 - lat) / SR * 1000.0
        devs.append(d)
        print(f"  downbeat beat {int(b):3d} @ {s0 / SR:8.4f} s  onset deviation {d:+.3f} ms")
    print(f"kick onset: detector latency {lat / SR * 1000:.3f} ms (subtracted), "
          f"max |deviation| {max(abs(d) for d in devs):.3f} ms over 8 random downbeats")
    every = [abs(kick_onset(lp, beat_sample(b)) - beat_sample(b) - lat) / SR * 1000.0 for b in downbeats]
    print(f"kick onset over all {len(every)} groove/drop downbeats: max |deviation| {max(every):.3f} ms")
    for s in SECTIONS:
        if s["role"] == "stop":
            a, b = beat_sample(s["from"]) + int(0.04 * SR), beat_sample(s["to"])
            rms = np.sqrt(np.mean(x[:, a:b] ** 2)) if b > a else 0.0
            print(f"stop beats {s['from']}-{s['to']}: RMS after first 40 ms {db(rms):.1f} dBFS")


def probe_all(paths):
    print("\nffprobe:")
    for p in paths:
        codec, sr, ch, dur = ffprobe(p)
        i_lufs, tp = ebur128(p)
        print(f"  {str(p.relative_to(ROOT)):40s} {codec:5s} {sr} Hz {ch} ch {dur:8.3f} s  "
              f"I {i_lufs:6.1f} LUFS  TP {tp:6.2f} dBFS")


# ================================================================= entry points

MUSIC_OUT = ROOT / ARR["output"]
OUTRO_OUT = ROOT / ARR["outro"]
SFX_DIR = ROOT / "assets/audio/sfx"


def do_music():
    print("music: rendering")
    x = master_bus(build_music(), MUSIC_LUFS)
    encode(x, MUSIC_OUT, "flac24")
    verify_music(MUSIC_OUT)
    return [MUSIC_OUT]


def do_outro():
    print("outro: rendering")
    x = master_bus(build_outro(), OUTRO_LUFS, drive=0.6)
    encode(x, OUTRO_OUT, "mp3")
    return [OUTRO_OUT]


def do_sfx():
    out = []
    for sid in SFX:
        x = render_sfx(sid)
        p = SFX_DIR / f"{sid}.mp3"
        encode(x, p, "mp3")
        print(f"sfx {sid:8s} {x.shape[-1] / SR:.2f} s  max momentary {max_momentary_lufs(x):6.1f} LUFS  "
              f"true peak {db(true_peak(x)):6.2f} dBFS")
        out.append(p)
    return out


def main():
    what = next((a for a in sys.argv[1:] if not a.startswith("--")), "all")
    jobs = {"music": [do_music], "outro": [do_outro], "sfx": [do_sfx], "all": [do_music, do_outro, do_sfx]}
    if what not in jobs:
        raise SystemExit(__doc__)
    outputs = [p for job in jobs[what] for p in job()]
    if what == "all":
        probe_all(outputs)


if __name__ == "__main__":
    main()
