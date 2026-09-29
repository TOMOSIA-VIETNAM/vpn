// Copies the brand logo, the app screenshots and the promo video's web encodes from
// the repository's assets/ folder into public/, so the site always shows the same
// files the README and the video project publish. assets/ stays the only committed
// copy: public/logo.png, public/screenshots/ and public/videos/ are gitignored and
// rebuilt by `npm run dev` / `npm run build`.
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
rmSync(join(pub, "videos"), { recursive: true, force: true });
cpSync(join(assets, "videos", "promo", "web"), join(pub, "videos"), { recursive: true });
console.log(`Copied logo.png, screenshots/ and videos/ from ${assets} into ${pub}`);
