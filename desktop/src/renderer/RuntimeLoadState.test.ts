import { afterEach, describe, expect, it, vi } from "vitest";
import type {
  ProjectListResult,
  RuntimeContext,
  Thread,
  WuuDesktopApi,
} from "../shared/protocol";
import {
  emptyRuntimeState,
  loadRuntime,
  selectRuntimeContext,
} from "./RuntimeLoadState";

afterEach(() => {
  delete (window as unknown as { wuu?: unknown }).wuu;
  vi.restoreAllMocks();
});

function installWuuStub(overrides: Partial<WuuDesktopApi>): void {
  (window as unknown as { wuu: WuuDesktopApi }).wuu = {
    ...overrides,
  } as WuuDesktopApi;
}

function thread(id: string, overrides: Partial<Thread> = {}): Thread {
  return {
    id,
    title: id,
    preview: id,
    cwd: "/tmp/wuu",
    status: "idle",
    pinned: false,
    archived: false,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    turns: [],
    ...overrides,
  } as Thread;
}

function projectList(activeContext?: RuntimeContext): ProjectListResult {
  return {
    projects: [],
    active_context: activeContext,
  } as ProjectListResult;
}

describe("runtime load helpers", () => {
  it("returns a no-runtime state without initializing when no active context exists", async () => {
    const initialize = vi.fn();
    installWuuStub({ initialize });

    const state = await loadRuntime(projectList(undefined));

    expect(state).toEqual(emptyRuntimeState(projectList(undefined)));
    expect(initialize).not.toHaveBeenCalled();
  });

  it("keeps an unavailable active project selected without starting its runtime", async () => {
    const activeContext: RuntimeContext = {
      kind: "project",
      project_id: "project-1",
      cwd: "/tmp/offline-project",
    };
    const message =
      "工作区目录当前不可用：/tmp/offline-project。请恢复该目录，或从工作区菜单选择“重新定位…”。";
    const projectState: ProjectListResult = {
      projects: [
        {
          id: "project-1",
          name: "offline-project",
          path: activeContext.cwd,
          created_at: "2026-01-01T00:00:00Z",
          updated_at: "2026-01-01T00:00:00Z",
          missing: true,
        },
      ],
      active_context: activeContext,
      active_project_id: "project-1",
      runtime_issue: {
        code: "active_project_unavailable",
        message,
        project_id: "project-1",
        cwd: activeContext.cwd,
      },
    };
    const initialize = vi.fn();
    const listThreads = vi.fn();
    const listArchivedThreads = vi.fn();
    installWuuStub({ initialize, listThreads, listArchivedThreads });

    const state = await loadRuntime(projectState);

    expect(state.projects).toEqual(projectState.projects);
    expect(state.activeContext).toEqual(activeContext);
    expect(state.activeProjectId).toBe("project-1");
    expect(state.initialized).toBeUndefined();
    expect(state.status).toBe(message);
    expect(state.threads).toEqual([]);
    expect(initialize).not.toHaveBeenCalled();
    expect(listThreads).not.toHaveBeenCalled();
    expect(listArchivedThreads).not.toHaveBeenCalled();
  });

  it("selects project and no-project contexts through the desktop API", async () => {
    const projectContext: RuntimeContext = {
      kind: "project",
      project_id: "project-1",
      cwd: "/tmp/wuu",
    };
    const noProjectContext: RuntimeContext = {
      kind: "no_project",
      cwd: "/tmp/scratch",
    };
    const selectProject = vi
      .fn()
      .mockResolvedValue(projectList(projectContext));
    const selectNoProject = vi
      .fn()
      .mockResolvedValue(projectList(noProjectContext));
    installWuuStub({ selectProject, selectNoProject });

    await selectRuntimeContext(projectContext);
    await selectRuntimeContext(noProjectContext);

    expect(selectProject).toHaveBeenCalledWith("project-1");
    expect(selectNoProject).toHaveBeenCalledWith(false, "/tmp/scratch");
  });

  it("resumes the latest non-pinned thread when loading an active runtime", async () => {
    const activeContext: RuntimeContext = {
      kind: "no_project",
      cwd: "/tmp/wuu",
    };
    const pinned = thread("pinned", {
      pinned: true,
      updated_at: "2026-04-01T00:00:00Z",
    });
    const latest = thread("latest", {
      updated_at: "2026-03-01T00:00:00Z",
    });
    const resumed = thread("latest", {
      status: "in_progress",
      turns: [
        {
          id: "turn-1",
          status: "in_progress",
          items_view: "full",
          items: [],
        },
      ],
    } as Partial<Thread>);
    installWuuStub({
      initialize: vi.fn().mockResolvedValue({ model: "gpt-test", ultra: true }),
      listThreads: vi.fn().mockResolvedValue({ threads: [pinned, latest] }),
      listArchivedThreads: vi.fn().mockResolvedValue({ threads: [] }),
      resumeThread: vi.fn().mockResolvedValue({ thread: resumed }),
    });

    const state = await loadRuntime(projectList(activeContext));

    expect(state.thread?.id).toBe("latest");
    expect(state.initialized?.ultra).toBe(true);
    expect(state.activeContext).toEqual(activeContext);
    expect(state.running).toBe(true);
    expect(state.threads?.some((item) => item.id === "latest")).toBe(true);
  });

  it("does not auto-resume a DM when collaboration is disabled", async () => {
    const activeContext: RuntimeContext = {
      kind: "no_project",
      cwd: "/tmp/wuu",
    };
    const latestDM = thread("andy-dm", {
      dm_participant_id: "participant-andy",
      workspace_kind: "dm",
      updated_at: "2026-03-02T00:00:00Z",
    });
    const regular = thread("regular", {
      updated_at: "2026-03-01T00:00:00Z",
    });
    const resumeThread = vi.fn().mockResolvedValue({ thread: regular });
    installWuuStub({
      initialize: vi.fn().mockResolvedValue({ model: "gpt-test" }),
      listThreads: vi
        .fn()
        .mockResolvedValue({ threads: [latestDM, regular] }),
      listArchivedThreads: vi.fn().mockResolvedValue({ threads: [] }),
      resumeThread,
    });

    const state = await loadRuntime(projectList(activeContext), {
      collaborationEnabled: false,
    });

    expect(resumeThread).toHaveBeenCalledWith("regular");
    expect(state.thread?.id).toBe("regular");
    expect(state.threads?.map((item) => item.id)).toContain("andy-dm");
  });

  it("keeps DM auto-resume available when collaboration is enabled", async () => {
    const activeContext: RuntimeContext = {
      kind: "no_project",
      cwd: "/tmp/wuu",
    };
    const latestDM = thread("andy-dm", {
      dm_participant_id: "participant-andy",
      workspace_kind: "dm",
      updated_at: "2026-03-02T00:00:00Z",
    });
    const resumeThread = vi.fn().mockResolvedValue({ thread: latestDM });
    installWuuStub({
      initialize: vi.fn().mockResolvedValue({ model: "gpt-test" }),
      listThreads: vi.fn().mockResolvedValue({ threads: [latestDM] }),
      listArchivedThreads: vi.fn().mockResolvedValue({ threads: [] }),
      resumeThread,
    });

    const state = await loadRuntime(projectList(activeContext), {
      collaborationEnabled: true,
    });

    expect(resumeThread).toHaveBeenCalledWith("andy-dm");
    expect(state.thread?.id).toBe("andy-dm");
  });

  it("skips archived conversations when resuming, even if one is the most recent", async () => {
    const activeContext: RuntimeContext = {
      kind: "no_project",
      cwd: "/tmp/wuu",
    };
    const live = thread("live", { updated_at: "2026-03-01T00:00:00Z" });
    const archivedLatest = thread("archived-latest", {
      archived: true,
      updated_at: "2026-03-05T00:00:00Z",
    });
    const resumeThread = vi.fn().mockResolvedValue({ thread: live });
    installWuuStub({
      initialize: vi.fn().mockResolvedValue({ model: "gpt-test" }),
      listThreads: vi.fn().mockResolvedValue({ threads: [live] }),
      listArchivedThreads: vi
        .fn()
        .mockResolvedValue({ threads: [archivedLatest] }),
      resumeThread,
    });

    const state = await loadRuntime(projectList(activeContext));

    expect(resumeThread).toHaveBeenCalledWith("live");
    expect(state.thread?.id).toBe("live");
  });

  it("resumes nothing when every conversation is archived", async () => {
    const activeContext: RuntimeContext = {
      kind: "no_project",
      cwd: "/tmp/wuu",
    };
    const resumeThread = vi.fn();
    installWuuStub({
      initialize: vi.fn().mockResolvedValue({ model: "gpt-test" }),
      listThreads: vi.fn().mockResolvedValue({ threads: [] }),
      listArchivedThreads: vi.fn().mockResolvedValue({
        threads: [thread("archived", { archived: true })],
      }),
      resumeThread,
    });

    const state = await loadRuntime(projectList(activeContext));

    expect(resumeThread).not.toHaveBeenCalled();
    expect(state.thread).toBeUndefined();
  });

  it("keeps the runtime available when credentials need setup", async () => {
    const activeContext: RuntimeContext = {
      kind: "no_project",
      cwd: "/tmp/wuu",
    };
    installWuuStub({
      initialize: vi
        .fn()
        .mockResolvedValue({
          status: "needs_setup",
          model: "gpt-test",
          issues: [{ code: "credential_missing", message: "请配置模型凭据" }],
        }),
      listThreads: vi.fn().mockResolvedValue({ threads: [] }),
      listArchivedThreads: vi.fn().mockResolvedValue({ threads: [] }),
    });

    const state = await loadRuntime(projectList(activeContext));

    expect(state.status).toBe("请配置模型凭据");
    expect(state.activeContext).toEqual(activeContext);
  });

  it("merges archived threads into state.threads so Settings → Archive can list them", async () => {
    const activeContext: RuntimeContext = {
      kind: "no_project",
      cwd: "/tmp/wuu",
    };
    const active = thread("active-1", {
      updated_at: "2026-03-02T00:00:00Z",
    });
    const archivedFromPriorCwd = thread("archived-2", {
      archived: true,
      updated_at: "2026-03-04T00:00:00Z",
      cwd: "/tmp/another-workspace",
    });
    installWuuStub({
      initialize: vi.fn().mockResolvedValue({ model: "gpt-test" }),
      listThreads: vi.fn().mockResolvedValue({ threads: [active] }),
      listArchivedThreads: vi
        .fn()
        .mockResolvedValue({ threads: [archivedFromPriorCwd] }),
    });

    const state = await loadRuntime(projectList(activeContext), {
      resumeLatestThread: false,
    });

    // The Settings → Archive panel filters state.threads by thread.archived,
    // so the cross-cwd archived session must end up in the same array as
    // the active one — otherwise the archive page stays empty after a
    // restart for any workspace the user has ever archived a thread in.
    const ids = (state.threads ?? []).map((t) => t.id);
    expect(ids).toContain("active-1");
    expect(ids).toContain("archived-2");
    const archived = state.threads?.find((t) => t.id === "archived-2");
    expect(archived?.archived).toBe(true);
    expect(archived?.cwd).toBe("/tmp/another-workspace");
  });
});
