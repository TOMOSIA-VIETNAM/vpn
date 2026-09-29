"use client";

import { useEffect, useRef, type KeyboardEvent, type MouseEvent } from "react";
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

function menuItems(root: HTMLElement | null): HTMLAnchorElement[] {
  return [...(root?.querySelectorAll<HTMLAnchorElement>("[role=menuitemradio]") ?? [])];
}

interface LanguageMenuProps {
  locale: Locale;
  /** Accessible name of the button, e.g. "Language". */
  label: string;
  /** Which way the menu opens from its button. */
  direction: "down" | "up";
}

/**
 * A button (globe, the current language's name, chevron) opening a small menu of the page in every locale, each named in
 * its own language, the current one checked. Built on <details>, so without
 * JavaScript it still opens and its entries are plain links. With JavaScript:
 * arrow keys, Home/End and Escape, a click outside closes it, and following an
 * entry keeps the section being read (`/vi#features`).
 */
export function LanguageMenu({ locale, label, direction }: LanguageMenuProps) {
  const root = useRef<HTMLDetailsElement>(null);


  useEffect(() => {
    const el = root.current;
    if (!el) return;
    const summary = el.querySelector("summary");
    const syncExpanded = () => {
      summary?.setAttribute("aria-expanded", String(el.open));
      if (el.open) (el.querySelector<HTMLElement>("[aria-checked=true]") ?? menuItems(el)[0])?.focus();
    };
    const onOutside = (event: PointerEvent) => {
      if (el.open && !el.contains(event.target as Node)) el.open = false;
    };
    el.addEventListener("toggle", syncExpanded);
    document.addEventListener("pointerdown", onOutside);
    syncExpanded();
    return () => {
      el.removeEventListener("toggle", syncExpanded);
      document.removeEventListener("pointerdown", onOutside);
    };
  }, []);

  function onKeyDown(event: KeyboardEvent<HTMLDetailsElement>) {
    const el = root.current;
    if (!el?.open) return;
    const list = menuItems(el);
    const index = list.indexOf(document.activeElement as HTMLAnchorElement);
    const move = (to: number) => {
      event.preventDefault();
      list[(to + list.length) % list.length]?.focus();
    };
    switch (event.key) {
      case "ArrowDown":
        return move(index + 1);
      case "ArrowUp":
        return move(index <= 0 ? list.length - 1 : index - 1);
      case "Home":
        return move(0);
      case "End":
        return move(list.length - 1);
      case "Escape":
        event.preventDefault();
        el.open = false;
        el.querySelector("summary")?.focus();
        return;
      case "Tab":
        el.open = false;
        return;
    }
  }

  function keepSection(event: MouseEvent<HTMLAnchorElement>, target: Locale) {
    const section = currentSectionId();
    // Point this click at the section; the browser then follows the link as usual.
    event.currentTarget.href = section ? `/${target}#${section}` : `/${target}`;
  }

  return (
    <details ref={root} className={`lang-menu lang-menu--${direction}`} onKeyDown={onKeyDown}>
      <summary className="lang-menu__button" aria-haspopup="menu" aria-label={`${label}: ${localeInfo[locale].label}`}>
        <svg viewBox="0 0 20 20" width="18" height="18" aria-hidden="true" focusable="false">
          <circle cx="10" cy="10" r="7.5" fill="none" stroke="currentColor" strokeWidth="1.5" />
          <path
            d="M2.5 10h15M10 2.5c-2 2-3 4.6-3 7.5s1 5.5 3 7.5M10 2.5c2 2 3 4.6 3 7.5s-1 5.5-3 7.5"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.5"
          />
        </svg>
        <span lang={localeInfo[locale].htmlLang}>{localeInfo[locale].label}</span>
        <svg viewBox="0 0 12 12" width="10" height="10" aria-hidden="true" focusable="false" className="lang-menu__chevron">
          <path d="M3 4.5 6 7.5 9 4.5" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      </summary>
      <div className="lang-menu__list" role="menu" aria-label={label}>
        {locales.map((target) => {
          const current = target === locale;
          return (
            <a
              key={target}
              role="menuitemradio"
              aria-checked={current}
              href={`/${target}`}
              hrefLang={localeInfo[target].htmlLang}
              lang={localeInfo[target].htmlLang}
              className="lang-menu__item"
              onClick={(event) => keepSection(event, target)}
            >
              <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true" focusable="false" className="lang-menu__check">
                <path d="M3.5 8.5 6.5 11.5 12.5 4.5" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
              </svg>
              {localeInfo[target].label}
            </a>
          );
        })}
      </div>
    </details>
  );
}
