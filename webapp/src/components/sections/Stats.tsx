import { appAlerts } from "@/config/app-ui";
import { site } from "@/config/site";
import { format } from "@/i18n";
import type { Dictionary } from "@/i18n/dictionary";
import { CountUp } from "../CountUp";
import { stagger } from "../motion/stagger";

// Menu bar switches needed to turn the VPN on, and clicks needed to reconnect
// after a drop: facts of the app's design, not settings.
const SWITCHES = 1;
const RECONNECT_CLICKS = 0;

/** Four facts in large type right under the hero. */
export function Stats({ dict }: { dict: Dictionary }) {
  const t = dict.stats;
  const items = [
    { key: "switch", tone: "blue", value: <CountUp value={SWITCHES} />, label: t.switchLabel },
    { key: "errors", tone: "red", value: <CountUp value={appAlerts.length} />, label: t.errorsLabel },
    { key: "reconnect", tone: "green", value: <CountUp value={RECONNECT_CLICKS} />, label: t.reconnectLabel },
    {
      key: "mac",
      tone: "indigo",
      value: <span className="stats__word">{t.macValue}</span>,
      label: format(t.macLabel, { version: site.minMacOS }),
    },
  ] as const;

  return (
    <section className="stats" aria-labelledby="stats-title">
      <h2 id="stats-title" className="visually-hidden">
        {t.title}
      </h2>
      <ul className="container stats__list" data-reveal>
        {items.map((item, i) => (
          <li key={item.key} className={`stats__item stats__item--${item.tone}`} data-item style={stagger(i)}>
            <p className="stats__value">{item.value}</p>
            <p className="stats__label">{item.label}</p>
          </li>
        ))}
      </ul>
    </section>
  );
}
