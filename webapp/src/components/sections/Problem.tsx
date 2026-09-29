import { appAlerts, builtInAlert } from "@/config/app-ui";
import type { Dictionary } from "@/i18n/dictionary";

export function Problem({ dict }: { dict: Dictionary }) {
  const t = dict.problem;
  return (
    <section id="why" className="section section--grouped" aria-labelledby="why-title">
      <div className="container">
        <div className="section__intro">
          <h2 id="why-title" className="section__title">
            {t.title}
          </h2>
          <p className="section__body">{t.body}</p>
        </div>

        <div className="compare">
          <figure className="compare__side">
            <figcaption className="compare__label">
              <strong>{t.builtInLabel}</strong>
              <span>{t.builtInCaption}</span>
            </figcaption>
            {/* The alert macOS shows, quoted in its original English. */}
            <div className="mac-alert" lang="en">
              <p className="mac-alert__title">{builtInAlert.title}</p>
              <p className="mac-alert__message">{builtInAlert.message}</p>
              <span className="mac-alert__button">{builtInAlert.action}</span>
            </div>
          </figure>

          <figure className="compare__side">
            <figcaption className="compare__label">
              <strong>{t.appLabel}</strong>
              <span>{t.appCaption}</span>
            </figcaption>
            <ul className="alerts" lang="en">
              {appAlerts.map((alert) => (
                <li key={alert.title} className="alerts__item">
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
