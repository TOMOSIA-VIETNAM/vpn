"use client";

import { useEffect, useState } from "react";

/**
 * Floating round button, bottom right, that scrolls back to the top. It shows
 * once the hero has scrolled out of view. The scroll follows the page's
 * scroll-behavior: smooth, or instant under reduced motion. Focus moves to the
 * header's home link, so keyboard users continue from the top.
 */
export function BackToTop({ label }: { label: string }) {
  const [shown, setShown] = useState(false);

  useEffect(() => {
    const hero = document.querySelector(".hero");
    if (!hero) return;
    const observer = new IntersectionObserver(([entry]) => setShown(!entry.isIntersecting));
    observer.observe(hero);
    return () => observer.disconnect();
  }, []);

  function toTop() {
    window.scrollTo({ top: 0 });
    document.querySelector<HTMLElement>("header a")?.focus({ preventScroll: true });
  }

  return (
    <button
      type="button"
      className={`back-to-top ${shown ? "is-shown" : ""}`}
      onClick={toTop}
      aria-label={label}
      tabIndex={shown ? 0 : -1}
      aria-hidden={shown ? undefined : true}
    >
      <svg viewBox="0 0 20 20" width="18" height="18" aria-hidden="true" focusable="false">
        <path d="M10 15.5v-11M5 9.5l5-5 5 5" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
    </button>
  );
}
