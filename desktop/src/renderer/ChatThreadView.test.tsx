/**
 * Tests for ChatThreadView, the chat-style stream for DM and group
 * threads (docs/plans/2026-07-03-chat-style-threads-design.md §2, §4).
 * The view flattens turns through the whitelist filter in
 * chatMessagesFromTurns — this test asserts on the rendered DOM only,
 * mirroring the render-harness pattern of EnvelopeNotice.test.tsx.
 */
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it } from "vitest";
import { createElement } from "react";
import type {
  ConversationSubthread,
  ParticipantSummary,
  ThreadItem,
  Turn,
} from "../shared/protocol";
import {
  CHAT_WINDOW_ROW_BATCH,
  ChatThreadView,
  findScrollParent,
  INITIAL_CHAT_WINDOW_ROWS,
} from "./ChatThreadView";
import { aggregateMarksBySeq, REACTION_KEYS } from "./MessageMarks";

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

const noel: ParticipantSummary = {
  id: "prt-noel",
  name: "Noel",
  kind: "resident",
};

function turns(items: Turn["items"]): ReadonlyArray<Pick<Turn, "id" | "items">> {
  return [{ id: "turn-1", items }];
}

describe("ChatThreadView", () => {
  it("renders a read-receipt ring and reaction chips when marks are provided", () => {
    const marksBySeq = aggregateMarksBySeq([
      { seq: 7, participant_id: "prt-a", kind: "seen", status: "completed" },
      { seq: 7, participant_id: "prt-b", kind: "seen", status: "completed" },
      { seq: 7, participant_id: "prt-c", kind: "reaction", reaction: "smug" },
    ]);
    const container = mount(
      createElement(ChatThreadView, {
        turns: turns([
          { id: "item-1", seq: 7, type: "user_message", text: "上线了吗" },
        ]),
        marksBySeq,
        readerCount: 2,
      }),
    );
    const ring = container.querySelector(".chat-receipt-ring");
    expect(ring).not.toBeNull();
    expect(ring!.getAttribute("data-all-seen")).toBe("true");
    expect(container.querySelector('[data-reaction="smug"]')).not.toBeNull();
  });

  it("renders no marks affordances when none are provided", () => {
    const container = mount(
      createElement(ChatThreadView, {
        turns: turns([
          { id: "item-1", seq: 7, type: "user_message", text: "上线了吗" },
        ]),
      }),
    );
    expect(container.querySelector(".chat-bubble-marks")).toBeNull();
  });

  it("renders a participant row with default avatar, name, and bubble text", () => {
    const container = mount(
      createElement(ChatThreadView, {
        turns: turns([
          {
            id: "item-1",
            type: "participant_message",
            text: "早上好",
            post_kind: "result",
            participant: noel,
          },
        ]),
      }),
    );
    const row = container.querySelector(".chat-row");
    expect(row).not.toBeNull();
    expect(container.querySelector(".chat-avatar .default-avatar")).not.toBeNull();
    expect(container.querySelector(".chat-sender-name")?.textContent).toBe("Noel");
    expect(container.querySelector(".chat-bubble")?.textContent).toContain("早上好");
  });

  it("falls back to a generated default avatar when there is no avatar image", () => {
    const container = mount(
      createElement(ChatThreadView, {
        turns: turns([
          {
            id: "item-1",
            type: "participant_message",
            text: "hi",
            post_kind: "result",
            participant: { id: "prt-x", name: "小青", kind: "resident" },
          },
        ]),
      }),
    );
    // No uploaded image (a direct <img> child of the face); the mascot
    // art inside .default-avatar is the generated fallback.
    expect(container.querySelector(".chat-avatar .default-avatar img")).not.toBeNull();
    expect(container.querySelector(".chat-avatar-face > img")).toBeNull();
  });

  it("renders an <img> when avatar_image is a data URL", () => {
    const container = mount(
      createElement(ChatThreadView, {
        turns: turns([
          {
            id: "item-1",
            type: "participant_message",
            text: "hi",
            post_kind: "result",
            participant: {
              id: "prt-x",
              name: "小青",
              kind: "resident",
              avatar_image: "data:image/png;base64,AAAA",
            },
          },
        ]),
      }),
    );
    const img = container.querySelector(".chat-avatar img");
    expect(img).not.toBeNull();
    expect(img?.getAttribute("src")).toBe("data:image/png;base64,AAAA");
  });

  it("shows participant status on the message-flow avatar", () => {
    const container = mount(
      createElement(ChatThreadView, {
        busyParticipantIDs: new Set(["prt-noel"]),
        turns: turns([
          {
            id: "item-1",
            type: "participant_message",
            text: "我在处理",
            post_kind: "result",
            participant: noel,
          },
          {
            id: "item-2",
            type: "participant_message",
            text: "我在线",
            post_kind: "result",
            participant: { id: "prt-lee", name: "Lee", kind: "resident" },
          },
        ]),
      }),
    );

    const dots = container.querySelectorAll(".chat-avatar-status");
    expect(dots).toHaveLength(2);
    expect(dots[0]?.getAttribute("data-status")).toBe("busy");
    expect(dots[1]?.getAttribute("data-status")).toBe("online");
    expect(container.querySelector(".chat-avatar")?.getAttribute("aria-label")).toBe(
      "Noel 正在响应",
    );
    expect(container.querySelector(".chat-avatar-status-card")?.textContent).toContain(
      "正在响应",
    );
  });

  it("renders user rows right-aligned with no avatar", () => {
    const container = mount(
      createElement(ChatThreadView, {
        turns: turns([
          { id: "item-1", type: "user_message", text: "明天上线吗" },
        ]),
      }),
    );
    const row = container.querySelector(".chat-row--user");
    expect(row).not.toBeNull();
    expect(row?.querySelector(".chat-avatar")).toBeNull();
    expect(row?.textContent).toContain("明天上线吗");
  });

  it("renders envelope rows via EnvelopeNotice", () => {
    const container = mount(
      createElement(ChatThreadView, {
        turns: turns([
          {
            id: "item-1",
            type: "user_message",
            text: "信封正文",
            envelope_meta: [
              { source_thread_id: "thread-x", addressed: true, hop: 1 },
            ],
          },
        ]),
      }),
    );
    expect(container.querySelector(".envelope-notice")).not.toBeNull();
  });

  it("renders consecutive envelope rows as one collapsed notice", () => {
    const container = mount(
      createElement(ChatThreadView, {
        turns: [
          {
            id: "turn-1",
            items: [
              {
                id: "item-1",
                type: "user_message",
                text: "第一条群聊消息",
                envelope_meta: [
                  {
                    source_thread_id: "thread-x",
                    source_thread_title: "all",
                    addressed: false,
                    hop: 0,
                  },
                ],
              },
            ],
          },
          {
            id: "turn-2",
            items: [
              {
                id: "item-2",
                type: "user_message",
                text: "第二条群聊消息",
                envelope_meta: [
                  {
                    source_thread_id: "thread-x",
                    source_thread_title: "all",
                    addressed: true,
                    hop: 1,
                  },
                ],
              },
            ],
          },
        ],
      }),
    );
    const notices = container.querySelectorAll(".envelope-notice");
    expect(notices).toHaveLength(1);
    const toggle = container.querySelector<HTMLButtonElement>(
      ".envelope-notice-toggle",
    );
    expect(toggle?.textContent).toContain("收到来自「all」的 2 条消息");
    expect(toggle?.textContent).toContain("点名");
    expect(toggle?.textContent).toContain("转发×1");

    act(() => {
      toggle?.click();
    });
    const body = container.querySelector(".envelope-notice-body");
    expect(body?.textContent).toContain("第一条群聊消息");
    expect(body?.textContent).toContain("第二条群聊消息");
  });

  it("starts a new envelope notice after a user query", () => {
    const container = mount(
      createElement(ChatThreadView, {
        turns: turns([
          {
            id: "item-1",
            type: "user_message",
            text: "群聊消息一",
            envelope_meta: [
              { source_thread_id: "thread-x", source_thread_title: "all" },
            ],
          },
          {
            id: "item-2",
            type: "user_message",
            text: "这是我新发给 agent 的问题",
          },
          {
            id: "item-3",
            type: "user_message",
            text: "群聊消息二",
            envelope_meta: [
              { source_thread_id: "thread-x", source_thread_title: "all" },
            ],
          },
        ]),
      }),
    );
    expect(container.querySelectorAll(".envelope-notice")).toHaveLength(2);
    expect(container.querySelector(".chat-row--user")?.textContent).toContain(
      "这是我新发给 agent 的问题",
    );
  });

  it("renders a decline post_kind as a muted line, not a bubble", () => {
    const container = mount(
      createElement(ChatThreadView, {
        turns: turns([
          {
            id: "item-1",
            type: "participant_message",
            text: "无需回应",
            post_kind: "decline",
            participant: noel,
          },
        ]),
      }),
    );
    expect(container.querySelector(".chat-decline-line")).not.toBeNull();
    expect(container.querySelector(".chat-bubble")).toBeNull();
  });

  it("never renders transcript-only items (agent_message)", () => {
    const container = mount(
      createElement(ChatThreadView, {
        turns: turns([
          { id: "item-1", type: "agent_message", text: "SECRET-TRANSCRIPT-TEXT" },
        ]),
      }),
    );
    expect(container.textContent).not.toContain("SECRET-TRANSCRIPT-TEXT");
  });
});

