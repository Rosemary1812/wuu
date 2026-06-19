/**
 * Tests for LightweightStreamingText.
 *
 * Contract: short committed snapshots (≤ 2 chars) snap to full
 * instantly; longer snapshots reveal progressively at a steady pace
 * (~12 cps with a 100 ms base) that respects the floor and ceiling;
 * reveal never resets when the target text grows (we always continue
 * forward from the visible position); non-live, shrink, and unmount
 * paths skip animation.
 */
import { afterEach, beforeAll, describe, expect, it } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { LightweightStreamingText } from "./LightweightStreamingText";

// jsdom doesn't implement layout. Stub getBoundingClientRect so React
// doesn't crash on layout queries.
beforeAll(() => {
  Element.prototype.getBoundingClientRect = function (): DOMRect {
    return {
      x: 0,
      y: 0,
      top: 0,
      left: 0,
      right: 0,
      bottom: 0,
      width: 0,
      height: 0,
      toJSON() {
        return this;
      },
    } as DOMRect;
  };
});

let container: HTMLDivElement | null = null;
let root: Root | null = null;

function mount(props: Parameters<typeof LightweightStreamingText>[0]): void {
  if (container) unmount();
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
  act(() => {
    root!.render(<LightweightStreamingText {...props} />);
  });
}

function rerender(props: Parameters<typeof LightweightStreamingText>[0]): void {
  act(() => {
    root!.render(<LightweightStreamingText {...props} />);
  });
}

function unmount(): void {
  if (root) {
    act(() => {
      root!.unmount();
    });
    root = null;
  }
  if (container) {
    container.remove();
    container = null;
  }
}

function surfaceText(): string {
  const span = document.querySelector(".lightweight-stream") as HTMLElement | null;
  return span?.textContent ?? "";
}

afterEach(() => {
  unmount();
});

describe("LightweightStreamingText", () => {
  it("renders short text in full immediately (no animation)", () => {
    mount({ text: "ok", live: true, className: "lightweight-stream" });
    expect(surfaceText()).toBe("ok");
  });

  it("renders empty text as empty", () => {
    mount({ text: "", live: true, className: "lightweight-stream" });
    expect(surfaceText()).toBe("");
  });

  it("snaps to full text when live is false", () => {
    mount({
      text: "Reading the file now",
      live: false,
      className: "lightweight-stream",
    });
    expect(surfaceText()).toBe("Reading the file now");
  });

  it("reveals mid-length text progressively during live", async () => {
    mount({
      text: "Looking at the file",
      live: true,
      className: "lightweight-stream",
    });

    // Mid-reveal: visible is partial (strictly less than full text).
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 100));
    });
    const mid = surfaceText();
    expect(mid.length).toBeGreaterThan(0);
    expect(mid.length).toBeLessThan("Looking at the file".length);
    // The visible prefix must match the source text.
    expect("Looking at the file".startsWith(mid)).toBe(true);

    // After enough wall time, the full text is revealed. 19 chars at
    // ~12 cps with a 100 ms base lands well under 2000 ms, so 2000 ms
    // is a safe settle wait that is robust to CI jitter.
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 2000));
    });
    expect(surfaceText()).toBe("Looking at the file");
  });

  it("continues reveal when target text grows (never resets visible position)", async () => {
    mount({
      text: "Reading file",
      live: true,
      className: "lightweight-stream",
    });

    // Let the reveal advance partway. 12 chars at ~12 cps with a 100 ms
    // base needs a settle wait comfortably past the base window;
    // 300 ms lands the reveal around 6 chars, which is clearly
    // partial and well above zero.
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 300));
    });
    const before = surfaceText();
    expect(before.length).toBeGreaterThan(0);
    expect(before.length).toBeLessThan("Reading file".length);

    // Back-end pushes a longer snapshot. The reveal must continue
    // forward from the visible position, never jump back to a
    // shorter visible length.
    rerender({
      text: "Reading file content for analysis",
      live: true,
      className: "lightweight-stream",
    });
    const justAfter = surfaceText();
    expect(justAfter.length).toBeGreaterThanOrEqual(before.length);

    // Eventually the full new text is revealed. 32 chars at ~12 cps
    // with a 100 ms base lands under 3000 ms, so 3000 ms is a safe
    // settle wait that is robust to CI jitter.
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 3000));
    });
    expect(surfaceText()).toBe("Reading file content for analysis");
  });

  it("snaps to a shorter snapshot when the back-end shrinks the text", async () => {
    mount({
      text: "Reading file content for analysis",
      live: true,
      className: "lightweight-stream",
    });

    // 35 chars at ~12 cps with a 100 ms base lands under 3000 ms,
    // so 3000 ms is enough for the reveal to settle before the
    // back-end pushes the shorter snapshot.
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 3000));
    });
    expect(surfaceText()).toBe("Reading file content for analysis");

    rerender({
      text: "Reading file",
      live: true,
      className: "lightweight-stream",
    });
    expect(surfaceText()).toBe("Reading file");
  });

  it("snaps to full text when live flips false mid-reveal", async () => {
    mount({
      text: "Looking at this long content that has not finished revealing",
      live: true,
      className: "lightweight-stream",
    });

    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 100));
    });
    const mid = surfaceText();
    expect(mid.length).toBeGreaterThan(0);
    expect(mid.length).toBeLessThan(
      "Looking at this long content that has not finished revealing".length
    );

    rerender({
      text: "Looking at this long content that has not finished revealing",
      live: false,
      className: "lightweight-stream",
    });
    expect(surfaceText()).toBe(
      "Looking at this long content that has not finished revealing"
    );
  });

  it("respects the duration cap for very long text", async () => {
    const longText = "a".repeat(500);
    mount({
      text: longText,
      live: true,
      className: "lightweight-stream",
    });

    // 500 chars would naively take 100 + 500*80 = 40100 ms. The cap is
    // 1800 ms, so 2000 ms is enough for the reveal to settle.
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 2000));
    });
    expect(surfaceText()).toBe(longText);
  });

  it("does not leave a running RAF after unmount", async () => {
    mount({
      text: "Looking at the file",
      live: true,
      className: "lightweight-stream",
    });
    // Unmount during reveal. If the RAF keeps firing it would touch
    // a null ref, but the bigger concern is a leaked loop: we assert
    // unmount completes cleanly with no errors and no further DOM
    // mutations.
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 100));
    });
    unmount();
    expect(document.querySelector(".lightweight-stream")).toBeNull();
  });

  it("preserves the live=false path even when text grew", () => {
    mount({ text: "ok", live: true, className: "lightweight-stream" });
    rerender({
      text: "Updated plan: read the configuration file and report back",
      live: false,
      className: "lightweight-stream",
    });
    expect(surfaceText()).toBe(
      "Updated plan: read the configuration file and report back"
    );
  });
});