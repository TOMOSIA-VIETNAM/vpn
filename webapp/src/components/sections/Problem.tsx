import { appAlerts, builtInAlert } from "@/config/app-ui";
import type { Dictionary } from "@/i18n/dictionary";
import { localeInfo, type Locale } from "@/i18n/locales";
import { SplitText, splitCount } from "../motion/SplitText";
import { stagger } from "../motion/stagger";

/** Dark band contrasting the built-in client's one message with the app's alerts. */
export function Problem({ locale, dict }: { locale: Locale; dict: Dictionary }) {
  const t = dict.problem;
  const byWord = localeInfo[locale].splitWords;
  const afterTitle = splitCount(t.title, byWord);

  return (
    <section id="why" className="section band" aria-labelledby="why-title">
      <div className="container">
        <div className="section__intro" data-reveal>
          <h2 id="why-title" className="section__title split">
            <SplitText text={t.title} byWord={byWord} />
          </h2>
          <p className="section__body" data-item style={stagger(afterTitle)}>
            {t.context}
          </p>
          <p className="section__body" data-item style={stagger(afterTitle + 1)}>
            {t.body}
          </p>
        </div>

        <div className="compare" data-reveal>
          <figure className="compare__side" data-item style={stagger(0)}>
            <figcaption className="compare__label">
              <strong>{t.builtInLabel}</strong>
              <span>{t.builtInCaption}</span>
            </figcaption>
            {/* The alert macOS shows, quoted in its original English. */}
            <div className="mac-alert" lang="en" data-parallax="0.08">
              <p className="mac-alert__title">{builtInAlert.title}</p>
              <p className="mac-alert__message">{builtInAlert.message}</p>
              <span className="mac-alert__button">{builtInAlert.action}</span>
            </div>
          </figure>

          <figure className="compare__side">
            <figcaption className="compare__label" data-item style={stagger(1)}>
              <strong>{t.appLabel}</strong>
              <span>{t.appCaption}</span>
            </figcaption>
            <ul className="alerts" lang="en" data-parallax="0.03">
              {appAlerts.map((alert, i) => (
                <li key={alert.title} className="alerts__item" data-item style={stagger(i + 2)}>
                  <span className={`status-dot status-dot--${alert.tone}`} aria-hidden="true" />
                  <div className="alerts__text">
                    <p className="alerts__title">{alert.title}</p>
                    <p className="alerts__message">{alert.message}</p>
                  </div>
                  {alert.action && <span className="alerts__action">{alert.action}</span>}
                </li>
              ))}
            </ul>
          </figure>
        </div>
      </div>
    </section>
  );
}
