// Mascot sticker art for the reaction vocabulary — the "custom art"
// REACTION_EMOJI was always a placeholder for (MessageMarks.ts). The
// backend only ever stores the reaction key; this map is pure
// presentation, so the vocabulary and the wire format stay untouched.
// Keys mirror REACTION_KEYS; any key without art falls back to the
// emoji glyph, so a future vocabulary addition degrades gracefully.

import eyes from "./assets/reactions/eyes.png";
import nice from "./assets/reactions/nice.png";
import shrug from "./assets/reactions/shrug.png";
import sideeye from "./assets/reactions/sideeye.png";
import smug from "./assets/reactions/smug.png";
import whoa from "./assets/reactions/whoa.png";

export const REACTION_ART: Record<string, string> = {
  eyes,
  nice,
  shrug,
  sideeye,
  smug,
  whoa,
};

export function reactionArt(key: string): string | undefined {
  return REACTION_ART[key];
}
