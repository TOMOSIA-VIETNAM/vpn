import type { Dictionary } from "../dictionary";

const en: Dictionary = {
  meta: {
    title: "TOMOSIA VPN — a Mac VPN that stays connected",
    description:
      "Menu bar L2TP/IPsec VPN client for TOMOSIA staff. It reconnects after drops on office and public Wi-Fi and tells you exactly what went wrong.",
  },
  header: {
    homeLabel: "TOMOSIA VPN home",
    languageLabel: "Language",
    github: "GitHub",
  },
  hero: {
    title: "A VPN that stays connected.",
    lead: "TOMOSIA VPN replaces the L2TP VPN built into macOS with one switch in the menu bar. When office or public Wi-Fi drops the tunnel, it reconnects on its own. When something is wrong, it says what to fix.",
    download: "Download for Mac",
    requirements: "macOS {version}+ · Apple Silicon & Intel",
    popoverAlt:
      "The TOMOSIA VPN menu: connected to Office, public IP 203.0.113.24, and a second configuration, Research Lab, switched off.",
    video: {
      pause: "Pause video",
      play: "Play video",
      soundOn: "Watch with sound",
      soundOff: "Mute",
    },
  },
  problem: {
    title: "The built-in VPN gives up without saying why.",
    context:
      "The company VPN runs L2TP/IPsec with a shared secret, an older protocol. The VPN built into macOS handles it unreliably, and many other VPN apps need their own certificates or protocol instead.",
    body: "In System Settings → VPN, the connection drops behind office and public Wi-Fi firewalls, then shows the same message whatever went wrong: a wrong password, a blocked port or no network at all.",
    builtInLabel: "macOS built-in VPN",
    builtInCaption: "One message for every failure.",
    appLabel: "TOMOSIA VPN",
    appCaption: "A specific alert for each cause, with the next step on the button.",
  },
  features: {
    title: "Made for networks that drop.",
    items: {
      menuBar: {
        title: "One switch in the menu bar",
        body: "Turn the VPN on and off from the menu bar. Keep several configurations, such as Office and a lab, and switch between them.",
      },
      reconnect: {
        title: "Reconnects on its own",
        body: "After a drop it keeps retrying until the tunnel is back. A macOS notification tells you when the connection is lost and when it returns.",
      },
      errors: {
        title: "Errors that say what to do",
        body: "A wrong password, a wrong shared secret, a network that blocks UDP 500/4500 or no network at all: each gets its own message, with [[Try Again]] or [[Update Password]].",
      },
      killSwitch: {
        title: "Kill switch",
        body: "If a full-tunnel VPN drops, internet traffic is blocked until it reconnects, so nothing leaks outside the tunnel.",
      },
      publicIp: {
        title: "Your public IP at a glance",
        body: "While connected, the menu shows the public IP your traffic leaves from.",
      },
      keychain: {
        title: "Secrets stay in the Keychain",
        body: "Passwords and shared secrets are stored in the macOS Keychain, never in a plain file.",
      },
    },
  },
  screens: {
    title: "Setup is one form.",
    settingsAlt: "The Settings window with MTU, Verbose log, Kill switch and Notifications.",
    settingsCaption:
      "Settings: MTU, Verbose log, Kill switch and Notifications. Changes apply on the next connection.",
    newConfigurationAlt:
      "The New Configuration form with display name, server address, account name, password, shared secret and Send all traffic over VPN.",
    newConfigurationCaption:
      "A new configuration needs only the server, account and shared secret your administrator gives you.",
  },
  install: {
    title: "Install",
    steps: [
      "Download {dmg}.",
      "Open it and drag **TOMOSIA VPN** into **Applications**.",
      "Open the app (if macOS blocks it: right-click the app → **Open**).",
    ],
    requirement: "Requires macOS {version} or later.",
  },
  uninstall: {
    title: "Uninstall",
    body: "Run this in Terminal as your normal user. It removes the app, the vpn command-line tool, its settings and the secrets it saved in the Keychain.",
    copy: "Copy",
    copied: "Copied",
  },
  footer: {
    developers: "For developers",
    source: "Source on GitHub",
  },
};

export default en;
