import { notFound } from "next/navigation";
import { LandingPage } from "@/components/LandingPage";
import { isLocale } from "@/i18n/locales";

export default async function LocalePage({ params }: { params: Promise<{ locale: string }> }) {
  const { locale } = await params;
  if (!isLocale(locale)) notFound();
  return <LandingPage locale={locale} />;
}
