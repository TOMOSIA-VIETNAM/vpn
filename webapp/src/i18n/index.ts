import type { Dictionary } from "./dictionary";
import type { Locale } from "./locales";
import en from "./dictionaries/en";
import ja from "./dictionaries/ja";
import vi from "./dictionaries/vi";

const dictionaries: Record<Locale, Dictionary> = { en, vi, ja };

export function getDictionary(locale: Locale): Dictionary {
  return dictionaries[locale];
}

/** Fills `{name}` placeholders, e.g. `format("macOS {version}+", { version: "12" })`. */
export function format(template: string, values: Record<string, string>): string {
  return template.replace(/\{(\w+)\}/g, (match, key: string) => values[key] ?? match);
}
