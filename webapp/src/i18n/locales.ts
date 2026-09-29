// Supported locales. Adding one: add it here, then add a dictionary in
// dictionaries/ and register it in index.ts (the Dictionary type forces every key).

export const locales = ["en", "vi", "ja"] as const;
export type Locale = (typeof locales)[number];

/** `/` always sends visitors here; there is no browser-language detection. */
export const defaultLocale: Locale = "en";

export const localeInfo: Record<Locale, { label: string; htmlLang: string; ogLocale: string }> = {
  en: { label: "English", htmlLang: "en", ogLocale: "en_US" },
  vi: { label: "Tiếng Việt", htmlLang: "vi", ogLocale: "vi_VN" },
  ja: { label: "日本語", htmlLang: "ja", ogLocale: "ja_JP" },
};

export function isLocale(value: string): value is Locale {
  return (locales as readonly string[]).includes(value);
}
