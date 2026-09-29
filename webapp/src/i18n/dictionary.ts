// Shape of one locale's copy. Every dictionary in dictionaries/ must satisfy it,
// so a missing or extra key in any locale fails the type check.
//
// Inline markup in strings: **text** renders bold; [[text]] marks a label quoted
// from the app's English UI; {dmg} renders the download link.

export interface Feature {
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
    github: string;
  };
  hero: {
    title: string;
    lead: string;
    download: string;
    /** {version} is replaced by the minimum macOS version. */
    requirements: string;
    popoverAlt: string;
  };
  problem: {
    title: string;
    /** Why the problem exists: the protocol the company VPN uses. */
    context: string;
    body: string;
    builtInLabel: string;
    builtInCaption: string;
    appLabel: string;
    appCaption: string;
  };
  features: {
    title: string;
    items: {
      menuBar: Feature;
      reconnect: Feature;
      errors: Feature;
      killSwitch: Feature;
      publicIp: Feature;
      keychain: Feature;
    };
  };
  screens: {
    title: string;
    settingsAlt: string;
    settingsCaption: string;
    newConfigurationAlt: string;
    newConfigurationCaption: string;
  };
  install: {
    title: string;
    steps: [string, string, string];
    /** {version} is replaced by the minimum macOS version. */
    requirement: string;
  };
  uninstall: {
    title: string;
    body: string;
    copy: string;
    copied: string;
  };
  footer: {
    developers: string;
    source: string;
  };
}
