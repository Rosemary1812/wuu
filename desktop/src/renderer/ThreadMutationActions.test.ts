import { afterEach, describe, expect, it, vi } from "vitest";
import type { Agent, RuntimeContext, Thread } from "../shared/protocol";
import {
  createDraftSessionTab,
  createThreadSessionTab,
  initialState,
  threadSessionTabID,
  type AppState,
  type ThreadSummary,
} from "./AppState";
import { createThreadMutationActions } from "./ThreadMutationActions";

const toastMocks = vi.hoisted(() => ({
  showErrorToast: vi.fn(),
}));

vi.mock("./Toast", () => ({
  showErrorToast: toastMocks.showErrorToast,
}));

const originalWuu = (window as unknown as { wuu?: unknown }).wuu;

function restoreWuu(): void {
  if (originalWuu === undefined) {
    delete (window as unknown as { wuu?: unknown }).wuu;
    return;
  }
  Object.defineProperty(window, "wuu", {
    configurable: true,
    value: originalWuu,
  });
}

afterEach(() => {
  toastMocks.showErrorToast.mockClear();
  restoreWuu();
});

function projectContext(): RuntimeContext {
  return { kind: "project", project_id: "project-1", cwd: "/tmp/project-1" };
}

function thread(id = "thread-1"): Thread {
  return {
    id,
    title: id,
    preview: id,
    model_provider: "fake",
    model: "fake-model",
    cwd: "/tmp/project-1",
    status: "idle",
    pinned: false,
    archived: false,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    turns: [],
  };
}

function summary(source: Thread): ThreadSummary {
  return {
    ...source,
    turns: [],
    turn_count: source.turns.length,
  };
}

function installWuuApi(baseThread: Thread): {
  pinThread: ReturnType<typeof vi.fn>;
  archiveThread: ReturnType<typeof vi.fn>;
  deleteThread: ReturnType<typeof vi.fn>;
} {
  const pinThread = vi.fn().mockResolvedValue({
    thread: { ...baseThread, pinned: true },
  });
  const archiveThread = vi.fn().mockResolvedValue({
    thread: { ...baseThread, archived: true },
  });
  const deleteThread = vi.fn().mockResolvedValue({});
  Object.defineProperty(window, "wuu", {
    configurable: true,
    value: {
      pinThread,
      archiveThread,
      deleteThread,
      renameThread: vi.fn().mockResolvedValue({ thread: baseThread }),
    },
  });
  return { pinThread, archiveThread, deleteThread };
}

function buildActions({
  initial,
  activeThreadID,
  
  archiveConfirmSubagentID,
  localDemoThreads = new Map<string, Thread>(),
}: {
  initial: AppState;
  activeThreadID?: string;
  
  archiveConfirmSubagentID?: string;
  localDemoThreads?: Map<string, Thread>;
}) {
  let appState = initial;
  let confirmSubagentID = archiveConfirmSubagentID;
  const localDemoThreadsRef = { current: localDemoThreads };
  const clearPrimaryComposerDraft = vi.fn();
  const resetSplitComposerDrafts = vi.fn();
  const updateCachedSidebarThread = vi.fn();
  const removeCachedSidebarThread = vi.fn();
  const clearThreadPendingComposerMessages = vi.fn();
  const actions = createThreadMutationActions({
    getAppState: () => appState,
    setAppState: (update) => {
      appState = typeof update === "function" ? update(appState) : update;
    },
    getActiveThreadID: () => activeThreadID,
    getArchiveConfirmSubagentID: () => confirmSubagentID,
    setArchiveConfirmSubagentID: (update) => {
      confirmSubagentID =
        typeof update === "function" ? update(confirmSubagentID) : update;
    },
    localDemoThreadsRef,
    nextDraftSessionTab: (context) =>
      createDraftSessionTab("draft:fallback", context),
    clearPrimaryComposerDraft,
    resetSplitComposerDrafts,
    updateCachedSidebarThread,
    removeCachedSidebarThread,
    clearThreadPendingComposerMessages,
  });

  return {
    actions,
    getAppState: () => appState,
    
    clearPrimaryComposerDraft,
    resetSplitComposerDrafts,
    updateCachedSidebarThread,
    removeCachedSidebarThread,
    clearThreadPendingComposerMessages,
  };
}

