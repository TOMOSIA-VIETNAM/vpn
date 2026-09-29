import { Fragment } from "react";
import { site } from "@/config/site";
import { format } from "@/i18n";
import type { Dictionary } from "@/i18n/dictionary";
import { CopyButton } from "../CopyButton";
import { DownloadButton } from "../DownloadButton";
import { stagger } from "../motion/stagger";
import { RichText } from "../RichText";

/** Lets a long URL wrap after its slashes instead of in the middle of a word. */
function withSlashBreaks(text: string) {
  return text.split("/").map((part, i, parts) => (
    <Fragment key={i}>
      {part}
      {i < parts.length - 1 && (
        <>
          /<wbr />
        </>
      )}
    </Fragment>
  ));
}

/** Install steps and the uninstall command, side by side on wide screens. */
export function Install({ dict }: { dict: Dictionary }) {
  const dmgLink = <a href={site.downloadUrl}>{site.dmgFileName}</a>;
  return (
    <div className="section">
      <div className="container setup">
        <section id="install" className="setup__install" aria-labelledby="install-title" data-reveal>
          <h2 id="install-title" className="section__title" data-item style={stagger(0)}>
            {dict.install.title}
          </h2>
          <ol className="steps">
            {dict.install.steps.map((step, i) => (
              <li key={i} className="steps__item" data-item style={stagger(i + 1)}>
                <RichText text={step} tokens={{ dmg: dmgLink }} />
              </li>
            ))}
          </ol>
          <p className="setup__note" data-item style={stagger(dict.install.steps.length + 1)}>
            {format(dict.install.requirement, { version: site.minMacOS })}
          </p>
          <div className="setup__download" data-item style={stagger(dict.install.steps.length + 2)}>
            <DownloadButton dict={dict} />
          </div>
        </section>

        <section id="uninstall" className="setup__uninstall" aria-labelledby="uninstall-title" data-reveal>
          <h2 id="uninstall-title" className="setup__subtitle" data-item style={stagger(0)}>
            {dict.uninstall.title}
          </h2>
          <p className="setup__note" data-item style={stagger(1)}>
            {dict.uninstall.body}
          </p>
          <div className="command" data-item style={stagger(2)}>
            <pre className="command__code">
              <code>{withSlashBreaks(site.uninstallCommand)}</code>
            </pre>
            <CopyButton text={site.uninstallCommand} label={dict.uninstall.copy} copiedLabel={dict.uninstall.copied} />
          </div>
        </section>
      </div>
    </div>
  );
}
