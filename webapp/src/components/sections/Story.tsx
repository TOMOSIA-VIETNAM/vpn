import { appAlert } from "@/config/app-ui";
import type { Dictionary } from "@/i18n/dictionary";
import { localeInfo, type Locale } from "@/i18n/locales";
import type { DeskState } from "../Desk";
import { SplitText } from "../motion/SplitText";
import { stagger } from "../motion/stagger";
import { StoryScroller } from "../StoryScroller";

// What the popover shows at each step of dict.story.steps: connected, the drop
// (reconnecting, "connection lost" banner), back ("reconnected" banner), and a
// failure with its alert card (Authentication Failed, Update Password).
const states: DeskState[] = [
  { phase: "connected", active: "office" },
  { phase: "reconnecting", active: "office", notice: "lost" },
  { phase: "connected", active: "office", notice: "back" },
  { phase: "failed", active: null, alert: appAlert("authFailed") },
];

/** Scroll story: a day with the app, step by step. */
export function Story({ locale, dict }: { locale: Locale; dict: Dictionary }) {
  const t = dict.story;
  return (
    <section id="how" className="section story" aria-labelledby="how-title">
      <div className="container">
        <div className="section__head" data-reveal>
          <p className="eyebrow eyebrow--green" data-item style={stagger(0)}>
            {t.eyebrow}
          </p>
          <h2 id="how-title" className="section__title split">
            <SplitText text={t.title} byWord={localeInfo[locale].splitWords} start={1} />
          </h2>
        </div>
        <StoryScroller steps={t.steps} states={states} year={new Date().getFullYear()} />
      </div>
    </section>
  );
}
