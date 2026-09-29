// Text quoted verbatim from the app (main.swift) and from macOS. The app's UI is
// English, so these strings stay English on every locale of the site.

export type StatusTone = "ok" | "warning" | "error";

export interface AppAlert {
  tone: StatusTone;
  title: string;
  message: string;
  action?: string;
}

/** Alerts the menu bar app shows for the most common failures. */
export const appAlerts: readonly AppAlert[] = [
  {
    tone: "error",
    title: "Authentication Failed",
    message: "PPP/CHAP authentication failed: wrong account name or password.",
    action: "Update Password",
  },
  {
    tone: "error",
    title: "IKE Handshake Failed",
    message: "IPsec IKE handshake failed: check the shared secret.",
    action: "Try Again",
  },
  {
    tone: "warning",
    title: "Server Not Responding",
    message:
      "The VPN server did not answer. This network may be blocking VPN traffic (UDP 500/4500), or the server is down — try another network.",
    action: "Try Again",
  },
  {
    tone: "warning",
    title: "Waiting for Network…",
    message: "No network — the VPN reconnects once this Mac is back online.",
  },
];

/** What the built-in macOS L2TP client reports for most failures. */
export const builtInAlert = {
  title: "VPN Connection",
  message:
    "The L2TP-VPN server did not respond. Try reconnecting. If the problem continues, verify your settings and contact your Administrator.",
  action: "OK",
} as const;

/** Example configurations shown in the popover demo (documentation hosts and IP). */
export const demoProfiles = [
  { id: "office", title: "Office", server: "vpn.example.com", user: "alice" },
  { id: "lab", title: "Research Lab", server: "lab.example.com", user: "alice" },
] as const;

export type DemoProfile = (typeof demoProfiles)[number];
export type DemoProfileId = DemoProfile["id"];

export const demoPublicIp = "203.0.113.24";

/**
 * Keychain entries the CLI creates for the Office configuration: one for the
 * shared secret, one for the account password (internal/keychain).
 */
export const demoKeychainItems = ["vpn.psk.office", "vpn.pwd.office.alice"] as const;

/** Connection phases of the popover, as the app names them. */
export type LinkPhase = "idle" | "connecting" | "reconnecting" | "connected" | "disconnecting";

/** Header status word, the hint after it, and its tone, for each phase. */
export function popoverHeader(phase: LinkPhase, profile: DemoProfile | null) {
  switch (phase) {
    case "idle":
      return { status: "Not Connected", detail: "Your traffic is not protected", tone: "idle" } as const;
    case "connecting":
      return { status: "Connecting…", detail: profile?.title ?? "Establishing the tunnel", tone: "warning" } as const;
    case "reconnecting":
      return { status: "Reconnecting…", detail: profile?.title ?? "Establishing the tunnel", tone: "warning" } as const;
    case "connected":
      return {
        status: "Connected",
        detail: profile ? `Protected via ${profile.title}` : "Your traffic is protected",
        tone: "ok",
      } as const;
    case "disconnecting":
      return { status: "Disconnecting…", detail: "Restoring your network settings", tone: "warning" } as const;
  }
}

/** The second line of a configuration row. */
export function profileSubtitle(profile: DemoProfile, phase: LinkPhase | null) {
  if (phase === "connecting") return `Connecting to ${profile.server}…`;
  if (phase === "reconnecting") return `Reconnecting to ${profile.server}…`;
  return `${profile.server} · ${profile.user}`;
}

export const popoverLabels = {
  publicIp: "Public IP",
  configurations: "Configurations",
  killSwitch: "Kill switch",
  killSwitchDetail: "If a full-tunnel VPN drops, block internet traffic until it reconnects instead of letting it leak.",
} as const;

/** The notifications the app posts when the tunnel drops (kill switch on) and when it is back. */
export function appNotifications(profile: DemoProfile) {
  return {
    lost: {
      title: "VPN connection lost",
      body: "Connection lost — reconnecting. Internet is blocked (kill switch) until it is back.",
    },
    back: { title: "VPN reconnected", body: `Protected via ${profile.title} again.` },
  } as const;
}

export type NotificationKind = keyof ReturnType<typeof appNotifications>;