describe("createThreadMutationActions", () => {
  it("pins a server thread and updates the sidebar cache", async () => {
    const context = projectContext();
    const base = thread();
    const api = installWuuApi(base);
    const harness = buildActions({
      initial: {
        ...initialState,
        activeContext: context,
        thread: base,
        threads: [base],
        status: "ready",
      },
    });

    await harness.actions.toggleThreadPinned(summary(base));

    expect(api.pinThread).toHaveBeenCalledWith(base.id, true);
    expect(harness.updateCachedSidebarThread).toHaveBeenCalledWith({
      ...base,
      pinned: true,
    });
    expect(harness.getAppState().thread?.pinned).toBe(true);
    expect(harness.getAppState().threads[0]?.pinned).toBe(true);
  });

  it("archives the active thread after confirmation and opens a fallback draft", async () => {
    const context = projectContext();
    const base = thread();
    const api = installWuuApi(base);
    const threadTab = createThreadSessionTab(base, context);
    const harness = buildActions({
      initial: {
        ...initialState,
        activeContext: context,
        thread: base,
        threads: [base],
        sessionTabs: [threadTab],
        activeSessionTabID: threadSessionTabID(base.id),
        status: "ready",
      },
      activeThreadID: base.id,
    });

    const outcome = await harness.actions.archiveThread(summary(base));
    expect(outcome).toEqual({ ok: true });
    expect(api.archiveThread).toHaveBeenCalledWith(base.id, true);
    expect(harness.removeCachedSidebarThread).toHaveBeenCalledWith(base.id);
    expect(harness.updateCachedSidebarThread).not.toHaveBeenCalled();
    expect(harness.clearThreadPendingComposerMessages).toHaveBeenCalledWith(
      base.id,
    );
    expect(harness.clearPrimaryComposerDraft).toHaveBeenCalled();
    expect(harness.resetSplitComposerDrafts).toHaveBeenCalled();
    expect(harness.getAppState().thread).toBeUndefined();
    expect(harness.getAppState().activeSessionTabID).toBe("draft:fallback");
  });

  it("allows archiving after crash recovery settles a child agent", async () => {
    const context = projectContext();
    const child = {
      id: "agent-interrupted",
      status: "failed",
    } as Agent;
    const base = { ...thread(), child_agents: [child] };
    const api = installWuuApi(base);
    const harness = buildActions({
      initial: {
        ...initialState,
        activeContext: context,
        thread: base,
        threads: [base],
        status: "ready",
      },
    });

    const outcome = await harness.actions.archiveThread(summary(base));

    expect(outcome).toEqual({ ok: true });
    expect(api.archiveThread).toHaveBeenCalledWith(base.id, true);
  });

  it("reports a running thread instead of claiming it was archived", async () => {
    const context = projectContext();
    const base = { ...thread(), status: "in_progress" as const };
    const api = installWuuApi(base);
    const harness = buildActions({
      initial: {
        ...initialState,
        activeContext: context,
        thread: base,
        threads: [base],
        status: "ready",
      },
    });

    const outcome = await harness.actions.archiveThread(summary(base));

    expect(outcome).toEqual({ ok: false, error: "会话仍在运行，结束后再归档" });
    expect(api.archiveThread).not.toHaveBeenCalled();
    expect(harness.getAppState().threads[0]?.archived).toBe(false);
    expect(harness.getAppState().status).toBe("ready");
    expect(toastMocks.showErrorToast).toHaveBeenCalledWith("会话仍在运行，结束后再归档");
  });

  it("reports a remotely owned running turn without showing archive success", async () => {
    const context = projectContext();
    const base = thread();
    const api = installWuuApi(base);
    api.archiveThread.mockRejectedValueOnce(
      new Error(
        `thread "${base.id}" already has a running turn in another app-server: thread execution is owned by another app-server`,
      ),
    );
    const harness = buildActions({
      initial: {
        ...initialState,
        activeContext: context,
        thread: base,
        threads: [base],
        status: "ready",
      },
    });

    const outcome = await harness.actions.archiveThread(summary(base));

    expect(outcome).toEqual({ ok: false, error: "会话仍在运行，结束后再归档" });
    expect(harness.getAppState().threads[0]?.archived).toBe(false);
    expect(harness.getAppState().status).toBe("ready");
    expect(toastMocks.showErrorToast).toHaveBeenCalledWith("会话仍在运行，结束后再归档");
  });

  it("reuses a parked workspace draft when archiving the active thread", async () => {
    const context = projectContext();
    const base = thread();
    const api = installWuuApi(base);
    const threadTab = createThreadSessionTab(base, context);
    const draftTab = createDraftSessionTab("draft:parked", context, {
      prompt: "keep this draft",
      images: [],
      files: [],
    });
    const harness = buildActions({
      initial: {
        ...initialState,
        activeContext: context,
        thread: base,
        threads: [base],
        sessionTabs: [threadTab, draftTab],
        activeSessionTabID: threadTab.id,
        status: "ready",
      },
      activeThreadID: base.id,
    });

    await harness.actions.archiveThread(summary(base));

    expect(api.archiveThread).toHaveBeenCalledWith(base.id, true);
    expect(harness.getAppState().activeSessionTabID).toBe(draftTab.id);
    expect(harness.getAppState().sessionTabs).toHaveLength(1);
    expect(harness.getAppState().sessionTabs[0]).toMatchObject({
      id: draftTab.id,
      prompt: "keep this draft",
    });
  });

  it("deletes a thread and removes it from sidebar caches", async () => {
    const context = projectContext();
    const base = thread();
    const api = installWuuApi(base);
    const harness = buildActions({
      initial: {
        ...initialState,
        activeContext: context,
        thread: base,
        threads: [base],
        status: "ready",
      },
    });

    await harness.actions.deleteThread(summary(base));

    expect(api.deleteThread).toHaveBeenCalledWith(base.id);
    expect(harness.removeCachedSidebarThread).toHaveBeenCalledWith(base.id);
    expect(harness.getAppState().threads).toHaveLength(0);
  });

  it("patches subagent pin state into every cached parent thread", async () => {
    const context = projectContext();
    const agent = { id: "agent-1", status: "idle", pinned: false } as Agent;
    const parent = { ...thread("parent"), child_agents: [agent] };
    installWuuApi({ ...thread("agent-1"), pinned: true });
    const harness = buildActions({
      initial: {
        ...initialState,
        activeContext: context,
        thread: parent,
        threads: [parent],
        status: "ready",
      },
    });

    await harness.actions.toggleSubagentPinned(agent);

    expect(harness.getAppState().thread?.child_agents?.[0]?.pinned).toBe(true);
    expect(harness.getAppState().threads[0]?.child_agents?.[0]?.pinned).toBe(
      true,
    );
  });
});
