/**
 * Tests for the SideThreadPanel — the right-column side chat panel bound
 * to a main thread. Uses the render-harness pattern that mirrors
 * ConversationSubthreadPanel.test.tsx and ChatThreadView.test.tsx, so we
 * don't pull in @testing-library/react (which isn't a dependency).
 */
import { act, createElement } from "react";
import { createRef } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import type {
  SideThreadMessage,
  SideThreadSummary
} from "../shared/protocol";
import {
  SideThreadPanel,
  type SideThreadPanelHandle
} from "./SideThreadPanel";
import type { SideThreadEntryState } from "./SideThreadState";

let mountedRoots: Root[] = [];
let mountedContainers: HTMLElement[] = [];

function mount(element: React.ReactElement): {
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
    }
  };
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
  vi.restoreAllMocks();
});

function makeSummary(overrides: Partial<SideThreadSummary> = {}): SideThreadSummary {
  return {
    side_thread_id: "side-1",
    main_thread_id: "main-1",
    status: "idle",
    created_at: "2026-01-01T00:00:00.000Z",
    updated_at: "2026-01-01T00:00:00.000Z",
    ...overrides
  };
}

function makeEntry(overrides: Partial<SideThreadEntryState> = {}): SideThreadEntryState {
  return {
    open: true,
    summary: null,
    messages: [],
    draft: "",
    streaming: false,
    ...overrides
  };
}

function makeMessage(overrides: Partial<SideThreadMessage> = {}): SideThreadMessage {
  return {
    id: "m-1",
    side_thread_id: "side-1",
    role: "user",
    text: "现在做到哪了？",
    created_at: "2026-01-01T00:00:00.000Z",
    ...overrides
  };
}

function renderPanel(
  entry: SideThreadEntryState,
  callbacks: {
    onClose?: () => void;
    onResizeStart?: (event: unknown) => void;
    onSend?: (text: string) => void;
    onInterrupt?: () => void;
    onChangeDraft?: (draft: string) => void;
    sendDisabledReason?: string;
  } = {}
) {
  return mount(
    createElement(SideThreadPanel, {
      entry,
      mainThreadId: "main-1",
      width: 400,
      onClose: callbacks.onClose ?? (() => {}),
      onResizeStart: callbacks.onResizeStart ?? (() => {}),
      onSend: callbacks.onSend ?? (() => {}),
      onInterrupt: callbacks.onInterrupt ?? (() => {}),
      onChangeDraft: callbacks.onChangeDraft ?? (() => {}),
      sendDisabledReason: callbacks.sendDisabledReason
    })
  );
}

