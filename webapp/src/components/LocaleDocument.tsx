import type { Metadata, Viewport } from "next";
import type { ReactNode } from "react";
import { site, screenshots, siteUrl } from "@/config/site";
import { getDictionary } from "@/i18n";
import { beVietnamPro } from "@/fonts";
import { defaultLocale, localeInfo, locales, typographyStyle, type Locale } from "@/i18n/locales";
import "@/app/globals.css";

// The <html> document, metadata and viewport shared by every route that renders
// the landing page: /{locale} and `/`, which shows the default locale in place so
// the first paint is the styled page rather than a redirect stub.

export const viewport: Viewport = {
  themeColor: [
    { media: "(prefers-color-scheme: light)", color: "#ffffff" },
    { media: "(prefers-color-scheme: dark)", color: "#000000" },
  ],
  colorScheme: "light dark",
};

/** Metadata for a locale's page; the canonical URL is always /{locale}. */
export function localeMetadata(locale: Locale): Metadata {
  const { meta } = getDictionary(locale);
  const languages = Object.fromEntries(locales.map((l) => [localeInfo[l].htmlLang, `/${l}`]));
  const og = screenshots.popover;

  return {
    metadataBase: siteUrl,
    title: meta.title,
    description: meta.description,
    applicationName: site.name,
    alternates: {
      canonical: `/${locale}`,
      languages: { ...languages, "x-default": `/${defaultLocale}` },
    },
    openGraph: {
      type: "website",
      siteName: site.name,
      title: meta.title,
      description: meta.description,
      url: `/${locale}`,
      locale: localeInfo[locale].ogLocale,
      alternateLocale: locales.filter((l) => l !== locale).map((l) => localeInfo[l].ogLocale),
      images: [{ url: `/screenshots/${og.name}-light.png`, width: og.width, height: og.height }],
    },
    twitter: { card: "summary_large_image", title: meta.title, description: meta.description },
    icons: { icon: "/logo.png", apple: "/logo.png" },
  };
}

export function LocaleDocument({ locale, children }: { locale: Locale; children: ReactNode }) {
  return (
    <html lang={localeInfo[locale].htmlLang} className={beVietnamPro.variable} style={typographyStyle(locale)}>
      <body>{children}</body>
    </html>
  );
}
