# TOMOSIA VPN promo video

A 40 s, 1920×1080 promo video. The Vietnamese edition is the first one: Vietnamese voice, captions and
on-screen text. `index.html` is a HyperFrames composition (HTML + GSAP). Audio, voice-over and renders are
generated files and are not committed (see `.gitignore`).

| File | Holds |
|---|---|
| `data/script.json` | the spoken lines per scene; `{key}` tags mark the words that visuals and sound effects follow |
| `data/voices.json` | narrator (Gemini TTS voice and model), tempo, music level, and the scene anchors on the beat grid |
| `data/strings.json` | on-screen text per language, keyed by the English text in `index.html` |
| `data/cues.json` | sound effects, each tied to a spoken word or a beat |
| `SOURCES.md` | where every claim in the video comes from |

Rebuild the Vietnamese edition (needs Node, ffmpeg, Python 3 and a Gemini API key in `~/.config/gemini/api_key`):

```bash
python3 -m venv .venv && .venv/bin/pip install faster-whisper numpy scipy fonttools
node scripts/generate-audio-assets.mjs --voice=vi --pair          # voice-over, one WAV per scene
.venv/bin/python scripts/align-voiceover.py --voice=vi             # word timings
node scripts/plan-schedule.mjs --voice=vi --write                  # scene cuts and music arrangement
.venv/bin/python scripts/synth-audio.py all --arrangement=data/music-arrangement.vi.json
node scripts/build-timeline.mjs --voice=vi --caption=vi            # mix + timing injected into index.html
npx --yes hyperframes@0.7.99 render -q high -f 30 --strict -o renders/tomosia-vpn-promo-vi.mp4
scripts/finish-render.sh renders/tomosia-vpn-promo-vi.mp4 assets/audio/mix-vi.m4a
scripts/encode-web.sh renders/tomosia-vpn-promo-vi.mp4 vi                  # hero video files for webapp/
```

`web/` is committed: it holds the encodes the landing page plays in its hero (`webapp/scripts/copy-assets.mjs`
copies them into the site). Re-run `scripts/encode-web.sh` after every new render; its header lists the files.

A scene may have no spoken lines: `"lines": []` with `"hold": <seconds>` keeps it on screen over the music
(the error windows scene does this; its windows follow the clicks in `data/cues.json`).

`plan-schedule.mjs` writes the scene anchors into `data/voices.json`; the anchors in it come straight from the
planner. `assets/bg/` holds the two background photos (see `SOURCES.md`).
