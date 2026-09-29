"use client";

import { useEffect, useRef, useState } from "react";
import { mobileVideoQuery, promoVideoSize } from "@/config/site";
import type { Dictionary } from "@/i18n/dictionary";
import { motionAllowed } from "./motion/motion";

interface HeroVideoProps {
  sources: { mp4: string; webm: string; mobileMp4: string; poster: string };
  labels: Dictionary["hero"]["video"];
}

/**
 * The promo video behind the hero: muted and looping, started by script so that
 * with reduced motion (or no JavaScript) only the poster shows. It pauses while
 * the hero is off screen. Controls: pause/play, and sound on/off (sound on also
 * sharpens the video, which is otherwise softened so the headline reads first).
 */
export function HeroVideo({ sources, labels }: HeroVideoProps) {
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
    <>
      {/* Deeper than the text: the video lags behind the scroll. With sound on it
          is shown sharp, for watching; muted it is softened behind the text. The range
          stays inside the extra height .hero__media has above and below the hero. */}
      <div
        className={muted ? "hero__media" : "hero__media is-watching"}
        data-parallax="0.18"
        data-parallax-range="100"
      >
      <video
        ref={video}
        className="hero__video"
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
      </div>
      <div className="hero__video-controls">
        <button type="button" className="video-button" onClick={togglePlay} aria-label={playing ? labels.pause : labels.play}>
          {playing ? (
            <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
              <rect x="3.5" y="2.5" width="3" height="11" rx="1" fill="currentColor" />
              <rect x="9.5" y="2.5" width="3" height="11" rx="1" fill="currentColor" />
            </svg>
          ) : (
            <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
              <path d="M4.5 2.8v10.4a.8.8 0 0 0 1.2.7l8.3-5.2a.8.8 0 0 0 0-1.4L5.7 2.1a.8.8 0 0 0-1.2.7z" fill="currentColor" />
            </svg>
          )}
        </button>
        <button type="button" className="video-button video-button--label" onClick={toggleSound} aria-pressed={!muted}>
          <svg viewBox="0 0 16 16" width="15" height="15" aria-hidden="true">
            <path d="M2.5 6v4h2.6L8.5 13V3L5.1 6z" fill="currentColor" />
            {muted ? (
              <path d="M11 6l3.5 4M14.5 6L11 10" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" />
            ) : (
              <path d="M10.8 5.4a3.6 3.6 0 0 1 0 5.2M12.6 3.8a6 6 0 0 1 0 8.4" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" />
            )}
          </svg>
          {muted ? labels.soundOn : labels.soundOff}
        </button>
      </div>
    </>
  );
}
