"use client";

import { useEffect, useRef, useState, type ReactNode } from "react";
import { motionAllowed } from "./motion/motion";

type Side = "builtIn" | "app";

interface CompareProps {
  labels: { group: string; builtIn: string; app: string };
  captions: Record<Side, string>;
  panes: Record<Side, ReactNode>;
}

// After the section scrolls into view, how long the built-in message shows before
// the toggle switches to TOMOSIA VPN by itself.
const AUTOPLAY_DELAY_MS = 1800;

/**
 * Segmented control swapping two panes that share one stage, so the stage keeps
 * its height and the panes crossfade. The TOMOSIA VPN side is the rendered
 * default (reduced motion, no JavaScript, or on screen at load). Otherwise it starts
 * on the built-in side and switches once, on its own, after it scrolls into view;
 * a click stops that.
 */
export function Compare({ labels, captions, panes }: CompareProps) {
  const [side, setSide] = useState<Side>("app");
  const root = useRef<HTMLDivElement>(null);
  const touched = useRef(false);

  useEffect(() => {
    const el = root.current;
    if (!el || !motionAllowed() || el.getBoundingClientRect().top < window.innerHeight) return;
    const timers: number[] = [];
    timers.push(window.setTimeout(() => setSide("builtIn"), 0));
    const observer = new IntersectionObserver(
      ([entry]) => {
        if (!entry.isIntersecting) return;
        observer.disconnect();
        timers.push(
          window.setTimeout(() => {
            if (!touched.current) setSide("app");
          }, AUTOPLAY_DELAY_MS),
        );
      },
      { threshold: 0.55 },
    );
    observer.observe(el);
    return () => {
      observer.disconnect();
      timers.forEach((t) => window.clearTimeout(t));
    };
  }, []);

  function choose(next: Side) {
    touched.current = true;
    setSide(next);
  }

  const sides: Side[] = ["builtIn", "app"];
  return (
    <div className="compare" ref={root} data-side={side}>
      <div className="segmented" role="group" aria-label={labels.group}>
        {sides.map((s) => (
          <button
            key={s}
            type="button"
            className="segmented__option"
            aria-pressed={side === s}
            onClick={() => choose(s)}
          >
            {labels[s]}
          </button>
        ))}
        <span className="segmented__thumb" aria-hidden="true" />
      </div>
      <div className="compare__stage field field--soft">
        {sides.map((s) => (
          <div key={s} className={`compare__pane compare__pane--${s}`} data-active={side === s} inert={side !== s}>
            {panes[s]}
          </div>
        ))}
      </div>
      <p className="compare__caption" aria-live="polite">
        {captions[side]}
      </p>
    </div>
  );
}
