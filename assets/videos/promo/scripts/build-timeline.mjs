// Builds the master timeline for the composition:
// - places every voice-over line on the music's beat grid (scene cuts land on beats),
// - times the karaoke captions against the spoken words,
// - resolves SFX cues (word / beat / scene-relative / absolute),
// - renders the final audio mix (ducked music + VO + SFX, loudness-normalized) with ffmpeg,
// - injects the timing data into index.html and writes data/timeline.json.
// Usage: node scripts/build-timeline.mjs [--no-audio] [--dry] [--stems] [--voice=en|ja] [--caption=vi|ja|en]
// Everything that depends on the narrator (voice files, alignment, anchors, music arrangement, mix, duration)
// comes from data/voices.json[--voice]. A spoken token may carry anchor keys, "ボトルネック{bottleneck}", so the
// composition and the SFX cues look up the same English keys whatever language is spoken.
import { execFileSync, spawnSync } from "node:child_process";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { basename, dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import os from "node:os";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const readJson = (p) => JSON.parse(readFileSync(join(root, p), "utf8"));
const script = readJson("data/script.json");
const VOICE = (process.argv.find((a) => a.startsWith("--voice=")) || "--voice=en").slice(8);
const V = readJson("data/voices.json")[VOICE];
if (!V) throw new Error(`unknown voice ${VOICE}; see data/voices.json`);
const vo = readJson(V.lines);
const noAudio = process.argv.includes("--no-audio");
const dry = process.argv.includes("--dry"); // print the schedule only
const stems = process.argv.includes("--stems"); // also write the ducked-music and voice stems to the temp dir
// Caption language: the script.json line field shown as karaoke captions. Tokens are whitespace-separated in
// every language; a language without spaces (ja) writes its phrase breaks as spaces, and the page joins its
// tokens without them. Anchor tags are stripped. Chunks break at punctuation and stay under a word or character budget.
// maxWords: greedy chunks of at most that many words. maxChars: the line is cut into the fewest chunks of at
// most that many characters, balanced in length, preferring cuts after punctuation.
const CAPTION_LANGS = { vi: { maxWords: 9 }, en: { maxWords: 9 }, ja: { maxChars: 26 } };
const CAPTION = (process.argv.find((a) => a.startsWith("--caption=")) || "--caption=vi").slice(10);
if (!CAPTION_LANGS[CAPTION]) throw new Error(`unknown caption language ${CAPTION}; known: ${Object.keys(CAPTION_LANGS).join(", ")}`);
const cues = dry ? [] : readJson("data/cues.json");
const work = join(os.tmpdir(), `${basename(root)}-mix`);
mkdirSync(work, { recursive: true });

// ---------- grid, duration and musical anchors ----------
// Music grid of the voice's arrangement: quarter note n sits at BEAT0 + n * BEAT. The track is synthesized by
// scripts/synth-audio.py on this exact grid, so no splice step is needed.
const arrangement = readJson(V.arrangement);
const BEAT0 = arrangement.beat0;
const BEAT = 60 / arrangement.bpm;
const beat = (n) => BEAT0 + BEAT * n;
const nextBeat = (t) => beat(Math.ceil((t - BEAT0) / BEAT - 1e-6));
// Quarter-note ranges [from, to) where the full beat plays, and the drop beats, both from the arrangement's sections.
const KICK = arrangement.sections.filter((x) => x.role === "groove" || x.role === "drop").map((x) => [x.from, x.to]);
const DROPS = arrangement.sections.filter((x) => x.role === "drop").map((x) => x.from);
const OUTRO_BEAT = arrangement.fadeOut.beat; // the main track fades out from here (and resumes at fadeOut.resume for the post-credits)
const OUTRO_BED = arrangement.outroBed; // the calm outro bed starts here, under the tail of the main track
const DURATION = V.duration;
const END_FADE = 1.5; // audio fade-out at the very end, under the still end card (END_HOLD in scripts/plan-schedule.mjs)
const FPS = 30;
const TEMPO = V.tempo; // energetic lines play this much faster; outro lines keep their natural pace
const CUT_AFTER = 0.9; // the next scene cuts on the first beat at least this long after the last spoken word
const VO_LEAD = 0.55; // voice segment begins this long after a scene cut (lets the headline land first)
const SEG_PRE = 0.08; // audio kept before a line's first word
const SEG_POST = 0.15; // audio kept after a line's last word

// Anchors from data/voices.json, in beats: `scene` = cut, `lines[i]` = onset of line i's first word;
// a number is a beat, [beat, seconds] adds an offset. Scenes without an anchor cut on the next beat after the
// previous voice ends.
const atBeat = (x) => (x == null ? null : Array.isArray(x) ? beat(x[0]) + x[1] : beat(x));
const ANCHORS = Object.fromEntries(
  Object.entries(V.anchors).map(([id, a]) => [id, { scene: atBeat(a.scene), lines: a.lines?.map(atBeat) }]),
);

const norm = (t) => t.toLowerCase().replace(/[^\p{L}\p{N}'-]/gu, "");
const TAG = /\{([^}]+)\}/g;
const untag = (t) => t.replace(TAG, "");
// lookup keys of a spoken token: its anchor tags, or the token itself. A voice with `tagsOnly` (its text
// mixes in English words such as "review") gives untagged tokens a key no anchor can match.
const keysOf = (t) => {
  const tags = [...t.matchAll(TAG)].map((m) => norm(m[1]));
  return tags.length ? tags : [(V.tagsOnly ? "~" : "") + norm(t)];
};
const round = (x, d = 3) => Math.round(x * 10 ** d) / 10 ** d;


// ---------- 1. schedule voice lines ----------
const scenes = [];
let prevOff = 0;
let prevVoEnd = 0;
for (const s of script.scenes) {
  const a = ANCHORS[s.id] || {};
  const tempo = s.outro ? 1 : TEMPO;
  const src = vo[s.id].lines;
  // Segment bounds: pad each line, but never cross the midpoint of the gap to its neighbour.
  const segs = src.map((l, i) => {
    const prevMid = i ? (src[i - 1].end + l.start) / 2 : 0;
    const nextMid = i < src.length - 1 ? (l.end + src[i + 1].start) / 2 : Infinity;
    return { a: Math.max(0, l.start - SEG_PRE, prevMid), b: Math.min(l.end + SEG_POST, nextMid) };
  });
  const sceneAt = a.scene ?? nextBeat(prevOff + CUT_AFTER);
  if (sceneAt < prevVoEnd - 0.2) console.warn(`${s.id}: cut at ${sceneAt.toFixed(2)} clips the previous voice (${prevVoEnd.toFixed(2)})`);
  const lines = [];
  src.forEach((l, i) => {
    const seg = segs[i];
    const lead = (l.start - seg.a) / tempo;
    let place;
    if (a.lines?.[i] != null) place = a.lines[i] - lead;
    else if (i === 0) place = sceneAt + VO_LEAD;
    else place = lines[i - 1].place + (seg.a - segs[i - 1].a) / tempo; // keep the natural gap
    if (i > 0 && place < lines[i - 1].segEnd + 0.05) {
      console.warn(`${s.id} line ${i}: overlaps previous line, pushed later`);
      place = lines[i - 1].segEnd + 0.05;
    }
    const at = (x) => place + (x - seg.a) / tempo;
    lines.push({
      place,
      segEnd: place + (seg.b - seg.a) / tempo,
      seg,
      tempo,
      on: at(l.start),
      off: at(l.end),
      words: l.words.flatMap((w) => keysOf(w.t).map((k) => [k, round(at(w.s)), round(at(w.e))])),
      caption: s.lines[i][CAPTION] && untag(s.lines[i][CAPTION]),
    });
  });
  prevVoEnd = lines[lines.length - 1].segEnd;
  prevOff = lines[lines.length - 1].off;
  scenes.push({ id: s.id, start: sceneAt, lines });
}
scenes.forEach((s, i) => (s.end = i < scenes.length - 1 ? scenes[i + 1].start : DURATION));
if (dry) {
  for (const s of scenes) {
    const b = ((s.start - BEAT0) / BEAT).toFixed(1);
    console.log(`${s.id.padEnd(18)} ${s.start.toFixed(2).padStart(7)} (b${b}) → ${s.end.toFixed(2).padStart(7)}  ` + s.lines.map((l) => `${l.on.toFixed(2)}–${l.off.toFixed(2)}`).join(" | "));
  }
  process.exit(0);
}
for (const s of scenes) {
  for (const l of s.lines) if (l.segEnd > s.end + 0.01) console.warn(`${s.id}: voice runs past its scene end (${l.segEnd.toFixed(2)} > ${s.end.toFixed(2)})`);
}

// ---------- 2. captions ----------
// Each caption token borrows the onset of the proportionally matching spoken word; long lines are split at punctuation.
const { maxWords, maxChars } = CAPTION_LANGS[CAPTION];
const size = (ws) => ws.reduce((n, w) => n + w.t.length, 0);
const captions = [];
for (const s of scenes) {
  for (const l of s.lines) {
    if (!l.caption) throw new Error(`${s.id}: a line has no "${CAPTION}" caption`);
    const toks = l.caption.split(/\s+/).filter(Boolean);
    const en = l.words;
    const words = toks.map((t, i) => ({ t, s: en[Math.min(en.length - 1, Math.floor((i * en.length) / toks.length))][1] }));
    for (let i = 1; i < words.length; i++) if (words[i].s <= words[i - 1].s) words[i].s = round(words[i - 1].s + 0.07);
    let cur = [];
    const flush = () => cur.length && (captions.push({ scene: s.id, lineOff: l.off, words: cur }), (cur = []));
    if (maxChars) {
      // try every set of cuts: squared distance from equal lengths, plus a penalty for a cut not after punctuation
      const n = Math.ceil(size(words) / maxChars);
      const target = size(words) / n;
      const punct = (w) => /[,.:;?!、。：？！」]$/.test(w.t);
      let best = { cost: Infinity, cuts: [] };
      const search = (from, left, cuts, cost) => {
        if (left === 1) {
          const c = cost + (size(words.slice(from)) - target) ** 2;
          if (c < best.cost) best = { cost: c, cuts };
          return;
        }
        for (let k = from + 1; k <= words.length - left + 1; k++) {
          const seg = size(words.slice(from, k));
          search(k, left - 1, [...cuts, k], cost + (seg - target) ** 2 + (punct(words[k - 1]) ? 0 : 200));
        }
      };
      search(0, n, [], 0);
      [0, ...best.cuts].forEach((from, j, all) => {
        cur = words.slice(from, all[j + 1] ?? words.length);
        flush();
      });
      continue;
    }
    words.forEach((w, i) => {
      cur.push(w);
      const left = words.length - i - 1;
      const punct = /[,.:;?!、。：？！」]$/.test(w.t);
      if (left === 0) return;
      if ((punct && cur.length >= 3 && left >= 2 && words.length > maxWords) || cur.length >= maxWords) flush();
    });
    flush();
  }
}
captions.forEach((c, i) => {
  c.start = round(c.words[0].s - 0.12);
  const next = captions[i + 1];
  const natural = next && next.lineOff === c.lineOff ? next.words[0].s - 0.12 : c.lineOff + 0.5;
  c.end = round(next ? Math.min(natural, next.words[0].s - 0.16) : natural);
});

// ---------- 3. SFX cues ----------
const wordTime = (sceneId, word, nth = 1) => {
  const s = scenes.find((x) => x.id === sceneId);
  if (!s) throw new Error(`cue: unknown scene ${sceneId}`);
  const hits = s.lines.flatMap((l) => l.words).filter((w) => w[0] === word);
  if (hits.length < nth) throw new Error(`cue: "${word}" #${nth} not found in ${sceneId}`);
  return hits[nth - 1][1];
};
const sfxEvents = cues.map((c) => {
  let t;
  if (c.word) t = wordTime(c.scene, c.word, c.nth);
  else if (c.beat != null) t = beat(c.beat);
  else if (c.abs != null) t = c.abs;
  else t = scenes.find((x) => x.id === c.scene).start + c.at;
  return { sfx: c.sfx, vol: c.vol ?? 0.5, t: Math.max(0, round(t + (c.offset || 0))) };
});

// ---------- 4. audio mix ----------
const ff = (args) => execFileSync("ffmpeg", ["-hide_banner", "-nostats", "-y", ...args], { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"], maxBuffer: 1 << 26 });
// ffmpeg writes its analysis reports (ebur128, loudnorm) to stderr.
const stderrOf = (args) => spawnSync("ffmpeg", ["-hide_banner", "-nostats", ...args], { encoding: "utf8" }).stderr;
const loudness = (file) => {
  const log = stderrOf(["-i", file, "-af", "ebur128=peak=true", "-f", "null", "-"]);
  const I = Number(/I:\s+(-?[\d.]+) LUFS/.exec(log.slice(log.lastIndexOf("Summary")))[1]);
  const peak = Number(/Peak:\s+(-?[\d.]+) dBFS/.exec(log.slice(log.lastIndexOf("Summary")))[1]);
  return { I, peak };
};
const gainFor = (file, target) => {
  const { I, peak } = loudness(file);
  return Math.min(target - I, -1 - peak); // dB, never push a peak above -1 dBFS
};

let env = [];
if (!noAudio) {
  const inputs = [join(root, arrangement.output)];
  const graph = [];
  const FMT = "aresample=48000,aformat=sample_fmts=fltp:channel_layouts=stereo";
  // main track, then the calm outro bed faded in under its tail
  inputs.push(join(root, arrangement.outro));
  graph.push(`[0:a]${FMT},atrim=0:${DURATION},volume=${V.musicDb}dB[main]`);
  graph.push(`[1:a]${FMT},volume=-5dB,afade=t=in:d=2.5,adelay=delays=${Math.round(OUTRO_BED * 1000)}:all=1[bed]`);
  graph.push(`[main][bed]amix=inputs=2:normalize=0:dropout_transition=0[mus]`);

  // voice: one input per scene file, split into its line segments
  const voLabels = [];
  for (const s of scenes) {
    const file = join(root, V.vo, `${s.id}.wav`);
    const idx = inputs.push(file) - 1;
    const g = gainFor(file, -16);
    const outs = s.lines.map((_, i) => `[v${idx}_${i}]`);
    graph.push(`[${idx}:a]${FMT},volume=${g.toFixed(2)}dB,asplit=${outs.length}${outs.join("")}`);
    s.lines.forEach((l, i) => {
      const len = (l.seg.b - l.seg.a) / l.tempo;
      const tempo = l.tempo !== 1 ? `,atempo=${l.tempo}` : "";
      const lbl = `[vl${idx}_${i}]`;
      graph.push(
        `[v${idx}_${i}]atrim=start=${l.seg.a.toFixed(4)}:end=${l.seg.b.toFixed(4)},asetpts=PTS-STARTPTS${tempo},` +
          `afade=t=in:d=0.02,afade=t=out:st=${Math.max(0, len - 0.05).toFixed(4)}:d=0.05,adelay=delays=${Math.round(l.place * 1000)}:all=1${lbl}`,
      );
      voLabels.push(lbl);
    });
  }
  const voOuts = stems ? "[vobus][voside][voenv][vostem]" : "[vobus][voside][voenv]";
  // padded to the full length: the ducking below stops at the end of its sidechain, which would cut the
  // music off at the last word
  graph.push(`${voLabels.join("")}amix=inputs=${voLabels.length}:normalize=0:dropout_transition=0,apad=whole_dur=${DURATION},asplit=${stems ? 4 : 3}${voOuts}`);
  // moderate ducking: the music sits ~5 dB under speech and comes back to voice level in the gaps
  const duck = "sidechaincompress=threshold=0.03:ratio=3:attack=15:release=350:makeup=1";
  graph.push(stems ? `[mus][voside]${duck},asplit=2[musd][musstem]` : `[mus][voside]${duck}[musd]`);

  // sound effects: one input per distinct file
  const sfxLabels = [];
  const byFile = new Map();
  sfxEvents.forEach((e) => byFile.set(e.sfx, [...(byFile.get(e.sfx) || []), e]));
  for (const [id, evs] of byFile) {
    const file = join(root, `assets/audio/sfx/${id}.mp3`);
    const idx = inputs.push(file) - 1;
    const g = gainFor(file, -16);
    const outs = evs.map((_, i) => `[x${idx}_${i}]`);
    graph.push(`[${idx}:a]${FMT},volume=${g.toFixed(2)}dB,asplit=${outs.length}${outs.join("")}`);
    evs.forEach((e, i) => {
      const lbl = `[xl${idx}_${i}]`;
      graph.push(`[x${idx}_${i}]volume=${e.vol},adelay=delays=${Math.round(e.t * 1000)}:all=1${lbl}`);
      sfxLabels.push(lbl);
    });
  }
  graph.push(
    `[musd][vobus]${sfxLabels.join("")}amix=inputs=${2 + sfxLabels.length}:normalize=0:dropout_transition=0,` +
      `afade=t=out:st=${DURATION - END_FADE}:d=${END_FADE},atrim=0:${DURATION},apad=whole_dur=${DURATION}[out]`,
  );
  graph.push(`[voenv]pan=mono|c0=0.5*c0+0.5*c1,aresample=24000,apad=whole_dur=${DURATION},atrim=0:${DURATION}[env]`);

  const graphFile = join(work, "mix-graph.txt");
  writeFileSync(graphFile, graph.join(";\n"));
  writeFileSync(join(work, "mix-inputs.json"), JSON.stringify(inputs)); // lets the graph be re-run for stem checks
  const premix = join(work, "premix.wav");
  const envRaw = join(work, "vo-env.f32");
  // --stems writes the ducked music and the voice bus so scripts/measure-mix-balance.py can check the balance
  const stemOut = stems
    ? ["-map", "[musstem]", "-c:a", "pcm_s16le", join(work, "stem-music.wav"), "-map", "[vostem]", "-c:a", "pcm_s16le", join(work, "stem-voice.wav")]
    : [];
  ff([...inputs.flatMap((f) => ["-i", f]), "-/filter_complex", graphFile, "-map", "[out]", "-c:a", "pcm_s16le", premix, "-map", "[env]", "-f", "f32le", envRaw, ...stemOut]);
  if (stems) console.log(`stems -> ${join(work, "stem-music.wav")}, ${join(work, "stem-voice.wav")}`);

  // two-pass loudness normalization to -14 LUFS / -2 dBTP
  const LN = "loudnorm=I=-14:TP=-2:LRA=11"; // -2 dBTP leaves room for the AAC encoder overshoot, keeping the file peak under -1 dBFS
  const m = JSON.parse(/\{[\s\S]*?\}/.exec(stderrOf(["-i", premix, "-af", `${LN}:print_format=json`, "-f", "null", "-"]).split("[Parsed_loudnorm")[1])[0]);
  const pass2 = `${LN}:measured_I=${m.input_i}:measured_TP=${m.input_tp}:measured_LRA=${m.input_lra}:measured_thresh=${m.input_thresh}:offset=${m.target_offset}:linear=true`;
  const mixOut = join(root, V.mix);
  // loudnorm falls back to its dynamic mode on this peaky premix and the AAC encoder still overshoots;
// a sample-peak limiter at -3 dBFS keeps the encoded true peak under -1 dBFS
const limit = "alimiter=limit=0.71:level=false:attack=1:release=60";
ff(["-i", premix, "-af", `${pass2},aresample=48000,${limit}`, "-c:a", "aac", "-b:a", "192k", mixOut]);
  console.log(`mix -> ${mixOut} (premix ${m.input_i} LUFS -> -14)`);

  // voice envelope at the video frame rate (drives the mascot mouth and the waveform)
  const buf = readFileSync(envRaw);
  const samples = new Float32Array(buf.buffer, buf.byteOffset, Math.floor(buf.length / 4));
  const per = 24000 / FPS;
  const rms = [];
  for (let f = 0; f < DURATION * FPS; f++) {
    let sum = 0;
    for (let i = f * per; i < (f + 1) * per && i < samples.length; i++) sum += samples[i] * samples[i];
    rms.push(Math.sqrt(sum / per));
  }
  const sorted = rms.filter((x) => x > 0.005).sort((x, y) => x - y);
  const ref = sorted[Math.floor(sorted.length * 0.9)] || 1;
  env = rms.map((x) => round(Math.min(1, x / ref), 2));
  writeFileSync(join(root, V.envelope), JSON.stringify(env));
} else {
  env = readJson(V.envelope);
}

// ---------- 5. write timing + inject into the composition ----------
const timing = {
  duration: DURATION,
  fps: FPS,
  beat0: BEAT0,
  beatLen: round(BEAT, 6),
  kick: KICK,
  drops: DROPS,
  outroBeat: OUTRO_BEAT,
  scenes: Object.fromEntries(
    scenes.map((s) => [s.id, { start: round(s.start), end: round(s.end), lines: s.lines.map((l) => ({ on: round(l.on), off: round(l.off), words: l.words })) }]),
  ),
  voice: VOICE,
  captionLang: CAPTION,
  // on-screen text follows the caption language; English is the text written in index.html
  strings: CAPTION === "en" ? {} : readJson("data/strings.json")[CAPTION],
  captions: captions.map((c) => ({ start: c.start, end: c.end, words: c.words.map((w) => [w.t, w.s]) })),
  sfx: sfxEvents,
  env,
};
writeFileSync(join(root, "data/timeline.json"), JSON.stringify({ ...timing, env: undefined }, null, 1));

const htmlPath = join(root, "index.html");
let html = readFileSync(htmlPath, "utf8");
const block = `/*TIMING:BEGIN*/window.TIMING=${JSON.stringify(timing)};/*TIMING:END*/`;
if (!html.includes("/*TIMING:BEGIN*/")) throw new Error("index.html is missing the /*TIMING:BEGIN*/ marker");
html = html.replace(/\/\*TIMING:BEGIN\*\/[\s\S]*?\/\*TIMING:END\*\//, () => block);
// the studio may prepend data-hf-id attributes, so match id anywhere in the tag
html = html.replace(/(<div\b[^>]*?\bid="root"[^>]*?data-duration=")[\d.]+"/, `$1${DURATION}"`);
html = html.replace(/(<audio\b[^>]*?\bid="mix"[^>]*?data-duration=")[\d.]+"/, `$1${DURATION}"`);
html = html.replace(/(<audio\b[^>]*?\bid="mix"[^>]*?\bsrc=")[^"]*"/, `$1${V.mix}"`);
writeFileSync(htmlPath, html);

for (const s of scenes) {
  console.log(
    `${s.id.padEnd(18)} ${s.start.toFixed(2).padStart(7)} → ${s.end.toFixed(2).padStart(7)}  ` +
      s.lines.map((l) => `${l.on.toFixed(2)}–${l.off.toFixed(2)}`).join(" | "),
  );
}
console.log(`captions ${captions.length}, sfx ${sfxEvents.length}, env frames ${env.length}`);
