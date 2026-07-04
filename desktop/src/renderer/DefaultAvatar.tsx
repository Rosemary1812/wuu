import type { CSSProperties, JSX } from "react";

/*
 * Default participant avatars.
 *
 * When a participant has no uploaded avatar_image (or the image was too
 * large to embed in a summary), surfaces used to fall back to the first
 * letter of the name on a flat grey disc. This module replaces that with
 * a fixed set of 12 geometric emblems: 12 distinct shapes paired with 12
 * muted hues, so a roster of agents reads as clearly different identities
 * even without uploads. Colours live in styles/default-avatar.css (light)
 * and theme.css (dark); this file only picks the shape and wires the
 * per-index colour tokens onto the SVG.
 *
 * Assignment is deterministic from a stable seed (the participant id):
 * the same participant always gets the same emblem, no new backend field
 * required. Collisions only appear past 12 distinct seeds, which is fine
 * for a fallback.
 */

export const DEFAULT_AVATAR_COUNT = 12;

// FNV-1a over the seed, folded into [0, DEFAULT_AVATAR_COUNT). Stable
// across sessions and machines for a given id.
export function defaultAvatarIndex(seed: string): number {
  let h = 0x811c9dc5;
  for (let i = 0; i < seed.length; i++) {
    h ^= seed.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  return (h >>> 0) % DEFAULT_AVATAR_COUNT;
}

// 12 motifs on a 48×48 grid. Shapes without an explicit fill inherit
// --da-fg from .default-avatar; cut-outs paint --da-bg so they read as
// holes punched in the mark. Kept simple enough to stay legible at 16px.
const MOTIFS: JSX.Element[] = [
  // 0 · disc
  <circle key="m" cx="24" cy="24" r="11" />,
  // 1 · ring
  <>
    <circle cx="24" cy="24" r="11" />
    <circle cx="24" cy="24" r="6" fill="var(--da-bg)" />
  </>,
  // 2 · half
  <path key="m" d="M12.5 25a11.5 11.5 0 0 1 23 0z" />,
  // 3 · crescent
  <>
    <circle cx="24" cy="24" r="11.5" />
    <circle cx="29.5" cy="19.5" r="9.6" fill="var(--da-bg)" />
  </>,
  // 4 · concentric
  <>
    <circle cx="24" cy="24" r="11.5" />
    <circle cx="24" cy="24" r="7.6" fill="var(--da-bg)" />
    <circle cx="24" cy="24" r="3.7" />
  </>,
  // 5 · orbit
  <>
    <ellipse
      cx="24"
      cy="24"
      rx="13"
      ry="5.4"
      fill="none"
      stroke="var(--da-fg)"
      strokeWidth="3"
      transform="rotate(-24 24 24)"
    />
    <circle cx="24" cy="24" r="5.4" />
  </>,
  // 6 · triangle
  <path key="m" d="M24 12.5 35.6 33.5H12.4z" />,
  // 7 · diamond
  <>
    <path d="M24 11 37 24 24 37 11 24z" />
    <path d="M24 17.5 30.5 24 24 30.5 17.5 24z" fill="var(--da-bg)" />
  </>,
  // 8 · chevron
  <path
    key="m"
    d="M12.5 30 24 18.5 35.5 30"
    fill="none"
    stroke="var(--da-fg)"
    strokeWidth="5"
    strokeLinecap="round"
    strokeLinejoin="round"
  />,
  // 9 · wave
  <path
    key="m"
    d="M11 26q6.5 -9.5 13 0t13 0"
    fill="none"
    stroke="var(--da-fg)"
    strokeWidth="5"
    strokeLinecap="round"
  />,
  // 10 · quatrefoil
  <>
    <circle cx="24" cy="13.5" r="5" />
    <circle cx="24" cy="34.5" r="5" />
    <circle cx="13.5" cy="24" r="5" />
    <circle cx="34.5" cy="24" r="5" />
  </>,
  // 11 · bars
  <>
    <rect x="12.5" y="15" width="23" height="4.6" rx="2.3" />
    <rect x="12.5" y="21.7" width="16" height="4.6" rx="2.3" />
    <rect x="12.5" y="28.4" width="20" height="4.6" rx="2.3" />
  </>,
];

/**
 * Renders the geometric default emblem for a participant, sized to fill
 * its container (the caller's circular avatar cell clips it). `seed`
 * should be a stable identity string — the participant id where
 * available, otherwise the display name.
 */
export function DefaultAvatarMark({ seed }: { seed: string }): JSX.Element {
  const index = defaultAvatarIndex(seed);
  const style = {
    "--da-bg": `var(--avatar-${index}-bg)`,
    "--da-fg": `var(--avatar-${index}-fg)`,
  } as CSSProperties;
  return (
    <svg
      className="default-avatar"
      viewBox="0 0 48 48"
      style={style}
      aria-hidden="true"
    >
      <rect x="0" y="0" width="48" height="48" fill="var(--da-bg)" />
      {MOTIFS[index]}
    </svg>
  );
}
