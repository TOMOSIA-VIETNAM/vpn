// Copies the brand logo and app screenshots from the repository's assets/ folder
// into public/, so the site always shows the same files the README uses.
// assets/ stays the only committed copy: public/logo.png and public/screenshots/
// are gitignored and rebuilt by `npm run dev` / `npm run build`.
import { cpSync, mkdirSync, rmSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const webapp = join(dirname(fileURLToPath(import.meta.url)), "..");
const assets = join(webapp, "..", "assets");
const pub = join(webapp, "public");

mkdirSync(pub, { recursive: true });
cpSync(join(assets, "logo.png"), join(pub, "logo.png"));
rmSync(join(pub, "screenshots"), { recursive: true, force: true });
cpSync(join(assets, "screenshots"), join(pub, "screenshots"), { recursive: true });
console.log(`Copied logo.png and screenshots/ from ${assets} into ${pub}`);
