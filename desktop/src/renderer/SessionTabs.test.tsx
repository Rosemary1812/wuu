import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { RuntimeContext, Thread } from "../shared/protocol";
import {
  createDraftSessionTab,
  createSkillsSessionTab,
  createThreadSessionTab,
  initialState,
  threadSessionTabID,
  type AppState,
} from "./AppState";
import { SessionTabStrip } from "./SessionTabs";

let container: HTMLDivElement;
let root: Root | null = null;

const conversationShellCSS = readFileSync(
  resolve(process.cwd(), "src/renderer/styles/conversation-shell.css"),
  "utf8",
);

function cssRule(selector: string): string {
  const escapedSelector = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const matches = Array.from(
    conversationShellCSS.matchAll(
      new RegExp(`^${escapedSelector}\\s*\\{([\\s\\S]*?)\\n\\}`, "gm"),
    ),
  );
  expect(matches).not.toHaveLength(0);
  return matches.at(-1)?.[1] ?? "";
}

/**
 * Find the rule body for `selector` even when it appears inside a
 * comma-separated selector list (not just at the start of a line).
 * Returns the body of the LAST matching rule so it reflects cascade
 * overrides — same semantics as `cssRule`.
 */
function cssRuleContaining(selector: string): string {
  const escapedSelector = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const matches = Array.from(
    conversationShellCSS.matchAll(
      new RegExp(
        `(?:^|,\\s*)${escapedSelector}\\s*(?:,|\\{)([\\s\\S]*?)\\n\\}`,
        "gm",
      ),
    ),
  );
  expect(matches).not.toHaveLength(0);
  return matches.at(-1)?.[1] ?? "";
}

beforeEach(() => {
  container = document.createElement("div");
  document.body.appendChild(container);
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  root = null;
  container.remove();
  document.body
    .querySelectorAll(".thread-row-context-menu")
    .forEach((menu) => menu.remove());
});

function makeThread(id: string, preview: string): Thread {
  return {
    id,
    preview,
    model_provider: "fake",
    model: "fake-model",
    cwd: "/tmp/project",
    status: "idle",
    created_at: "2026-06-18T00:00:00Z",
    updated_at: "2026-06-18T00:00:00Z",
    turns: [],
  };
}

function makeThreadWithTurn(
  id: string,
  turnID: string,
  status: "completed" | "in_progress" = "completed",
): Thread {
  return {
    ...makeThread(id, "Thread " + id),
    turns: [
      {
        id: turnID,
        items: [],
        items_view: "full",
        status,
      },
    ],
  };
}

function renderTabs(state: AppState): void {
  renderTabsWith(state, () => {});
}

function renderTabsWith(
  state: AppState,
  onClose: (tabID: string) => void,
  onSelect: (tabID: string) => void = () => {},
): void {
  act(() => {
    root = createRoot(container);
    root.render(
      <SessionTabStrip
        state={state}
        pendingComposerMessagesByThread={{
          "thread-a": {
            queued: [{ id: "queue-a", text: "queued", images: [], files: [] }],
            guides: [{ id: "guide-a", text: "guide", images: [], files: [] }],
          },
        }}
        canStartNewThread
        onSelect={onSelect}
        onClose={onClose}
        onCloseTabs={() => {}}
        onPopOut={() => {}}
        onNewThread={() => {}}
        onReorder={() => {}}
      />,
    );
  });
}

