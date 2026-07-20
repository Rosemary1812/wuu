import type { CSSProperties, JSX } from "react";

/*
 * Default participant avatars.
 *
 * When a participant has no uploaded avatar_image (or the image was too
 * large to embed in a summary), surfaces fall back to a fixed cast of 12
 * mascot characters — die-cut poses of the wuu flame (wizard, detective,
 * headphones, scholar, …) on the existing 12 muted background tints — so
 * a roster of agents reads as clearly different identities even without
 * uploads. Identity comes from silhouette + tint together. Tint values
 * live in styles/default-avatar.css (light) and theme.css (dark).
 *
 * Assignment is deterministic from a stable seed (the participant id):
 * the same participant always gets the same character, no new backend
 * field required. Collisions only appear past 12 distinct seeds, which
 * is fine for a fallback.
 */

import mascot0 from "./assets/avatars/mascot-0.png";
import mascot1 from "./assets/avatars/mascot-1.png";
import mascot2 from "./assets/avatars/mascot-2.png";
import mascot3 from "./assets/avatars/mascot-3.png";
import mascot4 from "./assets/avatars/mascot-4.png";
import mascot5 from "./assets/avatars/mascot-5.png";
import mascot6 from "./assets/avatars/mascot-6.png";
import mascot7 from "./assets/avatars/mascot-7.png";
import mascot8 from "./assets/avatars/mascot-8.png";
import mascot9 from "./assets/avatars/mascot-9.png";
import mascot10 from "./assets/avatars/mascot-10.png";
import mascot11 from "./assets/avatars/mascot-11.png";

// Two casts. Named agents are long-lived identities, so they
// draw from the dressed characters — an outfit reads as "someone" (巫师、
// 耳机程序员、侦探、读书人、学者). Everything else (ephemeral task
// workers, unknown seeds) draws from the plain expression cast.
const DRESSED_CAST: string[] = [mascot7, mascot8, mascot9, mascot10, mascot11];
const PLAIN_CAST: string[] = [
  mascot0,
  mascot1,
  mascot2,
  mascot3,
  mascot4,
  mascot5,
  mascot6,
];

export const DEFAULT_AVATAR_COUNT = 12;

// FNV-1a over the seed. Stable across sessions and machines for a
// given id.
function fnv1a(seed: string): number {
  let h = 0x811c9dc5;
  for (let i = 0; i < seed.length; i++) {
    h ^= seed.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  return h >>> 0;
}

// Folded into [0, DEFAULT_AVATAR_COUNT) — used for the background tint,
// which spans all 12 hues regardless of cast.
export function defaultAvatarIndex(seed: string): number {
  return fnv1a(seed) % DEFAULT_AVATAR_COUNT;
}

/**
 * Renders the default mascot avatar for a participant, sized to fill
 * its container (the caller's circular avatar cell clips it). `seed`
 * should be a stable identity string — the participant id where
 * available, otherwise the display name. Pass the participant `kind`
 * where known: "named" picks from the dressed cast.
 */
export function DefaultAvatarMark({
  seed,
  kind,
}: {
  seed: string;
  kind?: string;
}): JSX.Element {
  const cast = kind === "named" ? DRESSED_CAST : PLAIN_CAST;
  const style = {
    background: `var(--avatar-${defaultAvatarIndex(seed)}-bg)`,
  } as CSSProperties;
  return (
    <span className="default-avatar" style={style} aria-hidden="true">
      <img src={cast[fnv1a(seed) % cast.length]} alt="" draggable={false} />
    </span>
  );
}
