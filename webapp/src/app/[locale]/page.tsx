import { notFound } from "next/navigation";
import { BackToTop } from "@/components/BackToTop";
import { Footer } from "@/components/Footer";
import { Header } from "@/components/Header";
import { Parallax } from "@/components/motion/Parallax";
import { RevealObserver } from "@/components/motion/RevealObserver";
import { Cta } from "@/components/sections/Cta";
import { Features } from "@/components/sections/Features";
import { Hero } from "@/components/sections/Hero";
import { Install } from "@/components/sections/Install";
import { Problem } from "@/components/sections/Problem";
import { Screens } from "@/components/sections/Screens";
import { Stats } from "@/components/sections/Stats";
import { Story } from "@/components/sections/Story";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/locales";

// Page sections, top to bottom. A new section is a component in
// components/sections/ added to this list, with its copy in the Dictionary type.
export default async function LandingPage({ params }: { params: Promise<{ locale: string }> }) {
  const { locale } = await params;
  if (!isLocale(locale)) notFound();
  const dict = getDictionary(locale);

  return (
    <>
      <Header locale={locale} dict={dict} />
      <main>
        {/* The wallpaper field runs behind the hero and the facts panel under it. */}
        <div className="field">
          <Hero locale={locale} dict={dict} />
          <Stats dict={dict} />
        </div>
        <Problem locale={locale} dict={dict} />
        <Story locale={locale} dict={dict} />
        <Features locale={locale} dict={dict} />
        <Screens locale={locale} dict={dict} />
        <Install dict={dict} />
        <Cta dict={dict} />
      </main>
      <Footer locale={locale} dict={dict} />
      <BackToTop label={dict.backToTop} />
      <RevealObserver />
      <Parallax />
    </>
  );
}
