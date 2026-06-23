import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import type { RuntimeContext, Thread } from "../shared/protocol";
import {
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
        onSelect={() => {}}
        onClose={() => {}}
        onNewThread={() => {}}
        onReorder={() => {}}
      />,
    );
  });
}

describe("SessionTabStrip pending indicators", () => {
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

  it("applies has-unread to non-active thread tabs whose latest turn is not the last viewed", () => {
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
    expect(titlebarRule).toContain("column-gap: 16px;");
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
