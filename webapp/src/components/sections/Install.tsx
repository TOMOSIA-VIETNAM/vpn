import { Fragment } from "react";
import { site } from "@/config/site";
import { format } from "@/i18n";
import type { Dictionary } from "@/i18n/dictionary";
import { CopyButton } from "../CopyButton";
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
        <section id="install" className="setup__install" aria-labelledby="install-title">
          <h2 id="install-title" className="section__title">
            {dict.install.title}
          </h2>
          <ol className="steps">
            {dict.install.steps.map((step, i) => (
              <li key={i} className="steps__item">
                <RichText text={step} tokens={{ dmg: dmgLink }} />
              </li>
            ))}
          </ol>
          <p className="setup__note">{format(dict.install.requirement, { version: site.minMacOS })}</p>
        </section>

        <section id="uninstall" className="setup__uninstall" aria-labelledby="uninstall-title">
          <h2 id="uninstall-title" className="setup__subtitle">
            {dict.uninstall.title}
          </h2>
          <p className="setup__note">{dict.uninstall.body}</p>
          <div className="command">
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
