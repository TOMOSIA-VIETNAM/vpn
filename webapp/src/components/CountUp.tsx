"use client";

import { useEffect, useRef } from "react";
import { motionAllowed } from "./motion/motion";

const DURATION_MS = 1100;

/**
 * A number that counts up from zero the first time it scrolls into view. The
 * final value is what the server renders, and what stays with reduced motion,
 * without JavaScript, or when the number is already on screen at load. The
 * counting digits are hidden from screen readers, which read `value` once.
 */
export function CountUp({ value, suffix = "" }: { value: number; suffix?: string }) {
  const digits = useRef<HTMLSpanElement>(null);

  useEffect(() => {
    const el = digits.current;
    if (!el || value <= 0 || !motionAllowed()) return;
    if (el.getBoundingClientRect().top < window.innerHeight) return;

    const show = (n: number) => {
      el.textContent = `${n}${suffix}`;
    };
    show(0);
    let frame = 0;
    const observer = new IntersectionObserver(
      ([entry]) => {
        if (!entry.isIntersecting) return;
        observer.disconnect();
        const start = performance.now();
        const tick = (now: number) => {
          const t = Math.min(1, (now - start) / DURATION_MS);
          show(Math.round(value * (1 - Math.pow(1 - t, 3))));
          if (t < 1) frame = window.requestAnimationFrame(tick);
        };
        frame = window.requestAnimationFrame(tick);
      },
      { threshold: 0.6 },
    );
    observer.observe(el);
    return () => {
      observer.disconnect();
      if (frame) window.cancelAnimationFrame(frame);
      show(value);
    };
  }, [value, suffix]);

  return (
    <>
      <span ref={digits} aria-hidden="true">
        {value}
        {suffix}
      </span>
      <span className="visually-hidden">
        {value}
        {suffix}
      </span>
    </>
  );
}
