import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type Dispatch,
  type SetStateAction,
} from "react";
import type {
  DesktopProject,
  RuntimeContext,
  Thread,
} from "../shared/protocol";
import {
  SCRATCH_PSEUDO_PROJECT_ID,
  isScratchThread,
  sortThreads,
  threadBelongsToProject,
  upsertThread,
} from "./AppState";
import {
  reconcileSidebarSectionOrder,
  SIDEBAR_SECTION_AGENTS,
  SIDEBAR_SECTION_GROUP,
  SIDEBAR_SECTION_PINNED,
} from "./AppSidebar";
import { desktopApiErrorMessage } from "./WorkspaceReviewHelpers";

const PROJECT_COLLAPSED_IDS_KEY = "wuu.desktop.collapsedProjectIDs";
const PROJECT_EXPANDED_IDS_KEY = "wuu.desktop.expandedProjectIDs";
const SIDEBAR_SECTION_ORDER_KEY = "wuu.desktop.sidebarSectionOrder";

export type SidebarProjectStateController = {
  collapsedProjectIDs: Set<string>;
  expandedProjectIDs: Set<string>;
  collapsingProjectIDs: Set<string>;
  projectThreadsByProjectID: Record<string, Thread[]>;
  cachedScratchThreads: Thread[];
  sidebarSectionOrder: string[];
  setSidebarSectionOrder: Dispatch<SetStateAction<string[]>>;
  loadProjectThreads: (project: DesktopProject) => Promise<void>;
  updateCachedSidebarThread: (thread: Thread) => void;
  removeCachedSidebarThread: (threadID: string) => void;
  toggleProjectCollapsed: (projectID: string) => void;
};

function storedProjectIDSet(key: string): Set<string> {
  try {
    const stored = window.localStorage.getItem(key);
    const parsed: unknown = stored ? JSON.parse(stored) : [];
    if (!Array.isArray(parsed)) {
      return new Set();
    }
    return new Set(
      parsed.filter(
        (id): id is string => typeof id === "string" && id.length > 0,
      ),
    );
  } catch {
    return new Set();
  }
}

function storedSidebarSectionOrder(): string[] | undefined {
  try {
    const stored = window.localStorage.getItem(SIDEBAR_SECTION_ORDER_KEY);
    if (!stored) return undefined;
    const parsed: unknown = JSON.parse(stored);
    if (!Array.isArray(parsed)) return undefined;
    return parsed.filter(
      (id): id is string => typeof id === "string" && id.length > 0,
    );
  } catch {
    return undefined;
  }
}

function initialCollapsedProjectIDs(): Set<string> {
  return storedProjectIDSet(PROJECT_COLLAPSED_IDS_KEY);
}

function initialExpandedProjectIDs(): Set<string> {
  return storedProjectIDSet(PROJECT_EXPANDED_IDS_KEY);
}

export function projectExpanded(
  projectID: string,
  activeProjectID: string | undefined,
  expandedProjectIDs: ReadonlySet<string>,
  collapsedProjectIDs: ReadonlySet<string>,
): boolean {
  return (
    expandedProjectIDs.has(projectID) ||
    (projectID === activeProjectID && !collapsedProjectIDs.has(projectID))
  );
}

function removeMissingIDs(
  ids: Set<string>,
  validIDs: ReadonlySet<string>,
): Set<string> {
  const next = new Set<string>();
  for (const id of ids) {
    if (validIDs.has(id)) {
      next.add(id);
    }
  }
  return next.size === ids.size ? ids : next;
}

export function threadListsEquivalent(
  left: Thread[] | undefined,
  right: Thread[],
): boolean {
  if (!left || left.length !== right.length) {
    return false;
  }
  return left.every((thread, index) => {
    const candidate = right[index];
    return (
      candidate?.id === thread.id &&
      candidate.updated_at === thread.updated_at &&
      candidate.status === thread.status &&
      candidate.pinned === thread.pinned &&
      candidate.archived === thread.archived
    );
  });
}

export function threadsForDesktopProject(
  threads: Thread[],
  project: DesktopProject,
): Thread[] {
  return sortThreads(
    threads.filter((thread) => threadBelongsToProject(thread, project)),
  );
}

