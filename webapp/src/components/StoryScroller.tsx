"use client";

import { useEffect, useRef, useState } from "react";
import type { Step } from "@/i18n/dictionary";
import { Desk, type DeskState } from "./Desk";

interface StoryScrollerProps {
  steps: readonly Step[];
  /** What the desk shows for each step, in step order. */
  states: readonly DeskState[];
  /** Accessible name of the row of step buttons. */
  navLabel: string;
  year: number;
}

/** Index of the step whose centre is closest to the viewport's centre. */
function closestStep(items: HTMLElement[]): number {
  const middle = window.innerHeight / 2;
  let best = 0;
  let bestDistance = Infinity;
  items.forEach((item, i) => {
    const rect = item.getBoundingClientRect();
    const distance = Math.abs(rect.top + rect.height / 2 - middle);
    if (distance < bestDistance) {
      best = i;
      bestDistance = distance;
    }
  });
  return best;
}

/**
 * Steps on the left; on wide screens a sticky column on the right with one desk
 * per step, stacked, and the active step's desk faded in. The active step is the
 * one whose centre is closest to the viewport's centre, measured once per frame
 * while scrolling, so it follows the scroll at any viewport height. The dots and
 * the step numbers scroll to their step. On narrow screens the sticky column is
 * hidden and each step card carries its own desk. Without JavaScript every step
 * reads at full strength and the first desk shows.
 */
export function StoryScroller({ steps, states, navLabel, year }: StoryScrollerProps) {
  const [active, setActive] = useState(0);
  const [live, setLive] = useState(false);
  const list = useRef<HTMLOListElement>(null);

  useEffect(() => {
    const el = list.current;
    if (!el) return;
    const items = [...el.querySelectorAll<HTMLElement>("[data-step]")];
    let frame = 0;
    const update = () => {
      frame = 0;
      setActive(closestStep(items));
    };
    const schedule = () => {
      if (!frame) frame = window.requestAnimationFrame(update);
    };
    frame = window.requestAnimationFrame(() => {
      update();
      setLive(true);
    });
    window.addEventListener("scroll", schedule, { passive: true });
    window.addEventListener("resize", schedule);
    return () => {
      window.removeEventListener("scroll", schedule);
      window.removeEventListener("resize", schedule);
      if (frame) window.cancelAnimationFrame(frame);
    };
  }, []);

  function goTo(index: number) {
    const item = list.current?.querySelectorAll<HTMLElement>("[data-step]")[index];
    if (!item) return;
    const rect = item.getBoundingClientRect();
    // Scrolls with the page's scroll-behavior (instant under reduced motion).
    window.scrollTo({ top: window.scrollY + rect.top + rect.height / 2 - window.innerHeight / 2 });
  }

  return (
    <div className={`story__body ${live ? "is-live" : ""}`}>
      <ol className="story__steps" ref={list}>
        {steps.map((step, i) => (
          <li key={step.title} className="story__step" data-step={i} data-active={i === active}>
            <div className="story__text">
              <button
                type="button"
                className="story__number"
                onClick={() => goTo(i)}
                aria-label={step.title}
                tabIndex={-1}
              >
                {i + 1}
              </button>
              <h3 className="story__title">{step.title}</h3>
              <p className="story__copy">{step.body}</p>
            </div>
            <Desk state={states[i]} year={year} className="story__card-desk" />
          </li>
        ))}
      </ol>
      <div className="story__sticky">
        <div className="story__stack">
          {states.map((state, i) => (
            <Desk
              key={i}
              state={state}
              year={year}
              className={`story__desk ${i === active ? "is-active" : ""}`}
            />
          ))}
        </div>
        <div className="story__progress" role="group" aria-label={navLabel}>
          {steps.map((step, i) => (
            <button
              key={step.title}
              type="button"
              className="story__dot"
              aria-label={`${i + 1}. ${step.title}`}
              aria-current={i === active ? "step" : undefined}
              onClick={() => goTo(i)}
            />
          ))}
        </div>
      </div>
    </div>
  );
}
