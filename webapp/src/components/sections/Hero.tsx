import { preload } from "react-dom";
import { promoVideo } from "@/config/site";
import type { Dictionary } from "@/i18n/dictionary";
import type { Locale } from "@/i18n/locales";
import { DownloadButton } from "../DownloadButton";
import { HeroVideo } from "../HeroVideo";

/**
 * Hero over the promo video. It is on screen at load, so nothing in it waits for a
 * reveal: text and poster are complete on first paint, and the video fades in over
 * the poster once it plays.
 */
export function Hero({ locale, dict }: { locale: Locale; dict: Dictionary }) {
  const video = promoVideo(locale);
  preload(video.poster, { as: "image", fetchPriority: "high" });

  return (
    <section className="hero" aria-labelledby="hero-title">
      <HeroVideo sources={video} labels={dict.hero.video} />
      <div className="hero__scrim" aria-hidden="true" />
      <div className="container hero__inner" data-parallax="-0.08" data-parallax-range="60">
        <h1 id="hero-title" className="hero__title">
          {dict.hero.title}
        </h1>
        <p className="hero__lead">{dict.hero.lead}</p>
        <DownloadButton dict={dict} />
      </div>
    </section>
  );
}
