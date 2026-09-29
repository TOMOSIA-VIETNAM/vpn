import type { Metadata } from "next";
import { site, siteUrl } from "@/config/site";
import { defaultLocale } from "@/i18n/locales";

// `/` has no content of its own: a static host cannot redirect by itself, so this
// page sends every visitor to the default locale with a meta refresh, plus a link
// for clients that ignore it.
const target = `/${defaultLocale}`;

export const metadata: Metadata = {
  metadataBase: siteUrl,
  title: site.name,
  robots: { index: false },
  alternates: { canonical: target },
};

export default function RootRedirect() {
  return (
    <>
      <meta httpEquiv="refresh" content={`0; url=${target}`} />
      <p>
        <a href={target}>{site.name}</a>
      </p>
    </>
  );
}
