import type { Dictionary } from "./dictionary";
import type { Locale } from "./locales";
import en from "./dictionaries/en";
import ja from "./dictionaries/ja";
import vi from "./dictionaries/vi";

const dictionaries: Record<Locale, Dictionary> = { en, vi, ja };

export function getDictionary(locale: Locale): Dictionary {
  return dictionaries[locale];
}

// Client components import it from ./format, which does not pull in every dictionary.
export { format } from "./format";
