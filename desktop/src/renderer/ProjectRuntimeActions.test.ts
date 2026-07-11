import { describe, expect, it, vi } from "vitest";
import type { DesktopProject, RuntimeContext, Thread } from "../shared/protocol";
import {
  createDraftSessionTab,
  emptyComposerDraft,
  initialState,
  type AppState,
  type ComposerDraftState,
  type SessionTab,
} from "./AppState";
import { createProjectRuntimeActions } from "./ProjectRuntimeActions";

function projectContext(id = "project-1"): RuntimeContext {
  return { kind: "project", project_id: id, cwd: `/tmp/${id}` };
}

function project(id = "project-1"): DesktopProject {
  return {
    id,
    name: id,
    path: `/tmp/${id}`,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

function thread(id = "thread-1", status = "idle"): Thread {
  return {
    id,
    title: id,
    preview: id,
    cwd: "/tmp/project-1",
    status,
    model_provider: "fake",
    model: "fake-model",
    pinned: false,
    archived: false,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    turns: status === "in_progress"
      ? [{ id: "turn-1", status: "in_progress", items_view: "full", items: [] }]
      : [],
  } as Thread;
}

function buildActions({
  initial,
  draft = emptyComposerDraft(),
}: {
  initial: AppState;
  draft?: ComposerDraftState;
}) {
  let appState = initial;
  let currentDraft = draft;
  const closeProjectMenus = vi.fn();
  const clearPrimaryComposerDraft = vi.fn(() => {
    currentDraft = emptyComposerDraft();
  });
  const nextDraftSessionTab = vi.fn((context: RuntimeContext): SessionTab =>
    createDraftSessionTab("draft:test", context),
  );

  const actions = createProjectRuntimeActions({
    getAppState: () => appState,
    setAppState: (update) => {
      appState = typeof update === "function" ? update(appState) : update;
    },
    getPrimaryComposerDraft: () => currentDraft,
    clearPrimaryComposerDraft,
    restoreLoadedRuntimeComposerDraft: vi.fn(),
    nextDraftSessionTab,
    closeProjectMenus,
    beginViewSwitch: vi.fn(() => 1),
    finishViewSwitch: vi.fn(() => true),
    cancelViewSwitch: vi.fn(),
    loadRuntime: vi.fn(),
  });

  return {
    actions,
    getAppState: () => appState,
    closeProjectMenus,
    clearPrimaryComposerDraft,
    nextDraftSessionTab,
  };
}

describe("createProjectRuntimeActions", () => {
  it("opens the active project as a fresh draft when a thread is visible", async () => {
    const context = projectContext();
    const harness = buildActions({
      initial: {
        ...initialState,
        activeContext: context,
        activeProjectId: "project-1",
        projects: [project()],
        thread: thread(),
      },
    });

    await harness.actions.openProject("project-1");

    expect(harness.closeProjectMenus).toHaveBeenCalled();
    expect(harness.clearPrimaryComposerDraft).toHaveBeenCalled();
    expect(harness.getAppState().thread).toBeUndefined();
    expect(harness.getAppState().activeSessionTabID).toBe("draft:test");
  });

  it("refuses to select a different project for a new thread while work is running", async () => {
    const context = projectContext("project-1");
    const harness = buildActions({
      initial: {
        ...initialState,
        activeContext: context,
        activeProjectId: "project-1",
        projects: [project("project-1"), project("project-2")],
        thread: thread("running", "in_progress"),
      },
    });

    await harness.actions.selectProjectForNewThread("project-2");

    expect(harness.closeProjectMenus).toHaveBeenCalled();
    expect(harness.getAppState().status).toBe("任务运行中，暂不能切换项目");
  });
});
