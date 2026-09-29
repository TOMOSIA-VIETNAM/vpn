import type { ReactNode } from "react";
import {
  appAlerts,
  appNotifications,
  demoKeychainItems,
  demoProfiles,
  demoPublicIp,
  popoverLabels,
} from "@/config/app-ui";
import type { Dictionary } from "@/i18n/dictionary";
import { localeInfo, type Locale } from "@/i18n/locales";
import { Mark, type MarkStyle } from "../Mark";
import { SplitText } from "../motion/SplitText";
import { stagger } from "../motion/stagger";
import { Notification } from "../Notification";
import { RichText } from "../RichText";

type FeatureKey = keyof Dictionary["features"]["items"];

const office = demoProfiles[0];
const notices = appNotifications(office);

// The status item in each connection state, as the menu bar shows it.
const menuBarStates: { style: MarkStyle; tone: "idle" | "ok" | "error" }[] = [
  { style: "outline", tone: "idle" },
  { style: "solid", tone: "ok" },
  { style: "badged", tone: "error" },
];

// A piece of the real app shown in each feature tile.
const visuals: Record<FeatureKey, ReactNode> = {
  menuBar: (
    <div className="tile__menubar" aria-hidden="true">
      {menuBarStates.map(({ style, tone }) => (
        <span key={style} className="tile__menubar-item">
          <Mark style={style} size={26} />
          <span className={`status-dot status-dot--${tone}`} />
        </span>
      ))}
    </div>
  ),
  reconnect: (
    <div className="tile__notices" aria-hidden="true">
      <Notification {...notices.lost} className="tile__notice" style={stagger(1)} />
      <Notification {...notices.back} className="tile__notice" style={stagger(2)} />
    </div>
  ),
  errors: (
    <ul className="tile__alerts" aria-hidden="true" lang="en">
      {appAlerts.slice(0, 3).map((alert) => (
        <li key={alert.title}>
          <span className={`status-dot status-dot--${alert.tone}`} />
          {alert.title}
        </li>
      ))}
    </ul>
  ),
  killSwitch: (
    <div className="tile__setting" aria-hidden="true" lang="en">
      <span>{popoverLabels.killSwitch}</span>
      <span className="switch switch--static tile__kill-switch">
        <span className="switch__knob" />
      </span>
    </div>
  ),
  publicIp: (
    <div className="tile__ip" aria-hidden="true" lang="en">
      <span className="tile__ip-label">{popoverLabels.publicIp}</span>
      <span className="tile__ip-value">{demoPublicIp}</span>
    </div>
  ),
  keychain: (
    <ul className="tile__keychain" aria-hidden="true" lang="en">
      {demoKeychainItems.map((item) => (
        <li key={item}>
          <svg viewBox="0 0 20 20" width="16" height="16">
            <circle cx="7" cy="10" r="3.6" fill="none" stroke="currentColor" strokeWidth="1.6" />
            <path d="M10.6 10H17.5M15 10v2.6M17.5 10v2" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
          </svg>
          {item}
        </li>
      ))}
    </ul>
  ),
};

// Tile order; the grid in globals.css gives each position its size.
const order: FeatureKey[] = ["menuBar", "reconnect", "errors", "killSwitch", "publicIp", "keychain"];

/** Features as a bento grid of tiles, each with a small piece of the real app. */
export function Features({ locale, dict }: { locale: Locale; dict: Dictionary }) {
  return (
    <section id="features" className="section" aria-labelledby="features-title">
      <div className="container">
        <h2 id="features-title" className="section__title split" data-reveal>
          <SplitText text={dict.features.title} byWord={localeInfo[locale].splitWords} />
        </h2>
        <ul className="bento" data-reveal>
          {order.map((key, i) => {
            const feature = dict.features.items[key];
            return (
              <li key={key} className={`tile tile--${key}`} data-item style={stagger(i)}>
                <div className="tile__text">
                  <h3 className="tile__title">{feature.title}</h3>
                  <p className="tile__body">
                    <RichText text={feature.body} />
                  </p>
                </div>
                <div className="tile__visual">{visuals[key]}</div>
              </li>
            );
          })}
        </ul>
      </div>
    </section>
  );
}
