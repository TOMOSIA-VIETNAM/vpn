"use client";

import { useEffect } from "react";
import { READY_ATTRIBUTE, REVEALED_CLASS, motionAllowed } from "./motion";

/**
 * Adds the revealed class to every [data-reveal] element the first time it
 * enters the viewport. Mounted once per page; renders nothing.
 */
export function RevealObserver() {
  useEffect(() => {
    const root = document.documentElement;
    root.setAttribute(READY_ATTRIBUTE, "");
    if (!motionAllowed()) return;

    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          if (!entry.isIntersecting) continue;
          entry.target.classList.add(REVEALED_CLASS);
          observer.unobserve(entry.target);
        }
      },
      // Reveal a little before the element is fully in view, never above the fold later.
      { rootMargin: "0px 0px -8% 0px" },
    );
    document.querySelectorAll("[data-reveal]").forEach((el) => observer.observe(el));
    return () => observer.disconnect();
  }, []);

  return null;
}