describe("SessionTabStrip pending indicators", () => {
  it("names the center tablist as conversations rather than generic work objects", () => {
    const context: RuntimeContext = {
      kind: "project",
      project_id: "project-1",
      cwd: "/tmp/project",
    };
    const thread = makeThread("thread-a", "Thread A");
    renderTabs({
      ...initialState,
      activeContext: context,
      thread,
      activeSessionTabID: threadSessionTabID(thread.id),
      sessionTabs: [createThreadSessionTab(thread, context)],
      threads: [thread],
    });

    expect(container.querySelector(".session-tab-strip")?.getAttribute("aria-label")).toBe(
      "已打开的对话",
    );
    const tabList = container.querySelector(".session-tab-scroll");
    const tab = container.querySelector<HTMLButtonElement>(".session-tab-main");
    expect(tabList?.getAttribute("role")).toBe("tablist");
    expect(tab?.getAttribute("role")).toBe("tab");
    expect(tab?.getAttribute("aria-selected")).toBe("true");
    expect(tab?.tabIndex).toBe(0);
  });

  it("moves focus and selection across conversations with arrow keys", () => {
    const context: RuntimeContext = {
      kind: "project",
      project_id: "project-1",
      cwd: "/tmp/project",
    };
    const threadA = makeThread("thread-a", "Thread A");
    const threadB = makeThread("thread-b", "Thread B");
    const onSelect = vi.fn();
    renderTabsWith(
      {
        ...initialState,
        activeContext: context,
        thread: threadA,
        activeSessionTabID: threadSessionTabID(threadA.id),
        sessionTabs: [
          createThreadSessionTab(threadA, context),
          createThreadSessionTab(threadB, context),
        ],
        threads: [threadA, threadB],
      },
      () => {},
      onSelect,
    );

    const tabs = container.querySelectorAll<HTMLButtonElement>(".session-tab-main");
    tabs[0]?.focus();
    act(() => {
      tabs[0]?.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowRight", bubbles: true }));
    });

    expect(onSelect).toHaveBeenCalledWith(threadSessionTabID(threadB.id));
    expect(document.activeElement).toBe(tabs[1]);
  });

  it("shows pending count only on the owning thread tab", () => {
    const context: RuntimeContext = {
      kind: "project",
      project_id: "project-1",
      cwd: "/tmp/project",
    };
    const threadA = makeThread("thread-a", "Thread A");
    const threadB = makeThread("thread-b", "Thread B");
    renderTabs({
      ...initialState,
      activeContext: context,
      thread: threadA,
      activeSessionTabID: threadSessionTabID(threadA.id),
      sessionTabs: [
        createThreadSessionTab(threadA, context),
        createThreadSessionTab(threadB, context),
      ],
      threads: [threadA, threadB],
    });

    const tabs = Array.from(container.querySelectorAll(".session-tab"));
    expect(tabs).toHaveLength(2);
    expect(tabs[0]?.querySelector(".session-tab-pending-count")?.textContent).toBe("2");
    expect(tabs[1]?.querySelector(".session-tab-pending-count")).toBeNull();
  });

  it("calls onClose with the tab id when the main button is double-clicked", () => {
    const context: RuntimeContext = {
      kind: "project",
      project_id: "project-1",
      cwd: "/tmp/project",
    };
    const threadA = makeThread("thread-a", "Thread A");
    const threadB = makeThread("thread-b", "Thread B");
    const onClose = vi.fn();
    renderTabsWith(
      {
        ...initialState,
        activeContext: context,
        thread: threadA,
        activeSessionTabID: threadSessionTabID(threadA.id),
        sessionTabs: [
          createThreadSessionTab(threadA, context),
          createThreadSessionTab(threadB, context),
        ],
        threads: [threadA, threadB],
      },
      onClose,
    );

    const main = container.querySelectorAll(
      ".session-tab-main",
    )[0] as HTMLElement | undefined;
    expect(main).toBeDefined();
    act(() => {
      main?.dispatchEvent(new MouseEvent("dblclick", { bubbles: true }));
    });

    expect(onClose).toHaveBeenCalledTimes(1);
    expect(onClose).toHaveBeenCalledWith(threadSessionTabID(threadA.id));
  });

  it("does not close the tab on a single click", () => {
    const context: RuntimeContext = {
      kind: "project",
      project_id: "project-1",
      cwd: "/tmp/project",
    };
    const threadA = makeThreadWithTurn("thread-a", "turn-a-1");
    const threadB = makeThreadWithTurn("thread-b", "turn-b-1");
    renderTabs({
      ...initialState,
      activeContext: context,
      thread: threadA,
      activeSessionTabID: threadSessionTabID(threadA.id),
      sessionTabs: [
        createThreadSessionTab(threadA, context),
        createThreadSessionTab(threadB, context),
      ],
      threads: [threadA, threadB],
      lastViewedTurnByThreadID: {
        "thread-a": "turn-a-1",
      },
    });

    const tabs = Array.from(container.querySelectorAll(".session-tab"));
    expect(tabs).toHaveLength(2);
    expect(tabs[0]?.classList.contains("has-unread")).toBe(false);
    expect(tabs[1]?.classList.contains("has-unread")).toBe(true);
  });

  it("restores keyboard focus to the next active conversation after close", () => {
    const context: RuntimeContext = {
      kind: "project",
      project_id: "project-1",
      cwd: "/tmp/project",
    };
    const threadA = makeThread("thread-a", "Thread A");
    const threadB = makeThread("thread-b", "Thread B");
    const state = {
      ...initialState,
      activeContext: context,
      thread: threadB,
      activeSessionTabID: threadSessionTabID(threadB.id),
      sessionTabs: [
        createThreadSessionTab(threadA, context),
        createThreadSessionTab(threadB, context),
      ],
      threads: [threadA, threadB],
    };
    const renderState = (nextState: AppState): void => {
      root?.render(
        <SessionTabStrip
          state={nextState}
          pendingComposerMessagesByThread={{}}
          canStartNewThread
          onSelect={() => {}}
          onClose={(tabID) => {
            expect(tabID).toBe(threadSessionTabID(threadB.id));
            renderState({
              ...state,
              thread: threadA,
              activeSessionTabID: threadSessionTabID(threadA.id),
              sessionTabs: [createThreadSessionTab(threadA, context)],
            });
          }}
          onCloseTabs={() => {}}
          onPopOut={() => {}}
          onNewThread={() => {}}
          onReorder={() => {}}
        />,
      );
    };
    act(() => {
      root = createRoot(container);
      renderState(state);
    });

    const activeClose = container.querySelector<HTMLButtonElement>(
      ".session-tab.active .session-tab-close",
    );
    activeClose?.focus();
    act(() => activeClose?.click());

    expect(document.activeElement).toBe(
      container.querySelector(".session-tab.active .session-tab-main"),
    );
  });

  it("does not apply has-unread to running thread tabs even when their latest turn is new", () => {
    const context: RuntimeContext = {
      kind: "project",
      project_id: "project-1",
      cwd: "/tmp/project",
    };
    const threadA = makeThreadWithTurn("thread-a", "turn-a-1");
    const threadB = makeThreadWithTurn("thread-b", "turn-b-1", "in_progress");
    renderTabs({
      ...initialState,
      activeContext: context,
      thread: threadA,
      activeSessionTabID: threadSessionTabID(threadA.id),
      sessionTabs: [
        createThreadSessionTab(threadA, context),
        createThreadSessionTab(threadB, context),
      ],
      threads: [threadA, threadB],
    });

    const tabs = Array.from(container.querySelectorAll(".session-tab"));
    expect(tabs).toHaveLength(2);
    expect(tabs[0]?.classList.contains("has-unread")).toBe(false);
    expect(tabs[1]?.classList.contains("has-unread")).toBe(false);
    expect(tabs[1]?.classList.contains("running")).toBe(true);
  });
});

