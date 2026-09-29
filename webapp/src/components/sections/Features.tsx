import type { ReactNode } from "react";
import { appNotifications } from "@/config/app-ui";
import type { Dictionary } from "@/i18n/dictionary";
import { Mark } from "../Mark";
import { RichText } from "../RichText";

type FeatureKey = keyof Dictionary["features"]["items"];

// Features shown with a small piece of the real app next to the text.
const details: Partial<Record<FeatureKey, ReactNode>> = {
  menuBar: (
    <span className="feature__marks" aria-hidden="true">
      <Mark style="outline" size={18} />
      <Mark style="solid" size={18} />
      <Mark style="badged" size={18} />
    </span>
  ),
  reconnect: (
    <span className="feature__notes" lang="en">
      <span className="note">
        <span className="status-dot status-dot--warning" aria-hidden="true" />
        {appNotifications.lost}
      </span>
      <span className="note">
        <span className="status-dot status-dot--ok" aria-hidden="true" />
        {appNotifications.back}
      </span>
    </span>
  ),
};

const order: FeatureKey[] = ["menuBar", "reconnect", "errors", "killSwitch", "publicIp", "keychain"];

export function Features({ dict }: { dict: Dictionary }) {
  return (
    <section id="features" className="section" aria-labelledby="features-title">
      <div className="container">
        <h2 id="features-title" className="section__title">
          {dict.features.title}
        </h2>
        <ul className="features">
          {order.map((key) => {
            const feature = dict.features.items[key];
            return (
              <li key={key} className="feature">
                <h3 className="feature__title">{feature.title}</h3>
                <p className="feature__body">
                  <RichText text={feature.body} />
                </p>
                {details[key]}
              </li>
            );
          })}
        </ul>
      </div>
    </section>
  );
}
