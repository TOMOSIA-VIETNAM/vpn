import { appAlerts, builtInAlert } from "@/config/app-ui";
import type { Dictionary } from "@/i18n/dictionary";
import { localeInfo, type Locale } from "@/i18n/locales";
import { AlertCard } from "../AlertCard";
import { Compare } from "../Compare";
import { Icon } from "../Icon";
import { SplitText, splitCount } from "../motion/SplitText";
import { stagger } from "../motion/stagger";

/** Before and after: the built-in client's one message against the app's named alerts. */
export function Problem({ locale, dict }: { locale: Locale; dict: Dictionary }) {
  const t = dict.problem;
  const byWord = localeInfo[locale].splitWords;
  const afterTitle = splitCount(t.title, byWord) + 1;

  const builtIn = (
    <div className="before">
      <ul className="before__causes">
        {t.causes.map((cause) => (
          <li key={cause} className="cause">
            {cause}
          </li>
        ))}
      </ul>
      <svg className="before__funnel" viewBox="0 0 400 60" preserveAspectRatio="none" aria-hidden="true">
        {[50, 150, 250, 350].map((x) => (
          <path key={x} d={`M${x} 0 C ${x} 30, 200 30, 200 60`} />
        ))}
      </svg>
      {/* The alert macOS shows, quoted in its original English. */}
      <div className="mac-alert" lang="en">
        <span className="mac-alert__icon">
          <Icon name="network" size={26} />
        </span>
        <p className="mac-alert__title">{builtInAlert.title}</p>
        <p className="mac-alert__message">{builtInAlert.message}</p>
        <span className="mac-alert__button">{builtInAlert.action}</span>
      </div>
    </div>
  );

  const app = (
    <ul className="after">
      {appAlerts.map((alert, i) => (
        <li key={alert.title} className="after__item">
          <span className="cause">{t.causes[i]}</span>
          <AlertCard alert={alert} />
        </li>
      ))}
    </ul>
  );

  return (
    <section id="why" className="section section--alt" aria-labelledby="why-title">
      <div className="container">
        <div className="section__head section__head--center" data-reveal>
          <p className="eyebrow eyebrow--red" data-item style={stagger(0)}>
            {t.eyebrow}
          </p>
          <h2 id="why-title" className="section__title split">
            <SplitText text={t.title} byWord={byWord} start={1} />
          </h2>
          <p className="section__lead" data-item style={stagger(afterTitle)}>
            {t.body}
          </p>
          <p className="section__detail" data-item style={stagger(afterTitle + 1)}>
            {t.detail}
          </p>
        </div>
        <Compare
          labels={{ group: t.toggleLabel, builtIn: t.builtInLabel, app: t.appLabel }}
          captions={{ builtIn: t.builtInCaption, app: t.appCaption }}
          panes={{ builtIn, app }}
        />
      </div>
    </section>
  );
}
