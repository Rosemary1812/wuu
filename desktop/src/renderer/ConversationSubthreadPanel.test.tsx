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
import { afterEach, describe, expect, it } from "vitest";
import type {
  ConversationSubthread,
  ParticipantSummary,
  ThreadItem,
} from "../shared/protocol";
import { ConversationSubthreadPanel } from "./ConversationSubthreadPanel";
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

// Set a controlled textarea's value the React-aware way (native setter so the
// value tracker registers the change) and fire the input event that drives
// onChange, mirroring ComposerView.test.tsx's helper.
function typeInto(input: HTMLTextAreaElement, value: string): void {
  const setter = Object.getOwnPropertyDescriptor(
    HTMLTextAreaElement.prototype,
    "value",
  )?.set;
  setter?.call(input, value);
  input.dispatchEvent(new Event("input", { bubbles: true }));
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
      "1 条回复",
    );
  });

  it("escalates without a lead when the group has no named candidates", () => {
    // No lead pool → the header gate escalates straight away (backend allows an
    // empty lead); it does NOT open the picker.
    let lead: string | undefined;
    const container = mount(
      createElement(ConversationSubthreadPanel, {
        threadID: "group-1",
        subthread: subthreadWith(),
        onClose: () => {},
        onResolve: () => {},
        onEscalate: (leadParticipantId: string) => {
          lead = leadParticipantId;
        },
      }),
    );
    const button = container.querySelector<HTMLButtonElement>(
      ".conversation-subthread-escalate",
    );
    expect(button).not.toBeNull();
    act(() => {
      button!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    expect(lead).toBe("");
    // No picker was opened.
    expect(
      container.querySelector(".conversation-subthread-escalate-form"),
    ).toBeNull();
  });

  it("opens the lead picker and escalates with the picked named member", () => {
    let lead: string | undefined;
    const candidates: ParticipantSummary[] = [
      { id: "prt-a", name: "小青", kind: "named" },
      { id: "prt-b", name: "小白", kind: "named" },
    ];
    const container = mount(
      createElement(ConversationSubthreadPanel, {
        threadID: "group-1",
        subthread: subthreadWith(),
        onClose: () => {},
        onResolve: () => {},
        onEscalate: (leadParticipantId: string) => {
          lead = leadParticipantId;
        },
        leadCandidates: candidates,
      }),
    );
    // The header gate now opens the picker rather than escalating immediately.
    const gate = container.querySelector<HTMLButtonElement>(
      ".conversation-subthread-escalate",
    );
    act(() => {
      gate!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    expect(lead).toBeUndefined();
    const select = container.querySelector<HTMLSelectElement>(
      ".conversation-subthread-lead-select",
    );
    expect(select).not.toBeNull();
    // Two named candidates offered, first pre-selected.
    expect(select!.querySelectorAll("option").length).toBe(2);
    expect(select!.value).toBe("prt-a");
    // Pick the second member, then confirm.
    act(() => {
      select!.value = "prt-b";
      select!.dispatchEvent(new Event("change", { bubbles: true }));
    });
    const confirm = container.querySelector<HTMLButtonElement>(
      ".conversation-subthread-escalate-submit",
    );
    act(() => {
      confirm!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    expect(lead).toBe("prt-b");
    // Picker closes after confirming.
    expect(
      container.querySelector(".conversation-subthread-escalate-form"),
    ).toBeNull();
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
    // reaction row, never a reply button.
    expect(
      toolbar!.querySelectorAll(".chat-bubble-toolbar-reaction").length,
    ).toBe(REACTION_KEYS.length);
    expect(toolbar!.querySelector(".chat-bubble-toolbar-reply")).toBeNull();
  });

  it("offers 完成 Task only once escalated and bubbles the typed conclusion", () => {
    const bubbled: string[] = [];
    const taskSubthread = subthreadWith({
      status: "task",
      task: { id: "cth-1", status: "running", subthread_id: "cth-1" },
    });
    const container = mount(
      createElement(ConversationSubthreadPanel, {
        threadID: "group-1",
        subthread: taskSubthread,
        onClose: () => {},
        onResolve: () => {},
        onEscalate: () => {},
        onBubble: (summary: string) => bubbled.push(summary),
      }),
    );
    // Escalate gate is spent; the finalize gate takes its place.
    expect(
      container.querySelector(".conversation-subthread-finalize-toggle"),
    ).not.toBeNull();
    // The conclusion form is hidden until the human opens it.
    expect(container.querySelector(".conversation-subthread-finalize")).toBeNull();
    const toggle = container.querySelector<HTMLButtonElement>(
      ".conversation-subthread-finalize-toggle",
    )!;
    act(() => {
      toggle.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    const submit = container.querySelector<HTMLButtonElement>(
      ".conversation-subthread-finalize-submit",
    )!;
    // Submit is disabled while no conclusion is typed.
    expect(submit.disabled).toBe(true);
    const input = container.querySelector<HTMLTextAreaElement>(
      ".conversation-subthread-finalize .conversation-subthread-input",
    )!;
    act(() => {
      typeInto(input, "重试三次后成功,已合入");
    });
    expect(submit.disabled).toBe(false);
    act(() => {
      submit.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    expect(bubbled).toEqual(["重试三次后成功,已合入"]);
    // Form collapses after a successful bubble.
    expect(container.querySelector(".conversation-subthread-finalize")).toBeNull();
  });

  it("does not offer 完成 Task before escalation or when onBubble is absent", () => {
    // A plain (un-escalated) reply: no finalize gate.
    const plain = mount(
      createElement(ConversationSubthreadPanel, {
        threadID: "group-1",
        subthread: subthreadWith(),
        onClose: () => {},
        onResolve: () => {},
        onBubble: () => {},
      }),
    );
    expect(
      plain.querySelector(".conversation-subthread-finalize-toggle"),
    ).toBeNull();
    // A task with no onBubble wired: still no finalize gate.
    const noHandler = mount(
      createElement(ConversationSubthreadPanel, {
        threadID: "group-1",
        subthread: subthreadWith({
          status: "task",
          task: { id: "cth-1", status: "running", subthread_id: "cth-1" },
        }),
        onClose: () => {},
        onResolve: () => {},
      }),
    );
    expect(
      noHandler.querySelector(".conversation-subthread-finalize-toggle"),
    ).toBeNull();
  });

  it("hides 完成 Task once the task is resolved", () => {
    const container = mount(
      createElement(ConversationSubthreadPanel, {
        threadID: "group-1",
        subthread: subthreadWith({
          status: "resolved",
          task: { id: "cth-1", status: "completed", subthread_id: "cth-1" },
          summary: "重试三次后成功",
        }),
        onClose: () => {},
        onResolve: () => {},
        onBubble: () => {},
      }),
    );
    expect(
      container.querySelector(".conversation-subthread-finalize-toggle"),
    ).toBeNull();
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
  });
});
