import type { Metadata } from "next";
import { notFound } from "next/navigation";
import type { ReactNode } from "react";
import { LocaleDocument, localeMetadata } from "@/components/LocaleDocument";
import { isLocale, locales } from "@/i18n/locales";

export { viewport } from "@/components/LocaleDocument";

interface LocaleParams {
  params: Promise<{ locale: string }>;
}

// Only the listed locales exist; any other first path segment is a 404.
export const dynamicParams = false;

export function generateStaticParams() {
  return locales.map((locale) => ({ locale }));
}

export async function generateMetadata({ params }: LocaleParams): Promise<Metadata> {
  const { locale } = await params;
  if (!isLocale(locale)) notFound();
  return localeMetadata(locale);
}

export default async function LocaleLayout({ children, params }: LocaleParams & { children: ReactNode }) {
  const { locale } = await params;
  if (!isLocale(locale)) notFound();
  return <LocaleDocument locale={locale}>{children}</LocaleDocument>;
}
