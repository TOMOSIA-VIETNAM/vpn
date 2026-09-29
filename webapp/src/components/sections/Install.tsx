import { Fragment } from "react";
import { site } from "@/config/site";
import type { Dictionary } from "@/i18n/dictionary";
import { CopyButton } from "../CopyButton";
import { Icon, IconTile, type IconName } from "../Icon";
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

// Glyph for each of dict.install.steps: download, drag to Applications, open.
const stepIcons: IconName[] = ["download", "folder", "window"];

/** Three numbered install steps, and the uninstall command folded away. */
export function Install({ dict }: { dict: Dictionary }) {
  const t = dict.install;
  const dmgLink = <a href={site.downloadUrl}>{site.dmgFileName}</a>;
  return (
    <section id="install" className="section section--alt" aria-labelledby="install-title">
      <div className="container">
        <div className="section__head section__head--center" data-reveal>
          <p className="eyebrow eyebrow--blue" data-item style={stagger(0)}>
            {t.eyebrow}
          </p>
          <h2 id="install-title" className="section__title" data-item style={stagger(1)}>
            {t.title}
          </h2>
        </div>
        <ol className="install" data-reveal>
          {t.steps.map((step, i) => (
            <li key={step.title} className="install__step" data-item style={stagger(i)}>
              <span className="install__number" aria-hidden="true">
                {i + 1}
              </span>
              <IconTile name={stepIcons[i]} color="blue" size={44} />
              <h3 className="install__title">{step.title}</h3>
              <p className="install__body">
                <RichText text={step.body} tokens={{ dmg: dmgLink }} />
              </p>
            </li>
          ))}
        </ol>

        <details id="uninstall" className="uninstall">
          <summary className="uninstall__summary">
            <Icon name="chevron" size={16} className="uninstall__chevron" />
            {t.uninstall.summary}
          </summary>
          <div className="uninstall__body">
            <p>{t.uninstall.body}</p>
            <div className="command">
              <pre className="command__code">
                <code>{withSlashBreaks(site.uninstallCommand)}</code>
              </pre>
              <CopyButton text={site.uninstallCommand} label={t.uninstall.copy} copiedLabel={t.uninstall.copied} />
            </div>
          </div>
        </details>
      </div>
    </section>
  );
}