// --- Reply folding cards + task activity cards + reply badges ----------

describe("ChatThreadView reply / task affordances", () => {
  function subthread(over: Partial<ConversationSubthread>): ConversationSubthread {
    return {
      id: "cth-1",
      thread_id: "thread-1",
      anchor_item_id: "item-1",
      status: "open",
      created_at: "",
      reply_count: 0,
      ...over,
    };
  }

  it("renders an in-stream task_card as a task card (进行中/条数)", () => {
    const container = mount(
      createElement(ChatThreadView, {
        turns: turns([
          {
            id: "item-1",
            type: "task_card",
            task: {
              id: "task-1",
              name: "跑测试",
              status: "running",
              reply_count: 3,
            },
          },
        ]),
      }),
    );
    const card = container.querySelector(".chat-row--task .task-card");
    expect(card).not.toBeNull();
    expect(container.querySelector(".task-card-title")?.textContent).toBe("跑测试");
    expect(container.textContent).toContain("进行中");
    expect(container.textContent).toContain("3 条回复");
  });

  it("hangs a 'N 条回复' badge under a message that anchors a plain reply", () => {
    const map = new Map<string, ConversationSubthread>([
      ["item-1", subthread({ reply_count: 4 })],
    ]);
    const container = mount(
      createElement(ChatThreadView, {
        turns: turns([
          { id: "item-1", type: "user_message", text: "改这里" },
        ]),
        subthreadsByAnchor: map,
      }),
    );
    const badge = container.querySelector(".chat-reply-badge");
    expect(badge).not.toBeNull();
    expect(badge?.textContent).toBe("4 条回复");
  });

  it("caps the reply badge at 99+", () => {
    const map = new Map<string, ConversationSubthread>([
      ["item-1", subthread({ reply_count: 250 })],
    ]);
    const container = mount(
      createElement(ChatThreadView, {
        turns: turns([
          {
            id: "item-1",
            type: "participant_message",
            text: "刷屏",
            post_kind: "result",
            participant: noel,
          },
        ]),
        subthreadsByAnchor: map,
      }),
    );
    expect(container.querySelector(".chat-reply-badge")?.textContent).toBe(
      "99+ 条回复",
    );
  });

  it("reveals a hover 回复 entry (toolbar) on messages with no reply thread yet", () => {
    const opened: ThreadItem[] = [];
    const container = mount(
      createElement(ChatThreadView, {
        turns: turns([
          { id: "item-1", type: "user_message", text: "改这里" },
        ]),
        onOpenSubthread: (item: ThreadItem) => opened.push(item),
      }),
    );
    const reply = container.querySelector<HTMLButtonElement>(
      ".chat-bubble-toolbar-reply",
    );
    expect(reply).not.toBeNull();
    expect(reply?.textContent).toContain("回复");
    act(() => {
      reply!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    expect(opened.map((item) => item.id)).toEqual(["item-1"]);
  });

  it("renders no toolbar reply entry when onOpenSubthread is not wired", () => {
    const container = mount(
      createElement(ChatThreadView, {
        turns: turns([
          { id: "item-1", type: "user_message", text: "改这里" },
        ]),
        onReact: () => {},
      }),
    );
    expect(container.querySelector(".chat-bubble-toolbar-reply")).toBeNull();
  });

  it("opens the reply on badge click via onOpenSubthread", () => {
    const opened: ThreadItem[] = [];
    const map = new Map<string, ConversationSubthread>([
      ["item-1", subthread({ reply_count: 2 })],
    ]);
    const container = mount(
      createElement(ChatThreadView, {
        turns: turns([
          { id: "item-1", type: "user_message", text: "改这里" },
        ]),
        subthreadsByAnchor: map,
        onOpenSubthread: (item: ThreadItem) => opened.push(item),
      }),
    );
    const button = container.querySelector<HTMLButtonElement>(
      ".chat-reply-badge--button",
    );
    expect(button).not.toBeNull();
    act(() => {
      button!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    expect(opened).toHaveLength(1);
    expect(opened[0]?.id).toBe("item-1");
  });

  it("renders a task activity card instead of a badge once the reply is escalated", () => {
    const map = new Map<string, ConversationSubthread>([
      [
        "item-1",
        subthread({
          status: "task",
          reply_count: 6,
          task: {
            id: "task-9",
            name: "重构路由",
            status: "completed",
            description: "已合并到主流",
          },
        }),
      ],
    ]);
    const container = mount(
      createElement(ChatThreadView, {
        turns: turns([
          { id: "item-1", type: "user_message", text: "起个任务" },
        ]),
        subthreadsByAnchor: map,
      }),
    );
    // The escalated reply shows the shared task card (result summary), not the
    // plain "N 条回复" badge.
    expect(container.querySelector(".chat-reply-badge")).toBeNull();
    const card = container.querySelector(".chat-reply-task .task-card");
    expect(card).not.toBeNull();
    expect(container.querySelector(".task-card-title")?.textContent).toBe(
      "重构路由",
    );
    expect(container.textContent).toContain("已完成");
    expect(container.textContent).toContain("已合并到主流");
  });

  it("renders no reply affordance for a message without a subthread", () => {
    const container = mount(
      createElement(ChatThreadView, {
        turns: turns([
          { id: "item-1", type: "user_message", text: "普通消息" },
        ]),
        subthreadsByAnchor: new Map(),
      }),
    );
    expect(container.querySelector(".chat-reply-badge")).toBeNull();
    expect(container.querySelector(".chat-reply-task")).toBeNull();
  });
});

describe("ChatThreadView message hover toolbar", () => {
  function toolbar(
    container: Element,
    selector = ".chat-bubble--user",
  ): HTMLElement | null {
    return (
      container
        .querySelector(selector)
        ?.querySelector<HTMLElement>('[data-testid="chat-bubble-toolbar"]') ??
      null
    );
  }

  it("renders a 贴表情 trigger + 回复 button (no ⋯) on a user bubble when both wired", () => {
    const container = mount(
      createElement(ChatThreadView, {
        turns: turns([{ id: "item-1", type: "user_message", text: "改这里" }]),
        onOpenSubthread: () => {},
        onReact: () => {},
      }),
    );
    const bar = toolbar(container);
    expect(bar).not.toBeNull();
    // A single picker trigger plus the reply button — and nothing else.
    const trigger = bar!.querySelector<HTMLButtonElement>(".chat-bubble-toolbar-react");
    expect(trigger).not.toBeNull();
    expect(trigger!.getAttribute("aria-expanded")).toBe("false");
    expect(bar!.querySelector(".chat-bubble-toolbar-reply")).not.toBeNull();
    // The ⋯ overflow was intentionally dropped: the two affordances are the
    // whole toolbar.
    expect(bar!.querySelector(".chat-bubble-toolbar-more")).toBeNull();

    // Opening the picker shows every reaction key, large + labelled.
    act(() => {
      trigger!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    expect(trigger!.getAttribute("aria-expanded")).toBe("true");
    const options = bar!.querySelectorAll(".chat-reaction-picker-option");
    expect(options.length).toBe(REACTION_KEYS.length);
    expect(
      bar!.querySelector('.chat-reaction-picker-option[data-reaction="nice"] .chat-reaction-picker-label')
        ?.textContent,
    ).toBe("赞同");
  });

  it("stamps a reaction via the picker with the key + clicked message, then closes", () => {
    const reacted: Array<{ id?: string; reaction: string }> = [];
    const container = mount(
      createElement(ChatThreadView, {
        turns: turns([
          { id: "item-1", seq: 5, type: "user_message", text: "改这里" },
        ]),
        onOpenSubthread: () => {},
        onReact: (item: ThreadItem, reaction: string) =>
          reacted.push({ id: item.id, reaction }),
      }),
    );
    const bar = toolbar(container)!;
    act(() => {
      bar
        .querySelector<HTMLButtonElement>(".chat-bubble-toolbar-react")!
        .dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    const nice = bar.querySelector<HTMLButtonElement>(
      '.chat-reaction-picker-option[data-reaction="nice"]',
    );
    expect(nice).not.toBeNull();
    act(() => {
      nice!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    expect(reacted).toEqual([{ id: "item-1", reaction: "nice" }]);
    // Picking closes the panel.
    expect(bar.querySelector(".chat-reaction-picker")).toBeNull();
  });

  it("opens the reply subthread via onOpenSubthread on the clicked participant message", () => {
    const opened: ThreadItem[] = [];
    const container = mount(
      createElement(ChatThreadView, {
        turns: turns([
          {
            id: "item-9",
            type: "participant_message",
            text: "看这里",
            post_kind: "result",
            participant: noel,
          },
        ]),
        onOpenSubthread: (item: ThreadItem) => opened.push(item),
        onReact: () => {},
      }),
    );
    const reply = toolbar(
      container,
      ".chat-row--participant .chat-bubble",
    )!.querySelector<HTMLButtonElement>(".chat-bubble-toolbar-reply");
    expect(reply).not.toBeNull();
    act(() => {
      reply!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    expect(opened).toHaveLength(1);
    expect(opened[0]?.id).toBe("item-9");
  });

  it("omits 回复 when replies are disabled (一层不嵌套: inside a cth), keeps 贴表情", () => {
    // A reply-panel reuse of this view passes no onOpenSubthread, so a message
    // already inside a cth offers only the reaction row — never a nested reply.
    const container = mount(
      createElement(ChatThreadView, {
        turns: turns([{ id: "item-1", type: "user_message", text: "回复线内" }]),
        onReact: () => {},
      }),
    );
    const bar = toolbar(container);
    expect(bar).not.toBeNull();
    expect(bar!.querySelector(".chat-bubble-toolbar-react")).not.toBeNull();
    expect(bar!.querySelector(".chat-bubble-toolbar-reply")).toBeNull();
  });

  it("renders no toolbar when neither reply nor react is wired", () => {
    const container = mount(
      createElement(ChatThreadView, {
        turns: turns([{ id: "item-1", type: "user_message", text: "只读" }]),
      }),
    );
    expect(toolbar(container)).toBeNull();
  });
});

// --- Workspace-focus divider rows ------------------------------------

describe("ChatThreadView focus divider rows", () => {
  it("renders the all-workspaces divider", () => {
    const container = mount(
      createElement(ChatThreadView, {
        turns: turns([
          {
            id: "item-1",
            type: "user_message",
            focus_meta: { kind: "all" },
          },
        ]),
      }),
    );
    const row = container.querySelector(".chat-row--focus");
    expect(row).not.toBeNull();
    expect(
      container.querySelector(".chat-focus-divider")?.classList.contains(
        "chat-inline-divider",
      ),
    ).toBe(true);
    expect(container.querySelector(".chat-focus-divider-label")?.textContent).toBe(
      "⬒ 全部工作区",
    );
  });

  it("renders the personal-space divider", () => {
    const container = mount(
      createElement(ChatThreadView, {
        turns: turns([
          {
            id: "item-1",
            type: "user_message",
            focus_meta: { kind: "home" },
          },
        ]),
      }),
    );
    expect(container.querySelector(".chat-focus-divider-label")?.textContent).toBe(
      "⌂ 个人",
    );
  });

  it("renders the named-workspace divider using the focus_meta name", () => {
    const container = mount(
      createElement(ChatThreadView, {
        turns: turns([
          {
            id: "item-1",
            type: "user_message",
            focus_meta: { kind: "workspace", name: "wuu", root: "/home/user/wuu" },
          },
        ]),
      }),
    );
    expect(container.querySelector(".chat-focus-divider-label")?.textContent).toBe(
      "⬒ wuu",
    );
  });

  it("falls back to root, then a generic label, when a workspace focus has no name", () => {
    const withRoot = mount(
      createElement(ChatThreadView, {
        turns: turns([
          {
            id: "item-1",
            type: "user_message",
            focus_meta: { kind: "workspace", root: "/home/user/scratch" },
          },
        ]),
      }),
    );
    expect(
      withRoot.querySelector(".chat-focus-divider-label")?.textContent,
    ).toBe("⬒ /home/user/scratch");

    const withNeither = mount(
      createElement(ChatThreadView, {
        turns: turns([
          { id: "item-1", type: "user_message", focus_meta: { kind: "workspace" } },
        ]),
      }),
    );
    expect(
      withNeither.querySelector(".chat-focus-divider-label")?.textContent,
    ).toBe("⬒ 工作区");
  });

  it("counts focus divider rows toward the chat window like any other row", () => {
    installIntersectionObserverStub();
    const items: Turn["items"] = [];
    for (let i = 0; i < 90; i += 1) {
      items.push({ id: `item-${i}`, type: "user_message", text: `msg-${i}` });
    }
    items.splice(45, 0, {
      id: "focus-mid",
      type: "user_message",
      focus_meta: { kind: "home" },
    });
    const { container } = mountRoot(
      createElement(ChatThreadView, {
        turns: [{ id: "turn-bulk", items }],
      }),
    );
    // 91 total rows, window 80 -> 11 hidden, 80 rendered — the extra
    // focus row does not throw off the windowing math or get dropped.
    expect(container.querySelectorAll(".chat-row").length).toBe(
      INITIAL_CHAT_WINDOW_ROWS,
    );
    expect(container.querySelector(".chat-row--focus")).not.toBeNull();
    uninstallIntersectionObserverStub();
  });

  it("renders pending sends as dimmed user bubbles with a 发送中 hint (issue #10)", () => {
    const container = mount(
      createElement(ChatThreadView, {
        turns: turns([
          { id: "item-1", type: "user_message", text: "你们好" },
        ]),
        pendingMessages: [
          { id: "pending-1", text: "自我介绍", images: [], files: [] },
        ],
      }),
    );
    const pendingRow = container.querySelector(".chat-row--pending");
    expect(pendingRow).not.toBeNull();
    // Reads as a normal outgoing chat bubble, not a queue strip.
    expect(pendingRow?.classList.contains("chat-row--user")).toBe(true);
    expect(
      pendingRow?.querySelector(".chat-bubble--pending")?.textContent,
    ).toContain("自我介绍");
    expect(pendingRow?.querySelector(".chat-pending-hint")?.textContent).toBe(
      "发送中…",
    );
    // The already-delivered message renders as a regular row before it.
    const rows = Array.from(container.querySelectorAll(".chat-row"));
    expect(rows.length).toBe(2);
    expect(rows[1]).toBe(pendingRow);
  });

  it("shows an attachment summary inside a pending bubble", () => {
    const container = mount(
      createElement(ChatThreadView, {
        turns: [],
        pendingMessages: [
          {
            id: "pending-1",
            text: "",
            images: [{ id: "img-1", media_type: "image/png", data: "" }],
            files: [],
          },
        ],
      }),
    );
    expect(
      container.querySelector(".chat-pending-attachments")?.textContent,
    ).toBe("1 张图片");
  });
});

// --- Chat-view windowing (open on latest, reveal older on scroll-up,
// window only grows) -------------------------------------------------

function manyUserMessages(
  count: number,
): ReadonlyArray<Pick<Turn, "id" | "items">> {
  const items: Turn["items"] = [];
  for (let i = 0; i < count; i += 1) {
    items.push({ id: `item-${i}`, type: "user_message", text: `msg-${i}` });
  }
  return [{ id: "turn-bulk", items }];
}

function mountRoot(element: React.ReactElement): {
  container: HTMLElement;
  root: Root;
} {
  const container = document.createElement("div");
  document.body.appendChild(container);
  const root = createRoot(container);
  act(() => {
    root.render(element);
  });
  mountedRoots.push(root);
  mountedContainers.push(container);
  return { container, root };
}

type IntersectionCallback = (
  entries: IntersectionObserverEntry[],
  observer: IntersectionObserver,
) => void;

class MockIntersectionObserver implements IntersectionObserver {
  readonly root: Element | Document | null = null;
  readonly rootMargin: string = "";
  readonly thresholds: ReadonlyArray<number> = [];
  observedNodes: Element[] = [];
  callback: IntersectionCallback;

  constructor(callback: IntersectionCallback) {
    this.callback = callback;
    mockObservers.push(this);
  }
  observe(node: Element): void {
    this.observedNodes.push(node);
  }
  unobserve(node: Element): void {
    this.observedNodes = this.observedNodes.filter((n) => n !== node);
  }
  disconnect(): void {
    this.observedNodes = [];
  }
  takeRecords(): IntersectionObserverEntry[] {
    return [];
  }
}

let mockObservers: MockIntersectionObserver[] = [];

function installIntersectionObserverStub(): void {
  mockObservers = [];
  (
    globalThis as { IntersectionObserver?: typeof IntersectionObserver }
  ).IntersectionObserver = MockIntersectionObserver as typeof IntersectionObserver;
}

function uninstallIntersectionObserverStub(): void {
  Reflect.deleteProperty(globalThis, "IntersectionObserver");
  mockObservers = [];
}

// Simulates the sentinel scrolling into view: finds the (still-connected)
// mock observer instance watching `.chat-window-sentinel` and fires its
// callback with an intersecting entry, exactly like the real
// IntersectionObserver would when the reader scrolls to the top of the
// currently-rendered window.
function triggerSentinelIntersection(container: HTMLElement): void {
  const sentinel = container.querySelector(".chat-window-sentinel");
  expect(sentinel).not.toBeNull();
  const observer = [...mockObservers]
    .reverse()
    .find((instance) => instance.observedNodes.includes(sentinel as Element));
  expect(observer).toBeDefined();
  act(() => {
    observer!.callback(
      [
        {
          isIntersecting: true,
          target: sentinel,
        } as unknown as IntersectionObserverEntry,
      ],
      observer!,
    );
  });
}

describe("ChatThreadView windowing", () => {
  afterEach(() => {
    uninstallIntersectionObserverStub();
  });

  it("opens on only the most recent INITIAL_CHAT_WINDOW_ROWS rows", () => {
    installIntersectionObserverStub();
    const { container } = mountRoot(
      createElement(ChatThreadView, {
        turns: manyUserMessages(500),
      }),
    );
    const rows = container.querySelectorAll(".chat-row--user");
    expect(rows.length).toBe(INITIAL_CHAT_WINDOW_ROWS);
    expect(rows[0].textContent).toContain("msg-420");
    expect(rows[rows.length - 1].textContent).toContain("msg-499");
    expect(container.querySelector(".chat-window-sentinel")).not.toBeNull();
  });

  it("reveals the next batch when the top sentinel intersects", () => {
    installIntersectionObserverStub();
    const { container } = mountRoot(
      createElement(ChatThreadView, {
        turns: manyUserMessages(500),
      }),
    );
    expect(container.querySelectorAll(".chat-row--user").length).toBe(80);

    triggerSentinelIntersection(container);

    const rows = container.querySelectorAll(".chat-row--user");
    expect(rows.length).toBe(80 + CHAT_WINDOW_ROW_BATCH);
    expect(rows[0].textContent).toContain("msg-340");
  });

  it("keeps the visible window intact — new messages only append at the bottom", () => {
    installIntersectionObserverStub();
    const { container, root } = mountRoot(
      createElement(ChatThreadView, {
        turns: manyUserMessages(500),
      }),
    );
    expect(container.querySelectorAll(".chat-row--user").length).toBe(80);

    act(() => {
      root.render(
        createElement(ChatThreadView, {
          turns: manyUserMessages(501),
        }),
      );
    });

    const rows = container.querySelectorAll(".chat-row--user");
    // The window grew by exactly the one new message, not reset to 80 —
    // nothing that was already visible got pushed back out of view.
    expect(rows.length).toBe(81);
    expect(rows[0].textContent).toContain("msg-420");
    expect(rows[rows.length - 1].textContent).toContain("msg-500");
  });

  it("reopens on the latest window when rows shrink past the hidden count (history reset)", () => {
    installIntersectionObserverStub();
    const { container, root } = mountRoot(
      createElement(ChatThreadView, {
        turns: manyUserMessages(500),
      }),
    );
    // 500 rows -> 420 hidden. Shrinking to 100 rows leaves the old
    // hidden count above the whole history; merely clamping it to
    // rows.length would render an empty slice — a blank chat that in
    // jsdom (no layout, so the sentinel never intersects) would never
    // self-heal. The view must instead reopen on the latest
    // INITIAL_CHAT_WINDOW_ROWS, exactly as a fresh mount of the
    // rewritten history would.
    expect(container.querySelectorAll(".chat-row--user").length).toBe(80);

    act(() => {
      root.render(
        createElement(ChatThreadView, {
          turns: manyUserMessages(100),
        }),
      );
    });

    const rows = container.querySelectorAll(".chat-row--user");
    expect(rows.length).toBe(INITIAL_CHAT_WINDOW_ROWS);
    expect(rows[0].textContent).toContain("msg-20");
    expect(rows[rows.length - 1].textContent).toContain("msg-99");
    expect(container.querySelector(".chat-window-sentinel")).not.toBeNull();
  });

  it("resets the window when the caller remounts for a different thread (key change)", () => {
    installIntersectionObserverStub();
    const { container, root } = mountRoot(
      createElement(ChatThreadView, {
        key: "thread-a",
        turns: manyUserMessages(500),
      }),
    );
    expect(container.querySelectorAll(".chat-row--user").length).toBe(80);

    triggerSentinelIntersection(container);
    expect(container.querySelectorAll(".chat-row--user").length).toBe(160);

    act(() => {
      root.render(
        createElement(ChatThreadView, {
          key: "thread-b",
          turns: manyUserMessages(500),
        }),
      );
    });

    // A fresh mount for the new thread opens on the latest 80 again —
    // the previous thread's reveal state does not leak across threads.
    expect(container.querySelectorAll(".chat-row--user").length).toBe(80);
  });

  it("renders every row with no sentinel when there are fewer than the initial window size", () => {
    installIntersectionObserverStub();
    const { container } = mountRoot(
      createElement(ChatThreadView, {
        turns: manyUserMessages(5),
      }),
    );
    expect(container.querySelectorAll(".chat-row--user").length).toBe(5);
    expect(container.querySelector(".chat-window-sentinel")).toBeNull();
  });

  it("compensates scrollTop on the real scroll ancestor after revealing a batch", () => {
    installIntersectionObserverStub();
    // Build a synthetic scroll ancestor above the mount point — jsdom
    // never computes real layout, so scrollHeight/clientHeight are
    // stubbed by hand: scrollHeight tracks the number of rendered
    // `.chat-row` elements (mirroring how a real browser's scrollHeight
    // grows as more rows are inserted), clientHeight stays fixed.
    const scrollAncestor = document.createElement("div");
    scrollAncestor.style.overflowY = "auto";
    const mountPoint = document.createElement("div");
    scrollAncestor.appendChild(mountPoint);
    document.body.appendChild(scrollAncestor);
    Object.defineProperty(scrollAncestor, "clientHeight", {
      value: 400,
      configurable: true,
    });
    Object.defineProperty(scrollAncestor, "scrollHeight", {
      get: () => mountPoint.querySelectorAll(".chat-row").length * 40 + 400,
      configurable: true,
    });
    scrollAncestor.scrollTop = 0;

    const root = createRoot(mountPoint);
    act(() => {
      root.render(
        createElement(ChatThreadView, {
          turns: manyUserMessages(500),
        }),
      );
    });
    mountedRoots.push(root);
    mountedContainers.push(scrollAncestor);

    expect(findScrollParent(mountPoint.querySelector(".chat-thread"))).toBe(
      scrollAncestor,
    );

    triggerSentinelIntersection(mountPoint);

    // 80 rows before -> scrollHeight 3600; 160 rows after -> 6800. The
    // reader was pinned at scrollTop 0, so the compensation must add
    // exactly the height inserted above the viewport.
    expect(scrollAncestor.scrollTop).toBe(3200);
  });
});

describe("findScrollParent", () => {
  it("returns the nearest ancestor with overflow-y auto/scroll and real overflow", () => {
    const outer = document.createElement("div");
    outer.style.overflowY = "auto";
    Object.defineProperty(outer, "scrollHeight", {
      value: 1000,
      configurable: true,
    });
    Object.defineProperty(outer, "clientHeight", {
      value: 400,
      configurable: true,
    });
    const inner = document.createElement("div");
    outer.appendChild(inner);
    const leaf = document.createElement("div");
    inner.appendChild(leaf);
    document.body.appendChild(outer);

    expect(findScrollParent(leaf)).toBe(outer);

    outer.remove();
  });

  it("returns null when nothing scrolls (matches jsdom, which never computes layout)", () => {
    const outer = document.createElement("div");
    const leaf = document.createElement("div");
    outer.appendChild(leaf);
    document.body.appendChild(outer);

    expect(findScrollParent(leaf)).toBeNull();

    outer.remove();
  });

  it("returns null for a root node with no parent", () => {
    const lonely = document.createElement("div");
    expect(findScrollParent(lonely)).toBeNull();
  });
});
