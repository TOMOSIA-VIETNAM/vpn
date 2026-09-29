import type { Dictionary } from "@/i18n/dictionary";
import { DownloadButton } from "../DownloadButton";
import { Mark } from "../Mark";
import { Screenshot } from "../Screenshot";

export function Hero({ dict }: { dict: Dictionary }) {
  return (
    <section className="hero" aria-labelledby="hero-title">
      <div className="container hero__inner">
        <div className="hero__text">
          <h1 id="hero-title" className="hero__title">
            {dict.hero.title}
          </h1>
          <p className="hero__lead">{dict.hero.lead}</p>
          <DownloadButton dict={dict} />
        </div>

        {/* A slice of a Mac desktop: the menu bar with the connected status item, and
            the app's popover opened under it. */}
        <div className="desk">
          <div className="desk__menubar" aria-hidden="true" />
          <div className="desk__popover">
            <span className="desk__status-item" aria-hidden="true">
              <Mark style="solid" size={16} />
            </span>
            <Screenshot shot="popover" alt={dict.hero.popoverAlt} priority />
          </div>
        </div>
      </div>
    </section>
  );
}
