import { afterEach, describe, expect, it } from "vitest";
import {
  WORKSPACE_RIGHT_PANEL_MAIN_MIN_WIDTH,
  WORKSPACE_RIGHT_PANEL_MAX_WIDTH,
  WORKSPACE_RIGHT_PANEL_MIN_DRAG_RANGE,
  WORKSPACE_RIGHT_PANEL_MIN_WIDTH,
  clampWorkspaceRightPanelWidth,
} from "./AppLayoutState";

// Regression coverage for the "right panel can't be resized on a narrow window"
// bug. The right-panel resizer (live drag, commit, keyboard, and window-resize
// paths) all funnel width changes through clampWorkspaceRightPanelWidth, so if
// that clamp's [min, max] range collapses to a single value the drag handle
// grabs but nothing moves. This asserts the clamp always keeps a usable range.

// The exact ceiling the OLD (buggy) formula produced, inlined here so the test
// proves — not just asserts — that the previous logic pinned min === max on a
// tight window. Old body was:
//   maxWidth = Math.max(MIN, Math.min(MAX, innerWidth - sidebar - MAIN_MIN))
function oldMaxWidth(innerWidth: number, sidebarWidth: number): number {
  const maxForWindow = innerWidth - sidebarWidth - WORKSPACE_RIGHT_PANEL_MAIN_MIN_WIDTH;
  return Math.max(
    WORKSPACE_RIGHT_PANEL_MIN_WIDTH,
    Math.min(WORKSPACE_RIGHT_PANEL_MAX_WIDTH, maxForWindow)
  );
}

// Probe the effective [min, max] the current clamp exposes by clamping an
// absurdly large and an absurdly small target width. Whatever comes back is the
// real ceiling / floor the resizer can reach.
function effectiveRange(sidebarWidth: number): { min: number; max: number } {
  return {
    min: clampWorkspaceRightPanelWidth(-100000, sidebarWidth),
    max: clampWorkspaceRightPanelWidth(100000, sidebarWidth),
  };
}

const originalInnerWidth = window.innerWidth;

function setInnerWidth(value: number): void {
  // jsdom exposes innerWidth as a plain writable property.
  window.innerWidth = value;
}

afterEach(() => {
  setInnerWidth(originalInnerWidth);
});

