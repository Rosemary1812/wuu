// Default-avatar assignment, mirrored from the desktop DefaultAvatar.tsx so
// the same participant renders the same mascot + tint on both ends. Pure
// logic only (no react-native imports) — the image require table lives in
// components/Avatar.tsx because Metro needs static require() calls.

export const DEFAULT_AVATAR_COUNT = 12;

// Named agents draw from the dressed cast (mascot-7..11: 巫师、
// 耳机程序员、侦探、读书人、学者); everything else from the plain
// expression cast (mascot-0..6).
export const DRESSED_CAST_INDICES = [7, 8, 9, 10, 11] as const;
export const PLAIN_CAST_INDICES = [0, 1, 2, 3, 4, 5, 6] as const;

/** FNV-1a 32-bit over UTF-16 code units — identical to the desktop. */
export function fnv1a(seed: string): number {
  let h = 0x811c9dc5;
  for (let i = 0; i < seed.length; i++) {
    h ^= seed.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  return h >>> 0;
}

/** Background tint index — spans all 12 hues regardless of cast. */
export function avatarTintIndex(seed: string): number {
  return fnv1a(seed) % DEFAULT_AVATAR_COUNT;
}

/** Which mascot-N.png this participant falls back to. */
export function avatarMascotIndex(seed: string, kind?: string): number {
  const cast = kind === "named" ? DRESSED_CAST_INDICES : PLAIN_CAST_INDICES;
  return cast[fnv1a(seed) % cast.length];
}

/** Stable identity seed: participant id where available, else the name. */
export function avatarSeed(id?: string, name?: string): string {
  const trimmedId = id?.trim() ?? "";
  if (trimmedId !== "") return trimmedId;
  return name?.trim() ?? "";
}
