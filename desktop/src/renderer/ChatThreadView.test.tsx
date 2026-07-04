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
import type { ParticipantSummary, Turn } from "../shared/protocol";
import {
  CHAT_WINDOW_ROW_BATCH,
  ChatThreadView,
  findScrollParent,
  INITIAL_CHAT_WINDOW_ROWS,
} from "./ChatThreadView";

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
  it("renders a participant row with initial avatar, name, and bubble text", () => {
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
        typingParticipants: [],
      }),
    );
    const row = container.querySelector(".chat-row");
    expect(row).not.toBeNull();
    expect(container.querySelector(".chat-avatar")?.textContent).toBe("N");
    expect(container.querySelector(".chat-sender-name")?.textContent).toBe("Noel");
    expect(container.querySelector(".chat-bubble")?.textContent).toContain("早上好");
  });

  it("falls back to the first character of the name when there is no avatar image", () => {
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
        typingParticipants: [],
      }),
    );
    expect(container.querySelector(".chat-avatar")?.textContent).toBe("小");
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
        typingParticipants: [],
      }),
    );
    const img = container.querySelector(".chat-avatar img");
    expect(img).not.toBeNull();
    expect(img?.getAttribute("src")).toBe("data:image/png;base64,AAAA");
  });

  it("renders user rows right-aligned with no avatar", () => {
    const container = mount(
      createElement(ChatThreadView, {
        turns: turns([
          { id: "item-1", type: "user_message", text: "明天上线吗" },
        ]),
        typingParticipants: [],
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
        typingParticipants: [],
      }),
    );
    expect(container.querySelector(".envelope-notice")).not.toBeNull();
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
        typingParticipants: [],
      }),
    );
    expect(container.querySelector(".chat-decline-line")).not.toBeNull();
    expect(container.querySelector(".chat-bubble")).toBeNull();
  });

  it("renders a typing row per typing participant with an aria-label", () => {
    const container = mount(
      createElement(ChatThreadView, {
        turns: turns([]),
        typingParticipants: [noel],
      }),
    );
    const typingRow = container.querySelector(".chat-typing-row");
    expect(typingRow).not.toBeNull();
    expect(typingRow?.getAttribute("aria-label")).toBe("Noel 正在输入");
    expect(container.querySelectorAll(".chat-typing-dot").length).toBe(3);
  });

  it("never renders transcript-only items (agent_message)", () => {
    const container = mount(
      createElement(ChatThreadView, {
        turns: turns([
          { id: "item-1", type: "agent_message", text: "SECRET-TRANSCRIPT-TEXT" },
        ]),
        typingParticipants: [],
      }),
    );
    expect(container.textContent).not.toContain("SECRET-TRANSCRIPT-TEXT");
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
        typingParticipants: [],
      }),
    );
    const row = container.querySelector(".chat-row--focus");
    expect(row).not.toBeNull();
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
        typingParticipants: [],
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
        typingParticipants: [],
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
        typingParticipants: [],
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
        typingParticipants: [],
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
        typingParticipants: [],
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
        typingParticipants: [],
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
        typingParticipants: [],
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
        typingParticipants: [],
      }),
    );
    expect(container.querySelectorAll(".chat-row--user").length).toBe(80);

    act(() => {
      root.render(
        createElement(ChatThreadView, {
          turns: manyUserMessages(501),
          typingParticipants: [],
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
        typingParticipants: [],
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
          typingParticipants: [],
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
        typingParticipants: [],
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
          typingParticipants: [],
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
        typingParticipants: [],
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
          typingParticipants: [],
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
