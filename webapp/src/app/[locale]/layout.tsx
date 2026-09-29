import type { Metadata, Viewport } from "next";
import { notFound } from "next/navigation";
import type { ReactNode } from "react";
import { site, screenshots, siteUrl } from "@/config/site";
import { getDictionary } from "@/i18n";
import { beVietnamPro } from "@/fonts";
import { defaultLocale, isLocale, localeInfo, locales, typographyStyle } from "@/i18n/locales";
import "../globals.css";

interface LocaleParams {
  params: Promise<{ locale: string }>;
}

// Only the listed locales exist; any other first path segment is a 404.
export const dynamicParams = false;

export function generateStaticParams() {
  return locales.map((locale) => ({ locale }));
}

export const viewport: Viewport = {
  themeColor: [
    { media: "(prefers-color-scheme: light)", color: "#ffffff" },
    { media: "(prefers-color-scheme: dark)", color: "#000000" },
  ],
  colorScheme: "light dark",
};

export async function generateMetadata({ params }: LocaleParams): Promise<Metadata> {
  const { locale } = await params;
  if (!isLocale(locale)) notFound();
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

export default async function LocaleLayout({ children, params }: LocaleParams & { children: ReactNode }) {
  const { locale } = await params;
  if (!isLocale(locale)) notFound();
  return (
    <html lang={localeInfo[locale].htmlLang} className={beVietnamPro.variable} style={typographyStyle(locale)}>
      <body>{children}</body>
    </html>
  );
}
