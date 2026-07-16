/**
 * Tests for ConversationSubthreadPanel — the left-right split reply panel
 * (群中群). It renders a reply subthread's (cth) message stream through the SAME
 * full conversation view the main chat uses (ChatThreadView via its container),
 * carries the human-click 升级为 Task gate, and posts human replies back into the
 * cth. Render-harness pattern, mirroring ChatThreadView.test.tsx.
 */
import { act } from "react";
import { createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import type {
  ConversationSubthread,
  ParticipantSummary,
  ThreadItem,
  WuuDesktopApi,
} from "../shared/protocol";
import { ConversationSubthreadPanel } from "./ConversationSubthreadPanel";
import { I18nProvider, setActiveLocale } from "./i18n";
import { REACTION_KEYS } from "./MessageMarks";

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

function mountRerenderable(element: React.ReactElement): {
  container: HTMLElement;
  rerender: (next: React.ReactElement) => void;
} {
  const container = document.createElement("div");
  document.body.appendChild(container);
  const root = createRoot(container);
  act(() => {
    root.render(element);
  });
  mountedRoots.push(root);
  mountedContainers.push(container);
  return {
    container,
    rerender: (next) => {
      act(() => {
        root.render(next);
      });
    },
  };
}

function deferred<T>(): {
  promise: Promise<T>;
  resolve: (value: T) => void;
} {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
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
  delete (window as unknown as { wuu?: unknown }).wuu;
  setActiveLocale("zh-CN");
  vi.restoreAllMocks();
});

const noel: ParticipantSummary = {
  id: "prt-noel",
  name: "Noel",
  kind: "resident",
};

function subthreadWith(
  overrides: Partial<ConversationSubthread> = {},
): ConversationSubthread {
  return {
    id: "cth-1",
    thread_id: "group-1",
    anchor_item_id: "seq-3",
    title: "重试路径",
    status: "open",
    created_by: "prt-noel",
    thread_owner_participant_id: "prt-noel",
    created_at: "0",
    reply_count: 1,
    turns: [
      {
        id: "turn-1",
        items_view: "full",
        status: "completed",
        items: [
          {
            id: "item-1",
            seq: 12,
            type: "participant_message",
            text: "在看重试逻辑",
            post_kind: "result",
            participant: noel,
          },
        ],
      },
    ],
    ...overrides,
  };
}

