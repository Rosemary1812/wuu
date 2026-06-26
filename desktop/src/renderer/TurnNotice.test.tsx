/**
 * Tests for `ContextCompactionNotice`'s two-state rendering.
 *
 * The component now branches on `status`:
 *   - `in_progress` renders the centered event divider with the shared
 *     live-gray sweep host.
 *   - everything else keeps the same centered event divider without motion.
 *
 * These tests pin the markup contract: which class is added, what the
 * host reads as, and which child element holds the sweep. The CSS
 * itself is verified by visual review against the shared live-gray
 * selector group in `turns.css`.
 */
import { act, type ReactElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeAll, describe, expect, it } from "vitest";
import { ContextCompactionNotice } from "./TurnNotice";

beforeAll(() => {
  // jsdom does not lay out real heights. Stub getBoundingClientRect so
  // React's effects do not crash on layout queries.
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

let root: Root | null = null;
let container: HTMLDivElement | null = null;

afterEach(() => {
  if (root) {
    act(() => {
      root?.unmount();
    });
    root = null;
  }
  if (container) {
    container.remove();
    container = null;
  }
});

function mount(element: ReactElement): HTMLDivElement {
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
  act(() => {
    root?.render(element);
  });
  return container;
}

describe("ContextCompactionNotice", () => {
  it("renders the in_progress host with the shimmer-ready label when status is in_progress", () => {
    const host = mount(<ContextCompactionNotice status="in_progress" />);
    const aside = host.querySelector("aside.turn-notice.context-compaction-notice");
    expect(aside).not.toBeNull();
    expect(aside?.classList.contains("is-progress")).toBe(true);
    expect(aside?.getAttribute("role")).toBe("status");
    expect(aside?.getAttribute("aria-live")).toBe("polite");

    const label = host.querySelector(".turn-event-title");
    expect(label).not.toBeNull();
    expect(label?.textContent).toBe("正在自动压缩上下文");

    // The event divider uses text and line color as the affordance; icons
    // would make these lightweight stream events compete with message text.
    expect(host.querySelector(".turn-notice-icon")).toBeNull();
  });

  it("renders the established icon + copy layout when status is completed", () => {
    const host = mount(
      <ContextCompactionNotice
        status="completed"
        text="✦ Compacted history: 18 → 5 messages (was ~12k tokens)"
      />,
    );
    const aside = host.querySelector("aside.turn-notice.context-compaction-notice");
    expect(aside).not.toBeNull();
    expect(aside?.classList.contains("is-progress")).toBe(false);
    expect(aside?.getAttribute("aria-live")).toBe("polite");

    const title = host.querySelector(".turn-event-title");
    expect(title?.textContent).toBe("上下文已压缩");

    const detail = host.querySelector(".turn-event-detail");
    expect(detail?.textContent).toContain("18 条消息整理为 5 条");
    expect(host.querySelector(".turn-notice-icon")).toBeNull();
  });

  it("falls back to the completed layout when status is omitted", () => {
    const host = mount(<ContextCompactionNotice text="" />);
    const aside = host.querySelector("aside.turn-notice.context-compaction-notice");
    expect(aside?.classList.contains("is-progress")).toBe(false);
    expect(host.querySelector(".turn-event-title")?.textContent).toBe(
      "上下文已压缩",
    );
  });

  it("names context-overflow recovery separately from routine compaction", () => {
    const host = mount(
      <ContextCompactionNotice
        status="completed"
        text="Recovered from context overflow — compacted history: 18 → 5 messages (was ~12k tokens)"
      />,
    );

    expect(host.querySelector(".turn-event-title")?.textContent).toBe(
      "已从上下文超限中恢复",
    );
  });
});
