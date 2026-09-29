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

/** The notifications the app posts when the tunnel drops and when it is back. */
export const appNotifications = {
  lost: "VPN connection lost",
  back: "VPN reconnected",
} as const;

/** What the built-in macOS L2TP client reports for most failures. */
export const builtInAlert = {
  title: "VPN Connection",
  message:
    "The L2TP-VPN server did not respond. Try reconnecting. If the problem continues, verify your settings and contact your Administrator.",
  action: "OK",
} as const;
