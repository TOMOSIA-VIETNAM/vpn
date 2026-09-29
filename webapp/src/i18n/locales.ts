// Supported locales. Adding one: add it here, then add a dictionary in
// dictionaries/ and register it in index.ts (the Dictionary type forces every key).

export const locales = ["en", "vi", "ja"] as const;
export type Locale = (typeof locales)[number];

/** `/` always sends visitors here; there is no browser-language detection. */
export const defaultLocale: Locale = "en";

const systemText = `-apple-system, BlinkMacSystemFont, "SF Pro Text", system-ui, "Segoe UI", "Helvetica Neue", Arial, sans-serif`;
const systemDisplay = `-apple-system, BlinkMacSystemFont, "SF Pro Display", system-ui, "Segoe UI", "Helvetica Neue", Arial, sans-serif`;
const japanese = `-apple-system, BlinkMacSystemFont, "Hiragino Sans", "Hiragino Kaku Gothic ProN", "Noto Sans JP", "Yu Gothic", Meiryo, system-ui, sans-serif`;
// Defined by the font class in src/fonts (next/font/local).
const beVietnamPro = `var(--font-be-vietnam-pro), ${systemText}`;

/**
 * Type settings of a locale, applied as CSS custom properties on <html>. Tight
 * negative tracking suits Latin display type in SF Pro, but it makes stacked
 * Vietnamese marks collide, and Japanese is set without it.
 */
export interface Typography {
  /** Body text and small UI. */
  text: string;
  /** Headlines. */
  display: string;
  /** Display headlines (hero, section titles, closing line): weight, tracking, line height. */
  displayWeight: number;
  displayTracking: string;
  displayLeading: number;
  /** Smaller headings (story steps, feature tiles, install steps). */
  headingTracking: string;
  headingLeading: number;
  bodyLeading: number;
}

export interface LocaleInfo {
  label: string;
  htmlLang: string;
  ogLocale: string;
  /**
   * Whether headlines animate word by word. Off for scripts without spaces between
   * words (Japanese): splitting them would stop the browser breaking lines between
   * phrases, so the whole headline animates as one piece instead.
   */
  splitWords: boolean;
  typography: Typography;
}

export const localeInfo: Record<Locale, LocaleInfo> = {
  en: {
    label: "English",
    htmlLang: "en",
    ogLocale: "en_US",
    splitWords: true,
    typography: {
      text: systemText,
      display: systemDisplay,
      displayWeight: 700,
      displayTracking: "-0.035em",
      displayLeading: 1.05,
      headingTracking: "-0.015em",
      headingLeading: 1.2,
      bodyLeading: 1.55,
    },
  },
  // Be Vietnam Pro is drawn for Vietnamese: its marks sit clear of the letters and
  // of the line above, given near-zero tracking and a taller line. It sets wider
  // and darker than SF Pro, so its headlines use semibold.
  vi: {
    label: "Tiếng Việt",
    htmlLang: "vi",
    ogLocale: "vi_VN",
    splitWords: true,
    typography: {
      text: beVietnamPro,
      display: beVietnamPro,
      displayWeight: 600,
      displayTracking: "-0.02em",
      displayLeading: 1.18,
      headingTracking: "-0.005em",
      headingLeading: 1.32,
      bodyLeading: 1.65,
    },
  },
  ja: {
    label: "日本語",
    htmlLang: "ja",
    ogLocale: "ja_JP",
    splitWords: false,
    typography: {
      text: japanese,
      display: japanese,
      displayWeight: 700,
      displayTracking: "0",
      displayLeading: 1.28,
      headingTracking: "0",
      headingLeading: 1.35,
      bodyLeading: 1.75,
    },
  },
};

/** The locale's typography as the CSS custom properties globals.css reads. */
export function typographyStyle(locale: Locale): Record<string, string> {
  const t = localeInfo[locale].typography;
  return {
    "--font": t.text,
    "--font-display": t.display,
    "--display-weight": String(t.displayWeight),
    "--display-tracking": t.displayTracking,
    "--display-leading": String(t.displayLeading),
    "--heading-tracking": t.headingTracking,
    "--heading-leading": String(t.headingLeading),
    "--body-leading": String(t.bodyLeading),
  };
}

export function isLocale(value: string): value is Locale {
  return (locales as readonly string[]).includes(value);
}
