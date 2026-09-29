// Scroll animations are CSS transitions driven by classes that JavaScript sets
// after hydration:
//   [data-reveal].is-pending   the block starts below the fold: its [data-item]
//                              children wait hidden, just out of view
//   (class removed)            the block scrolled into view: children rise into place
// Content that is on screen when the page loads is never hidden, so the first
// paint is complete. Without JavaScript, or with prefers-reduced-motion, no class
// is ever set and everything renders in its final state.

export const PENDING_CLASS = "is-pending";
export const REDUCED_MOTION_QUERY = "(prefers-reduced-motion: reduce)";

export function motionAllowed(): boolean {
  return !window.matchMedia(REDUCED_MOTION_QUERY).matches;
}
