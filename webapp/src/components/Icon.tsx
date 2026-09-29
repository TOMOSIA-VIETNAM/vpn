import type { ReactNode } from "react";

// Line glyphs on a 24 × 24 grid, drawn in currentColor. They follow the SF Symbols
// the app uses where one exists (lock.fill, network.slash, wifi.exclamationmark).

const paths = {
  switch: (
    <>
      <rect x="2.5" y="6.5" width="19" height="11" rx="5.5" />
      <circle cx="16" cy="12" r="3.2" fill="currentColor" stroke="none" />
    </>
  ),
  reconnect: (
    <>
      <path d="M19.5 12a7.5 7.5 0 0 1-13.4 4.6" />
      <path d="M4.5 12a7.5 7.5 0 0 1 13.4-4.6" />
      <path d="M18.4 3.8v4h-4" />
      <path d="M5.6 20.2v-4h4" />
    </>
  ),
  network: (
    <>
      <path d="M2.8 9.2a13.5 13.5 0 0 1 18.4 0" />
      <path d="M5.9 12.6a9 9 0 0 1 12.2 0" />
      <path d="M9 15.9a4.6 4.6 0 0 1 6 0" />
      <circle cx="12" cy="19.2" r="1.3" fill="currentColor" stroke="none" />
    </>
  ),
  alert: (
    <>
      <path d="M12 3.8 21 19.6H3z" />
      <path d="M12 9.6v4.6" />
      <circle cx="12" cy="17" r="1.1" fill="currentColor" stroke="none" />
    </>
  ),
  lock: (
    <>
      <rect x="5" y="10.5" width="14" height="10" rx="2.5" />
      <path d="M8.2 10.5V8a3.8 3.8 0 0 1 7.6 0v2.5" />
    </>
  ),
  shield: (
    <>
      <path d="M12 3.2 19.2 6v5.6c0 4.4-3 7.7-7.2 9.2-4.2-1.5-7.2-4.8-7.2-9.2V6z" />
      <path d="M9 12h6" />
    </>
  ),
  networkSlash: (
    <>
      <circle cx="12" cy="12" r="8.5" />
      <path d="M3.5 12h17M12 3.5c-2.4 2.3-3.5 5.2-3.5 8.5s1.1 6.2 3.5 8.5M12 3.5c2.4 2.3 3.5 5.2 3.5 8.5s-1.1 6.2-3.5 8.5" />
      <path d="M4.5 4.5l15 15" />
    </>
  ),
  wifiExclamation: (
    <>
      <path d="M2.8 9.2a13.5 13.5 0 0 1 12.6-3" />
      <path d="M5.9 12.6a9 9 0 0 1 8.2-2.4" />
      <path d="M9 15.9a4.6 4.6 0 0 1 4-1" />
      <path d="M19 9v5.5" />
      <circle cx="19" cy="18.2" r="1.2" fill="currentColor" stroke="none" />
    </>
  ),
  key: (
    <>
      <circle cx="8" cy="12" r="4" />
      <path d="M12 12h9M18 12v3M21 12v2.4" />
    </>
  ),
  download: (
    <>
      <path d="M12 3.8v11" />
      <path d="M7.5 10.5 12 15l4.5-4.5" />
      <path d="M4.5 19.5h15" />
    </>
  ),
  folder: (
    <>
      <path d="M3.5 7.5a2 2 0 0 1 2-2h4l2 2h7a2 2 0 0 1 2 2v8.5a2 2 0 0 1-2 2h-13a2 2 0 0 1-2-2z" />
      <path d="M12 10.5v5.5M9.6 13.8 12 16.2l2.4-2.4" />
    </>
  ),
  window: (
    <>
      <rect x="3" y="4.5" width="18" height="15" rx="3" />
      <path d="M3 9h18" />
      <circle cx="6" cy="6.8" r=".6" fill="currentColor" />
      <circle cx="8.2" cy="6.8" r=".6" fill="currentColor" />
    </>
  ),
  chevron: <path d="M9.5 6l6 6-6 6" />,
} satisfies Record<string, ReactNode>;

export type IconName = keyof typeof paths;

/** A decorative glyph; the text next to it carries the meaning. */
export function Icon({ name, size = 24, className }: { name: IconName; size?: number; className?: string }) {
  return (
    <svg
      viewBox="0 0 24 24"
      width={size}
      height={size}
      className={className}
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
    >
      {paths[name]}
    </svg>
  );
}

/** System colors of the rounded icon tiles, as in System Settings. */
export type TileColor = "blue" | "green" | "orange" | "red" | "gray" | "indigo";

/** A glyph on a rounded square of a system color. */
export function IconTile({ name, color, size = 40 }: { name: IconName; color: TileColor; size?: number }) {
  return (
    <span className={`icon-tile icon-tile--${color}`} style={{ width: size, height: size }}>
      <Icon name={name} size={Math.round(size * 0.58)} />
    </span>
  );
}
