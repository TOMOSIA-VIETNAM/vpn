// Shape of one locale's copy. Every dictionary in dictionaries/ must satisfy it,
// so a missing or extra key in any locale fails the type check.
//
// Inline markup in strings: **text** renders bold; [[text]] marks a label quoted
// from the app's English UI; {dmg} renders the download link.
//
// Copy is written for office staff, not network engineers: say what happens to
// them, and keep protocol names in the small `detail` lines.

export interface Feature {
  title: string;
  /** One plain-language sentence. */
  body: string;
  /** Smaller secondary line for the specifics. */
  detail: string;
}

export interface Step {
  title: string;
  body: string;
}

export interface Dictionary {
  meta: {
    title: string;
    description: string;
  };
  header: {
    homeLabel: string;
    languageLabel: string;
    /** Short label of the download button in the header. */
    download: string;
  };
  hero: {
    /** Headline, first line: the problem, shown muted. */
    titleLead: string;
    /** Headline, second line: the answer, shown strong. */
    titleStrong: string;
    lead: string;
    download: string;
    /** {version} is replaced by the minimum macOS version. */
    requirements: string;
    /** Controls of the promo video in the hero. */
    video: {
      pause: string;
      play: string;
      /** Accessible names of the corner sound toggle, once sound was on. */
      soundOn: string;
      soundOff: string;
      /** Label the sound button opens to while the video plays muted. */
      turnOnSound: string;
      /** Its label under reduced motion, when nothing plays yet. */
      playWithSound: string;
    };
  };
  /** Facts band under the hero; the numbers come from the config. */
  stats: {
    /** Heading for screen readers only. */
    title: string;
    switchLabel: string;
    errorsLabel: string;
    reconnectLabel: string;
    macValue: string;
    /** {version} is replaced by the minimum macOS version. */
    macLabel: string;
  };
  problem: {
    eyebrow: string;
    title: string;
    body: string;
    /** Small print naming the protocol. */
    detail: string;
    toggleLabel: string;
    builtInLabel: string;
    appLabel: string;
    builtInCaption: string;
    appCaption: string;
    /** What actually went wrong, in the order of appAlerts in config/app-ui.ts. */
    causes: [string, string, string, string];
  };
  /** Scroll story: connect, the Wi-Fi drops, it reconnects, it names an error. */
  story: {
    eyebrow: string;
    title: string;
    steps: [Step, Step, Step, Step];
  };
  features: {
    eyebrow: string;
    title: string;
    lead: string;
    items: {
      menuBar: Feature;
      reconnect: Feature;
      errors: Feature;
      network: Feature;
      killSwitch: Feature;
      keychain: Feature;
    };
  };
  screens: {
    eyebrow: string;
    title: string;
    newConfigurationAlt: string;
    newConfigurationCaption: string;
    settingsAlt: string;
    settingsCaption: string;
  };
  install: {
    eyebrow: string;
    title: string;
    /** Download, drag to Applications, open. */
    steps: [Step, Step, Step];
    uninstall: {
      summary: string;
      body: string;
      copy: string;
      copied: string;
    };
  };
  /** Closing download band. */
  cta: {
    title: string;
    body: string;
  };
  /** Floating button that scrolls back to the top of the page. */
  backToTop: string;
  footer: {
    tagline: string;
    github: string;
    releases: string;
    developers: string;
  };
}
