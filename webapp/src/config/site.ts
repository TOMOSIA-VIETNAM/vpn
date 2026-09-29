// Links and product facts shared by every locale. Change them here only.

import type { Locale } from "@/i18n/locales";

const repoSlug = "TOMOSIA-VIETNAM/vpn";

export const site = {
  name: "TOMOSIA VPN",
  company: "TOMOSIA VIET NAM",
  repoUrl: `https://github.com/${repoSlug}`,
  contributingUrl: `https://github.com/${repoSlug}/blob/main/CONTRIBUTING.md`,
  // Stable across releases: GitHub redirects /latest/download/<asset> to the newest release.
  downloadUrl: `https://github.com/${repoSlug}/releases/latest/download/TOMOSIA-VPN.dmg`,
  dmgFileName: "TOMOSIA-VPN.dmg",
  uninstallCommand: `curl -fsSL https://raw.githubusercontent.com/${repoSlug}/main/uninstall.sh | bash`,
  minMacOS: "12",
} as const;

/**
 * Absolute origin used for canonical, hreflang and Open Graph URLs.
 * Set SITE_URL for a custom domain; on Vercel the production domain is used
 * automatically; local builds fall back to localhost.
 */
export const siteUrl = new URL(
  process.env.SITE_URL ??
    (process.env.VERCEL_PROJECT_PRODUCTION_URL
      ? `https://${process.env.VERCEL_PROJECT_PRODUCTION_URL}`
      : "http://localhost:3000"),
);

/** Screenshots copied from ../assets/screenshots, each with -light and -dark variants. */
export const screenshots = {
  popover: { name: "popover", width: 824, height: 736 },
  settings: { name: "settings", width: 984, height: 1218 },
  newConfiguration: { name: "new-configuration", width: 984, height: 1102 },
} as const;

export type ScreenshotKey = keyof typeof screenshots;

/**
 * Promo video editions, copied from ../assets/videos/promo/web into public/videos.
 * Each edition is a set of files named <name>.mp4 (H.264), <name>.webm (VP9),
 * <name>-mobile.mp4 (720p) and <name>-poster.jpg. A locale without its own
 * edition shows the fallback locale's; add a line here when one is published.
 */
const promoEditions = { vi: "promo-vi" } satisfies Partial<Record<Locale, string>>;
const promoFallback: keyof typeof promoEditions = "vi";

export const promoVideoSize = { width: 1920, height: 1080 } as const;

export function promoVideo(locale: Locale) {
  const name = promoEditions[locale as keyof typeof promoEditions] ?? promoEditions[promoFallback];
  return {
    mp4: `/videos/${name}.mp4`,
    webm: `/videos/${name}.webm`,
    mobileMp4: `/videos/${name}-mobile.mp4`,
    poster: `/videos/${name}-poster.jpg`,
  };
}

/** Screens at most this wide get the 720p encode. */
export const mobileVideoQuery = "(max-width: 640px)";
