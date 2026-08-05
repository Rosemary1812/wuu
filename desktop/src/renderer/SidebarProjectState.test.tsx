import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { DesktopProject, RuntimeContext, Thread } from "../shared/protocol";
import {
  isThreadRunning,
  isThreadUnread,
  SCRATCH_PSEUDO_PROJECT_ID,
} from "./AppState";
import {
  SIDEBAR_SECTION_COLLAB,
  SIDEBAR_SECTION_PINNED,
} from "./AppSidebar";
import {
  useSidebarProjectState,
  type SidebarProjectStateController,
} from "./SidebarProjectState";

let mountedRoots: Root[] = [];

afterEach(() => {
  act(() => {
    for (const root of mountedRoots) root.unmount();
  });
  mountedRoots = [];
  document.body.innerHTML = "";
  window.localStorage.clear();
  Reflect.deleteProperty(window, "wuu");
  vi.restoreAllMocks();
});

async function flushEffects(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
}

function project(id: string, path = `/tmp/${id}`): DesktopProject {
  return {
    id,
    name: id,
    path,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

function thread(id: string, cwd: string): Thread {
  return {
    id,
    title: id,
    preview: id,
    cwd,
    status: "idle",
    pinned: false,
    archived: false,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    turns: [],
  } as unknown as Thread;
}

async function renderSidebarProjectState({
  projects = [],
  threads = [],
  activeContext,
  activeProjectID,
}: {
  projects?: DesktopProject[];
  threads?: Thread[];
  activeContext?: RuntimeContext;
  activeProjectID?: string;
} = {}): Promise<{
  get: () => SidebarProjectStateController;
  rerender: (next: {
    projects?: DesktopProject[];
    threads?: Thread[];
    activeContext?: RuntimeContext;
    activeProjectID?: string;
  }) => Promise<void>;
}> {
  let latest: SidebarProjectStateController | undefined;
  let props = { projects, threads, activeContext, activeProjectID };

  function Probe(nextProps: typeof props) {
    latest = useSidebarProjectState({
      projects: nextProps.projects,
      threads: nextProps.threads,
      activeContext: nextProps.activeContext,
      activeProjectID: nextProps.activeProjectID,
      setStatus: vi.fn(),
    });
    return null;
  }

  const container = document.createElement("div");
  document.body.appendChild(container);
  const root = createRoot(container);
  mountedRoots.push(root);

  async function rerender(next: Partial<typeof props>): Promise<void> {
    props = { ...props, ...next };
    await act(async () => {
      root.render(createElement(Probe, props));
      await flushEffects();
    });
  }

  await rerender(props);

  return {
    get: () => {
      if (!latest) {
        throw new Error("sidebar project state was not rendered");
      }
      return latest;
    },
    rerender,
  };
}

describe("useSidebarProjectState", () => {
  it("prunes missing project IDs while preserving pseudo section collapse IDs", async () => {
    window.localStorage.setItem(
      "wuu.desktop.collapsedProjectIDs",
      JSON.stringify(["missing-project", SIDEBAR_SECTION_PINNED]),
    );

    const hook = await renderSidebarProjectState({ projects: [] });

    expect([...hook.get().collapsedSidebarSectionIDs]).toEqual([
      SIDEBAR_SECTION_PINNED,
    ]);
  });

  it("toggles fixed pseudo sections with one click", async () => {
    const hook = await renderSidebarProjectState();

    act(() => {
      hook.get().toggleSidebarSectionCollapsed(SIDEBAR_SECTION_PINNED);
    });
    expect(hook.get().collapsedSidebarSectionIDs.has(SIDEBAR_SECTION_PINNED)).toBe(true);

    act(() => {
      hook.get().toggleSidebarSectionCollapsed(SIDEBAR_SECTION_PINNED);
    });
    expect(hook.get().collapsedSidebarSectionIDs.has(SIDEBAR_SECTION_PINNED)).toBe(false);

    act(() => {
      hook.get().toggleSidebarSectionCollapsed(SIDEBAR_SECTION_COLLAB);
    });
    expect(hook.get().collapsedSidebarSectionIDs.has(SIDEBAR_SECTION_COLLAB)).toBe(true);
    expect(hook.get().expandedSidebarSectionIDs.has(SIDEBAR_SECTION_COLLAB)).toBe(false);

    act(() => {
      hook.get().toggleSidebarSectionCollapsed(SIDEBAR_SECTION_COLLAB);
    });
    expect(hook.get().collapsedSidebarSectionIDs.has(SIDEBAR_SECTION_COLLAB)).toBe(false);
  });

  it("mirrors active project threads into the sidebar cache", async () => {
    const alpha = project("alpha", "/tmp/alpha");
    const alphaThread = thread("thread-alpha", "/tmp/alpha");
    const scratchThread = thread("thread-scratch", "/tmp/other");
    const activeContext: RuntimeContext = {
      kind: "project",
      project_id: alpha.id,
      cwd: alpha.path,
    };

    const hook = await renderSidebarProjectState({
      projects: [alpha],
      threads: [alphaThread, scratchThread],
      activeContext,
      activeProjectID: alpha.id,
    });

    expect(
      hook.get().projectThreadsByProjectID.alpha?.map((item) => item.id),
    ).toEqual(["thread-alpha"]);
  });

  it("marks expanded project sessions as loading until their snapshot arrives", async () => {
    const alpha = project("alpha", "/tmp/alpha");
    window.localStorage.setItem(
      "wuu.desktop.expandedSidebarSectionIDs",
      JSON.stringify([alpha.id]),
    );
    let resolveThreads: ((value: { threads: Thread[] }) => void) | undefined;
    Object.defineProperty(window, "wuu", {
      configurable: true,
      value: {
        listThreads: vi.fn(
          () =>
            new Promise((resolve) => {
              resolveThreads = resolve;
            }),
        ),
      },
    });

    const hook = await renderSidebarProjectState({ projects: [alpha] });
    expect(hook.get().loadingProjectThreadIDs.has(alpha.id)).toBe(true);

    await act(async () => {
      resolveThreads?.({ threads: [thread("thread-alpha", alpha.path)] });
      await flushEffects();
    });

    expect(hook.get().loadingProjectThreadIDs.has(alpha.id)).toBe(false);
    expect(hook.get().projectThreadsByProjectID.alpha?.map((item) => item.id)).toEqual([
      "thread-alpha",
    ]);
  });

  it("keeps cached sessions while an active project snapshot is incomplete", async () => {
    const alpha = project("alpha", "/tmp/alpha");
    const first = thread("thread-first", "/tmp/alpha");
    const second = thread("thread-second", "/tmp/alpha");
    const activeContext: RuntimeContext = {
      kind: "project",
      project_id: alpha.id,
      cwd: alpha.path,
    };
    const hook = await renderSidebarProjectState({
      projects: [alpha],
      threads: [first, second],
      activeContext,
      activeProjectID: alpha.id,
    });

    await hook.rerender({ threads: [second] });

    expect(
      hook.get().projectThreadsByProjectID.alpha?.map((item) => item.id).sort(),
    ).toEqual(["thread-first", "thread-second"]);
  });

  it("keeps folded workspace session status current from background server events", async () => {
    const alpha = project("alpha", "/tmp/alpha");
    const beta = project("beta", "/tmp/beta");
    const betaThread = thread("thread-beta", beta.path);
    const hook = await renderSidebarProjectState({ projects: [alpha, beta] });

    act(() => {
      hook.get().cacheSidebarThreads([betaThread]);
      hook.get().syncSidebarServerEvent({
        kind: "notification",
        workdir: beta.path,
        message: {
          method: "turn/started",
          params: {
            thread_id: betaThread.id,
            turn: {
              id: "turn-beta",
              items: [],
              items_view: "full",
              status: "in_progress",
            },
          },
        },
      });
    });

    expect(hook.get().expandedSidebarSectionIDs.has(beta.id)).toBe(false);
    const runningThread = hook.get().projectThreadsByProjectID.beta?.[0];
    expect(runningThread?.turns.at(-1)).toMatchObject({
      id: "turn-beta",
      status: "in_progress",
    });
    expect(isThreadRunning(runningThread)).toBe(true);

    act(() => {
      hook.get().syncSidebarServerEvent({
        kind: "notification",
        workdir: beta.path,
        message: {
          method: "turn/completed",
          params: {
            thread_id: betaThread.id,
            turn: {
              id: "turn-beta",
              items: [],
              items_view: "full",
              status: "completed",
            },
          },
        },
      });
    });

    const completedThread = hook.get().projectThreadsByProjectID.beta?.[0];
    expect(completedThread?.turns.at(-1)).toMatchObject({
      id: "turn-beta",
      status: "completed",
    });
    expect(isThreadRunning(completedThread)).toBe(false);
    expect(isThreadUnread(completedThread, undefined)).toBe(true);
  });

  it("caches search results across projects without requiring sidebar expansion", async () => {
    const alpha = project("alpha", "/tmp/alpha");
    const beta = project("beta", "/tmp/beta");
    const scratch = thread("thread-scratch", "/tmp/scratch");
    const hook = await renderSidebarProjectState({ projects: [alpha, beta] });

    act(() => {
      hook.get().cacheSidebarThreads([
        thread("thread-alpha", alpha.path),
        thread("thread-beta", beta.path),
        scratch,
      ]);
    });

    expect(hook.get().projectThreadsByProjectID.alpha?.map((item) => item.id)).toEqual([
      "thread-alpha",
    ]);
    expect(hook.get().projectThreadsByProjectID.beta?.map((item) => item.id)).toEqual([
      "thread-beta",
    ]);
    expect(hook.get().cachedScratchThreads.map((item) => item.id)).toEqual([
      "thread-scratch",
    ]);
  });

  it("patches a cached project session pin immediately", async () => {
    const alpha = project("alpha", "/tmp/alpha");
    const cached = thread("thread-alpha", alpha.path);
    const hook = await renderSidebarProjectState({ projects: [alpha] });
    act(() => {
      hook.get().cacheSidebarThreads([cached]);
    });

    act(() => {
      hook.get().updateCachedSidebarThreadPinned(cached.id, true);
    });

    expect(hook.get().projectThreadsByProjectID.alpha?.[0]?.pinned).toBe(true);
  });

  it("keeps scratch threads cached for the no-project context", async () => {
    const alpha = project("alpha", "/tmp/alpha");
    const scratchThread = thread("thread-scratch", "/tmp/other");
    const activeContext: RuntimeContext = {
      kind: "no_project",
      cwd: "/tmp/other",
    };

    const hook = await renderSidebarProjectState({
      projects: [alpha],
      threads: [scratchThread],
      activeContext,
    });

    expect(hook.get().cachedScratchThreads.map((item) => item.id)).toEqual([
      "thread-scratch",
    ]);
    expect(
      hook.get().collapsedSidebarSectionIDs.has(SCRATCH_PSEUDO_PROJECT_ID),
    ).toBe(false);
  });
});
