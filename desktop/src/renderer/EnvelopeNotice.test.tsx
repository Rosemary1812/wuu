/**
 * Tests for the DM envelope meta row (docs/plans/
 * 2026-07-03-resident-named-agents.md §7.3). A user message that the
 * envelope router injected into a resident agent's DM thread must NOT
 * render as a user bubble — it renders as a collapsed meta line
 * ("收到来自「{title}」的 {n} 条消息") that expands to the original
 * envelope text on click. The line also surfaces the envelope's
 * structural facts (consistency plan 2026-07-04 §1 #12): a "点名"
 * badge when any record is addressed, and "转发×N" when the highest
 * hop is above 0 (0 = straight from the source thread, each resident
 * relay increments it).
 */
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it } from "vitest";
import { createElement } from "react";
import type { ThreadItem } from "../shared/protocol";
import { EnvelopeNotice } from "./EnvelopeNotice";
import { ThreadItemView } from "./ThreadItemView";

let mountedRoots: Root[] = [];
let mountedContainers: HTMLElement[] = [];

function mount(element: React.ReactElement): HTMLElement {
  const container = document.createElement("div");
  document.body.appendChild(container);
  const root = createRoot(container);
  act(() => {
    root.render(element);
  });
  mountedRoots.push(root);
  mountedContainers.push(container);
  return container;
}

afterEach(() => {
  for (const root of mountedRoots) {
    act(() => {
      root.unmount();
    });
  }
  for (const container of mountedContainers) {
    container.remove();
  }
  mountedRoots = [];
  mountedContainers = [];
});

