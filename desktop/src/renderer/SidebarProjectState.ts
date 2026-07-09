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

const SIDEBAR_COLLAPSED_SECTION_IDS_KEY =
  "wuu.desktop.collapsedSidebarSectionIDs";
const SIDEBAR_EXPANDED_SECTION_IDS_KEY =
  "wuu.desktop.expandedSidebarSectionIDs";
const LEGACY_PROJECT_COLLAPSED_IDS_KEY = "wuu.desktop.collapsedProjectIDs";
const LEGACY_PROJECT_EXPANDED_IDS_KEY = "wuu.desktop.expandedProjectIDs";
const SIDEBAR_SECTION_ORDER_KEY = "wuu.desktop.sidebarSectionOrder";

export type SidebarProjectStateController = {
  collapsedSidebarSectionIDs: Set<string>;
  expandedSidebarSectionIDs: Set<string>;
  collapsingSidebarSectionIDs: Set<string>;
  projectThreadsByProjectID: Record<string, Thread[]>;
  cachedScratchThreads: Thread[];
  sidebarSectionOrder: string[];
  setSidebarSectionOrder: Dispatch<SetStateAction<string[]>>;
  loadProjectThreads: (project: DesktopProject) => Promise<void>;
  updateCachedSidebarThread: (thread: Thread) => void;
  removeCachedSidebarThread: (threadID: string) => void;
  toggleSidebarSectionCollapsed: (sectionID: string) => void;
};

