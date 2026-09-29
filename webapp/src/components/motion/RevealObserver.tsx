"use client";

import { useEffect } from "react";
import { PENDING_CLASS, motionAllowed } from "./motion";

/**
 * Holds every [data-reveal] block that starts below the fold in its pending
 * (hidden) state, and releases it the first time it scrolls into view. Blocks
 * already on screen at load are left alone. Mounted once per page; renders nothing.
 */
export function RevealObserver() {
  useEffect(() => {
    if (!motionAllowed()) return;

    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          if (!entry.isIntersecting) continue;
          entry.target.classList.remove(PENDING_CLASS);
          observer.unobserve(entry.target);
        }
      },
      { rootMargin: "0px 0px -8% 0px" },
    );

    const fold = window.innerHeight;
    for (const el of document.querySelectorAll<HTMLElement>("[data-reveal]")) {
      if (el.getBoundingClientRect().top < fold) continue;
      el.classList.add(PENDING_CLASS);
      observer.observe(el);
    }
    return () => observer.disconnect();
  }, []);

  return null;
}
