import type { Dictionary } from "@/i18n/dictionary";
import { localeInfo, type Locale } from "@/i18n/locales";
import { DownloadButton } from "../DownloadButton";
import { Mark } from "../Mark";
import { SplitText, splitCount } from "../motion/SplitText";
import { stagger } from "../motion/stagger";
import { PopoverDemo } from "../PopoverDemo";

export function Hero({ locale, dict }: { locale: Locale; dict: Dictionary }) {
  const byWord = localeInfo[locale].splitWords;
  // Stagger order: headline words, then the lead, the download button and the desktop.
  const afterTitle = splitCount(dict.hero.title, byWord);

  return (
    <section className="hero" aria-labelledby="hero-title" data-reveal>
      <div className="container hero__inner">
        <div className="hero__text">
          <h1 id="hero-title" className="hero__title split">
            <SplitText text={dict.hero.title} byWord={byWord} />
          </h1>
          <p className="hero__lead" data-item style={stagger(afterTitle + 1)}>
            {dict.hero.lead}
          </p>
          <div data-item style={stagger(afterTitle + 2)}>
            <DownloadButton dict={dict} />
          </div>
        </div>

        {/* A slice of a Mac desktop: the menu bar with the app's status item, and
            the popover opened under it. */}
        <div className="desk" data-item style={stagger(afterTitle + 3)}>
          <div className="desk__menubar" aria-hidden="true">
            <span className="desk__status-item">
              <Mark style="solid" size={15} />
            </span>
          </div>
          <PopoverDemo label={dict.hero.popoverAlt} year={new Date().getFullYear()} />
        </div>
      </div>
    </section>
  );
}