function storedSidebarSectionIDSet(
  key: string,
  legacyKey?: string,
): Set<string> {
  try {
    const stored =
      window.localStorage.getItem(key) ??
      (legacyKey ? window.localStorage.getItem(legacyKey) : null);
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

function initialCollapsedSidebarSectionIDs(): Set<string> {
  return storedSidebarSectionIDSet(
    SIDEBAR_COLLAPSED_SECTION_IDS_KEY,
    LEGACY_PROJECT_COLLAPSED_IDS_KEY,
  );
}

function initialExpandedSidebarSectionIDs(): Set<string> {
  return storedSidebarSectionIDSet(
    SIDEBAR_EXPANDED_SECTION_IDS_KEY,
    LEGACY_PROJECT_EXPANDED_IDS_KEY,
  );
}

export function activeSidebarSectionID(
  activeContext: RuntimeContext | undefined,
  activeProjectID: string | undefined,
): string | undefined {
  return activeContext?.kind === "no_project"
    ? SCRATCH_PSEUDO_PROJECT_ID
    : activeProjectID;
}

export function sessionTreeSectionExpanded(
  sectionID: string,
  activeSectionID: string | undefined,
  expandedSidebarSectionIDs: ReadonlySet<string>,
  collapsedSidebarSectionIDs: ReadonlySet<string>,
): boolean {
  return (
    expandedSidebarSectionIDs.has(sectionID) ||
    (sectionID === activeSectionID &&
      !collapsedSidebarSectionIDs.has(sectionID))
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
  const [collapsedSidebarSectionIDs, setCollapsedSidebarSectionIDs] =
    useState<Set<string>>(initialCollapsedSidebarSectionIDs);
  const [expandedSidebarSectionIDs, setExpandedSidebarSectionIDs] =
    useState<Set<string>>(initialExpandedSidebarSectionIDs);
  const [collapsingSidebarSectionIDs, setCollapsingSidebarSectionIDs] =
    useState<Set<string>>(() => new Set());
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
  const sidebarSectionCollapseTimersRef = useRef(new Map<string, number>());
  const loadingProjectThreadIDsRef = useRef(new Set<string>());
  const projectsByID = useMemo(
    () => new Map(projects.map((project) => [project.id, project])),
    [projects],
  );

  useEffect(() => {
    window.localStorage.setItem(
      SIDEBAR_COLLAPSED_SECTION_IDS_KEY,
      JSON.stringify([...collapsedSidebarSectionIDs]),
    );
  }, [collapsedSidebarSectionIDs]);

  useEffect(() => {
    window.localStorage.setItem(
      SIDEBAR_EXPANDED_SECTION_IDS_KEY,
      JSON.stringify([...expandedSidebarSectionIDs]),
    );
  }, [expandedSidebarSectionIDs]);

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
    setCollapsedSidebarSectionIDs((current) =>
      removeMissingIDs(current, validSectionIDs),
    );
    setExpandedSidebarSectionIDs((current) =>
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
    if (!collapsedSidebarSectionIDs.has(activeProjectID)) {
      setExpandedSidebarSectionIDs((current) =>
        current.has(activeProjectID)
          ? current
          : new Set(current).add(activeProjectID),
      );
    }
  }, [
    activeContext?.kind,
    activeProjectID,
    collapsedSidebarSectionIDs,
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
    const activeSectionID = activeSidebarSectionID(
      activeContext,
      activeProjectID,
    );
    for (const project of projects) {
      if (
        !sessionTreeSectionExpanded(
          project.id,
          activeSectionID,
          expandedSidebarSectionIDs,
          collapsedSidebarSectionIDs,
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
    activeContext,
    activeProjectID,
    collapsedSidebarSectionIDs,
    expandedSidebarSectionIDs,
    projectThreadsByProjectID,
    projects,
  ]);

  useEffect(
    () => () => {
      for (const timer of sidebarSectionCollapseTimersRef.current.values()) {
        window.clearTimeout(timer);
      }
      sidebarSectionCollapseTimersRef.current.clear();
    },
    [],
  );

  function clearSidebarSectionCollapseTimer(sectionID: string): void {
    const timer = sidebarSectionCollapseTimersRef.current.get(sectionID);
    if (timer === undefined) {
      return;
    }
    window.clearTimeout(timer);
    sidebarSectionCollapseTimersRef.current.delete(sectionID);
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

  function toggleSidebarSectionCollapsed(sectionID: string): void {
    // Pinned / Agents / 群聊 sections are pure manual sidebar sections:
    // expanded ⇔ !collapsedSidebarSectionIDs.has(id). The 对话 / project
    // tree sections below also account for the currently active sidebar
    // section and keep the delayed body-collapse behavior.
    if (
      sectionID === SIDEBAR_SECTION_PINNED ||
      sectionID === SIDEBAR_SECTION_AGENTS ||
      sectionID === SIDEBAR_SECTION_GROUP
    ) {
      setCollapsedSidebarSectionIDs((current) => {
        if (!current.has(sectionID)) {
          return new Set(current).add(sectionID);
        }
        const next = new Set(current);
        next.delete(sectionID);
        return next;
      });
      return;
    }
    const activeSectionID = activeSidebarSectionID(
      activeContext,
      activeProjectID,
    );
    const expanded =
      sessionTreeSectionExpanded(
        sectionID,
        activeSectionID,
        expandedSidebarSectionIDs,
        collapsedSidebarSectionIDs,
      ) || collapsingSidebarSectionIDs.has(sectionID);
    if (!expanded || collapsingSidebarSectionIDs.has(sectionID)) {
      clearSidebarSectionCollapseTimer(sectionID);
      setCollapsedSidebarSectionIDs((current) => {
        if (!current.has(sectionID)) {
          return current;
        }
        const next = new Set(current);
        next.delete(sectionID);
        return next;
      });
      setCollapsingSidebarSectionIDs((current) => {
        if (!current.has(sectionID)) {
          return current;
        }
        const next = new Set(current);
        next.delete(sectionID);
        return next;
      });
      setExpandedSidebarSectionIDs((current) =>
        current.has(sectionID) ? current : new Set(current).add(sectionID),
      );
      const project = projectsByID.get(sectionID);
      if (
        project &&
        !Object.prototype.hasOwnProperty.call(
          projectThreadsByProjectID,
          sectionID,
        )
      ) {
        void loadProjectThreads(project);
      }
      return;
    }

    setCollapsingSidebarSectionIDs((current) =>
      current.has(sectionID) ? current : new Set(current).add(sectionID),
    );
    clearSidebarSectionCollapseTimer(sectionID);
    const timer = window.setTimeout(() => {
      sidebarSectionCollapseTimersRef.current.delete(sectionID);
      setCollapsedSidebarSectionIDs((current) =>
        current.has(sectionID) ? current : new Set(current).add(sectionID),
      );
      setExpandedSidebarSectionIDs((current) => {
        if (!current.has(sectionID)) {
          return current;
        }
        const next = new Set(current);
        next.delete(sectionID);
        return next;
      });
      setCollapsingSidebarSectionIDs((current) => {
        if (!current.has(sectionID)) {
          return current;
        }
        const next = new Set(current);
        next.delete(sectionID);
        return next;
      });
    }, collapseMs);
    sidebarSectionCollapseTimersRef.current.set(sectionID, timer);
  }

  return {
    collapsedSidebarSectionIDs,
    expandedSidebarSectionIDs,
    collapsingSidebarSectionIDs,
    projectThreadsByProjectID,
    cachedScratchThreads,
    sidebarSectionOrder,
    setSidebarSectionOrder,
    loadProjectThreads,
    updateCachedSidebarThread,
    removeCachedSidebarThread,
    toggleSidebarSectionCollapsed,
  };
}
