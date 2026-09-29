"use client";

import { useEffect, useRef, useState } from "react";
import { mobileVideoQuery, promoVideoSize, site } from "@/config/site";
import type { Dictionary } from "@/i18n/dictionary";
import { motionAllowed } from "./motion/motion";

interface HeroVideoProps {
  sources: { mp4: string; webm: string; mobileMp4: string; poster: string };
  labels: Dictionary["hero"]["video"];
}

// How much wider than its resting size the frame may grow while scrolling, at most.
const MAX_GROWTH = 1.4;

/**
 * The promo video in a macOS window frame under the hero text. Muted and looping,
 * started by script so that with reduced motion (or no JavaScript) only the poster
 * shows; paused while off screen. Controls: pause/play and sound on/off.
 *
 * With motion allowed, the frame scales up toward the full viewport width as the
 * page scrolls, reaching it when its top meets the header. Only transform changes;
 * the stage below it is given the room the scaled frame needs (--grow-room), so it
 * never covers the next section.
 */
export function HeroVideo({ sources, labels }: HeroVideoProps) {
  const stage = useRef<HTMLDivElement>(null);
  const frame = useRef<HTMLDivElement>(null);
  const video = useRef<HTMLVideoElement>(null);
  const [playing, setPlaying] = useState(false);
  const [muted, setMuted] = useState(true);
  // Set when the visitor pauses, so scrolling back does not restart the video.
  const pausedByUser = useRef(false);

  useEffect(() => {
    const el = video.current;
    if (!el) return;
    const onPlay = () => setPlaying(true);
    const onPause = () => setPlaying(false);
    el.addEventListener("play", onPlay);
    el.addEventListener("pause", onPause);

    if (!motionAllowed()) pausedByUser.current = true;
    const observer = new IntersectionObserver(([entry]) => {
      if (entry.isIntersecting && !pausedByUser.current) el.play().catch(() => {});
      if (!entry.isIntersecting) el.pause();
    });
    observer.observe(el);
    return () => {
      observer.disconnect();
      el.removeEventListener("play", onPlay);
      el.removeEventListener("pause", onPause);
    };
  }, []);

  useEffect(() => {
    const box = frame.current;
    const room = stage.current;
    if (!box || !room || !motionAllowed()) return;

    let maxScale = 1;
    let distance = 1;
    let frameId = 0;

    const measure = () => {
      // The frame scales from its top edge, so its top and layout size do not
      // depend on the current scale.
      const header = document.querySelector("header")?.getBoundingClientRect().height ?? 0;
      const top = box.getBoundingClientRect().top + window.scrollY;
      maxScale = Math.min(MAX_GROWTH, document.documentElement.clientWidth / box.offsetWidth);
      distance = Math.max(1, top - header);
      room.style.setProperty("--grow-room", `${((maxScale - 1) * box.offsetHeight).toFixed(0)}px`);
    };
    const update = () => {
      frameId = 0;
      const progress = Math.min(1, Math.max(0, window.scrollY / distance));
      // Ease out: most of the growth happens early in the scroll.
      const eased = 1 - (1 - progress) * (1 - progress);
      box.style.setProperty("--frame-scale", (1 + (maxScale - 1) * eased).toFixed(4));
    };
    const schedule = () => {
      if (!frameId) frameId = window.requestAnimationFrame(update);
    };
    const onResize = () => {
      measure();
      schedule();
    };

    measure();
    update();
    window.addEventListener("scroll", schedule, { passive: true });
    window.addEventListener("resize", onResize);
    return () => {
      window.removeEventListener("scroll", schedule);
      window.removeEventListener("resize", onResize);
      if (frameId) window.cancelAnimationFrame(frameId);
    };
  }, []);

  function togglePlay() {
    const el = video.current;
    if (!el) return;
    if (el.paused) {
      pausedByUser.current = false;
      el.play().catch(() => {});
    } else {
      pausedByUser.current = true;
      el.pause();
    }
  }

  function toggleSound() {
    const el = video.current;
    if (!el) return;
    el.muted = !el.muted;
    setMuted(el.muted);
    if (!el.muted && el.paused) {
      pausedByUser.current = false;
      el.play().catch(() => {});
    }
  }

  return (
    <div className="hero__stage" ref={stage}>
      <div className="window" ref={frame}>
        <div className="window__bar" aria-hidden="true">
          <span className="window__lights">
            <span />
            <span />
            <span />
          </span>
          <span className="window__title">{site.name}</span>
        </div>
        <div className="window__screen">
          <video
            ref={video}
            className="window__video"
            poster={sources.poster}
            width={promoVideoSize.width}
            height={promoVideoSize.height}
            muted
            loop
            playsInline
            preload="metadata"
            aria-hidden="true"
            tabIndex={-1}
          >
            <source src={sources.mobileMp4} type="video/mp4" media={mobileVideoQuery} />
            <source src={sources.webm} type="video/webm" />
            <source src={sources.mp4} type="video/mp4" />
          </video>
          <div className="window__controls">
            <button
              type="button"
              className="video-button"
              onClick={togglePlay}
              aria-label={playing ? labels.pause : labels.play}
            >
              {playing ? (
                <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
                  <rect x="3.5" y="2.5" width="3" height="11" rx="1" fill="currentColor" />
                  <rect x="9.5" y="2.5" width="3" height="11" rx="1" fill="currentColor" />
                </svg>
              ) : (
                <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
                  <path
                    d="M4.5 2.8v10.4a.8.8 0 0 0 1.2.7l8.3-5.2a.8.8 0 0 0 0-1.4L5.7 2.1a.8.8 0 0 0-1.2.7z"
                    fill="currentColor"
                  />
                </svg>
              )}
            </button>
            <button
              type="button"
              className="video-button video-button--label"
              onClick={toggleSound}
              aria-pressed={!muted}
            >
              <svg viewBox="0 0 16 16" width="15" height="15" aria-hidden="true">
                <path d="M2.5 6v4h2.6L8.5 13V3L5.1 6z" fill="currentColor" />
                {muted ? (
                  <path d="M11 6l3.5 4M14.5 6L11 10" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" />
                ) : (
                  <path
                    d="M10.8 5.4a3.6 3.6 0 0 1 0 5.2M12.6 3.8a6 6 0 0 1 0 8.4"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="1.4"
                    strokeLinecap="round"
                  />
                )}
              </svg>
              {muted ? labels.soundOn : labels.soundOff}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
