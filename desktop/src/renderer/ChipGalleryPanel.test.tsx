/**
 * Tests for `ChipGalleryPanel` — the dev-mode catalog of every chip
 * variant the renderer can produce. The panel itself is gated by the
 * debug-controls switch and the `ENABLE_CHIP_GALLERY` build flag, so
 * these tests only run in dev builds, but the component contract is
 * what we pin here: which chips appear in the gallery, which mock
 * turns appear in the in-context section, and how the panel opens
 * and closes.
 */
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { ASSISTANT_TURN_PRESENTATION_STABILIZE_MS } from "./AssistantTurnPresentation";
import { ChipGalleryPanel } from "./ChipGalleryPanel";

beforeAll(() => {
  // jsdom does not lay out real heights. Stub getBoundingClientRect so
  // React's effects do not crash on layout queries (the panel uses
  // overflow / flex sizing that touches layout during effect runs).
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

let root: Root | undefined;
let container: HTMLDivElement | undefined;

afterEach(() => {
  vi.useRealTimers();
  act(() => {
    root?.unmount();
  });
  container?.remove();
  root = undefined;
  container = undefined;
});

function mount(props: {
  open: boolean;
  onClose: () => void;
}): HTMLDivElement {
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
  act(() => {
    root!.render(<ChipGalleryPanel {...props} />);
  });
  return container;
}

describe("ChipGalleryPanel", () => {
  it("renders nothing when closed", () => {
    const host = mount({ open: false, onClose: () => {} });
    expect(host.querySelector(".chip-gallery-panel")).toBeNull();
  });

  it("renders the gallery section with all chip variants when open", () => {
    const host = mount({ open: true, onClose: () => {} });

    const entries = host.querySelectorAll(".chip-gallery-entry");
    // 13 entries: missing reply + 4 context compaction
    // + 4 provider/network + 2 auth + 2 tool/internal.
    expect(entries.length).toBe(13);

    // Manual interruptions are intentionally silent and are not chip variants.
    expect(host.textContent).not.toContain("已停止");
    expect(host.textContent).not.toContain("回复已中断");
    expect(host.textContent).toContain("无最终回答");
    // A few representative chip titles from the error / auth path.
    expect(host.textContent).toContain("401 unauthorized");
    expect(host.textContent).toContain("context_length_exceeded");
  });

  it("renders each gallery entry with its chip, label, kind badge, and description", () => {
    const host = mount({ open: true, onClose: () => {} });

    const firstEntry = host.querySelector(".chip-gallery-entry");
    expect(firstEntry).not.toBeNull();
    // The chip itself (turn-event-notice) is rendered inside the entry.
    expect(
      firstEntry?.querySelector(".turn-event-notice"),
    ).not.toBeNull();
    // The kind badge is a <code> with the "kind · tone" string.
    expect(firstEntry?.querySelector("code")?.textContent).toMatch(
      /· (neutral|warning|auth|error|gray)/,
    );
    // The description is a one-liner explaining when the chip fires.
    expect(firstEntry?.querySelector(".chip-gallery-entry-description")?.textContent?.length).toBeGreaterThan(0);
  });

  it("renders the in-context section with one mock turn per scenario", () => {
    vi.useFakeTimers();
    const host = mount({ open: true, onClose: () => {} });
    // Advance past the presentation buffer so the chips inside the
    // mock TurnView instances have time to mount.
    act(() => {
      vi.advanceTimersByTime(ASSISTANT_TURN_PRESENTATION_STABILIZE_MS);
    });

    const items = host.querySelectorAll(".chip-gallery-context-item");
    // 3 representative scenarios: missing-reply, context-compaction,
    // failed-provider.
    expect(items.length).toBe(3);

    // Each item has a heading and a framed mock turn.
    const firstItem = items[0];
    expect(firstItem?.querySelector("h4")?.textContent).toBe(
      "完成但只有 commentary，无 final_answer",
    );
    expect(
      firstItem?.querySelector(".chip-gallery-context-frame"),
    ).not.toBeNull();
  });

  it("calls onClose when the backdrop is clicked", () => {
    const onClose = vi.fn();
    const host = mount({ open: true, onClose });
    const backdrop = host.querySelector(".chip-gallery-backdrop");
    expect(backdrop).not.toBeNull();
    act(() => {
      backdrop?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("does not call onClose when clicking inside the panel body", () => {
    const onClose = vi.fn();
    const host = mount({ open: true, onClose });
    const panel = host.querySelector(".chip-gallery-panel");
    act(() => {
      panel?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    expect(onClose).not.toHaveBeenCalled();
  });

  it("calls onClose when the close button is clicked", () => {
    const onClose = vi.fn();
    const host = mount({ open: true, onClose });
    const closeButton = host.querySelector(
      'button[aria-label="关闭"]',
    );
    expect(closeButton).not.toBeNull();
    act(() => {
      closeButton?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
