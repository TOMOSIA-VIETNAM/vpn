# TOMOSIA VPN landing page

Next.js (App Router, TypeScript) exported as a static site: `npm run build` writes
plain HTML, CSS and JS to `out/`, with no server code. Deployed on Vercel with the
project's Root Directory set to `webapp`.

## Run

```bash
npm install
npm run dev      # http://localhost:3000/en (the Network URL works too, see next.config.ts)
npm run build    # static site in out/
npm run lint     # ESLint and the TypeScript check
npx serve out    # preview the exported site
```

`npm run dev` and `npm run build` first run `scripts/copy-assets.mjs`, which copies
`../assets/logo.png`, `../assets/screenshots/` and the promo video's web encodes in
`../assets/videos/promo/web/` into `public/`. Those copies are gitignored: change the
files in `assets/`, not in `public/`. On Vercel this needs
the default "Include files outside the Root Directory" setting.

## Where things live

| Path | What |
|---|---|
| `src/i18n/dictionaries/{en,vi,ja}.ts` | All page copy, one file per locale |
| `src/i18n/dictionary.ts` | The `Dictionary` type every locale must satisfy |
| `src/i18n/locales.ts` | Locale list, default locale, `<html lang>`, Open Graph locale and each locale's typography (font, tracking, line height) |
| `src/fonts/` | Be Vietnam Pro for the Vietnamese pages (subset WOFF2 written by `scripts/subset-be-vietnam-pro.sh`, OFL) |
| `src/config/site.ts` | Download URL, repository links, uninstall command, minimum macOS, screenshot sizes, promo video edition per locale |
| `src/config/app-ui.ts` | Text quoted from the app's English UI (alerts, notifications, popover states) and the demo data |
| `src/components/sections/` | Page sections; `src/app/[locale]/page.tsx` lists them in order |
| `src/components/motion/` | `RevealObserver`, `Parallax`, `SplitText` |
| `src/components/HeroVideo.tsx` | Promo video in a window frame under the hero, with pause and sound controls and the scroll growth |
| `src/components/Popover.tsx`, `Desk.tsx`, `AlertCard.tsx` | Replicas of the app's popover, its desktop (menu bar, notifications) and its alert card, drawn in a given state |
| `src/components/Compare.tsx`, `StoryScroller.tsx`, `Odometer.tsx` | Before/after toggle, sticky scroll story, rolling numbers |
| `src/components/Icon.tsx` | Line glyphs and the system-colored icon tiles |
| `src/components/LanguageMenu.tsx`, `BackToTop.tsx` | Globe language menu (header and footer), floating back-to-top button |
| `src/app/globals.css` | Design tokens (macOS system colors and the wallpaper field, light and dark) and all styles |

Dictionary strings accept `**bold**`, `[[App UI label]]` (kept English and on one
line) and `{dmg}` (the download link).

`/` renders the English page in place (canonical URL `/en`), so the first paint is
the styled page rather than a redirect. The language
menu keeps the section being read (`/vi#features`); it is a `<details>`, so without
JavaScript it still opens and lists plain links.

## Motion

No animation library: CSS transitions, IntersectionObservers and
requestAnimationFrame loops. With `prefers-reduced-motion` or without JavaScript,
everything renders in its final state and the hero shows the video poster.

- A block marked `data-reveal` that starts below the fold waits hidden
  (`is-pending`) and animates its `data-item` children in, ordered by `--i`
  (`stagger(n)`), when it scrolls into view. Content on screen at load is never
  hidden, so the first paint is complete.
- Headlines use `SplitText`, word by word where the locale has `splitWords`.
- `data-parallax="<depth>"` shifts an element with the scroll, capped at
  `data-parallax-range` px (default 16); keep the range below the free space around it.
- The hero video (`promoVideo()` in `src/config/site.ts`, one edition per locale)
  plays muted and looping, starts when a fifth of it is on screen, pauses when it
  leaves, and gets the 720p file on narrow screens; a refused start is logged to the
  console with its reason. Its frame scales up toward the full viewport width as the
  page scrolls (transform only); the stage under it reserves the room.
- While it plays muted, the corner sound button shows moving equalizer bars and,
  a moment after the start, opens once into a pill with its label and two soft
  rings; hovering the picture or focusing the button opens it again. That button,
  or a click on the picture, restarts the video with sound; from then on it is a
  plain sound toggle. A thin line along the bottom edge shows the progress.
- The facts panel under the hero rolls its numbers up like an odometer, draws a thin
  accent line under each and fades the labels in, one fact after another.
- The before/after toggle starts on the built-in VPN and switches to TOMOSIA VPN once,
  after it scrolls into view; a click stops that. Its rendered default is TOMOSIA VPN.
- The story's sticky column holds one desk per step and fades in the one for the
  step whose centre is closest to the viewport's centre; its dots and the step
  numbers scroll to their step. Below 1000px each step card carries its own desk.
- Each feature tile rises in when it scrolls into view and plays its piece of the
  app once (switch flips on, banners slide in, alert cards stack up, and so on).
- The back-to-top button appears once the hero is out of view.

## Add a locale

1. Add its code to `locales` and an entry to `localeInfo` in `src/i18n/locales.ts`
   (with its typography), and its promo video edition to `promoEditions` in
   `src/config/site.ts`.
2. Copy `src/i18n/dictionaries/en.ts` to the new code and translate it. Quoted app
   UI labels stay in English.
3. Register it in `dictionaries` in `src/i18n/index.ts`.

The type check fails until every key is present. Routes, `generateStaticParams`,
hreflang alternates and the language menu pick up the new locale from the list.

## Add a section

Create a component in `src/components/sections/`, add its copy to the
`Dictionary` type and every dictionary, and place it in the list in
`src/app/[locale]/page.tsx`. Give its `<section>` an `id` so the language menu
can return to it.