export function useSidebarProjectState({
  projects,
  threads,
  activeContext,
  activeProjectID,
  setStatus,
  collapseMs,
}: {
  projects: DesktopProject[];
  threads: Thread[];
  activeContext?: RuntimeContext;
  activeProjectID?: string;
  setStatus: (status: string) => void;
  collapseMs: number;
}): SidebarProjectStateController {
  const [collapsedProjectIDs, setCollapsedProjectIDs] = useState<Set<string>>(
    initialCollapsedProjectIDs,
  );
  const [expandedProjectIDs, setExpandedProjectIDs] = useState<Set<string>>(
    initialExpandedProjectIDs,
  );
  const [collapsingProjectIDs, setCollapsingProjectIDs] = useState<Set<string>>(
    () => new Set(),
  );
  const [projectThreadsByProjectID, setProjectThreadsByProjectID] = useState<
    Record<string, Thread[]>
  >({});
  const [cachedScratchThreads, setCachedScratchThreads] = useState<Thread[]>(
    [],
  );
  const [sidebarSectionOrder, setSidebarSectionOrder] = useState<string[]>(
    () =>
      reconcileSidebarSectionOrder(
        storedSidebarSectionOrder(),
        [],
      ),
  );
  const projectCollapseTimersRef = useRef(new Map<string, number>());
  const loadingProjectThreadIDsRef = useRef(new Set<string>());
  const projectsByID = useMemo(
    () => new Map(projects.map((project) => [project.id, project])),
    [projects],
  );

  useEffect(() => {
    window.localStorage.setItem(
      PROJECT_COLLAPSED_IDS_KEY,
      JSON.stringify([...collapsedProjectIDs]),
    );
  }, [collapsedProjectIDs]);

  useEffect(() => {
    window.localStorage.setItem(
      PROJECT_EXPANDED_IDS_KEY,
      JSON.stringify([...expandedProjectIDs]),
    );
  }, [expandedProjectIDs]);

  useEffect(() => {
    window.localStorage.setItem(
      SIDEBAR_SECTION_ORDER_KEY,
      JSON.stringify(sidebarSectionOrder),
    );
  }, [sidebarSectionOrder]);

  useEffect(() => {
    const validProjectIDs = projects.map((project) => project.id);
    setSidebarSectionOrder((current) =>
      reconcileSidebarSectionOrder(current, validProjectIDs),
    );
  }, [projects]);

  useEffect(() => {
    const validProjectIDs = new Set(projects.map((project) => project.id));
    const validSectionIDs = new Set([
      ...validProjectIDs,
      SIDEBAR_SECTION_PINNED,
      SIDEBAR_SECTION_AGENTS,
      SIDEBAR_SECTION_GROUP,
      SCRATCH_PSEUDO_PROJECT_ID,
    ]);
    setCollapsedProjectIDs((current) =>
      removeMissingIDs(current, validSectionIDs),
    );
    setExpandedProjectIDs((current) =>
      removeMissingIDs(current, validSectionIDs),
    );
    setProjectThreadsByProjectID((current) => {
      const next: Record<string, Thread[]> = {};
      let changed = false;
      for (const [projectID, projectThreads] of Object.entries(current)) {
        if (validProjectIDs.has(projectID)) {
          next[projectID] = projectThreads;
        } else {
          changed = true;
        }
      }
      return changed ? next : current;
    });
  }, [projects]);

  useEffect(() => {
    if (activeContext?.kind !== "project" || !activeProjectID) {
      return;
    }
    const activeProject = projectsByID.get(activeProjectID);
    if (!activeProject) {
      return;
    }
    const activeProjectThreads = threadsForDesktopProject(threads, activeProject);
    setProjectThreadsByProjectID((current) => {
      if (threadListsEquivalent(current[activeProjectID], activeProjectThreads)) {
        return current;
      }
      return { ...current, [activeProjectID]: activeProjectThreads };
    });
    if (!collapsedProjectIDs.has(activeProjectID)) {
      setExpandedProjectIDs((current) =>
        current.has(activeProjectID)
          ? current
          : new Set(current).add(activeProjectID),
      );
    }
  }, [
    activeContext?.kind,
    activeProjectID,
    collapsedProjectIDs,
    projectsByID,
    threads,
  ]);

  useEffect(() => {
    if (activeContext?.kind !== "no_project") {
      return;
    }
    const activeScratchThreads = sortThreads(
      threads.filter((thread) => isScratchThread(thread, projects)),
    );
    setCachedScratchThreads((current) =>
      threadListsEquivalent(current, activeScratchThreads)
        ? current
        : activeScratchThreads,
    );
  }, [activeContext?.kind, projects, threads]);

  useEffect(() => {
    for (const project of projects) {
      if (
        !projectExpanded(
          project.id,
          activeProjectID,
          expandedProjectIDs,
          collapsedProjectIDs,
        )
      ) {
        continue;
      }
      if (project.id === activeProjectID) {
        continue;
      }
      if (Object.prototype.hasOwnProperty.call(projectThreadsByProjectID, project.id)) {
        continue;
      }
      void loadProjectThreads(project);
    }
  }, [
    activeProjectID,
    collapsedProjectIDs,
    expandedProjectIDs,
    projectThreadsByProjectID,
    projects,
  ]);

  useEffect(
    () => () => {
      for (const timer of projectCollapseTimersRef.current.values()) {
        window.clearTimeout(timer);
      }
      projectCollapseTimersRef.current.clear();
    },
    [],
  );

  function clearProjectCollapseTimer(projectID: string): void {
    const timer = projectCollapseTimersRef.current.get(projectID);
    if (timer === undefined) {
      return;
    }
    window.clearTimeout(timer);
    projectCollapseTimersRef.current.delete(projectID);
  }

  async function loadProjectThreads(project: DesktopProject): Promise<void> {
    if (loadingProjectThreadIDsRef.current.has(project.id)) {
      return;
    }
    loadingProjectThreadIDsRef.current.add(project.id);
    try {
      const listed = await window.wuu.listThreads(project.path);
      setProjectThreadsByProjectID((current) => ({
        ...current,
        [project.id]: threadsForDesktopProject(listed.threads, project),
      }));
    } catch (error) {
      setStatus(desktopApiErrorMessage(error, "加载项目会话失败"));
    } finally {
      loadingProjectThreadIDsRef.current.delete(project.id);
    }
  }

  function updateCachedProjectThread(thread: Thread): void {
    const projectID = projects.find(
      (project) => threadBelongsToProject(thread, project),
    )?.id;
    if (!projectID) {
      return;
    }
    setProjectThreadsByProjectID((current) => {
      const currentThreads = current[projectID];
      if (!currentThreads) {
        return current;
      }
      return {
        ...current,
        [projectID]: upsertThread(currentThreads, thread),
      };
    });
  }

  function updateCachedSidebarThread(thread: Thread): void {
    if (isScratchThread(thread, projects)) {
      setCachedScratchThreads((current) => upsertThread(current, thread));
      return;
    }
    updateCachedProjectThread(thread);
  }

  function removeCachedSidebarThread(threadID: string): void {
    setCachedScratchThreads((current) =>
      current.filter((thread) => thread.id !== threadID),
    );
    setProjectThreadsByProjectID((current) => {
      let changed = false;
      const next: Record<string, Thread[]> = {};
      for (const [projectID, projectThreads] of Object.entries(current)) {
        const filtered = projectThreads.filter((thread) => thread.id !== threadID);
        if (filtered.length !== projectThreads.length) {
          changed = true;
        }
        next[projectID] = filtered;
      }
      return changed ? next : current;
    });
  }

  function toggleProjectCollapsed(projectID: string): void {
    if (
      projectID === SIDEBAR_SECTION_PINNED ||
      projectID === SIDEBAR_SECTION_AGENTS ||
      projectID === SIDEBAR_SECTION_GROUP
    ) {
      setCollapsedProjectIDs((current) => {
        if (!current.has(projectID)) {
          return new Set(current).add(projectID);
        }
        const next = new Set(current);
        next.delete(projectID);
        return next;
      });
      return;
    }
    const expanded =
      projectExpanded(
        projectID,
        activeProjectID,
        expandedProjectIDs,
        collapsedProjectIDs,
      ) || collapsingProjectIDs.has(projectID);
    if (!expanded || collapsingProjectIDs.has(projectID)) {
      clearProjectCollapseTimer(projectID);
      setCollapsedProjectIDs((current) => {
        if (!current.has(projectID)) {
          return current;
        }
        const next = new Set(current);
        next.delete(projectID);
        return next;
      });
      setCollapsingProjectIDs((current) => {
        if (!current.has(projectID)) {
          return current;
        }
        const next = new Set(current);
        next.delete(projectID);
        return next;
      });
      setExpandedProjectIDs((current) =>
        current.has(projectID) ? current : new Set(current).add(projectID),
      );
      const project = projectsByID.get(projectID);
      if (
        project &&
        !Object.prototype.hasOwnProperty.call(
          projectThreadsByProjectID,
          projectID,
        )
      ) {
        void loadProjectThreads(project);
      }
      return;
    }

    setCollapsingProjectIDs((current) =>
      current.has(projectID) ? current : new Set(current).add(projectID),
    );
    clearProjectCollapseTimer(projectID);
    const timer = window.setTimeout(() => {
      projectCollapseTimersRef.current.delete(projectID);
      setCollapsedProjectIDs((current) =>
        current.has(projectID) ? current : new Set(current).add(projectID),
      );
      setExpandedProjectIDs((current) => {
        if (!current.has(projectID)) {
          return current;
        }
        const next = new Set(current);
        next.delete(projectID);
        return next;
      });
      setCollapsingProjectIDs((current) => {
        if (!current.has(projectID)) {
          return current;
        }
        const next = new Set(current);
        next.delete(projectID);
        return next;
      });
    }, collapseMs);
    projectCollapseTimersRef.current.set(projectID, timer);
  }

  return {
    collapsedProjectIDs,
    expandedProjectIDs,
    collapsingProjectIDs,
    projectThreadsByProjectID,
    cachedScratchThreads,
    sidebarSectionOrder,
    setSidebarSectionOrder,
    loadProjectThreads,
    updateCachedSidebarThread,
    removeCachedSidebarThread,
    toggleProjectCollapsed,
  };
}
