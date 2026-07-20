// Avatar assignment must be byte-identical to desktop DefaultAvatar.tsx —
// the same participant gets the same mascot + tint on both ends. The pinned
// values below were computed from the desktop algorithm; if this test ever
// needs regenerating, the two implementations have diverged and that is the
// bug.

import { describe, expect, it } from "vitest";

import { avatarMascotIndex, avatarSeed, avatarTintIndex, fnv1a } from "../src/lib/avatar";

describe("fnv1a", () => {
  it("matches the desktop hash for pinned seeds", () => {
    expect(fnv1a("")).toBe(2166136261);
    expect(fnv1a("andy")).toBe(3183330573);
    expect(fnv1a("participant-42")).toBe(3254981055);
    expect(fnv1a("墨白")).toBe(3064423518);
    expect(fnv1a("shitou")).toBe(3280474227);
  });
});

describe("avatar assignment", () => {
  it("tint spans all 12 hues regardless of cast", () => {
    expect(avatarTintIndex("andy")).toBe(9);
    expect(avatarTintIndex("participant-42")).toBe(3);
    expect(avatarTintIndex("墨白")).toBe(6);
  });

  it("named agents draw from the dressed cast (7..11), others from plain (0..6)", () => {
    expect(avatarMascotIndex("andy", "named")).toBe(10);
    expect(avatarMascotIndex("andy")).toBe(3);
    expect(avatarMascotIndex("墨白", "named")).toBe(10);
    expect(avatarMascotIndex("墨白", "task")).toBe(2);
    expect(avatarMascotIndex("shitou", "named")).toBe(9);
  });

  it("is deterministic", () => {
    for (const seed of ["a", "b", "长长的中文名字", "participant-1"]) {
      expect(avatarMascotIndex(seed, "named")).toBe(avatarMascotIndex(seed, "named"));
      expect(avatarTintIndex(seed)).toBe(avatarTintIndex(seed));
    }
  });

  it("seeds from id first, then display name", () => {
    expect(avatarSeed("id-1", "Name")).toBe("id-1");
    expect(avatarSeed("  ", "Name")).toBe("Name");
    expect(avatarSeed(undefined, " Name ")).toBe("Name");
    expect(avatarSeed(undefined, undefined)).toBe("");
  });
});
