import type { Dictionary } from "@/i18n/dictionary";
import { localeInfo, type Locale } from "@/i18n/locales";
import { SplitText } from "../motion/SplitText";
import { stagger } from "../motion/stagger";
import { Screenshot } from "../Screenshot";

/**
 * The two windows a new user meets, one in each appearance, overlapping on a
 * wallpaper with a little parallax depth; captions underneath.
 */
export function Screens({ locale, dict }: { locale: Locale; dict: Dictionary }) {
  const t = dict.screens;
  return (
    <section id="screenshots" className="section" aria-labelledby="screenshots-title">
      <div className="container">
        <div className="section__head section__head--center" data-reveal>
          <p className="eyebrow eyebrow--indigo" data-item style={stagger(0)}>
            {t.eyebrow}
          </p>
          <h2 id="screenshots-title" className="section__title split">
            <SplitText text={t.title} byWord={localeInfo[locale].splitWords} start={1} />
          </h2>
        </div>
        <figure className="shots" data-reveal>
          <div className="shots__stage field">
            <div className="shots__window shots__window--back" data-item style={stagger(0)}>
              <div data-parallax="0.05" data-parallax-range="24">
                <Screenshot shot="newConfiguration" appearance="light" alt={t.newConfigurationAlt} />
              </div>
            </div>
            <div className="shots__window shots__window--front" data-item style={stagger(2)}>
              <div data-parallax="-0.07" data-parallax-range="32">
                <Screenshot shot="settings" appearance="dark" alt={t.settingsAlt} />
              </div>
            </div>
          </div>
          <figcaption className="shots__captions">
            <p data-item style={stagger(3)}>{t.newConfigurationCaption}</p>
            <p data-item style={stagger(4)}>{t.settingsCaption}</p>
          </figcaption>
        </figure>
      </div>
    </section>
  );
}
