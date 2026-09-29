import { site } from "@/config/site";
import { format } from "@/i18n";
import type { Dictionary } from "@/i18n/dictionary";

/** Primary download link with the system requirements underneath. */
export function DownloadButton({ dict }: { dict: Dictionary }) {
  return (
    <div className="download">
      <a className="button button--primary" href={site.downloadUrl}>
        {dict.hero.download}
      </a>
      <p className="download__requirements">{format(dict.hero.requirements, { version: site.minMacOS })}</p>
    </div>
  );
}
