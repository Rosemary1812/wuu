import { afterEach, describe, expect, it, vi } from "vitest";
import type {
  DesktopProject,
  ProjectListResult,
  RuntimeContext,
  Thread,
  WuuDesktopApi,
} from "../shared/protocol";
import {
  createDraftSessionTab,
  createThreadSessionTab,
  emptyComposerDraft,
  initialState,
  threadSessionTabID,
  type AppState,
  type ComposerDraftState,
  type SessionTab,
} from "./AppState";
import { createProjectRuntimeActions } from "./ProjectRuntimeActions";

function projectContext(id = "project-1"): RuntimeContext {
  return { kind: "project", project_id: id, cwd: `/tmp/${id}` };
}

function noProjectContext(): RuntimeContext {
  return { kind: "no_project", cwd: "/tmp/scratch/default" };
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

function sessionTabPrompt(tabs: SessionTab[], id: string): string | undefined {
  const tab = tabs.find((candidate) => candidate.id === id);
  return tab?.kind === "draft" || tab?.kind === "thread" ? tab.prompt : undefined;
}

function buildActions({
  initial,
  draft = emptyComposerDraft(),
  loadRuntime = vi.fn(),
}: {
  initial: AppState;
  draft?: ComposerDraftState;
  loadRuntime?: ReturnType<typeof vi.fn>;
}) {
  let appState = initial;
  let currentDraft = draft;
  const closeProjectMenus = vi.fn();
  const clearPrimaryComposerDraft = vi.fn(() => {
    currentDraft = emptyComposerDraft();
  });
  const restorePrimaryComposerDraft = vi.fn((nextDraft: ComposerDraftState) => {
    currentDraft = {
      prompt: nextDraft.prompt,
      images: [...nextDraft.images],
      files: [...nextDraft.files],
    };
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
    restorePrimaryComposerDraft,
    clearPrimaryComposerDraft,
    restoreLoadedRuntimeComposerDraft: vi.fn(),
    nextDraftSessionTab,
    closeProjectMenus,
    beginViewSwitch: vi.fn(() => 1),
    finishViewSwitch: vi.fn(() => true),
    cancelViewSwitch: vi.fn(),
    loadRuntime,
  });

  return {
    actions,
    getAppState: () => appState,
    closeProjectMenus,
    clearPrimaryComposerDraft,
    restorePrimaryComposerDraft,
    getCurrentDraft: () => currentDraft,
    nextDraftSessionTab,
  };
}

afterEach(() => {
  Reflect.deleteProperty(window, "wuu");
  vi.restoreAllMocks();
});

describe("createProjectRuntimeActions", () => {
  it("preserves an unsent draft when the project workspace plus opens a session", async () => {
    const context = projectContext();
    const source = thread();
    const sourceTab = createThreadSessionTab(source, context);
    const harness = buildActions({
      initial: {
        ...initialState,
        activeContext: context,
        activeProjectId: "project-1",
        projects: [project()],
        thread: source,
        sessionTabs: [sourceTab],
        activeSessionTabID: sourceTab.id,
      },
      draft: { prompt: "keep this draft", images: [], files: [] },
    });

    await harness.actions.startNewThreadForProject("project-1");

    expect(harness.getAppState().activeSessionTabID).toBe("draft:test");
    expect(sessionTabPrompt(harness.getAppState().sessionTabs, sourceTab.id)).toBe(
      "keep this draft",
    );
  });

  it("focuses the existing project draft instead of adding another one", async () => {
    const context = projectContext();
    const source = thread();
    const sourceTab = createThreadSessionTab(source, context);
    const existingDraft = createDraftSessionTab("draft:existing", context, {
      prompt: "finish this project task",
      images: [],
      files: [],
    });
    const harness = buildActions({
      initial: {
        ...initialState,
        activeContext: context,
        activeProjectId: "project-1",
        projects: [project()],
        thread: source,
        sessionTabs: [sourceTab, existingDraft],
        activeSessionTabID: sourceTab.id,
      },
      draft: { prompt: "keep this thread draft", images: [], files: [] },
    });

    await harness.actions.startNewThreadForProject("project-1");

    expect(harness.nextDraftSessionTab).not.toHaveBeenCalled();
    expect(harness.getAppState().activeSessionTabID).toBe(existingDraft.id);
    expect(harness.getCurrentDraft().prompt).toBe("finish this project task");
    expect(sessionTabPrompt(harness.getAppState().sessionTabs, sourceTab.id)).toBe(
      "keep this thread draft",
    );
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

  it("opens a blank draft when creating a no-project conversation", async () => {
    const context = noProjectContext();
    const source = { ...thread(), cwd: context.cwd };
    const projectState = {
      projects: [],
      active_context: context,
    } as ProjectListResult;
    const loadRuntime = vi.fn().mockResolvedValue({
      activeContext: context,
      thread: undefined,
      threads: [source],
      status: "ready",
    });
    const selectNoProject = vi.fn().mockResolvedValue(projectState);
    Object.defineProperty(window, "wuu", {
      configurable: true,
      value: { selectNoProject } as Partial<WuuDesktopApi>,
    });
    const sourceTab = createThreadSessionTab(source, context);
    const harness = buildActions({
      initial: {
        ...initialState,
        activeContext: context,
        thread: source,
        sessionTabs: [sourceTab],
        activeSessionTabID: threadSessionTabID(source.id),
      },
      draft: { prompt: "keep this draft", images: [], files: [] },
      loadRuntime,
    });

    await harness.actions.useNoProject(true);

    expect(selectNoProject).toHaveBeenCalledWith(true);
    expect(loadRuntime).toHaveBeenCalledWith(projectState, {
      resumeLatestThread: false,
    });
    expect(harness.nextDraftSessionTab).toHaveBeenCalledWith(context);
    expect(harness.getAppState().thread).toBeUndefined();
    expect(harness.getAppState().activeSessionTabID).toBe("draft:test");
    expect(harness.getCurrentDraft()).toEqual(emptyComposerDraft());
  });

  it("lands on the 对话 draft page instead of resuming an old conversation when the picker selects 不使用项目", async () => {
    const sourceContext = projectContext();
    const context = noProjectContext();
    const oldScratchThread = { ...thread("old-scratch"), cwd: context.cwd };
    const sourceDraftTab = createDraftSessionTab("draft:new-project", sourceContext);
    const projectState = {
      projects: [project()],
      active_context: context,
    } as ProjectListResult;
    // The runtime load must be asked NOT to resume: even though the 对话
    // workspace holds an older conversation, retargeting a draft lands on
    // the draft page, never inside history.
    const loadRuntime = vi.fn().mockResolvedValue({
      activeContext: context,
      thread: undefined,
      threads: [oldScratchThread],
      status: "ready",
    });
    const selectNoProject = vi.fn().mockResolvedValue(projectState);
    Object.defineProperty(window, "wuu", {
      configurable: true,
      value: { selectNoProject } as Partial<WuuDesktopApi>,
    });
    const harness = buildActions({
      initial: {
        ...initialState,
        activeContext: sourceContext,
        activeProjectId: "project-1",
        projects: [project()],
        sessionTabs: [sourceDraftTab],
        activeSessionTabID: sourceDraftTab.id,
      },
      draft: { prompt: "draft aimed at no project", images: [], files: [] },
      loadRuntime,
    });

    await harness.actions.useNoProject(false);

    expect(selectNoProject).toHaveBeenCalledWith(false);
    expect(loadRuntime).toHaveBeenCalledWith(projectState, {
      resumeLatestThread: false,
    });
    const state = harness.getAppState();
    expect(state.thread).toBeUndefined();
    const landingTab = state.sessionTabs.find(
      (tab) => tab.id === state.activeSessionTabID,
    );
    expect(landingTab?.kind).toBe("draft");
    expect(
      landingTab?.kind === "draft" ? landingTab.context : undefined,
    ).toEqual(context);
    // The typed draft travels with the user into the landing tab.
    expect(sessionTabPrompt(state.sessionTabs, state.activeSessionTabID)).toBe(
      "draft aimed at no project",
    );
  });

  it("returns to the 对话 draft page when 不使用项目 is re-selected over an open conversation", async () => {
    const context = noProjectContext();
    const source = { ...thread(), cwd: context.cwd };
    const sourceTab = createThreadSessionTab(source, context);
    const harness = buildActions({
      initial: {
        ...initialState,
        activeContext: context,
        thread: source,
        sessionTabs: [sourceTab],
        activeSessionTabID: sourceTab.id,
      },
      draft: { prompt: "keep this thread draft", images: [], files: [] },
    });

    await harness.actions.useNoProject(false);

    expect(harness.closeProjectMenus).toHaveBeenCalled();
    expect(harness.nextDraftSessionTab).toHaveBeenCalledWith(context);
    expect(harness.getAppState().thread).toBeUndefined();
    expect(harness.getAppState().activeSessionTabID).toBe("draft:test");
    expect(sessionTabPrompt(harness.getAppState().sessionTabs, sourceTab.id)).toBe(
      "keep this thread draft",
    );
  });

  it("refuses to switch to 不使用项目 while work is running", async () => {
    const context = projectContext("project-1");
    const harness = buildActions({
      initial: {
        ...initialState,
        activeContext: context,
        activeProjectId: "project-1",
        projects: [project("project-1")],
        thread: thread("running", "in_progress"),
      },
    });

    await harness.actions.useNoProject(false);

    expect(harness.closeProjectMenus).toHaveBeenCalled();
    expect(harness.getAppState().status).toBe("任务运行中，暂不能切换项目");
  });

  it("reuses an existing no-project draft when the 对话 workspace plus is clicked", async () => {
    const sourceContext = projectContext();
    const context = noProjectContext();
    const source = thread();
    const sourceTab = createThreadSessionTab(source, sourceContext);
    const existingDraft = createDraftSessionTab("draft:existing", context);
    const projectState = {
      projects: [],
      active_context: context,
    } as ProjectListResult;
    const loadRuntime = vi.fn().mockResolvedValue({
      activeContext: context,
      thread: undefined,
      threads: [],
      status: "ready",
    });
    Object.defineProperty(window, "wuu", {
      configurable: true,
      value: {
        selectNoProject: vi.fn().mockResolvedValue(projectState),
      } as Partial<WuuDesktopApi>,
    });
    const harness = buildActions({
      initial: {
        ...initialState,
        activeContext: sourceContext,
        activeProjectId: "project-1",
        thread: source,
        sessionTabs: [sourceTab, existingDraft],
        activeSessionTabID: sourceTab.id,
      },
      draft: { prompt: "keep this draft", images: [], files: [] },
      loadRuntime,
    });

    await harness.actions.useNoProject(true);

    expect(harness.nextDraftSessionTab).not.toHaveBeenCalled();
    expect(harness.getAppState().activeSessionTabID).toBe(existingDraft.id);
    expect(sessionTabPrompt(harness.getAppState().sessionTabs, sourceTab.id)).toBe(
      "keep this draft",
    );
  });
});
