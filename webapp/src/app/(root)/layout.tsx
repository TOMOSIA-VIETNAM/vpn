import type { ReactNode } from "react";
import { defaultLocale } from "@/i18n/locales";

// Root layout for `/` only; every real page lives under app/[locale].
export default function RootRedirectLayout({ children }: { children: ReactNode }) {
  return (
    <html lang={defaultLocale}>
      <body>{children}</body>
    </html>
  );
}
