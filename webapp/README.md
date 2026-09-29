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
`../assets/logo.png` and `../assets/screenshots/` into `public/`. Those copies are
gitignored: change the files in `assets/`, not in `public/`. On Vercel this needs
the default "Include files outside the Root Directory" setting.

## Where things live

| Path | What |
|---|---|
| `src/i18n/dictionaries/{en,vi,ja}.ts` | All page copy, one file per locale |
| `src/i18n/dictionary.ts` | The `Dictionary` type every locale must satisfy |
| `src/i18n/locales.ts` | Locale list, default locale, `<html lang>` and Open Graph locale |
| `src/config/site.ts` | Download URL, repository links, uninstall command, minimum macOS, screenshot sizes |
| `src/config/app-ui.ts` | Text quoted from the app's English UI (alerts, notifications, popover states) and the demo data |
| `src/components/sections/` | Page sections; `src/app/[locale]/page.tsx` lists them in order |
| `src/components/motion/` | Reveal animations: boot script, `RevealObserver`, `SplitText` |
| `src/components/PopoverDemo.tsx` | Working replica of the menu bar popover in the hero |
| `src/app/globals.css` | Design tokens (macOS system colors, light and dark) and all styles |

Dictionary strings accept `**bold**`, `[[App UI label]]` (kept English and on one
line) and `{dmg}` (the download link).

`/` has no content: it sends visitors to `/en` with a meta refresh. The language
switcher keeps the section being read (`/vi#features`).

## Motion

No animation library: CSS transitions plus one IntersectionObserver.

- A block marked `data-reveal` animates its `data-item` children into place, in the
  order of their `--i` (set with `stagger(n)`), the first time it scrolls into view.
- Headlines use `SplitText`, word by word where the locale has `splitWords`.
- The hero popover plays a connect, drop and reconnect once on load, with the app's
  notifications; its switches work.
- An inline script sets `html.motion` before first paint only when
  `prefers-reduced-motion` is off. Without it (reduced motion, no JavaScript) every
  element renders in its final state and the popover shows "Connected".

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
