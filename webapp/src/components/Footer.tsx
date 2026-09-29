import { site } from "@/config/site";
import type { Dictionary } from "@/i18n/dictionary";

export function Footer({ dict }: { dict: Dictionary }) {
  return (
    <footer className="footer">
      <div className="container footer__inner">
        <p>
          © {new Date().getFullYear()} {site.company}
        </p>
        <nav className="footer__links" aria-label={site.name}>
          <a href={site.repoUrl}>{dict.footer.source}</a>
          <a href={site.contributingUrl}>{dict.footer.developers}</a>
        </nav>
      </div>
    </footer>
  );
}
