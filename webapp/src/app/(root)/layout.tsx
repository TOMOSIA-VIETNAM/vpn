import type { ReactNode } from "react";
import { LocaleDocument, localeMetadata } from "@/components/LocaleDocument";
import { defaultLocale } from "@/i18n/locales";

export { viewport } from "@/components/LocaleDocument";

// `/` renders the default locale's page in place (its canonical URL is
// /{defaultLocale}), so a first visit paints the styled page instead of a redirect.
export const metadata = localeMetadata(defaultLocale);

export default function RootLayout({ children }: { children: ReactNode }) {
  return <LocaleDocument locale={defaultLocale}>{children}</LocaleDocument>;
}
