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
      initialize: vi.fn().mockResolvedValue({ model: "gpt-test" }),
      listThreads: vi.fn().mockResolvedValue({ threads: [pinned, latest] }),
      resumeThread: vi.fn().mockResolvedValue({ thread: resumed }),
    });

    const state = await loadRuntime(projectList(activeContext));

    expect(state.thread?.id).toBe("latest");
    expect(state.activeContext).toEqual(activeContext);
    expect(state.running).toBe(true);
    expect(state.threads?.some((item) => item.id === "latest")).toBe(true);
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
    });

    const state = await loadRuntime(projectList(activeContext));

    expect(state.status).toBe("请配置模型凭据");
    expect(state.activeContext).toEqual(activeContext);
  });
});
