import type { Dictionary } from "../dictionary";

const en: Dictionary = {
  meta: {
    title: "TOMOSIA VPN — the company VPN that reconnects by itself",
    description:
      "A menu bar app for TOMOSIA staff on Mac. Turn the company VPN on with one switch; when Wi-Fi drops the connection, it reconnects by itself and tells you in plain words what is wrong.",
  },
  header: {
    homeLabel: "TOMOSIA VPN home",
    languageLabel: "Language",
    download: "Download",
  },
  hero: {
    titleLead: "Wi-Fi drops out.",
    titleStrong: "Your VPN comes right back.",
    lead: "TOMOSIA VPN puts the company VPN behind one switch in your Mac’s menu bar. When the connection drops, it reconnects by itself. When something is wrong, it tells you what to fix.",
    download: "Download for Mac",
    requirements: "macOS {version}+ · Apple Silicon & Intel",
    video: {
      pause: "Pause video",
      play: "Play video",
      soundOn: "Watch with sound",
      soundOff: "Mute",
      turnOnSound: "Turn on sound",
      playWithSound: "Play with sound",
      about: "{seconds}-second video · with narration",
    },
  },
  stats: {
    title: "TOMOSIA VPN at a glance",
    switchLabel: "switch in the menu bar turns the VPN on and off",
    errorsLabel: "common problems named precisely, each with its fix",
    reconnectLabel: "clicks needed to reconnect after the Wi-Fi drops",
    macValue: "Apple Silicon + Intel",
    macLabel: "Every Mac on macOS {version} or later",
  },
  problem: {
    eyebrow: "Why it exists",
    title: "The built-in VPN drops out and won’t say why.",
    body: "The company VPN uses an older kind of connection that the VPN built into macOS handles badly. On office and public Wi-Fi it cuts out, then shows the same vague message whether your password is wrong, the network blocks it or you are offline.",
    detail: "Technical note: the company VPN is L2TP/IPsec with a shared secret.",
    toggleLabel: "Compare the messages",
    builtInLabel: "Built-in VPN",
    appLabel: "TOMOSIA VPN",
    builtInCaption: "Four different problems, one vague message.",
    appCaption: "Each problem named, with the button that fixes it.",
    causes: ["Wrong password", "Wrong shared secret", "Network blocks the VPN", "No internet"],
  },
  story: {
    eyebrow: "How it works",
    title: "You work. It keeps you connected.",
    steps: [
      {
        title: "Turn it on",
        body: "Click the shield in the menu bar and flip the switch for Office. That is all the setup your day needs.",
      },
      {
        title: "The Wi-Fi drops",
        body: "The office Wi-Fi hiccups, or you move to a café. A notification tells you the connection is lost.",
      },
      {
        title: "It reconnects by itself",
        body: "TOMOSIA VPN keeps trying until the connection is back, then lets you know. Nothing to click.",
      },
      {
        title: "It tells you what’s wrong",
        body: "If it cannot connect, it says why, for example a wrong password, and puts the fix on the button.",
      },
    ],
  },
  features: {
    eyebrow: "What you get",
    title: "Built for Wi-Fi that drops.",
    lead: "Small things that save you a call to IT.",
    items: {
      menuBar: {
        title: "One switch for every connection",
        body: "Keep Office, a lab or a client site in one list and turn each on with its switch.",
        detail: "While connected, the menu shows the public IP your traffic uses.",
      },
      reconnect: {
        title: "Comes back on its own",
        body: "After a drop it keeps retrying and tells you when you are connected again.",
        detail: "macOS notifications when the connection is lost and when it returns.",
      },
      errors: {
        title: "Errors in plain words",
        body: "Wrong password, wrong shared secret, a blocked network or no internet: each has its own message and its own fix.",
        detail: "Buttons like [[Update Password]] and [[Try Again]] take you to the next step.",
      },
      network: {
        title: "Waits out a lost network",
        body: "No Wi-Fi? It waits, and reconnects as soon as your Mac is back online.",
        detail: "Shows [[Waiting for Network…]] instead of failing.",
      },
      killSwitch: {
        title: "Nothing slips out",
        body: "If the VPN drops, internet pauses until it reconnects, so nothing leaves unprotected.",
        detail: "Kill switch, for configurations that send all traffic over the VPN.",
      },
      keychain: {
        title: "Passwords stay locked away",
        body: "Your password and shared secret are kept in the macOS Keychain, never in a file.",
        detail: "One Keychain item per secret, removed again when you uninstall.",
      },
    },
  },
  screens: {
    eyebrow: "Setting up",
    title: "Set it up once, with what IT gives you.",
    newConfigurationAlt:
      "The New Configuration form: display name, server address, account name, password, shared secret and Send all traffic over VPN.",
    newConfigurationCaption: "Type in the server, account and shared secret from your administrator. That’s it.",
    settingsAlt: "The Settings window in Dark mode, with connection options, Kill switch and Notifications.",
    settingsCaption: "Choose whether internet pauses when the VPN drops, and which notifications you get.",
  },
  install: {
    eyebrow: "Install",
    title: "Ready in three steps.",
    steps: [
      { title: "Download", body: "Get {dmg}." },
      { title: "Drag to Applications", body: "Open the file and drag **TOMOSIA VPN** into **Applications**." },
      { title: "Open it", body: "Launch the app. If macOS blocks it, right-click the app and choose **Open**." },
    ],
    uninstall: {
      summary: "For uninstalling",
      body: "Run this in Terminal as your normal user. It removes the app, its command-line tool, its settings and the passwords it saved in the Keychain.",
      copy: "Copy",
      copied: "Copied",
    },
  },
  cta: {
    title: "Stay connected, wherever you work.",
    body: "Download it, flip the switch and get back to work.",
  },
  footer: {
    tagline: "The company VPN, one switch away.",
    github: "GitHub",
    releases: "All releases",
    developers: "For developers",
  },
};

export default en;