describe("SideThreadPanel", () => {
  it("renders the title and main task status", () => {
    const { container } = renderPanel(
      makeEntry({ summary: makeSummary({ status: "running" }) })
    );
    expect(container.textContent).toContain("侧聊");
    expect(container.textContent).toContain("主任务执行中");
  });

  it("shows the empty state copy and quick prompts when no messages", () => {
    const { container } = renderPanel(makeEntry());
    expect(container.textContent).toContain(
      "可以询问当前任务的进度和相关信息"
    );
    expect(container.textContent).toContain("现在做到哪了？");
    expect(container.textContent).toContain("解释刚才的错误");
    expect(container.textContent).toContain("当前方案有什么风险？");
  });

  it("renders user and assistant messages with role classes", () => {
    const { container } = renderPanel(
      makeEntry({
        messages: [
          makeMessage({ id: "m-1", role: "user", text: "进度？" }),
          makeMessage({
            id: "m-2",
            role: "assistant",
            text: "正在修复 test/foo.ts",
            status: "completed"
          })
        ]
      })
    );
    const items = container.querySelectorAll(".side-thread-panel__message");
    expect(items).toHaveLength(2);
    const classes = Array.from(items).map((el) => el.className);
    expect(classes[0]).toContain("side-thread-panel__message--user");
    expect(classes[1]).toContain("side-thread-panel__message--assistant");
  });

  it("renders a streaming dot while the assistant is streaming", () => {
    const { container } = renderPanel(
      makeEntry({
        messages: [
          makeMessage({
            id: "m-a",
            role: "assistant",
            text: "正在",
            status: "streaming"
          })
        ],
        streaming: true
      })
    );
    expect(
      container.querySelector(".side-thread-panel__streaming-dot")
    ).toBeTruthy();
  });

  it("invokes onClose when the close button is clicked", () => {
    const onClose = vi.fn();
    const { container } = renderPanel(makeEntry(), { onClose });
    const closeButton = container.querySelector(
      ".side-thread-panel__close"
    ) as HTMLButtonElement;
    expect(closeButton).toBeTruthy();
    act(() => {
      closeButton.click();
    });
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("sends the trimmed draft on Enter", () => {
    const onSend = vi.fn();
    const { container } = renderPanel(makeEntry({ draft: "hello" }), { onSend });
    const textarea = container.querySelector(
      ".side-thread-panel__textarea"
    ) as HTMLTextAreaElement;
    act(() => {
      textarea.dispatchEvent(
        new KeyboardEvent("keydown", { key: "Enter", bubbles: true })
      );
    });
    expect(onSend).toHaveBeenCalledWith("hello");
  });

  it("does not send empty drafts", () => {
    const onSend = vi.fn();
    const { container } = renderPanel(makeEntry({ draft: "   " }), { onSend });
    const textarea = container.querySelector(
      ".side-thread-panel__textarea"
    ) as HTMLTextAreaElement;
    act(() => {
      textarea.dispatchEvent(
        new KeyboardEvent("keydown", { key: "Enter", bubbles: true })
      );
    });
    expect(onSend).not.toHaveBeenCalled();
  });

  it("disables send while sendDisabledReason is set", () => {
    const { container } = renderPanel(makeEntry({ draft: "hello" }), {
      sendDisabledReason: "正在连接"
    });
    const button = container.querySelector(
      ".side-thread-panel__send"
    ) as HTMLButtonElement;
    expect(button.disabled).toBe(true);
  });

  it("renders an error bar when lastError is present", () => {
    const { container } = renderPanel(makeEntry({ lastError: "rate limited" }));
    const alert = container.querySelector('[role="alert"]');
    expect(alert?.textContent).toBe("rate limited");
  });

  it("exposes focusComposer through the imperative handle", () => {
    const ref = createRef<SideThreadPanelHandle>();
    mount(
      createElement(SideThreadPanel, {
        ref,
        entry: makeEntry(),
        mainThreadId: "main-1",
        width: 400,
        onClose: () => {},
        onResizeStart: () => {},
        onSend: () => {},
        onInterrupt: () => {},
        onChangeDraft: () => {}
      })
    );
    expect(ref.current).not.toBeNull();
    expect(typeof ref.current?.focusComposer).toBe("function");
    // jsdom 不真正移动焦点，但我们要确认实现存在并不抛错。
    expect(() => ref.current?.focusComposer()).not.toThrow();
  });

  it("fills the draft when a quick prompt is clicked", () => {
    const onChangeDraft = vi.fn();
    const { container } = renderPanel(makeEntry(), { onChangeDraft });
    const buttons = container.querySelectorAll(
      ".side-thread-panel__quick-prompt"
    );
    const target = Array.from(buttons).find(
      (el) => el.textContent === "当前方案有什么风险？"
    ) as HTMLButtonElement;
    expect(target).toBeTruthy();
    act(() => {
      target.click();
    });
    expect(onChangeDraft).toHaveBeenCalledWith(
      "当前方案可能存在哪些风险或后续影响？"
    );
  });

  it("shows the interrupt button while streaming and calls onInterrupt", () => {
    const onInterrupt = vi.fn();
    const { container } = renderPanel(
      makeEntry({ streaming: true }),
      { onInterrupt }
    );
    const button = container.querySelector(
      ".side-thread-panel__interrupt"
    ) as HTMLButtonElement;
    expect(button).toBeTruthy();
    act(() => {
      button.click();
    });
    expect(onInterrupt).toHaveBeenCalledTimes(1);
  });

  it("uses the supplied width as the panel's inline width", () => {
    const { container } = renderPanel(makeEntry());
    const aside = container.querySelector(
      ".side-thread-panel"
    ) as HTMLElement;
    expect(aside.style.width).toBe("400px");
  });
});