describe("EnvelopeNotice", () => {
  it("renders a collapsed meta line with source title and count", () => {
    const container = mount(
      createElement(EnvelopeNotice, {
        meta: [
          {
            source_thread_id: "thread-group",
            source_thread_title: "发布排期讨论",
            addressed: true,
            hop: 1,
          },
          { source_thread_id: "thread-group", addressed: false, hop: 1 },
          { source_thread_id: "thread-group", addressed: false, hop: 2 },
        ],
        text: "<message from=\"user\">明天能上线吗</message>",
      }),
    );
    const toggle = container.querySelector<HTMLButtonElement>(
      ".envelope-notice-toggle",
    );
    expect(toggle).not.toBeNull();
    expect(toggle?.textContent).toContain("收到来自「发布排期讨论」的 3 条消息");
    expect(toggle?.getAttribute("aria-expanded")).toBe("false");
    expect(container.querySelector(".envelope-notice-body")).toBeNull();
    // One record is addressed and the deepest record is 2 hops out.
    expect(
      container.querySelector(".envelope-notice-addressed")?.textContent,
    ).toBe("点名");
    expect(container.querySelector(".envelope-notice-hop")?.textContent).toBe(
      "转发×2",
    );
  });

  it("shows neither badge for a direct, unaddressed user message", () => {
    const container = mount(
      createElement(EnvelopeNotice, {
        meta: [
          {
            source_thread_id: "thread-group",
            source_thread_title: "发布排期讨论",
            addressed: false,
            hop: 0,
          },
        ],
        text: "内容",
      }),
    );
    expect(container.querySelector(".envelope-notice-addressed")).toBeNull();
    expect(container.querySelector(".envelope-notice-hop")).toBeNull();
  });

  it("shows the addressed badge without a hop marker for a direct mention", () => {
    const container = mount(
      createElement(EnvelopeNotice, {
        meta: [{ source_thread_title: "发布排期讨论", addressed: true, hop: 0 }],
        text: "内容",
      }),
    );
    expect(
      container.querySelector(".envelope-notice-addressed")?.textContent,
    ).toBe("点名");
    expect(container.querySelector(".envelope-notice-hop")).toBeNull();
  });

  it("treats missing addressed/hop fields as direct and unaddressed", () => {
    const container = mount(
      createElement(EnvelopeNotice, {
        meta: [{ source_thread_id: "thread-x" }],
        text: "内容",
      }),
    );
    expect(container.querySelector(".envelope-notice-addressed")).toBeNull();
    expect(container.querySelector(".envelope-notice-hop")).toBeNull();
  });

  it("expands the original text on click and collapses on second click", () => {
    const container = mount(
      createElement(EnvelopeNotice, {
        meta: [{ source_thread_title: "发布排期讨论", addressed: true, hop: 1 }],
        text: "原始信封内容",
      }),
    );
    const toggle = container.querySelector<HTMLButtonElement>(
      ".envelope-notice-toggle",
    );
    act(() => {
      toggle?.click();
    });
    expect(toggle?.getAttribute("aria-expanded")).toBe("true");
    const body = container.querySelector(".envelope-notice-body");
    expect(body?.textContent).toContain("原始信封内容");
    act(() => {
      toggle?.click();
    });
    expect(container.querySelector(".envelope-notice-body")).toBeNull();
  });

  it("renders without a count when only a single record is present", () => {
    const container = mount(
      createElement(EnvelopeNotice, {
        meta: [{ source_thread_title: "发布排期讨论", addressed: true, hop: 1 }],
        text: "内容",
      }),
    );
    const toggle = container.querySelector<HTMLButtonElement>(
      ".envelope-notice-toggle",
    );
    expect(toggle?.textContent).toContain("收到来自「发布排期讨论」的消息");
    expect(toggle?.textContent).not.toContain("条消息");
  });

  it("falls back to a generic source label when no record has a title", () => {
    const container = mount(
      createElement(EnvelopeNotice, {
        meta: [{ source_thread_id: "thread-x", addressed: false, hop: 1 }],
        text: "内容",
      }),
    );
    const toggle = container.querySelector<HTMLButtonElement>(
      ".envelope-notice-toggle",
    );
    expect(toggle?.textContent).toContain("收到来自其他会话的消息");
    expect(toggle?.textContent).not.toContain("条消息");
  });

  it("falls back to a generic source label for an empty array", () => {
    const container = mount(
      createElement(EnvelopeNotice, {
        meta: [],
        text: "内容",
      }),
    );
    const toggle = container.querySelector<HTMLButtonElement>(
      ".envelope-notice-toggle",
    );
    expect(toggle?.textContent).toContain("收到来自其他会话的消息");
  });
});

describe("ThreadItemView envelope routing", () => {
  function renderItem(item: ThreadItem): HTMLElement {
    return mount(
      createElement(ThreadItemView, {
        turnID: "turn-1",
        turnStatus: "completed",
        item,
        streaming: false,
        onStreamFrame: () => {},
        onNoticeAction: () => {},
      }),
    );
  }

  it("renders envelope-tagged user messages as a meta row, not a bubble", () => {
    const container = renderItem({
      id: "item-1",
      type: "user_message",
      text: "信封正文",
      envelope_meta: [
        { source_thread_id: "thread-group", source_thread_title: "群聊", addressed: true, hop: 1 },
        { source_thread_id: "thread-group", addressed: false, hop: 1 },
      ],
    });
    expect(container.querySelector(".envelope-notice")).not.toBeNull();
    expect(container.querySelector(".user-message")).toBeNull();
  });

  it("keeps plain user messages on the bubble path", () => {
    const container = renderItem({
      id: "item-2",
      type: "user_message",
      text: "普通消息",
    });
    expect(container.querySelector(".envelope-notice")).toBeNull();
    expect(container.querySelector(".user-message")).not.toBeNull();
  });

  it("keeps user messages with an empty envelope_meta array on the bubble path", () => {
    const container = renderItem({
      id: "item-3",
      type: "user_message",
      text: "普通消息",
      envelope_meta: [],
    });
    expect(container.querySelector(".envelope-notice")).toBeNull();
    expect(container.querySelector(".user-message")).not.toBeNull();
  });
});
