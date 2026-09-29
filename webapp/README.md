# TOMOSIA VPN landing page

Next.js (App Router, TypeScript) exported as a static site: `npm run build` writes
plain HTML, CSS and JS to `out/`, with no server code. Deployed on Vercel with the
project's Root Directory set to `webapp`.

## Run

```bash
npm install
npm run dev      # http://localhost:3000/en
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
| `src/i18n/locales.ts` | Locale list, default locale, `<html lang>` and Open Graph locale |
| `src/config/site.ts` | Download URL, repository links, uninstall command, minimum macOS, screenshot sizes, promo video edition per locale |
| `src/config/app-ui.ts` | Text quoted from the app's English UI (alerts, notifications, popover states) and the demo data |
| `src/components/sections/` | Page sections; `src/app/[locale]/page.tsx` lists them in order |
| `src/components/motion/` | `RevealObserver`, `Parallax`, `SplitText` |
| `src/components/HeroVideo.tsx` | Promo video behind the hero, with pause and sound controls |
| `src/components/PopoverDemo.tsx` | Working replica of the menu bar popover (first feature tile) |
| `src/app/globals.css` | Design tokens (macOS system colors, light and dark) and all styles |

Dictionary strings accept `**bold**`, `[[App UI label]]` (kept English and on one
line) and `{dmg}` (the download link).

`/` has no content: it sends visitors to `/en` with a meta refresh. The language
switcher keeps the section being read (`/vi#features`).

## Motion

No animation library: CSS transitions, one IntersectionObserver and one
requestAnimationFrame loop. With `prefers-reduced-motion` or without JavaScript,
everything renders in its final state and the hero shows the video poster.

- A block marked `data-reveal` that starts below the fold waits hidden
  (`is-pending`) and animates its `data-item` children in, ordered by `--i`
  (`stagger(n)`), when it scrolls into view. Content on screen at load is never
  hidden, so the first paint is complete.
- Headlines use `SplitText`, word by word where the locale has `splitWords`.
- `data-parallax="<depth>"` shifts an element with the scroll, capped at
  `data-parallax-range` px (default 16); keep the range below the free space around it.
- The hero video (`promoVideo()` in `src/config/site.ts`) plays muted and looping,
  pauses off screen, and gets the 720p file on narrow screens. Locales without their
  own edition use the Vietnamese one.
- The popover demo plays a connect, drop and reconnect once when it comes into view;
  its switches work.

## Add a locale

1. Add its code to `locales` and an entry to `localeInfo` in `src/i18n/locales.ts`.
2. Copy `src/i18n/dictionaries/en.ts` to the new code and translate it. Quoted app
   UI labels stay in English.
3. Register it in `dictionaries` in `src/i18n/index.ts`.

The type check fails until every key is present. Routes, `generateStaticParams`,
hreflang alternates and the switcher pick up the new locale from the list.

## Add a section

Create a component in `src/components/sections/`, add its copy to the
`Dictionary` type and every dictionary, and place it in the list in
`src/app/[locale]/page.tsx`. Give its `<section>` an `id` so the language switcher
can return to it.
