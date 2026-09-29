import { notFound } from "next/navigation";
import { Footer } from "@/components/Footer";
import { Header } from "@/components/Header";
import { Features } from "@/components/sections/Features";
import { Hero } from "@/components/sections/Hero";
import { Install } from "@/components/sections/Install";
import { Problem } from "@/components/sections/Problem";
import { Screens } from "@/components/sections/Screens";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/locales";

// Page sections, top to bottom. A new section (for example an embedded video) is
// a component in components/sections/ added to this list, with its copy in the
// Dictionary type.
export default async function LandingPage({ params }: { params: Promise<{ locale: string }> }) {
  const { locale } = await params;
  if (!isLocale(locale)) notFound();
  const dict = getDictionary(locale);

  return (
    <>
      <Header locale={locale} dict={dict} />
      <main>
        <Hero dict={dict} />
        <Problem dict={dict} />
        <Features dict={dict} />
        <Screens dict={dict} />
        <Install dict={dict} />
      </main>
      <Footer dict={dict} />
    </>
  );
}
