import { site } from "@/config/site";
import type { Dictionary } from "@/i18n/dictionary";
import type { Locale } from "@/i18n/locales";
import { LanguageSwitcher } from "./LanguageSwitcher";

/** Translucent sticky bar: brand, language switcher, download. */
export function Header({ locale, dict }: { locale: Locale; dict: Dictionary }) {
  return (
    <header className="header">
      <div className="container header__inner">
        <a className="header__brand" href={`/${locale}`} aria-label={dict.header.homeLabel}>
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img src="/logo.png" width={28} height={28} alt="" />
          <span>{site.name}</span>
        </a>
        <div className="header__actions">
          <LanguageSwitcher locale={locale} label={dict.header.languageLabel} />
          <a className="button button--primary button--compact" href={site.downloadUrl}>
            {dict.header.download}
          </a>
        </div>
      </div>
    </header>
  );
}
