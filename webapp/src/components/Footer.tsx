import { site } from "@/config/site";
import type { Dictionary } from "@/i18n/dictionary";
import type { Locale } from "@/i18n/locales";
import { LanguageMenu } from "./LanguageMenu";

export function Footer({ locale, dict }: { locale: Locale; dict: Dictionary }) {
  return (
    <footer className="footer">
      <div className="container footer__inner">
        <div className="footer__brand">
          <p className="footer__name">
            {/* eslint-disable-next-line @next/next/no-img-element */}
            <img src="/logo.png" width={28} height={28} alt="" />
            {site.name}
          </p>
          <p className="footer__tagline">{dict.footer.tagline}</p>
        </div>
        <nav className="footer__links" aria-label={site.name}>
          <a href={site.repoUrl}>{dict.footer.github}</a>
          <a href={site.releasesUrl}>{dict.footer.releases}</a>
          <a href={site.contributingUrl}>{dict.footer.developers}</a>
        </nav>
        <LanguageMenu locale={locale} label={dict.header.languageLabel} direction="up" />
        <p className="footer__copyright">
          © {new Date().getFullYear()} {site.company}
        </p>
      </div>
    </footer>
  );
}
