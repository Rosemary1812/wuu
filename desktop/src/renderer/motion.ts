/* The single JS-side bridge to the motion tokens in styles/base.css.
 * CSS owns the values. JS reads them at call time, so a timeout that waits
 * for a transition always matches the stylesheet, and prefers-reduced-motion
 * (which zeroes the token ladder) collapses the JS waits with it.
 * Fallbacks only cover environments without real computed styles (jsdom). */

export function prefersReducedMotion(): boolean {
  if (typeof window === "undefined" || typeof window.matchMedia !== "function") {
    return false;
  }
  return window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}

export function motionDurationMs(token: string, fallbackMs: number): number {
  if (
    typeof window === "undefined" ||
    typeof window.getComputedStyle !== "function"
  ) {
    return fallbackMs;
  }
  const raw = window
    .getComputedStyle(document.documentElement)
    .getPropertyValue(token)
    .trim();
  if (raw === "") {
    return fallbackMs;
  }
  const value = Number.parseFloat(raw);
  if (!Number.isFinite(value)) {
    return fallbackMs;
  }
  if (raw.endsWith("ms")) {
    return value;
  }
  return raw.endsWith("s") ? value * 1000 : value;
}
