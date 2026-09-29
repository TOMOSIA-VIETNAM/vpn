import type { Dictionary } from "@/i18n/dictionary";
import { Screenshot } from "../Screenshot";

export function Screens({ dict }: { dict: Dictionary }) {
  const t = dict.screens;
  return (
    <section id="screenshots" className="section section--grouped" aria-labelledby="screenshots-title">
      <div className="container">
        <h2 id="screenshots-title" className="section__title">
          {t.title}
        </h2>
        <div className="screens">
          <figure className="screens__item">
            <Screenshot shot="newConfiguration" alt={t.newConfigurationAlt} />
            <figcaption>{t.newConfigurationCaption}</figcaption>
          </figure>
          <figure className="screens__item">
            <Screenshot shot="settings" alt={t.settingsAlt} />
            <figcaption>{t.settingsCaption}</figcaption>
          </figure>
        </div>
      </div>
    </section>
  );
}
