import type { ReactNode } from "react";
import {
  appAlert,
  appNotifications,
  demoKeychainItems,
  demoProfiles,
  popoverHeader,
  popoverLabels,
} from "@/config/app-ui";
import { site } from "@/config/site";
import type { Dictionary } from "@/i18n/dictionary";
import { localeInfo, type Locale } from "@/i18n/locales";
import { AlertCard } from "../AlertCard";
import { Icon, IconTile, type IconName, type TileColor } from "../Icon";
import { Mark } from "../Mark";
import { SplitText } from "../motion/SplitText";
import { stagger } from "../motion/stagger";
import { Notification } from "../Notification";
import { ProfileList, PublicIpCard } from "../Popover";
import { RichText } from "../RichText";

type FeatureKey = keyof Dictionary["features"]["items"];

const office = demoProfiles[0];
const notices = appNotifications(office);
const reconnecting = popoverHeader("reconnecting", office);

// Tile order, glyph and system color; the grid in globals.css gives each position its size.
const tiles: { key: FeatureKey; icon: IconName; color: TileColor }[] = [
  { key: "menuBar", icon: "switch", color: "blue" },
  { key: "reconnect", icon: "reconnect", color: "green" },
  { key: "errors", icon: "alert", color: "red" },
  { key: "killSwitch", icon: "shield", color: "indigo" },
  { key: "network", icon: "network", color: "orange" },
  { key: "keychain", icon: "lock", color: "gray" },
];

// A piece of the real app in each tile, on a small wallpaper tinted with the tile's color.
const visuals: Record<FeatureKey, ReactNode> = {
  menuBar: (
    <div className="mini-popover">
      <div className="mini-popover__bar">
        <Mark style="solid" size={14} />
      </div>
      <PublicIpCard open />
      <ProfileList phase="connected" active="office" />
    </div>
  ),
  reconnect: (
    <div className="timeline">
      <Notification {...notices.lost} className="timeline__event" />
      <p className="timeline__status">
        <span className="status-dot status-dot--warning is-pulsing" />
        {reconnecting.status}
      </p>
      <Notification {...notices.back} className="timeline__event" />
    </div>
  ),
  errors: (
    <div className="deck">
      <span className="deck__card deck__card--back2" />
      <span className="deck__card deck__card--back1" />
      <AlertCard alert={appAlert("noResponse")} className="deck__front" />
    </div>
  ),
  killSwitch: (
    <div className="setting-row">
      <span className="setting-row__text">
        <span className="setting-row__title">{popoverLabels.killSwitch}</span>
        <span className="setting-row__detail">{popoverLabels.killSwitchDetail}</span>
      </span>
      <span className="switch is-on">
        <span className="switch__knob" />
      </span>
    </div>
  ),
  network: (
    <div className="waiting">
      <Icon name="wifiExclamation" size={44} className="waiting__glyph" />
      <p className="waiting__status">
        <span className="status-dot status-dot--warning is-pulsing" />
        {appAlert("offline").title}
      </p>
      <p className="waiting__detail">{office.title}</p>
    </div>
  ),
  keychain: (
    <div className="keychain">
      <Icon name="lock" size={40} className="keychain__lock" />
      <ul className="keychain__items">
        {demoKeychainItems.map((item) => (
          <li key={item}>
            <Icon name="key" size={18} className="keychain__key" />
            <span className="keychain__name">{item}</span>
            <span className="keychain__where">{site.name}</span>
          </li>
        ))}
      </ul>
    </div>
  ),
};

/**
 * Features as a bento grid; every tile has an icon, a plain-language line and a
 * visual. With motion, a tile below the fold rises in when it scrolls into view
 * and its piece of the app plays once (globals.css, "Feature tile demos").
 */
export function Features({ locale, dict }: { locale: Locale; dict: Dictionary }) {
  const t = dict.features;
  return (
    <section id="features" className="section section--alt" aria-labelledby="features-title">
      <div className="container">
        <div className="section__head section__head--split" data-reveal>
          <div>
            <p className="eyebrow eyebrow--blue" data-item style={stagger(0)}>
              {t.eyebrow}
            </p>
            <h2 id="features-title" className="section__title split">
              <SplitText text={t.title} byWord={localeInfo[locale].splitWords} start={1} />
            </h2>
          </div>
          <p className="section__lead" data-item style={stagger(4)}>
            {t.lead}
          </p>
        </div>
        {/* Each tile reveals on its own as it scrolls in, then plays its demo once. */}
        <ul className="bento">
          {tiles.map(({ key, icon, color }, i) => {
            const feature = t.items[key];
            return (
              <li key={key} className={`tile tile--${key} tile--${color}`} data-reveal style={stagger(i % 3)}>
                <div className="tile__text">
                  <IconTile name={icon} color={color} />
                  <h3 className="tile__title">{feature.title}</h3>
                  <p className="tile__body">{feature.body}</p>
                  <p className="tile__detail">
                    <RichText text={feature.detail} />
                  </p>
                </div>
                <div className="tile__visual" aria-hidden="true">
                  {visuals[key]}
                </div>
              </li>
            );
          })}
        </ul>
      </div>
    </section>
  );
}
