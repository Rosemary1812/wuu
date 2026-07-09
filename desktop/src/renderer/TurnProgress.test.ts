/**
 * Boundary tests for `formatChineseDuration` (desktop/src/renderer/TurnProgress.tsx).
 *
 * Locks in the current top-2-units formatting contract so future refactors
 * don't quietly change what users see. The two quirks documented inside are
 * design choices that may want a second look — see comments inline.
 */

import { describe, expect, it } from "vitest";
import { formatChineseDuration } from "./TurnProgress";

describe("formatChineseDuration", () => {
  describe("sub-second input", () => {
    it("returns '0 秒' for ms below 1000", () => {
      expect(formatChineseDuration(0)).toBe("0 秒");
      expect(formatChineseDuration(1)).toBe("0 秒");
      expect(formatChineseDuration(999)).toBe("0 秒");
    });

    it("rounds down to whole seconds", () => {
      expect(formatChineseDuration(1001)).toBe("1 秒");
      expect(formatChineseDuration(1500)).toBe("1 秒");
      expect(formatChineseDuration(1999)).toBe("1 秒");
    });

    it("clamps negative ms to 0", () => {
      // Caller is expected to handle < 0 cleanly (e.g. clock skew).
      // The helper must not throw or return a wild string.
      expect(formatChineseDuration(-1)).toBe("0 秒");
      expect(formatChineseDuration(-1000)).toBe("0 秒");
      expect(formatChineseDuration(-999_999)).toBe("0 秒");
    });
  });

  describe("seconds-only range (< 1 min)", () => {
    it("renders pure seconds", () => {
      expect(formatChineseDuration(1_000)).toBe("1 秒");
      expect(formatChineseDuration(3_000)).toBe("3 秒");
      expect(formatChineseDuration(59_000)).toBe("59 秒");
    });
  });

  describe("minute boundaries", () => {
    it("drops trailing zero seconds at exact minute marks", () => {
      expect(formatChineseDuration(60_000)).toBe("1 分");
      expect(formatChineseDuration(120_000)).toBe("2 分");
      expect(formatChineseDuration(600_000)).toBe("10 分");
    });

    it("renders minutes + seconds when both are non-zero", () => {
      expect(formatChineseDuration(61_000)).toBe("1 分 1 秒");
      expect(formatChineseDuration(90_000)).toBe("1 分 30 秒");
      expect(formatChineseDuration(3_599_000)).toBe("59 分 59 秒");
    });
  });

  describe("hour boundaries", () => {
    it("drops trailing zero minutes at exact hour marks", () => {
      expect(formatChineseDuration(3_600_000)).toBe("1 小时");
      expect(formatChineseDuration(7_200_000)).toBe("2 小时");
    });

    it("renders hours + minutes when both are non-zero", () => {
      expect(formatChineseDuration(3_900_000)).toBe("1 小时 5 分");
    });

    // Current behavior: in the hours branch non-zero seconds get dropped,
    // because the branch only surfaces hours and minutes. The minute+seconds
    // range is fully covered above; once we're in hours-land the helper
    // deliberately collapses to "hours minutes" and loses seconds.
    it("drops non-zero seconds when hours is the top unit", () => {
      expect(formatChineseDuration(86_390_000)).toBe("23 小时 59 分");
    });
  });

  describe("day boundaries", () => {
    it("drops trailing zero hours at exact day marks", () => {
      expect(formatChineseDuration(86_400_000)).toBe("1 天");
    });

    it("renders days + hours when both are non-zero", () => {
      expect(formatChineseDuration(90_000_000)).toBe("1 天 1 小时");
    });

    // Same shape as the hours branch: in days-land the helper only surfaces
    // days + hours. Anything finer than an hour is collapsed, even if the
    // intermediate unit (minutes) is non-zero.
    it("drops sub-hour remainder when days is the top unit", () => {
      // 91_800_000 ms = 1 day 1 hour 30 minutes 0 seconds
      expect(formatChineseDuration(91_800_000)).toBe("1 天 1 小时");
    });
  });
});
