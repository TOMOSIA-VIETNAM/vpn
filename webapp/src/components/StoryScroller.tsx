"use client";

import { useEffect, useRef, useState } from "react";
import type { Step } from "@/i18n/dictionary";
import { Desk, type DeskState } from "./Desk";

interface StoryScrollerProps {
  steps: readonly Step[];
  /** What the desk shows for each step, in step order. */
  states: readonly DeskState[];
  year: number;
}

/**
 * Steps on the left; on wide screens one sticky desk on the right that takes the
 * state of the step crossing the middle of the viewport. On narrow screens the
 * sticky desk is hidden and each step card carries its own desk. Without
 * JavaScript every step reads at full strength and the desk shows the first state.
 */
export function StoryScroller({ steps, states, year }: StoryScrollerProps) {
  const [active, setActive] = useState(0);
  const [live, setLive] = useState(false);
  const list = useRef<HTMLOListElement>(null);

  useEffect(() => {
    const el = list.current;
    if (!el) return;
    const items = [...el.querySelectorAll<HTMLElement>("[data-step]")];
    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          if (entry.isIntersecting) setActive(Number((entry.target as HTMLElement).dataset.step));
        }
      },
      // A line across the middle of the viewport.
      { rootMargin: "-50% 0px -50% 0px" },
    );
    items.forEach((item) => observer.observe(item));
    const timer = window.setTimeout(() => setLive(true), 0);
    return () => {
      observer.disconnect();
      window.clearTimeout(timer);
    };
  }, []);

  return (
    <div className={`story__body ${live ? "is-live" : ""}`}>
      <ol className="story__steps" ref={list}>
        {steps.map((step, i) => (
          <li key={step.title} className="story__step" data-step={i} data-active={i === active}>
            <div className="story__text">
              <span className="story__number" aria-hidden="true">
                {i + 1}
              </span>
              <h3 className="story__title">{step.title}</h3>
              <p className="story__copy">{step.body}</p>
            </div>
            <Desk state={states[i]} year={year} className="story__card-desk" />
          </li>
        ))}
      </ol>
      <div className="story__sticky">
        <Desk state={states[active]} year={year} className="story__desk" />
        <div className="story__progress" aria-hidden="true">
          {steps.map((step, i) => (
            <span key={step.title} data-active={i === active} />
          ))}
        </div>
      </div>
    </div>
  );
}
