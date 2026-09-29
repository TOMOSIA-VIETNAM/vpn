"use client";

import type { MouseEvent } from "react";
import { localeInfo, locales, type Locale } from "@/i18n/locales";

// Distance from the top of the viewport, below the sticky header, at which a
// section counts as the one being read.
const READING_LINE = 120;

/** The id of the section under the reading line, or null at the top of the page. */
function currentSectionId(): string | null {
  let current: string | null = null;
  for (const section of document.querySelectorAll<HTMLElement>("main section[id]")) {
    if (section.getBoundingClientRect().top <= READING_LINE) current = section.id;
  }
  return current;
}

/**
 * Links to the same page in every locale. With JavaScript, following a link keeps
 * the visitor on the section they are reading; without it, the link opens the page top.
 */
export function LanguageSwitcher({ locale, label }: { locale: Locale; label: string }) {
  function keepSection(event: MouseEvent<HTMLAnchorElement>, target: Locale) {
    const section = currentSectionId();
    // Point this click at the section; the browser then follows the link as usual.
    event.currentTarget.href = section ? `/${target}#${section}` : `/${target}`;
  }

  return (
    <nav className="lang" aria-label={label}>
      {locales.map((target) => (
        <a
          key={target}
          href={`/${target}`}
          hrefLang={localeInfo[target].htmlLang}
          lang={localeInfo[target].htmlLang}
          aria-current={target === locale ? "page" : undefined}
          aria-label={localeInfo[target].label}
          className="lang__option"
          onClick={(event) => keepSection(event, target)}
        >
          {target.toUpperCase()}
        </a>
      ))}
    </nav>
  );
}
