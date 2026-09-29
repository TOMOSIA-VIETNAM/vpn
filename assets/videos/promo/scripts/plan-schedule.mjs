// Plans one voice's schedule from its measured speech: scene cuts on the beat grid, drops, the silent stop
// before a drop, the calm outro and an optional post-credits scene. Writes the voice's anchors and duration
// into data/voices.json and its music arrangement (sections, fade, outro bed) into the voice's arrangement file.
// Run it after scripts/align-voiceover.py, then synthesize the music and build the timeline.
//
// data/script.json scene flags read here:
//   drop: true        the scene opens on a music drop (the beat before it builds up)
//   lofi: true        the scene's groove is the dull "generic universe" sound
//   outro: true       calm ending on the outro bed; the main track fades out under it
//   stopLine: true    (on a line) the line lands in a short silence right before the next drop
// A scene after the outro that has drop: true is a post-credits scene: the music resumes for it.
//
// Usage: node scripts/plan-schedule.mjs --voice=<id> [--write]   (prints the plan; --write saves it)
import { readFileSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const readJson = (p) => JSON.parse(readFileSync(join(root, p), "utf8"));
const args = process.argv.slice(2);
const VOICE = (args.find((a) => a.startsWith("--voice=")) || "--voice=en").slice(8);
const write = args.includes("--write");
const voices = readJson("data/voices.json");
const V = voices[VOICE];
if (!V) throw new Error(`unknown voice ${VOICE}; see data/voices.json`);
const script = readJson("data/script.json");
const vo = readJson(V.lines);
const arrangement = readJson(V.arrangement);

// ---------- timing rules (kept in step with scripts/build-timeline.mjs) ----------
const BEAT = 60 / arrangement.bpm;
const INTRO_BEATS = 2; // the opening hook plays alone this long before the narrator starts
const VO_LEAD = 0.55; // voice starts this long after a cut (build-timeline VO_LEAD)
const CUT_AFTER = 0.9; // the next cut waits this long after the last word
const DROP_LEAD = 0.35; // the first word after a drop, seconds after the drop
const BUILD_BEATS = 6; // snare roll before a drop
const STOP_BEATS = 2; // silence right before a drop that follows a stopLine
const OUTRO_GAP = 0.8; // seconds between outro lines
const POST_CREDITS_GAP = 3; // seconds of outro card before a post-credits scene
const TAIL_BEATS = 12; // the music thins out and ends this long after the last word
// the end: the end card finishes revealing END_REVEAL after the last word (index.html, s07), stays still for
// END_HOLD, then the picture fades to black over END_FADE (index.html, the final "#fade" tween)
const END_REVEAL = 1.0;
const END_HOLD = 4;
const END_FADE = 1.0;
const END_PAD = END_REVEAL + END_HOLD + END_FADE; // seconds from the last word to the end of the video

const up = (beats, step = 2) => Math.ceil(beats / step - 1e-9) * step;
const toBeat = (t) => t / BEAT;
const lineLen = (l, tempo) => (l.end - l.start) / tempo;

const anchors = {};
const sections = [];
let beat = 0; // next free cut, in beats
let lastWord = 0; // seconds
let groove = null; // [from, lofi] of the running groove
let outroSeen = false;
let fade = null;
const closeGroove = (to) => {
  if (groove && to > groove.from) sections.push({ from: groove.from, to, role: "groove", ...(groove.lofi ? { lofi: true } : {}) });
  groove = null;
};

script.scenes.forEach((s, i) => {
  const lines = vo[s.id].lines;
  const tempo = s.outro ? 1 : V.tempo;
  const a = { lines: lines.map(() => null) };
  if (i === 0) {
    a.scene = 0;
    a.lines[0] = INTRO_BEATS;
    sections.push({ from: 0, to: INTRO_BEATS, role: "intro" });
    lastWord = INTRO_BEATS * BEAT + (lines[lines.length - 1].end - lines[0].start) / tempo;
    beat = up(toBeat(lastWord + CUT_AFTER));
    sections.push({ from: INTRO_BEATS, to: beat, role: "intro" });
    anchors[s.id] = a;
    return;
  }
  if (s.outro) {
    closeGroove(beat);
    fade = { beat, seconds: 5 };
    sections.push({ from: beat, to: beat + TAIL_BEATS, role: "tail" });
    a.scene = beat;
    let t = beat * BEAT + BEAT; // first outro line one beat after the cut
    lines.forEach((l, k) => {
      const at = up(toBeat(t), 1);
      a.lines[k] = at;
      t = at * BEAT + lineLen(l, 1) + OUTRO_GAP;
    });
    lastWord = t - OUTRO_GAP;
    beat = up(toBeat(lastWord + POST_CREDITS_GAP), 4);
    outroSeen = true;
    anchors[s.id] = a;
    return;
  }
  if (s.drop) {
    // the drop needs a build (and, after a stopLine, a silence) before it
    const prev = script.scenes[i - 1];
    const stop = !outroSeen && prev.lines[prev.lines.length - 1].stopLine ? STOP_BEATS : 0;
    // after a stopLine the drop beat is already fixed (the line was placed to end in its silence)
    let drop = stop ? beat : up(Math.max(beat, toBeat(lastWord + CUT_AFTER)));
    if (outroSeen) {
      // post-credits: silence under the outro card, then the music comes back
      sections.push({ from: fade.beat + TAIL_BEATS, to: beat, role: "stop" });
      fade.resume = beat;
      a.scene = beat;
      a.lines[0] = beat + 1;
      drop = up(Math.max(beat + 8, toBeat((beat + 1) * BEAT + lineLen(lines[0], tempo) + 0.8)), 4);
      sections.push({ from: beat, to: drop, role: "build" });
      a.lines[1] = [drop, +(DROP_LEAD - 0.25).toFixed(2)];
    } else {
      closeGroove(drop - BUILD_BEATS - stop);
      sections.push({ from: drop - BUILD_BEATS - stop, to: drop - stop, role: "build" });
      if (stop) sections.push({ from: drop - stop, to: drop, role: "stop" });
      a.scene = drop;
      a.lines[0] = [drop, DROP_LEAD];
    }
    sections.push({ from: drop, role: "drop" }); // "to" is set when the next section starts
    groove = null;
    const firstAnchored = outroSeen ? 1 : 0;
    const startT = (outroSeen ? drop * BEAT + DROP_LEAD - 0.25 : drop * BEAT + DROP_LEAD) - lines[firstAnchored].start / tempo;
    lastWord = startT + lines[lines.length - 1].end / tempo;
    anchors[s.id] = a;
  } else {
    a.scene = beat;
    lastWord = beat * BEAT + VO_LEAD + (lines[lines.length - 1].end - lines[0].start) / tempo;
    if (!groove && sections.at(-1)?.role !== "drop") groove = { from: beat, lofi: !!s.lofi };
    else if (s.lofi && !groove) groove = { from: beat, lofi: true };
    anchors[s.id] = a;
  }
  // a stopLine ends right before the next drop: anchor it so its last word lands in the silence
  const last = s.lines.length - 1;
  const next = script.scenes[i + 1];
  if (s.lines[last].stopLine && next?.drop) {
    const dur = lineLen(lines[last], tempo);
    const before = lastWord - dur; // when the natural schedule would start the line
    // the line keeps its natural start (delayed at most to the next 2-beat step); the build and the
    // silence sit under its end
    const drop = up(toBeat(before + dur + 0.1));
    a.lines[last] = Math.floor((drop - toBeat(dur + 0.15)) * 10) / 10; // round down: the last word never crosses the drop
    lastWord = drop * BEAT - 0.1;
    beat = drop;
  } else {
    beat = up(toBeat(lastWord + CUT_AFTER));
  }
  // a drop section runs until the next groove scene; the groove after it starts on the next cut
  const d = sections.at(-1);
  if (d.role === "drop" && d.to === undefined && next && !next.drop) {
    d.to = beat;
    groove = { from: beat, lofi: !!next?.lofi };
  }
  if (next?.lofi && groove && !groove.lofi) {
    closeGroove(beat);
    groove = { from: beat, lofi: true };
  } else if (groove?.lofi && next && !next.lofi && !next.drop && !next.outro) {
    closeGroove(beat);
    groove = { from: beat, lofi: false };
  }
});

// the music ends on a tail after the last word
const end = up(toBeat(lastWord), 2);
const open = sections.findLast((x) => x.to === undefined);
if (open) open.to = end;
closeGroove(end);
if (sections.at(-1).role !== "tail") sections.push({ from: end, to: end + TAIL_BEATS, role: "tail" });
sections.sort((x, y) => x.from - y.from);
for (let k = 1; k < sections.length; k++) if (sections[k - 1].to !== sections[k].from) console.warn(`gap or overlap between sections at beat ${sections[k - 1].to}/${sections[k].from}`);
const duration = +Math.max(lastWord + END_PAD, sections.at(-1).to * BEAT).toFixed(1);
const outroScene = script.scenes.find((s) => s.outro);
const plan = {
  anchors: Object.fromEntries(Object.entries(anchors).map(([id, a]) => [id, { scene: a.scene, ...(a.lines.some((x) => x != null) ? { lines: a.lines } : {}) }])),
  duration,
  fadeOut: fade,
  outroBed: outroScene ? +(anchors[outroScene.id].scene * BEAT - 1.8).toFixed(1) : null,
  sections,
};

for (const [id, a] of Object.entries(plan.anchors)) console.log(`${id.padEnd(18)} cut b${String(a.scene).padEnd(4)} ${(a.scene * BEAT).toFixed(2).padStart(7)} s  lines ${JSON.stringify(a.lines || [])}`);
console.log("sections", sections.map((x) => `${x.from}-${x.to} ${x.role}${x.lofi ? " lofi" : ""}`).join(" | "));
console.log(`duration ${duration} s, fadeOut ${JSON.stringify(fade)}, outroBed ${plan.outroBed}`);

if (write) {
  V.anchors = plan.anchors;
  V.duration = duration;
  writeFileSync(join(root, "data/voices.json"), JSON.stringify(voices, null, 2) + "\n");
  Object.assign(arrangement, { sections, ...(fade ? { fadeOut: fade } : {}), ...(plan.outroBed != null ? { outroBed: plan.outroBed } : {}) });
  writeFileSync(join(root, V.arrangement), JSON.stringify(arrangement, null, 2) + "\n");
  console.log(`wrote data/voices.json[${VOICE}] and ${V.arrangement}`);
}
