// Supported locales. Adding one: add it here, then add a dictionary in
// dictionaries/ and register it in index.ts (the Dictionary type forces every key).

export const locales = ["en", "vi", "ja"] as const;
export type Locale = (typeof locales)[number];

/** `/` always sends visitors here; there is no browser-language detection. */
export const defaultLocale: Locale = "en";

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
}

export const localeInfo: Record<Locale, LocaleInfo> = {
  en: { label: "English", htmlLang: "en", ogLocale: "en_US", splitWords: true },
  vi: { label: "Tiếng Việt", htmlLang: "vi", ogLocale: "vi_VN", splitWords: true },
  ja: { label: "日本語", htmlLang: "ja", ogLocale: "ja_JP", splitWords: false },
};

export function isLocale(value: string): value is Locale {
  return (locales as readonly string[]).includes(value);
}
