/**
 * Tests for the DM envelope meta row (docs/plans/
 * 2026-07-03-resident-named-agents.md §7.3). A user message that the
 * envelope router injected into a resident agent's DM thread must NOT
 * render as a user bubble — it renders as a collapsed meta line
 * ("收到来自「{title}」的 {n} 条消息") that expands to the original
 * envelope text on click.
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
        meta: {
          source_thread_id: "thread-group",
          source_thread_title: "发布排期讨论",
          message_count: 3,
        },
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
  });

  it("expands the original text on click and collapses on second click", () => {
    const container = mount(
      createElement(EnvelopeNotice, {
        meta: { source_thread_title: "发布排期讨论", message_count: 1 },
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

  it("falls back to a generic source label and omits the count when missing", () => {
    const container = mount(
      createElement(EnvelopeNotice, {
        meta: { source_thread_id: "thread-x" },
        text: "内容",
      }),
    );
    const toggle = container.querySelector<HTMLButtonElement>(
      ".envelope-notice-toggle",
    );
    expect(toggle?.textContent).toContain("收到来自其他会话的消息");
    expect(toggle?.textContent).not.toContain("条消息");
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
      envelope_meta: {
        source_thread_id: "thread-group",
        source_thread_title: "群聊",
        message_count: 2,
      },
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
});
