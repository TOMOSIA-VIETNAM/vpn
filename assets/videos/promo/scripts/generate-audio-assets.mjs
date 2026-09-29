// Generates the voice-over: one WAV per scene through the Gemini TTS REST API.
// API keys: GEMINI_API_KEY, else every ~/.config/gemini/api_key* file (never printed). When a key hits its
// daily quota the next key takes over.
// Sound effects and music are synthesized locally by scripts/synth-audio.py.
// Usage: node scripts/generate-audio-assets.mjs [--voice=en|ja|vi] [--force] [--only=id1,id2] [--model=<tts model>] [--pair] [--tts-voice=<name> --out=<dir>]
// --voice picks the spoken field and the output folder from data/voices.json (anchor tags are not spoken).
// --tts-voice=<name> and --out=<dir> try another narrator into a scratch folder without touching the voice.
// --pair speaks two consecutive scenes of the same style in one request and cuts the result at the silence
// nearest to the expected boundary: half the requests, for daily request quotas. Check the clips afterwards
// (scripts/transcribe.py); the aligner warns when a clip does not match its scene.
// --model overrides data/script.json voice.model, e.g. when the free-tier daily quota of one model is used up.
import { spawnSync } from "node:child_process";
import { existsSync, mkdirSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import os from "node:os";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const script = JSON.parse(readFileSync(join(root, "data/script.json"), "utf8"));
const args = process.argv.slice(2);
const force = args.includes("--force");
const pair = args.includes("--pair");
const only = (args.find((a) => a.startsWith("--only=")) || "").slice(7).split(",").filter(Boolean);
const VOICE = (args.find((a) => a.startsWith("--voice=")) || "--voice=en").slice(8);
const V = JSON.parse(readFileSync(join(root, "data/voices.json"), "utf8"))[VOICE];
if (!V) throw new Error(`unknown voice ${VOICE}; see data/voices.json`);
const voiceOverride = (args.find((a) => a.startsWith("--tts-voice=")) || "").slice(12);
if (voiceOverride) V.ttsVoice = voiceOverride;
const outDir = (args.find((a) => a.startsWith("--out=")) || "").slice(6);
if (outDir) V.vo = outDir;
const model = (args.find((a) => a.startsWith("--model=")) || "").slice(8) || V.model || script.voice.model;
const keyDir = join(os.homedir(), ".config/gemini");
const KEYS = process.env.GEMINI_API_KEY
  ? [process.env.GEMINI_API_KEY]
  : existsSync(keyDir)
    ? readdirSync(keyDir).filter((f) => f.startsWith("api_key")).sort().map((f) => readFileSync(join(keyDir, f), "utf8").trim()).filter(Boolean)
    : [];
if (!KEYS.length) throw new Error(`no Gemini key: set GEMINI_API_KEY or write it to ${keyDir}/api_key`);
let keyIndex = 0;

// Gemini TTS returns raw 16-bit little-endian mono PCM at 24 kHz; wrap it in a WAV header.
function wav(pcm, rate = 24000) {
  const h = Buffer.alloc(44);
  h.write("RIFF", 0);
  h.writeUInt32LE(36 + pcm.length, 4);
  h.write("WAVEfmt ", 8);
  h.writeUInt32LE(16, 16);
  h.writeUInt16LE(1, 20);
  h.writeUInt16LE(1, 22);
  h.writeUInt32LE(rate, 24);
  h.writeUInt32LE(rate * 2, 28);
  h.writeUInt16LE(2, 32);
  h.writeUInt16LE(16, 34);
  h.write("data", 36);
  h.writeUInt32LE(pcm.length, 40);
  return Buffer.concat([h, pcm]);
}

async function speak(text, style) {
  const url = `https://generativelanguage.googleapis.com/v1beta/models/${model}:generateContent`;
  const body = {
    // TTS models reject system instructions and read a plain style prefix aloud;
    // director's notes followed by a TRANSCRIPT heading make them speak the transcript only.
    contents: [{ parts: [{ text: `### DIRECTOR'S NOTES\n${style}\n\n#### TRANSCRIPT\n${text}` }] }],
    generationConfig: {
      responseModalities: ["AUDIO"],
      speechConfig: { voiceConfig: { prebuiltVoiceConfig: { voiceName: V.ttsVoice || script.voice.voice } } },
    },
  };
  for (let attempt = 1; ; attempt++) {
    const res = await fetch(url, { method: "POST", headers: { "content-type": "application/json", "x-goog-api-key": KEYS[keyIndex] }, body: JSON.stringify(body) });
    const json = await res.json();
    const data = json.candidates?.[0]?.content?.parts?.find((p) => p.inlineData)?.inlineData?.data;
    if (res.ok && data) return Buffer.from(data, "base64");
    const msg = json.error?.message || JSON.stringify(json).slice(0, 300);
    // a daily quota does not come back within retries: move to the next key (identified by position only)
    if (res.status === 429 && /free_tier_requests|quota/i.test(msg) && /per ?day|PerDay|limit: \d+/.test(msg) && keyIndex < KEYS.length - 1) {
      keyIndex++;
      console.warn(`key ${keyIndex} of ${KEYS.length} hit its quota for ${model}; switching to key ${keyIndex + 1}`);
      attempt = 0;
      continue;
    }
    if (attempt >= 3 || (res.status !== 429 && res.status < 500)) throw new Error(`TTS ${res.status}: ${msg}`);
    console.warn(`TTS ${res.status}, retry ${attempt}: ${msg.slice(0, 160)}`);
    await new Promise((r) => setTimeout(r, 20000 * attempt));
  }
}

const dir = resolve(root, V.vo);
mkdirSync(dir, { recursive: true });
const spokenText = (s) => s.lines.map((l) => (l[V.say] || l[V.text]).replace(/\{[^}]+\}/g, "")).join(" ");
// a voice in data/voices.json may override the narrator (ttsVoice) and the delivery (style, outroStyle)
const styleOf = (s) => (s.outro ? V.outroStyle || script.outroStyle : V.style || script.voice.style);
const RATE = 24000; // Gemini TTS PCM: 16-bit mono
const secs = (pcm) => pcm.length / (2 * RATE);

