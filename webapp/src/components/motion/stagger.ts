import type { CSSProperties } from "react";

/** Inline style giving a reveal item its position in the stagger order. */
export function stagger(index: number): CSSProperties {
  return { "--i": index } as CSSProperties;
}