describe("SessionTabStrip layout styles", () => {
  it("keeps the new-conversation glyph compact inside its 18px icon box", () => {
    renderTabs(initialState);

    const newConversationIcon = container.querySelector(".session-tab-new svg");
    expect(newConversationIcon?.classList.contains("icon-lg")).toBe(true);
    expect(newConversationIcon?.getAttribute("viewBox")).toBe("0 0 24 24");
  });

  it("uses a flat document rail instead of raised pill tabs", () => {
    const tabRule = cssRule(".session-tab");
    const activeRule = cssRule(".session-tab.active");
    const activeEdgeRule = cssRule(".session-tab.active::after");

    expect(tabRule).toContain("border: 0;");
    expect(tabRule).toContain("border-radius: var(--radius-xs);");
    expect(tabRule).toContain("background: transparent;");
    expect(activeRule).not.toContain("box-shadow:");
    expect(activeEdgeRule).toContain("background: var(--wuu-accent);");
    expect(activeEdgeRule).toContain("height: 2px;");
  });

  it("keeps crowded tabs equal width with stable close targets", () => {
    const titlebarRule = cssRule(".titlebar");
    const titleBlockRule = cssRule(".title-block");
    const titleActionsRule = cssRule(".title-actions");
    const tabStripRule = cssRule(".session-tab-strip");
    const tabListShellRule = cssRule(".session-tab-list-shell");
    const tabScrollRule = cssRule(".session-tab-scroll");
    const tabRule = cssRule(".session-tab");
    const closeRule = cssRule(".session-tab-close");

    expect(titlebarRule).toContain("display: grid;");
    expect(titlebarRule).toContain(
      "grid-template-columns: minmax(0, 1fr) max-content;",
    );
    expect(titlebarRule).toContain("column-gap: 10px;");
    expect(titleBlockRule).toContain("display: grid;");
    expect(titleBlockRule).toContain(
      "grid-template-columns: max-content minmax(0, 1fr);",
    );
    expect(titleBlockRule).toContain("overflow: hidden;");
    expect(titleActionsRule).toContain("flex: 0 0 auto;");
    expect(titleActionsRule).toContain("min-width: max-content;");
    expect(tabStripRule).toContain("display: grid;");
    expect(tabStripRule).toContain(
      "grid-template-columns: minmax(0, 1fr) max-content;",
    );
    expect(tabStripRule).toContain("overflow: hidden;");
    expect(tabListShellRule).toContain("min-width: 0;");
    expect(tabListShellRule).toContain("max-width: 100%;");
    expect(tabListShellRule).toContain("overflow: hidden;");
    expect(tabScrollRule).toContain("width: 100%;");
    expect(tabScrollRule).toContain("flex: 1 1 0%;");
    expect(tabScrollRule).toContain("max-width: 100%;");
    expect(tabScrollRule).toContain("overflow-x: auto;");
    expect(tabRule).toContain("flex: 1 1 0%;");
    expect(tabRule).toContain("min-width: 56px;");
    expect(tabRule).toContain("max-width: 236px;");
    expect(closeRule).toContain("width: 24px;");
    expect(closeRule).toContain("flex: 0 0 auto;");
  });

  it("keeps titlebar controls outside the draggable window region", () => {
    expect(cssRule(".titlebar button")).toContain(
      "-webkit-app-region: no-drag;",
    );
    expect(cssRule(".titlebar button *")).toContain(
      "-webkit-app-region: no-drag;",
    );

    // Titlebar toggle buttons (left/right sidebar, info panel, task
    // board) share a uniform 26×26 visible size with a 30×30 click hit
    // area: 2px padding extends the click region while
    // `background-clip: content-box` keeps the hover background inside
    // the inner 26×26, so the icon reads as a tight, proportional
    // control and the titlebar keeps clear top/bottom breathing room.
    // The 30×30 hit area is intentionally smaller than the original
    // 34×34 `.icon-button` base — visual proportion is prioritized
    // over the flakiness-fix margin from commit 4e69bef7; if titlebar
    // click reliability regresses we can revisit.
    //
    // The four top toolbar buttons (info panel toggle, task board,
    // right panel toggle, "+" new conversation) are intentionally
    // excluded from this inset group — they form one visual cluster
    // at the top of the conversation area and must share a uniform
    // 30×30 background so the visible-edge gap between any two of
    // them is the same. The "+" additionally matches the workspace
    // right-panel "select tool" `+` (sharing `.workspace-panel-add`).
    // See the override rule below.
    for (const selector of [
      ".titlebar .sidebar-toggle-button",
      ".settings-titlebar .sidebar-toggle-button",
    ]) {
      expect(conversationShellCSS).toContain(selector);
    }
    // The four top toolbar buttons opt out of the titlebar inset
    // treatment so they paint a uniform 30×30 background and read as
    // one cluster with the same visible-edge gap between any pair.
    for (const selector of [
      ".titlebar .task-board-button",
      ".titlebar .environment-toggle-button",
      ".titlebar .side-panel-toggle-button",
      ".workspace-panel-add.session-tab-new",
    ]) {
      const rule = cssRuleContaining(selector);
      expect(rule).toMatch(/padding:\s*0;/);
      expect(rule).toMatch(/background-clip:\s*border-box;/);
    }
    // The `.titlebar` column-gap matches `.title-actions { gap: 10px }`
    // so the "+" and the first title-action toggle button sit on the
    // same 10px hit-area grid as the other top toolbar buttons.
  });

  it("keeps drag internals inside the tab list column", () => {
    const context: RuntimeContext = {
      kind: "project",
      project_id: "project-1",
      cwd: "/tmp/project",
    };
    const threadA = makeThread("thread-a", "Thread A");
    const threadB = makeThread("thread-b", "Thread B");
    renderTabs({
      ...initialState,
      activeContext: context,
      thread: threadA,
      activeSessionTabID: threadSessionTabID(threadA.id),
      sessionTabs: [
        createThreadSessionTab(threadA, context),
        createThreadSessionTab(threadB, context),
      ],
      threads: [threadA, threadB],
    });

    const strip = container.querySelector(".session-tab-strip");
    const directChildren = Array.from(strip?.children ?? []);
    expect(directChildren).toHaveLength(2);
    expect(directChildren[0]?.classList.contains("session-tab-list-shell")).toBe(true);
    expect(directChildren[0]?.querySelector(".session-tab-scroll")).not.toBeNull();
    expect(directChildren[1]?.classList.contains("session-tab-new")).toBe(true);
  });
});

