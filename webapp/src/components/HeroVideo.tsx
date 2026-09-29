"use client";

import { useEffect, useRef, useState } from "react";
import { mobileVideoQuery, promoVideoSize, promoVideoTypes, site } from "@/config/site";
import type { Dictionary } from "@/i18n/dictionary";
import { motionAllowed } from "./motion/motion";

interface HeroVideoProps {
  sources: { mp4: string; webm: string; mobileMp4: string; poster: string };
  labels: Dictionary["hero"]["video"];
}

// How long the muted video plays in view before the sound button opens its
// label, and how long the label then stays open.
const INVITE_DELAY_MS = 1500;
const INVITE_OPEN_MS = 4000;

// How much wider than its resting size the frame may grow while scrolling, at most.
const MAX_GROWTH = 1.4;

/**
 * Starts the video and reports whether it plays. A refused start (an autoplay
 * policy, or a source the browser cannot decode) is logged with its reason; a
 * refused start with sound is retried muted, which every browser allows.
 */
async function startPlayback(el: HTMLVideoElement, reason: string): Promise<boolean> {
  try {
    await el.play();
    return true;
  } catch (error) {
    const name = error instanceof DOMException ? error.name : String(error);
    // AbortError: a pause() came in before playback began; not a failure.
    if (name === "AbortError") return false;
    console.warn(`Promo video did not start (${reason}): ${name}`, el.error ?? "");
    if (name === "NotAllowedError" && !el.muted) {
      el.muted = true;
      try {
        await el.play();
        return true;
      } catch {
        return false;
      }
    }
    return false;
  }
}

