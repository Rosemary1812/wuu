import { describe, expect, it } from "vitest";
import {
  MAIN_WINDOW_MAX_HEIGHT,
  MAIN_WINDOW_MAX_WIDTH,
  MAIN_WINDOW_MIN_HEIGHT,
  MAIN_WINDOW_MIN_WIDTH,
  computeDefaultMainWindowBounds,
} from "./windowState";

describe("computeDefaultMainWindowBounds", () => {
  it("uses the relative ratio on a 1440p workArea", () => {
    expect(computeDefaultMainWindowBounds({ width: 2560, height: 1400 })).toEqual({
      width: 1024,
      height: 840,
    });
  });

  it("clamps the width to the floor on a 1080p workArea", () => {
    // 40% × 1920 = 768, below the 880 floor → floor wins.
    // 60% × 1048 = 629, above the 560 floor → relative wins.
    expect(computeDefaultMainWindowBounds({ width: 1920, height: 1048 })).toEqual({
      width: MAIN_WINDOW_MIN_WIDTH,
      height: 629,
    });
  });

  it("clamps the height to the cap on a 4K workArea", () => {
    // 40% × 3840 = 1536, below the 1600 cap → relative wins.
    // 60% × 2120 = 1272, above the 1100 cap → cap wins.
    expect(computeDefaultMainWindowBounds({ width: 3840, height: 2120 })).toEqual({
      width: 1536,
      height: MAIN_WINDOW_MAX_HEIGHT,
    });
  });

  it("clamps the height to the cap on a wide 5K workArea", () => {
    // A typical 27" 5K has 5120 × 2880 workArea; height should hit the cap.
    expect(computeDefaultMainWindowBounds({ width: 5120, height: 2880 })).toEqual({
      width: MAIN_WINDOW_MAX_WIDTH,
      height: MAIN_WINDOW_MAX_HEIGHT,
    });
  });

  it("falls back to the absolute minimum on a tiny workArea", () => {
    expect(computeDefaultMainWindowBounds({ width: 1024, height: 700 })).toEqual({
      width: MAIN_WINDOW_MIN_WIDTH,
      height: MAIN_WINDOW_MIN_HEIGHT,
    });
  });

  it("rounds fractional pixels so we always pass integers to BrowserWindow", () => {
    const result = computeDefaultMainWindowBounds({ width: 1441, height: 901 });
    expect(Number.isInteger(result.width)).toBe(true);
    expect(Number.isInteger(result.height)).toBe(true);
  });
});
