import { LandingPage } from "@/components/LandingPage";
import { defaultLocale } from "@/i18n/locales";

export default function RootPage() {
  return <LandingPage locale={defaultLocale} />;
}
