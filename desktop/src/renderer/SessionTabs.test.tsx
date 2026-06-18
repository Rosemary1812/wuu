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
});
