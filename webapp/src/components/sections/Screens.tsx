import type { Dictionary } from "@/i18n/dictionary";
import { localeInfo, type Locale } from "@/i18n/locales";
import { SplitText } from "../motion/SplitText";
import { stagger } from "../motion/stagger";
import { Screenshot } from "../Screenshot";

/** Screenshots with their captions; on wide screens each caption stays pinned beside its screenshot. */
export function Screens({ locale, dict }: { locale: Locale; dict: Dictionary }) {
  const t = dict.screens;
  const shots = [
    { shot: "newConfiguration", alt: t.newConfigurationAlt, caption: t.newConfigurationCaption },
    { shot: "settings", alt: t.settingsAlt, caption: t.settingsCaption },
  ] as const;

  return (
    <section id="screenshots" className="section section--grouped" aria-labelledby="screenshots-title">
      <div className="container">
        <h2 id="screenshots-title" className="section__title split" data-reveal>
          <SplitText text={t.title} byWord={localeInfo[locale].splitWords} />
        </h2>
        <div className="screens">
          {shots.map(({ shot, alt, caption }) => (
            <figure key={shot} className="screens__item" data-reveal>
              <figcaption className="screens__caption" data-item style={stagger(0)}>
                {caption}
              </figcaption>
              <div className="screens__shot" data-item style={stagger(1)}>
                <Screenshot shot={shot} alt={alt} />
              </div>
            </figure>
          ))}
        </div>
      </div>
    </section>
  );
}