describe("ConversationSubthreadPanel", () => {
  it("renders the cth stream through the full chat conversation view", () => {
    const container = mount(
      createElement(ConversationSubthreadPanel, {
        threadID: "group-1",
        subthread: subthreadWith(),
        onClose: () => {},
        onResolve: () => {},
      }),
    );
    // Same chat-style stream as the main thread (a .chat-row / .chat-bubble),
    // not the old stripped-down turn transcript.
    expect(container.querySelector(".chat-thread")).not.toBeNull();
    expect(
      container.querySelector(".chat-row--participant .chat-bubble")?.textContent,
    ).toContain("在看重试逻辑");
    expect(container.querySelector(".conversation-subthread-meta")?.textContent).toBe(
      "收敛中 · 1 条回复",
    );
  });

  it("renders interface labels in English while preserving task data", () => {
    window.wuu = {
      initialLanguagePreference: "en-US",
      initialSystemLocale: "en-US",
    } as unknown as WuuDesktopApi;
    const task = subthreadWith({
      title: "验证修复",
      status: "task",
      exec_state: "awaiting_lead",
      reply_count: 2,
      task: { id: "cth-1", status: "running", subthread_id: "cth-1" },
      plan: [
        {
          id: "verify",
          title: "保留原始任务标题",
          status: "failed",
          failure_reason: "后端原始失败原因",
          attempts: 2,
        },
      ],
    });
    const container = mount(
      <I18nProvider>
        <ConversationSubthreadPanel
          threadID="group-1"
          subthread={task}
          onClose={() => {}}
          onResolve={() => {}}
        />
      </I18nProvider>,
    );

    expect(container.querySelector(".conversation-subthread-meta")?.textContent)
      .toBe("Waiting for Lead review · 2 replies");
    expect(container.textContent).toContain("The Lead is reviewing worker results");
    expect(container.textContent).toContain("Tried 2 times");
    expect(container.textContent).toContain("Reason: 后端原始失败原因");
    expect(container.textContent).toContain("保留原始任务标题");
  });

  it("shows the source and owner, then upgrades without a second lead choice", () => {
    let escalations = 0;
    const container = mount(
      createElement(ConversationSubthreadPanel, {
        threadID: "group-1",
        subthread: subthreadWith(),
        sourceItem: {
          id: "seq-3",
          type: "user_message",
          text: "先把重试路径讨论清楚",
        },
        resolveParticipantName: (id: string) =>
          id === "prt-noel" ? "Noel" : id,
        onClose: () => {},
        onResolve: () => {},
        onEscalate: () => {
          escalations += 1;
        },
      }),
    );
    expect(container.querySelector(".conversation-subthread-source")?.textContent)
      .toContain("先把重试路径讨论清楚");
    expect(container.querySelector(".conversation-subthread-owner")?.textContent)
      .toBe("Owner · Noel");
    const button = container.querySelector<HTMLButtonElement>(
      ".conversation-subthread-escalate",
    );
    expect(button?.disabled).toBe(false);
    act(() => {
      button!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    expect(escalations).toBe(1);
    expect(container.querySelector("[aria-label='Task lead']")).toBeNull();
  });

  it("does not allow a Thread without an owner to become a Task", () => {
    const container = mount(
      createElement(ConversationSubthreadPanel, {
        threadID: "group-1",
        subthread: subthreadWith({ thread_owner_participant_id: undefined }),
        onClose: () => {},
        onResolve: () => {},
        onEscalate: () => {},
      }),
    );
    const gate = container.querySelector<HTMLButtonElement>(
      ".conversation-subthread-escalate",
    );
    expect(gate?.disabled).toBe(true);
  });

  it("hides 升级为 Task once the reply is already a task", () => {
    const container = mount(
      createElement(ConversationSubthreadPanel, {
        threadID: "group-1",
        subthread: subthreadWith({
          status: "task",
          task: { id: "cth-1", status: "running", subthread_id: "cth-1" },
        }),
        onClose: () => {},
        onResolve: () => {},
        onEscalate: () => {},
      }),
    );
    expect(
      container.querySelector(".conversation-subthread-escalate"),
    ).toBeNull();
  });

  it("renders the host-provided full composer slot in the footer (not a stripped shell)", () => {
    // The panel no longer self-builds a one-line textarea + send arrow; the
    // host passes in the SAME full conversation composer the main dock uses,
    // rendered where the old stripped footer sat.
    const container = mount(
      createElement(ConversationSubthreadPanel, {
        threadID: "group-1",
        subthread: subthreadWith(),
        onClose: () => {},
        onResolve: () => {},
        composer: createElement(
          "div",
          { className: "test-composer-slot" },
          "COMPOSER",
        ),
      }),
    );
    const footer = container.querySelector(".conversation-subthread-composer");
    expect(footer).not.toBeNull();
    expect(footer!.querySelector(".test-composer-slot")).not.toBeNull();
    // The old self-built stripped footer input/send are gone.
    expect(container.querySelector(".conversation-subthread-send")).toBeNull();
  });

  it("does not render the composer footer without a slot", () => {
    const container = mount(
      createElement(ConversationSubthreadPanel, {
        threadID: "group-1",
        subthread: subthreadWith(),
        onClose: () => {},
        onResolve: () => {},
      }),
    );
    expect(
      container.querySelector(".conversation-subthread-composer"),
    ).toBeNull();
  });

  it("offers only 贴表情 on a cth message hover toolbar (一层不嵌套)", () => {
    const container = mount(
      createElement(ConversationSubthreadPanel, {
        threadID: "group-1",
        subthread: subthreadWith(),
        onClose: () => {},
        onResolve: () => {},
        onReact: (_item: ThreadItem, _reaction: string) => {},
      }),
    );
    const toolbar = container
      .querySelector(".chat-row--participant .chat-bubble")!
      .querySelector<HTMLElement>('[data-testid="chat-bubble-toolbar"]');
    expect(toolbar).not.toBeNull();
    // A reply panel never offers a nested 回复 — the toolbar shows only the
    // 贴表情 trigger (whose picker carries every reaction key), never a
    // reply button.
    const trigger = toolbar!.querySelector<HTMLButtonElement>(
      ".chat-bubble-toolbar-react",
    );
    expect(trigger).not.toBeNull();
    act(() => {
      trigger!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    expect(
      toolbar!.querySelectorAll(".chat-reaction-picker-option").length,
    ).toBe(REACTION_KEYS.length);
    expect(toolbar!.querySelector(".chat-bubble-toolbar-reply")).toBeNull();
  });

  it("shows Task lead and runtime state without human resolve or completion controls", () => {
    const taskSubthread = subthreadWith({
      status: "task",
      exec_state: "awaiting_lead",
      task: { id: "cth-1", status: "running", subthread_id: "cth-1" },
    });
    const container = mount(
      createElement(ConversationSubthreadPanel, {
        threadID: "group-1",
        subthread: taskSubthread,
        onClose: () => {},
        onResolve: () => {},
        onEscalate: () => {},
        resolveParticipantName: () => "Noel",
      }),
    );
    expect(container.querySelector(".conversation-subthread-overview-meta")?.textContent)
      .toContain("等待 Lead 验收");
    expect(container.querySelector(".conversation-subthread-overview-meta")?.textContent)
      .toContain("Lead · Noel");
    expect(container.textContent).toContain("Lead 正在验收 worker 结果");
    expect(container.querySelector(".conversation-subthread-finalize-toggle"))
      .toBeNull();
    expect(container.querySelector('button[aria-label="标记已解决"]')).toBeNull();
  });

  it("shows explicit WorkItem details without inventing liveness warnings", () => {
    const container = mount(
      createElement(ConversationSubthreadPanel, {
        threadID: "group-1",
        subthread: subthreadWith({
          status: "task",
          task: { id: "cth-1", status: "running", subthread_id: "cth-1" },
          plan: [
            {
              id: "verify",
              title: "验证修复",
              status: "blocked",
              assignee: "prt-noel",
              depends_on: ["implement"],
              failure_reason: "等待测试环境",
              attempts: 2,
              current_attempt_id: "tat-2",
              last_activity_at: "2020-01-01T00:00:00Z",
              last_progress_at: "2020-01-01T00:00:00Z",
            },
          ],
        }),
        onClose: () => {},
        onResolve: () => {},
      }),
    );
    expect(container.textContent).toContain("等待：implement");
    expect(container.textContent).toContain("等待测试环境");
    expect(container.textContent).toContain("第 2 次尝试");
    expect(container.textContent).not.toContain("疑似失联");
    expect(container.textContent).not.toContain("进展慢");
  });

  it("shows 弹出独立窗口 and fires onPopOut when clicked", () => {
    let popped = 0;
    const container = mount(
      createElement(ConversationSubthreadPanel, {
        threadID: "group-1",
        subthread: subthreadWith(),
        onClose: () => {},
        onResolve: () => {},
        onPopOut: () => {
          popped += 1;
        },
      }),
    );
    const button = container.querySelector<HTMLButtonElement>(
      'button[aria-label="弹出独立窗口"]',
    );
    expect(button).not.toBeNull();
    act(() => {
      button!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    expect(popped).toBe(1);
  });

  it("hides 弹出独立窗口 when onPopOut is absent (already detached / no handler)", () => {
    const container = mount(
      createElement(ConversationSubthreadPanel, {
        threadID: "group-1",
        subthread: subthreadWith(),
        onClose: () => {},
        onResolve: () => {},
      }),
    );
    expect(
      container.querySelector('button[aria-label="弹出独立窗口"]'),
    ).toBeNull();
  });

  it("hides the composer slot once the reply is resolved", () => {
    const container = mount(
      createElement(ConversationSubthreadPanel, {
        threadID: "group-1",
        subthread: subthreadWith({ status: "resolved" }),
        onClose: () => {},
        onResolve: () => {},
        composer: createElement("div", { className: "test-composer-slot" }),
      }),
    );
    expect(container.querySelector(".conversation-subthread-composer")).toBeNull();
    expect(container.querySelector('button[aria-label="重新打开"]')).toBeNull();
    expect(container.querySelector('button[aria-label="标记已解决"]')).toBeNull();
  });

  it("wires the jump-to-latest pill into anchored mode while the composer is mounted", () => {
    const container = mount(
      createElement(ConversationSubthreadPanel, {
        threadID: "group-1",
        subthread: subthreadWith(),
        onClose: () => {},
        onResolve: () => {},
        composer: createElement("div", { className: "test-composer-slot" }),
      }),
    );
    const body = container.querySelector<HTMLElement>(
      ".conversation-subthread-body",
    )!;
    Object.defineProperties(body, {
      scrollHeight: { configurable: true, value: 1000 },
      clientHeight: { configurable: true, value: 400 },
      scrollTop: { configurable: true, writable: true, value: 0 },
    });
    act(() => {
      body.dispatchEvent(new Event("scroll"));
    });

    expect(container.querySelector(".jump-to-latest-pill")).toBeNull();
    expect(
      document.body.querySelector(".jump-to-latest-pill-anchored"),
    ).not.toBeNull();
  });

  it("passes no anchor to the jump-to-latest pill once the thread is resolved", () => {
    // Mirror of the previous test for the resolved branch: the composer slot
    // is gone, so the pill receives a null bottomAnchor and renders nothing.
    const container = mount(
      createElement(ConversationSubthreadPanel, {
        threadID: "group-1",
        subthread: subthreadWith({ status: "resolved" }),
        onClose: () => {},
        onResolve: () => {},
        composer: createElement("div", { className: "test-composer-slot" }),
      }),
    );
    expect(container.querySelector(".conversation-subthread-composer")).toBeNull();
    expect(container.querySelector(".jump-to-latest-pill")).toBeNull();
  });

  it("hides technical task trace unless debug controls expose it", () => {
    const task = subthreadWith({
      status: "task",
      task: { id: "cth-1", status: "running", subthread_id: "cth-1" },
    });
    const normal = mount(
      createElement(ConversationSubthreadPanel, {
        threadID: "group-1",
        subthread: task,
        onClose: () => {},
        onResolve: () => {},
      }),
    );
    expect(normal.querySelector(".conversation-subthread-trace")).toBeNull();

    const debug = mount(
      createElement(ConversationSubthreadPanel, {
        threadID: "group-1",
        subthread: task,
        onClose: () => {},
        onResolve: () => {},
        showTechnicalTrace: true,
      }),
    );
    expect(debug.querySelector(".conversation-subthread-trace")).not.toBeNull();
  });

  it("does not let a late trace response from Thread A overwrite Thread B", async () => {
    const traceA = deferred<{ events: Array<{ seq: number; kind: string; summary: string; at: string }> }>();
    const taskEvents = vi
      .fn()
      .mockReturnValueOnce(traceA.promise)
      .mockResolvedValueOnce({
        events: [
          {
            seq: 1,
            kind: "node_progress",
            summary: "B progress",
            at: "2026-01-01T00:00:00Z",
          },
        ],
      });
    (window as unknown as { wuu: { taskEvents: typeof taskEvents } }).wuu = {
      taskEvents,
    };
    const taskA = subthreadWith({
      id: "cth-a",
      status: "task",
      task: { id: "cth-a", status: "running", subthread_id: "cth-a" },
    });
    const taskB = subthreadWith({
      id: "cth-b",
      status: "task",
      task: { id: "cth-b", status: "running", subthread_id: "cth-b" },
    });
    const props = {
      threadID: "group-1",
      onClose: () => {},
      onResolve: () => {},
      showTechnicalTrace: true,
    };
    const view = mountRerenderable(
      createElement(ConversationSubthreadPanel, { ...props, subthread: taskA }),
    );
    await act(async () => {
      view.container
        .querySelector<HTMLButtonElement>(".conversation-subthread-trace-toggle")!
        .click();
      await Promise.resolve();
    });
    view.rerender(
      createElement(ConversationSubthreadPanel, { ...props, subthread: taskB }),
    );
    await act(async () => {
      view.container
        .querySelector<HTMLButtonElement>(".conversation-subthread-trace-toggle")!
        .click();
      await Promise.resolve();
      await Promise.resolve();
    });
    await act(async () => {
      traceA.resolve({
        events: [
          {
            seq: 1,
            kind: "node_failed",
            summary: "A stale failure",
            at: "2026-01-01T00:00:00Z",
          },
        ],
      });
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(view.container.textContent).toContain("B progress");
    expect(view.container.textContent).not.toContain("A stale failure");
  });
});
