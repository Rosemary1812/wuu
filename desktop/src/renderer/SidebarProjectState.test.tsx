import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { DesktopProject, RuntimeContext, Thread } from "../shared/protocol";
import {
  SCRATCH_PSEUDO_PROJECT_ID,
} from "./AppState";
import {
  SIDEBAR_SECTION_AGENTS,
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
      collapseMs: 25,
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
      JSON.stringify(["missing-project", SIDEBAR_SECTION_PINNED, SIDEBAR_SECTION_AGENTS]),
    );

    const hook = await renderSidebarProjectState({ projects: [] });

    expect([...hook.get().collapsedProjectIDs].sort()).toEqual([
      SIDEBAR_SECTION_AGENTS,
      SIDEBAR_SECTION_PINNED,
    ]);
  });

  it("toggles pseudo sections without starting project loading", async () => {
    const hook = await renderSidebarProjectState();

    act(() => {
      hook.get().toggleProjectCollapsed(SIDEBAR_SECTION_PINNED);
    });
    expect(hook.get().collapsedProjectIDs.has(SIDEBAR_SECTION_PINNED)).toBe(true);

    act(() => {
      hook.get().toggleProjectCollapsed(SIDEBAR_SECTION_PINNED);
    });
    expect(hook.get().collapsedProjectIDs.has(SIDEBAR_SECTION_PINNED)).toBe(false);
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
    expect(hook.get().collapsedProjectIDs.has(SCRATCH_PSEUDO_PROJECT_ID)).toBe(
      false,
    );
  });
});
