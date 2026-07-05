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
import { ContextCompactionNotice, TurnNotice } from "./TurnNotice";
import { userFacingErrorForMessage } from "./UserFacingErrors";

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
    expect(label?.classList.contains("live-progress-chip")).toBe(true);

    // The event divider uses text and line color as the affordance; icons
    // would make these lightweight stream events compete with message text.
    expect(host.querySelector(".turn-notice-icon")).toBeNull();
  });

  it("uses the manual compact progress label for slash compact", () => {
    const host = mount(
      <ContextCompactionNotice
        status="in_progress"
        reason="manual"
        text="Manual context compaction in progress."
      />,
    );

    expect(host.querySelector(".turn-event-title")?.textContent).toBe(
      "正在压缩上下文",
    );
    expect(host.querySelector(".live-progress-chip")).not.toBeNull();
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

  it("labels manual compact completion as success", () => {
    const host = mount(
      <ContextCompactionNotice
        status="completed"
        reason="manual"
        text="✦ Manually compacted history: 18 → 5 messages (was ~12k tokens)"
      />,
    );

    expect(host.querySelector(".turn-event-title")?.textContent).toBe(
      "压缩成功",
    );
    expect(host.querySelector(".turn-event-detail")?.textContent).toContain(
      "18 条消息整理为 5 条",
    );
  });

  it("labels failed manual compact status as failed", () => {
    const host = mount(
      <ContextCompactionNotice
        status="failed"
        reason="manual"
        text="Manual context compaction failed; history is unchanged."
      />,
    );

    expect(host.querySelector(".turn-event-title")?.textContent).toBe(
      "压缩失败",
    );
    expect(host.querySelector(".turn-event-detail")?.textContent).toContain(
      "当前对话仍保留原上下文",
    );
  });

  it("falls back to the completed layout when status is omitted", () => {
    const host = mount(<ContextCompactionNotice text="" />);
    const aside = host.querySelector("aside.turn-notice.context-compaction-notice");
    expect(aside?.classList.contains("is-progress")).toBe(false);
    expect(host.querySelector(".turn-event-title")?.textContent).toBe(
      "上下文已压缩",
    );
  });

  it("uses the same completed title for overflow recovery compaction", () => {
    const host = mount(
      <ContextCompactionNotice
        status="completed"
        text="Recovered from context overflow — compacted history: 18 → 5 messages (was ~12k tokens)"
      />,
    );

    expect(host.querySelector(".turn-event-title")?.textContent).toBe(
      "上下文已压缩",
    );
  });

  it("labels inception compaction with an explicit Inception marker", () => {
    const host = mount(
      <ContextCompactionNotice
        status="completed"
        reason="inception"
        text="✦ Inception rewrote history: 227 → 3 messages (was ~175k tokens)"
      />,
    );

    expect(host.querySelector(".turn-event-title")?.textContent).toBe(
      "已压缩上下文（Inception）",
    );
    expect(host.querySelector(".turn-event-detail")?.textContent).toContain(
      "续接摘要",
    );
  });

  it("labels HelpMe compaction as merged recovery result", () => {
    const host = mount(
      <ContextCompactionNotice
        status="completed"
        reason="helpme"
        text="✦ HelpMe recovered and compacted history: 42 → 2 messages (was ~90k tokens)"
      />,
    );

    expect(host.querySelector(".turn-event-title")?.textContent).toBe(
      "已合并求助结果",
    );
    expect(host.querySelector(".turn-event-detail")?.textContent).toContain(
      "HelpMe 恢复结果",
    );
  });

  it("does not present failed proactive compaction as a successful compact", () => {
    const failedCompactText =
      "Context compaction failed; continuing without compacting history.";
    const host = mount(
      <ContextCompactionNotice
        status="completed"
        text={failedCompactText}
      />,
    );

    expect(host.querySelector(".turn-event-title")?.textContent).toBe(
      "压缩失败",
    );
    expect(host.querySelector(".turn-event-detail")?.textContent).toContain(
      "当前对话仍保留原上下文",
    );
    expect(host.textContent).not.toContain("上下文已压缩");
  });
});

describe("TurnNotice compact chip", () => {
  it("renders the title as the only visible label and moves the full detail into a hover tooltip", () => {
    const display = userFacingErrorForMessage("connection reset by peer", "turn");
    const host = mount(<TurnNotice display={display} />);
    const aside = host.querySelector("aside.turn-event-notice");
    expect(aside).not.toBeNull();
    // Tone class drives the visual treatment.
    expect(aside?.classList.contains(display.tone)).toBe(true);
    // The title element shows only the short title (single-line).
    expect(host.querySelector(".turn-event-title")?.textContent).toBe(display.title);
    // The full detail is reachable via the host's `title` attribute and
    // mirrored on `aria-label` for assistive tech.
    expect(aside?.getAttribute("title")).toContain(display.title);
    expect(aside?.getAttribute("title")).toContain(display.detail);
    expect(aside?.getAttribute("aria-label")).toContain(display.title);
    expect(aside?.getAttribute("aria-label")).toContain(display.detail);
    // Detail stays in the DOM for the hover text path, and actions remain
    // clickable so the recovery path is not lost.
    expect(host.querySelector(".turn-event-detail")?.textContent).toBe(display.detail);
    expect(host.querySelector(".turn-notice-actions")).not.toBeNull();
  });

  it("does not render the actions row when the display has no recommended actions", () => {
    const display = userFacingErrorForMessage("context canceled", "turn");
    const host = mount(
      <TurnNotice display={{ ...display, recommendedActions: [] }} />,
    );
    expect(host.querySelector(".turn-notice-actions")).toBeNull();
    // The host still carries the hover text and title element.
    const aside = host.querySelector("aside.turn-event-notice");
    expect(aside?.getAttribute("title")).toContain(display.title);
    expect(host.querySelector(".turn-event-title")?.textContent).toBe(display.title);
  });

  it("applies the auth tone to the host aside", () => {
    const display = userFacingErrorForMessage("401 unauthorized", "turn");
    const host = mount(<TurnNotice display={display} />);
    const aside = host.querySelector("aside.turn-event-notice");
    expect(aside?.classList.contains("auth")).toBe(true);
    // Auth is the only category that resolves to the "auth" tone today.
    expect(display.tone).toBe("auth");
  });
});