// silences (start, end in seconds) of at least 0.35 s in a PCM clip
function silences(pcm) {
  const tmp = join(os.tmpdir(), `tts-split-${process.pid}.wav`);
  writeFileSync(tmp, wav(pcm));
  const log = spawnSync("ffmpeg", ["-hide_banner", "-nostats", "-i", tmp, "-af", "silencedetect=n=-40dB:d=0.35", "-f", "null", "-"], { encoding: "utf8" }).stderr;
  const starts = [...log.matchAll(/silence_start: ([\d.]+)/g)].map((m) => +m[1]);
  const ends = [...log.matchAll(/silence_end: ([\d.]+)/g)].map((m) => +m[1]);
  return starts.map((a, i) => [a, ends[i] ?? secs(pcm)]);
}

// a silent "hold" scene has no lines and no clip
const todo = script.scenes.filter((x) => x.lines.length && (!only.length || only.includes(x.id)) && (force || !existsSync(join(dir, `${x.id}.wav`))));
for (const x of script.scenes.filter((x) => !todo.includes(x) && (!only.length || only.includes(x.id)))) console.log(`skip vo ${x.id}`);
// Sequential on purpose: the free tier allows only a few requests per minute.
for (let i = 0; i < todo.length; i++) {
  const a = todo[i], b = todo[i + 1];
  if (pair && b && styleOf(a) === styleOf(b)) {
    i++;
    const pcm = await speak(`${spokenText(a)}\n\n${spokenText(b)}`, `${styleOf(a)} Leave a two-second pause between the two paragraphs.`);
    // the boundary is the silence nearest to where the first scene's share of the text ends
    const expect = (secs(pcm) * spokenText(a).length) / (spokenText(a).length + spokenText(b).length);
    const gaps = silences(pcm).filter(([s0, s1]) => s0 > 0.5 && s1 < secs(pcm) - 0.5);
    if (!gaps.length) throw new Error(`${a.id}+${b.id}: no pause found to split the pair; run them without --pair`);
    const [g0, g1] = gaps.sort((p, q) => Math.abs((p[0] + p[1]) / 2 - expect) - Math.abs((q[0] + q[1]) / 2 - expect))[0];
    const cut = (t) => 2 * Math.round(t * RATE);
    const pa = pcm.subarray(0, cut(g0 + 0.15)), pb = pcm.subarray(cut(g1 - 0.15));
    writeFileSync(join(dir, `${a.id}.wav`), wav(pa));
    writeFileSync(join(dir, `${b.id}.wav`), wav(pb));
    console.log(`vo ${a.id} -> ${secs(pa).toFixed(2)} s, ${b.id} -> ${secs(pb).toFixed(2)} s (one request, cut at ${((g0 + g1) / 2).toFixed(2)} s, expected ${expect.toFixed(2)} s)`);
    continue;
  }
  const pcm = await speak(spokenText(a), styleOf(a));
  writeFileSync(join(dir, `${a.id}.wav`), wav(pcm));
  console.log(`vo ${a.id} -> ${secs(pcm).toFixed(2)} s`);
}
