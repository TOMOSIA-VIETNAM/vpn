"use client";

import { useEffect } from "react";
import { motionAllowed } from "./motion";

const DEFAULT_RANGE = 16;

/**
 * Scroll-linked parallax for every [data-parallax] element. Its value is a depth:
 * the element is shifted by (distance of its parent's centre from the viewport
 * centre) × depth, so positive depths lag behind the scroll and negative ones lead.
 * The shift is capped at data-parallax-range px (default DEFAULT_RANGE), which must
 * stay below the free space around the element so it never covers its neighbours.
 * Only transform changes (through --parallax-y), once per animation frame.
 * Mounted once per page; renders nothing.
 */
export function Parallax() {
  useEffect(() => {
    if (!motionAllowed()) return;
    const layers = [...document.querySelectorAll<HTMLElement>("[data-parallax]")].map((el) => ({
      el,
      anchor: el.parentElement ?? el,
      depth: Number(el.dataset.parallax) || 0,
      range: Number(el.dataset.parallaxRange) || DEFAULT_RANGE,
    }));
    if (layers.length === 0) return;

    let frame = 0;
    const update = () => {
      frame = 0;
      const middle = window.innerHeight / 2;
      for (const { el, anchor, depth, range } of layers) {
        const rect = anchor.getBoundingClientRect();
        if (rect.bottom < -middle || rect.top > window.innerHeight + middle) continue;
        const offset = middle - (rect.top + rect.height / 2);
        const shift = Math.max(-range, Math.min(range, offset * depth));
        el.style.setProperty("--parallax-y", `${shift.toFixed(1)}px`);
      }
    };
    const schedule = () => {
      if (!frame) frame = window.requestAnimationFrame(update);
    };

    update();
    window.addEventListener("scroll", schedule, { passive: true });
    window.addEventListener("resize", schedule);
    return () => {
      window.removeEventListener("scroll", schedule);
      window.removeEventListener("resize", schedule);
      if (frame) window.cancelAnimationFrame(frame);
    };
  }, []);

  return null;
}