describe("clampWorkspaceRightPanelWidth", () => {
  it("keeps a real drag range on a narrow window with the sidebar open", () => {
    // Narrow portrait-ish window, sidebar open at its default 326px.
    setInnerWidth(900);
    const sidebar = 326;

    // maxForWindow = 900 - 326 - 360 = 214 (well below MIN=300).
    // OLD formula: Math.max(300, Math.min(860, 214)) = 300 === MIN  -> ZERO range.
    expect(oldMaxWidth(900, sidebar)).toBe(WORKSPACE_RIGHT_PANEL_MIN_WIDTH);
    expect(oldMaxWidth(900, sidebar)).toBe(300);

    // Under the old logic the clamp pinned every width to a single value:
    // clamping a huge and a tiny target would return the SAME number. The fix
    // must instead expose distinct floor/ceiling.
    const { min, max } = effectiveRange(sidebar);
    expect(min).toBe(WORKSPACE_RIGHT_PANEL_MIN_WIDTH); // 300
    expect(max).toBeGreaterThan(min); // real range, not a pinned point
    expect(max - min).toBeGreaterThanOrEqual(WORKSPACE_RIGHT_PANEL_MIN_DRAG_RANGE);
    // Concretely: max = clamp(214, 300+120, 860) = 420, range = 120.
    expect(max).toBe(420);

    // And a mid-range target genuinely lands between the floor and ceiling,
    // i.e. dragging actually changes the width.
    expect(clampWorkspaceRightPanelWidth(360, sidebar)).toBe(360);
    expect(clampWorkspaceRightPanelWidth(1000, sidebar)).toBe(420);
    expect(clampWorkspaceRightPanelWidth(100, sidebar)).toBe(300);
  });

  it("is unchanged on a wide window and honors the preferred main-min there", () => {
    setInnerWidth(1600);
    const sidebar = 326;

    const { min, max } = effectiveRange(sidebar);
    expect(max).toBeGreaterThan(min); // range present
    expect(max - min).toBeGreaterThanOrEqual(WORKSPACE_RIGHT_PANEL_MIN_DRAG_RANGE);

    // Ceiling never exceeds the absolute max.
    expect(max).toBeLessThanOrEqual(WORKSPACE_RIGHT_PANEL_MAX_WIDTH); // <= 860

    // Preferred conversation main-min is honored: there is still room for the
    // main pane to keep >= MAIN_MIN (360) at the ceiling.
    expect(max).toBeLessThanOrEqual(1600 - sidebar - WORKSPACE_RIGHT_PANEL_MAIN_MIN_WIDTH); // <= 914

    // On a wide window the new formula produces the exact same ceiling as the
    // old one (maxForWindow >= MIN+range already), so behavior is unchanged.
    expect(max).toBe(oldMaxWidth(1600, sidebar));
    expect(max).toBe(WORKSPACE_RIGHT_PANEL_MAX_WIDTH); // 860
  });

  it("keeps a drag range with the sidebar collapsed (0), even on a tight window", () => {
    // Sidebar collapsed, but window still narrow enough that the old formula
    // would have pinned the panel.
    setInnerWidth(600);
    const collapsed = 0;

    // maxForWindow = 600 - 0 - 360 = 240 -> OLD max = 300 === MIN -> zero range.
    expect(oldMaxWidth(600, collapsed)).toBe(WORKSPACE_RIGHT_PANEL_MIN_WIDTH);

    const tight = effectiveRange(collapsed);
    expect(tight.min).toBe(WORKSPACE_RIGHT_PANEL_MIN_WIDTH);
    expect(tight.max - tight.min).toBeGreaterThanOrEqual(WORKSPACE_RIGHT_PANEL_MIN_DRAG_RANGE);
    expect(tight.max).toBe(420); // clamp(240, 420, 860)

    // Sidebar collapsed on a roomy window: sensible ceiling, capped at MAX.
    setInnerWidth(1400);
    const roomy = effectiveRange(collapsed);
    expect(roomy.max - roomy.min).toBeGreaterThanOrEqual(WORKSPACE_RIGHT_PANEL_MIN_DRAG_RANGE);
    expect(roomy.max).toBe(WORKSPACE_RIGHT_PANEL_MAX_WIDTH); // clamp(1040, 420, 860) = 860
  });

  it("guarantees the minimum drag range across a sweep of window / sidebar sizes", () => {
    for (const innerWidth of [320, 480, 600, 768, 900, 1024, 1280, 1600, 1920]) {
      for (const sidebar of [0, 200, 326, 520]) {
        setInnerWidth(innerWidth);
        const { min, max } = effectiveRange(sidebar);
        expect(min).toBe(WORKSPACE_RIGHT_PANEL_MIN_WIDTH);
        expect(max - min).toBeGreaterThanOrEqual(WORKSPACE_RIGHT_PANEL_MIN_DRAG_RANGE);
        expect(max).toBeLessThanOrEqual(WORKSPACE_RIGHT_PANEL_MAX_WIDTH);
      }
    }
  });
});

import {
  THREAD_PANEL_MAIN_MIN_WIDTH,
  THREAD_PANEL_MAX_WIDTH,
  THREAD_PANEL_MIN_DRAG_RANGE,
  THREAD_PANEL_MIN_WIDTH,
  clampThreadPanelWidth,
} from "./AppLayoutState";

// The thread (reply subthread) panel funnels every width change through
// clampThreadPanelWidth, same contract as the workspace right panel: the
// effective [min, max] range must never collapse, or the separator grabs but
// nothing moves.
describe("clampThreadPanelWidth", () => {
  function effectiveThreadRange(sidebarWidth: number): { min: number; max: number } {
    return {
      min: clampThreadPanelWidth(-100000, sidebarWidth),
      max: clampThreadPanelWidth(100000, sidebarWidth),
    };
  }

  it("keeps a usable drag range on a roomy window", () => {
    window.innerWidth = 1600;
    const range = effectiveThreadRange(326);
    expect(range.min).toBe(THREAD_PANEL_MIN_WIDTH);
    expect(range.max).toBe(THREAD_PANEL_MAX_WIDTH);
    window.innerWidth = 1024;
  });

  it("never collapses min === max on a tight window", () => {
    window.innerWidth = THREAD_PANEL_MIN_WIDTH + THREAD_PANEL_MAIN_MIN_WIDTH + 100;
    const range = effectiveThreadRange(326);
    expect(range.max - range.min).toBeGreaterThanOrEqual(THREAD_PANEL_MIN_DRAG_RANGE);
    window.innerWidth = 1024;
  });

  it("passes stored widths through unchanged when inside the range", () => {
    window.innerWidth = 1600;
    expect(clampThreadPanelWidth(400, 326)).toBe(400);
    window.innerWidth = 1024;
  });
});
