import { useId } from "react";

// The TOMOSIA VPN mark as SVG, using the exact geometry of shieldPath() and
// letterPath() in assets/render-icons.swift: a 64 × 64 grid with y pointing down,
// round joins and caps, stroke width 4.5. The three styles match the status item
// images that script renders (menubar-idle, menubar-connected, menubar-lost).

const SHIELD = "M32 5.5 L53.5 12.5 L53.5 30 C53.5 44.2 44.6 54 32 58.8 C19.4 54 10.5 44.2 10.5 30 L10.5 12.5 Z";
const LETTER = "M22 40 L22 22.5 L32 32.5 L42 22.5 L42 40";
const STROKE = 4.5;
// Centre of the circled "!" on the error style.
const BADGE = { x: 45, y: 45 };

export type MarkStyle = "outline" | "solid" | "badged";

interface MarkProps {
  style: MarkStyle;
  size: number;
  className?: string;
  /** Accessible name; omit when the mark sits next to text that already names it. */
  title?: string;
}

export function Mark({ style, size, className, title }: MarkProps) {
  const maskId = useId();
  const stroke = {
    fill: "none",
    stroke: "currentColor",
    strokeWidth: STROKE,
    strokeLinejoin: "round",
    strokeLinecap: "round",
  } as const;

  return (
    <svg
      viewBox="0 0 64 64"
      width={size}
      height={size}
      className={className}
      role={title ? "img" : undefined}
      aria-label={title}
      aria-hidden={title ? undefined : true}
      focusable="false"
    >
      {style === "outline" && (
        <>
          <path d={SHIELD} {...stroke} />
          <path d={LETTER} {...stroke} />
        </>
      )}

      {style === "solid" && (
        <>
          <mask id={maskId} maskUnits="userSpaceOnUse" x="0" y="0" width="64" height="64">
            <rect width="64" height="64" fill="white" />
            <path d={LETTER} {...stroke} stroke="black" />
          </mask>
          <g mask={`url(#${maskId})`}>
            <path d={SHIELD} {...stroke} fill="currentColor" />
          </g>
        </>
      )}

      {style === "badged" && (
        <>
          <mask id={maskId} maskUnits="userSpaceOnUse" x="0" y="0" width="64" height="64">
            <rect width="64" height="64" fill="white" />
            <circle cx={BADGE.x} cy={BADGE.y} r={20.5} fill="black" />
          </mask>
          <g mask={`url(#${maskId})`}>
            <path d={SHIELD} {...stroke} />
            <path d={LETTER} {...stroke} />
          </g>
          <circle cx={BADGE.x} cy={BADGE.y} r={15.5} {...stroke} />
          <path d={`M${BADGE.x} ${BADGE.y - 8.5} L${BADGE.x} ${BADGE.y + 2}`} {...stroke} />
          <circle cx={BADGE.x} cy={BADGE.y + 8.2} r={2.7} fill="currentColor" />
        </>
      )}
    </svg>
  );
}
