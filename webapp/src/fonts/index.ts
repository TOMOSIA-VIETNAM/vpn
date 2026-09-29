import localFont from "next/font/local";

/**
 * Be Vietnam Pro, self-hosted (subset WOFF2 from scripts/subset-be-vietnam-pro.sh,
 * OFL). Its class only defines --font-be-vietnam-pro; the typography of a locale
 * that uses it (src/i18n/locales.ts) refers to the variable, so the files load on
 * that locale's pages only. Not preloaded, for the same reason.
 */
export const beVietnamPro = localFont({
  src: [
    { path: "./be-vietnam-pro/BeVietnamPro-Regular.woff2", weight: "400", style: "normal" },
    { path: "./be-vietnam-pro/BeVietnamPro-Medium.woff2", weight: "500", style: "normal" },
    { path: "./be-vietnam-pro/BeVietnamPro-SemiBold.woff2", weight: "600", style: "normal" },
    { path: "./be-vietnam-pro/BeVietnamPro-Bold.woff2", weight: "700", style: "normal" },
  ],
  variable: "--font-be-vietnam-pro",
  display: "swap",
  preload: false,
});