/**
 * The promo video in a macOS window frame under the hero text. Muted and looping,
 * started by script when at least a fifth of it is on screen (also at load), and
 * paused when it leaves; it resumes on return if it was playing. With reduced
 * motion (or no JavaScript) only the poster shows until something is pressed.
 *
 * Browsers only autoplay muted, so the corner sound button invites the visitor to
 * turn sound on: while muted, tiny equalizer bars move beside its speaker; a
 * moment after the video starts, it opens into a pill with its label and two soft
 * rings, then closes again after a few seconds; hovering the picture or focusing
 * the button opens it again. It, or a click anywhere on the picture, restarts the
 * video from the beginning with sound. From then on it is a plain sound toggle.
 * Under reduced motion the label shows from the start and nothing moves.
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
  const progress = useRef<HTMLSpanElement>(null);
  const [playing, setPlaying] = useState(false);
  const [muted, setMuted] = useState(true);
  // Sound has been turned on once: the invitation is over.
  const [heard, setHeard] = useState(false);
  // The timed invitation: label open (with the rings, once).
  const [inviting, setInviting] = useState(false);
  // Reduced motion: the label stays open and nothing moves.
  const [calm, setCalm] = useState(false);
  // Whether the video should play while it is on screen: on for autoplay, then
  // whatever the visitor chose last (play, pause, sound on).
  const wanted = useRef(false);

  useEffect(() => {
    const el = video.current;
    if (!el) return;
    // Browsers only autoplay a muted inline video; set both as properties too,
    // since hydration does not reapply attributes.
    el.muted = true;
    el.playsInline = true;
    const autoplay = motionAllowed();
    wanted.current = autoplay;

    const timers: number[] = [];
    let invited = false;
    const onPlay = () => setPlaying(true);
    const onPause = () => setPlaying(false);
    const onVolume = () => setMuted(el.muted);
    // The invitation runs once, a moment after the muted video starts playing.
    const onPlaying = () => {
      if (invited || !el.muted || !autoplay) return;
      invited = true;
      timers.push(
        window.setTimeout(() => setInviting(true), INVITE_DELAY_MS),
        window.setTimeout(() => setInviting(false), INVITE_DELAY_MS + INVITE_OPEN_MS),
      );
    };

    // The thin progress line moves every frame while the video plays (a transform).
    let tick = 0;
    const showProgress = () => {
      if (Number.isFinite(el.duration) && el.duration > 0) {
        progress.current?.style.setProperty("--progress", (el.currentTime / el.duration).toFixed(4));
      }
    };
    const loop = () => {
      showProgress();
      tick = el.paused ? 0 : window.requestAnimationFrame(loop);
    };
    const onRun = () => {
      if (!tick) tick = window.requestAnimationFrame(loop);
    };

    el.addEventListener("play", onPlay);
    el.addEventListener("pause", onPause);
    el.addEventListener("volumechange", onVolume);
    el.addEventListener("playing", onPlaying);
    el.addEventListener("playing", onRun);
    el.addEventListener("seeked", showProgress);
    if (!autoplay) timers.push(window.setTimeout(() => setCalm(true), 0));

    const observer = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting && wanted.current && el.paused) void startPlayback(el, "in view");
        if (!entry.isIntersecting && !el.paused) el.pause();
      },
      { threshold: 0.2 },
    );
    observer.observe(el);
    return () => {
      observer.disconnect();
      timers.forEach((t) => window.clearTimeout(t));
      if (tick) window.cancelAnimationFrame(tick);
      el.removeEventListener("play", onPlay);
      el.removeEventListener("pause", onPause);
      el.removeEventListener("volumechange", onVolume);
      el.removeEventListener("playing", onPlaying);
      el.removeEventListener("playing", onRun);
      el.removeEventListener("seeked", showProgress);
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
      const scrolled = Math.min(1, Math.max(0, window.scrollY / distance));
      // Ease out: most of the growth happens early in the scroll.
      const eased = 1 - (1 - scrolled) * (1 - scrolled);
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

  /** Sound on for the first time: from the start, with sound. */
  function playWithSound() {
    const el = video.current;
    if (!el) return;
    setHeard(true);
    setInviting(false);
    wanted.current = true;
    el.currentTime = 0;
    el.muted = false;
    void startPlayback(el, "sound on");
  }

  function togglePlay() {
    const el = video.current;
    if (!el) return;
    if (el.paused) {
      wanted.current = true;
      void startPlayback(el, "play button");
    } else {
      wanted.current = false;
      el.pause();
    }
  }

  function toggleSound() {
    const el = video.current;
    if (!el) return;
    if (!heard) {
      playWithSound();
      return;
    }
    el.muted = !el.muted;
    if (!el.muted && el.paused) {
      wanted.current = true;
      void startPlayback(el, "sound button");
    }
  }

  /** A click anywhere on the picture: sound on the first time, then play/pause. */
  function onScreenClick() {
    if (!heard && video.current?.muted) playWithSound();
    else togglePlay();
  }

  const invite = !heard && muted;
  const soundClass = [
    "sound-button",
    invite && "is-inviting",
    invite && (inviting || calm) && "is-open",
    invite && inviting && "is-rippling",
    calm && "is-calm",
  ]
    .filter(Boolean)
    .join(" ");

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
        {/* The whole picture is a pointer target; the corner buttons are the
            keyboard route to the same actions. */}
        <div className="window__screen" onClick={onScreenClick}>
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
            <source src={sources.mobileMp4} type={promoVideoTypes.mobileMp4} media={mobileVideoQuery} />
            <source src={sources.webm} type={promoVideoTypes.webm} />
            <source src={sources.mp4} type={promoVideoTypes.mp4} />
          </video>
          <span className="window__progress" ref={progress} aria-hidden="true" />
          <div className="window__controls" onClick={(event) => event.stopPropagation()}>
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
              className={`video-button ${soundClass}`}
              onClick={toggleSound}
              aria-pressed={!muted}
              aria-label={invite ? undefined : muted ? labels.soundOn : labels.soundOff}
            >
              <svg viewBox="0 0 16 16" width="15" height="15" aria-hidden="true" className="sound-button__icon">
                <path d="M2.5 6v4h2.6L8.5 13V3L5.1 6z" fill="currentColor" />
                {!invite &&
                  (muted ? (
                    <path d="M11 6l3.5 4M14.5 6L11 10" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" />
                  ) : (
                    <path
                      d="M10.8 5.4a3.6 3.6 0 0 1 0 5.2M12.6 3.8a6 6 0 0 1 0 8.4"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="1.4"
                      strokeLinecap="round"
                    />
                  ))}
              </svg>
              {invite && (
                <>
                  <span className="sound-button__bars" aria-hidden="true">
                    <span />
                    <span />
                    <span />
                  </span>
                  <span className="sound-button__label">{calm ? labels.playWithSound : labels.turnOnSound}</span>
                </>
              )}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
