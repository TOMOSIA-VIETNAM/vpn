// Scroll and load animations are CSS transitions switched on by two classes:
//   html.motion      set before first paint, only when the visitor allows motion
//   [data-reveal].is-in   set by RevealObserver when the element scrolls into view
// Without JavaScript, or with prefers-reduced-motion, neither class is set and
// every element renders in its final, readable state.

export const MOTION_CLASS = "motion";
export const REVEALED_CLASS = "is-in";
export const REDUCED_MOTION_QUERY = "(prefers-reduced-motion: reduce)";

/** Set on <html> by RevealObserver once it runs. */
export const READY_ATTRIBUTE = "data-reveal-ready";

// If RevealObserver has not started this long after load (script error, blocked
// JS), the motion class is removed so nothing stays hidden.
const FALLBACK_MS = 3000;

/** Inline <head> script: runs before first paint, so hidden-then-revealed content never flashes. */
export const motionBootScript = `(function(){try{var d=document.documentElement;if(matchMedia(${JSON.stringify(
  REDUCED_MOTION_QUERY,
)}).matches)return;d.classList.add(${JSON.stringify(MOTION_CLASS)});setTimeout(function(){if(!d.hasAttribute(${JSON.stringify(
  READY_ATTRIBUTE,
)}))d.classList.remove(${JSON.stringify(MOTION_CLASS)})},${FALLBACK_MS})}catch(e){}})();`;

export function motionAllowed(): boolean {
  return document.documentElement.classList.contains(MOTION_CLASS);
}
