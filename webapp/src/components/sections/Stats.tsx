import type { ReactNode } from "react";
import { appAlerts } from "@/config/app-ui";
import { site } from "@/config/site";
import { format } from "@/i18n";
import type { Dictionary } from "@/i18n/dictionary";
import { IconTile, type IconName, type TileColor } from "../Icon";
import { stagger } from "../motion/stagger";
import { Odometer } from "../Odometer";

// Menu bar switches needed to turn the VPN on, and clicks needed to reconnect
// after a drop: facts of the app's design, not settings.
const SWITCHES = 1;
const RECONNECT_CLICKS = 0;

interface Stat {
  key: string;
  icon: IconName;
  color: TileColor;
  value: ReactNode;
  label: string;
}

/**
 * Four facts on a frosted panel over the hero's wallpaper. Numbers and words are
 * in the text color; the small icon tile and the accent line are the only color.
 * Revealed on scroll: wheels roll up, lines draw, labels rise, one stat after another.
 */
export function Stats({ dict }: { dict: Dictionary }) {
  const t = dict.stats;
  const items: Stat[] = [
    { key: "switch", icon: "switch", color: "blue", value: <Odometer value={SWITCHES} />, label: t.switchLabel },
    { key: "errors", icon: "alert", color: "red", value: <Odometer value={appAlerts.length} />, label: t.errorsLabel },
    {
      key: "reconnect",
      icon: "reconnect",
      color: "green",
      value: <Odometer value={RECONNECT_CLICKS} />,
      label: t.reconnectLabel,
    },
    {
      key: "mac",
      icon: "chip",
      color: "indigo",
      value: <span className="stats__word">{t.macValue}</span>,
      label: format(t.macLabel, { version: site.minMacOS }),
    },
  ];

  return (
    <section className="stats" aria-labelledby="stats-title">
      <h2 id="stats-title" className="visually-hidden">
        {t.title}
      </h2>
      <div className="container">
        <ul className="stats__panel" data-reveal>
          {items.map((item, i) => (
            <li key={item.key} className={`stats__item stats__item--${item.color}`} style={stagger(i)}>
              <IconTile name={item.icon} color={item.color} size={28} />
              <p className="stats__value">{item.value}</p>
              <span className="stats__line" aria-hidden="true" />
              <p className="stats__label" data-item style={stagger(i * 2 + 2)}>
                {item.label}
              </p>
            </li>
          ))}
        </ul>
      </div>
    </section>
  );
}
