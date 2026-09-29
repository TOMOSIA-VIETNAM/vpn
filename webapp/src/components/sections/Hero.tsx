import { preload } from "react-dom";
import { promoVideo } from "@/config/site";
import type { Dictionary } from "@/i18n/dictionary";
import type { Locale } from "@/i18n/locales";
import { DownloadButton } from "../DownloadButton";
import { HeroVideo } from "../HeroVideo";

/**
 * Centred headline on the wallpaper field, then the promo video in a window frame.
 * It is on screen at load, so nothing in it waits for a reveal: text and poster
 * are complete on first paint.
 */
export function Hero({ locale, dict }: { locale: Locale; dict: Dictionary }) {
  const video = promoVideo(locale);
  preload(video.poster, { as: "image", fetchPriority: "high" });
  const t = dict.hero;

  return (
    <section className="hero field" aria-labelledby="hero-title">
      <div className="container hero__inner">
        <h1 id="hero-title" className="hero__title">
          <span className="hero__title-lead">{t.titleLead}</span> <span className="hero__title-strong">{t.titleStrong}</span>
        </h1>
        <p className="hero__lead">{t.lead}</p>
        <DownloadButton dict={dict} />
      </div>
      <HeroVideo sources={video} labels={t.video} />
    </section>
  );
}