describe("SessionTabStrip right-click menu", () => {
  type Captured = {
    closed: string[];
    closedBatches: string[][];
    poppedOut: string[];
  };

  function makeCaptured(): Captured & {
    onSelect: (id: string) => void;
    onClose: (id: string) => void;
    onCloseTabs: (ids: string[]) => void;
    onPopOut: (id: string) => void;
  } {
    const captured: Captured = {
      closed: [],
      closedBatches: [],
      poppedOut: [],
    };
    return {
      ...captured,
      onSelect: () => {},
      onClose: (id) => {
        captured.closed.push(id);
      },
      onCloseTabs: (ids) => {
        captured.closedBatches.push([...ids]);
      },
      onPopOut: (id) => {
        captured.poppedOut.push(id);
      },
    };
  }

  function renderWith(
    state: AppState,
    captured: ReturnType<typeof makeCaptured>,
    crossWorkspaceThreads?: Thread[],
  ): void {
    act(() => {
      root = createRoot(container);
      root.render(
        <SessionTabStrip
          state={state}
          crossWorkspaceThreads={crossWorkspaceThreads}
          pendingComposerMessagesByThread={{}}
          canStartNewThread
          onSelect={captured.onSelect}
          onClose={captured.onClose}
          onCloseTabs={captured.onCloseTabs}
          onPopOut={captured.onPopOut}
          onNewThread={() => {}}
          onReorder={() => {}}
        />,
      );
    });
  }

  function rightClickTab(index: number): void {
    act(() => {
      const tabs = container.querySelectorAll(".session-tab");
      const event = new MouseEvent("contextmenu", {
        bubbles: true,
        cancelable: true,
        clientX: 50,
        clientY: 20,
      });
      tabs[index]?.dispatchEvent(event);
    });
  }

  function getMenuItems(): HTMLButtonElement[] {
    return Array.from(
      document.body.querySelectorAll("[role='menuitem']"),
    ) as HTMLButtonElement[];
  }

  function clickMenuItem(label: string): void {
    act(() => {
      const target = getMenuItems().find((item) => item.textContent === label);
      target?.click();
    });
  }

  function projectContext(): RuntimeContext {
    return {
      kind: "project",
      project_id: "project-1",
      cwd: "/tmp/project",
    };
  }

  it("opens a menu with the close actions when a tab is right-clicked", () => {
    const context = projectContext();
    const threadA = makeThreadWithTurn("thread-a", "turn-a-1", "completed");
    const threadB = makeThreadWithTurn("thread-b", "turn-b-1", "in_progress");
    const captured = makeCaptured();
    renderWith(
      {
        ...initialState,
        activeContext: context,
        thread: threadA,
        activeSessionTabID: threadSessionTabID(threadA.id),
        sessionTabs: [
          createThreadSessionTab(threadA, context),
          createThreadSessionTab(threadB, context),
        ],
        threads: [threadA, threadB],
      },
      captured,
    );

    rightClickTab(0);

    const menu = document.body.querySelector(".thread-row-context-menu");
    expect(menu).not.toBeNull();
    expect(
      menu?.querySelector("[role='separator']"),
    ).not.toBeNull();
    expect(getMenuItems().map((item) => item.textContent)).toEqual([
      "在新窗口打开",
      "关闭",
      "关闭其他",
      "关闭未运行的",
      "关闭全部",
    ]);
  });

  it("calls onPopOut with the right-clicked thread tab", () => {
    const context = projectContext();
    const threadA = makeThread("thread-a", "A");
    const threadB = makeThread("thread-b", "B");
    const captured = makeCaptured();
    renderWith(
      {
        ...initialState,
        activeContext: context,
        thread: threadA,
        activeSessionTabID: threadSessionTabID(threadA.id),
        sessionTabs: [
          createThreadSessionTab(threadA, context),
          createThreadSessionTab(threadB, context),
        ],
        threads: [threadA, threadB],
      },
      captured,
    );

    rightClickTab(1);
    clickMenuItem("在新窗口打开");

    expect(captured.poppedOut).toEqual([threadSessionTabID(threadB.id)]);
    expect(document.body.querySelector(".thread-row-context-menu")).toBeNull();
  });

  it("calls onPopOut with a draft tab", () => {
    const context = projectContext();
    const captured = makeCaptured();
    renderWith(
      {
        ...initialState,
        activeContext: context,
        activeSessionTabID: "draft:test",
        sessionTabs: [createDraftSessionTab("draft:test", context)],
      },
      captured,
    );

    rightClickTab(0);
    clickMenuItem("在新窗口打开");

    expect(captured.poppedOut).toEqual(["draft:test"]);
  });

  it("disables pop-out for non-session tool tabs", () => {
    const context = projectContext();
    const captured = makeCaptured();
    renderWith(
      {
        ...initialState,
        activeContext: context,
        activeSessionTabID: "skills:project:project-1",
        sessionTabs: [createSkillsSessionTab(context)],
      },
      captured,
    );

    rightClickTab(0);

    const popOut = getMenuItems().find(
      (item) => item.textContent === "在新窗口打开",
    );
    expect(popOut?.disabled).toBe(true);
  });

  it("calls onClose with the right-clicked tab for 'close'", () => {
    const context = projectContext();
    const threadA = makeThread("thread-a", "A");
    const threadB = makeThread("thread-b", "B");
    const captured = makeCaptured();
    renderWith(
      {
        ...initialState,
        activeContext: context,
        thread: threadA,
        activeSessionTabID: threadSessionTabID(threadA.id),
        sessionTabs: [
          createThreadSessionTab(threadA, context),
          createThreadSessionTab(threadB, context),
        ],
        threads: [threadA, threadB],
      },
      captured,
    );

    rightClickTab(1);
    clickMenuItem("关闭");

    expect(captured.closed).toEqual([threadSessionTabID(threadB.id)]);
    expect(captured.closedBatches).toEqual([]);
    expect(document.body.querySelector(".thread-row-context-menu")).toBeNull();
  });

  it("calls onCloseTabs with every tab for 'close all'", () => {
    const context = projectContext();
    const threadA = makeThread("thread-a", "A");
    const threadB = makeThread("thread-b", "B");
    const captured = makeCaptured();
    renderWith(
      {
        ...initialState,
        activeContext: context,
        thread: threadA,
        activeSessionTabID: threadSessionTabID(threadA.id),
        sessionTabs: [
          createThreadSessionTab(threadA, context),
          createThreadSessionTab(threadB, context),
        ],
        threads: [threadA, threadB],
      },
      captured,
    );

    rightClickTab(0);
    clickMenuItem("关闭全部");

    expect(captured.closedBatches).toEqual([
      [threadSessionTabID(threadA.id), threadSessionTabID(threadB.id)],
    ]);
  });

  it("calls onCloseTabs with everything except the right-clicked tab for 'close others'", () => {
    const context = projectContext();
    const threadA = makeThread("thread-a", "A");
    const threadB = makeThread("thread-b", "B");
    const threadC = makeThread("thread-c", "C");
    const captured = makeCaptured();
    renderWith(
      {
        ...initialState,
        activeContext: context,
        thread: threadA,
        activeSessionTabID: threadSessionTabID(threadA.id),
        sessionTabs: [
          createThreadSessionTab(threadA, context),
          createThreadSessionTab(threadB, context),
          createThreadSessionTab(threadC, context),
        ],
        threads: [threadA, threadB, threadC],
      },
      captured,
    );

    rightClickTab(1);
    clickMenuItem("关闭其他");

    expect(captured.closedBatches).toEqual([
      [threadSessionTabID(threadA.id), threadSessionTabID(threadC.id)],
    ]);
  });

  it("only passes non-running thread tabs to 'close non-running'", () => {
    const context = projectContext();
    const threadA = makeThreadWithTurn("thread-a", "turn-a-1", "completed");
    const threadB = makeThreadWithTurn("thread-b", "turn-b-1", "in_progress");
    const threadC = makeThreadWithTurn("thread-c", "turn-c-1", "completed");
    const captured = makeCaptured();
    renderWith(
      {
        ...initialState,
        activeContext: context,
        thread: threadA,
        activeSessionTabID: threadSessionTabID(threadA.id),
        sessionTabs: [
          createThreadSessionTab(threadA, context),
          createThreadSessionTab(threadB, context),
          createThreadSessionTab(threadC, context),
        ],
        threads: [threadA, threadB, threadC],
      },
      captured,
    );

    rightClickTab(0);
    clickMenuItem("关闭未运行的");

    expect(captured.closedBatches).toEqual([
      [threadSessionTabID(threadA.id), threadSessionTabID(threadC.id)],
    ]);
  });

  it("keeps a running tab from another workspace spinning and out of 'close non-running'", () => {
    const contextA = projectContext();
    const contextB: RuntimeContext = {
      kind: "project",
      project_id: "project-2",
      cwd: "/tmp/project-2",
    };
    const threadA = makeThreadWithTurn("thread-a", "turn-a-1", "completed");
    const threadB = {
      ...makeThreadWithTurn("thread-b", "turn-b-1", "in_progress"),
      cwd: contextB.cwd,
    };
    const captured = makeCaptured();
    renderWith(
      {
        ...initialState,
        activeContext: contextA,
        thread: threadA,
        activeSessionTabID: threadSessionTabID(threadA.id),
        sessionTabs: [
          createThreadSessionTab(threadA, contextA),
          createThreadSessionTab(threadB, contextB),
        ],
        threads: [threadA],
      },
      captured,
      [threadA, threadB],
    );

    expect(container.querySelectorAll(".session-tab")[1]?.classList.contains("running")).toBe(
      true,
    );

    rightClickTab(0);
    clickMenuItem("关闭未运行的");

    expect(captured.closedBatches).toEqual([[threadSessionTabID(threadA.id)]]);
  });

  it("disables 'close others' and 'close all' when only one tab is open", () => {
    const context = projectContext();
    const threadA = makeThread("thread-a", "A");
    const captured = makeCaptured();
    renderWith(
      {
        ...initialState,
        activeContext: context,
        thread: threadA,
        activeSessionTabID: threadSessionTabID(threadA.id),
        sessionTabs: [createThreadSessionTab(threadA, context)],
        threads: [threadA],
      },
      captured,
    );

    rightClickTab(0);

    const items = getMenuItems();
    const closeOthers = items.find((item) => item.textContent === "关闭其他");
    const closeAll = items.find((item) => item.textContent === "关闭全部");
    expect(closeOthers?.disabled).toBe(true);
    expect(closeAll?.disabled).toBe(true);
  });

  it("disables 'close non-running' when every thread tab is running", () => {
    const context = projectContext();
    const threadA = makeThreadWithTurn("thread-a", "turn-a-1", "in_progress");
    const threadB = makeThreadWithTurn("thread-b", "turn-b-1", "in_progress");
    const captured = makeCaptured();
    renderWith(
      {
        ...initialState,
        activeContext: context,
        thread: threadA,
        activeSessionTabID: threadSessionTabID(threadA.id),
        sessionTabs: [
          createThreadSessionTab(threadA, context),
          createThreadSessionTab(threadB, context),
        ],
        threads: [threadA, threadB],
      },
      captured,
    );

    rightClickTab(0);

    const closeNonRunning = getMenuItems().find(
      (item) => item.textContent === "关闭未运行的",
    );
    expect(closeNonRunning?.disabled).toBe(true);
  });
});
