import type { Dictionary } from "@/i18n/dictionary";
import { DownloadButton } from "../DownloadButton";
import { stagger } from "../motion/stagger";

/** Closing band: the app icon on the wallpaper, a last line and the download. */
export function Cta({ dict }: { dict: Dictionary }) {
  return (
    <section className="cta field" aria-labelledby="cta-title">
      <div className="container cta__inner" data-reveal>
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img src="/logo.png" width={128} height={128} alt="" className="cta__icon" data-item style={stagger(0)} />
        <h2 id="cta-title" className="cta__title" data-item style={stagger(1)}>
          {dict.cta.title}
        </h2>
        <p className="cta__body" data-item style={stagger(2)}>
          {dict.cta.body}
        </p>
        <div data-item style={stagger(3)}>
          <DownloadButton dict={dict} />
        </div>
      </div>
    </section>
  );
}
