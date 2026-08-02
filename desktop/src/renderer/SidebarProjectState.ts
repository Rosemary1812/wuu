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
  SIDEBAR_SECTION_COLLAB,
  SIDEBAR_SECTION_PINNED,
} from "./AppSidebar";
import { desktopApiErrorMessage } from "./WorkspaceReviewHelpers";
import { translateCurrent } from "./i18n";

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
  loadingProjectThreadIDs: ReadonlySet<string>;
  projectThreadsByProjectID: Record<string, Thread[]>;
  cachedScratchThreads: Thread[];
  sidebarSectionOrder: string[];
  setSidebarSectionOrder: Dispatch<SetStateAction<string[]>>;
  loadProjectThreads: (project: DesktopProject) => Promise<void>;
  cacheSidebarThreads: (threads: Thread[]) => void;
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
  const expanded = storedSidebarSectionIDSet(
    SIDEBAR_EXPANDED_SECTION_IDS_KEY,
    LEGACY_PROJECT_EXPANDED_IDS_KEY,
  );
  // 对话 defaults to expanded. Older builds auto-expanded it whenever the
  // no_project context was active without ever persisting that into the
  // expanded set, so a stored state without an explicit collapse marker
  // means "open". A user collapse writes the marker and wins from then on.
  if (!initialCollapsedSidebarSectionIDs().has(SCRATCH_PSEUDO_PROJECT_ID)) {
    expanded.add(SCRATCH_PSEUDO_PROJECT_ID);
  }
  return expanded;
}

/**
 * Session-tree (对话 / project) sections expand ONLY via their own header
 * toggle: expanded ⇔ the id is in the persisted expanded set. Selecting a
 * session, switching the active project/context, or opening a pinned
 * session must never change any section's expand state — the sidebar's
 * expand/collapse is purely manual (user mental model: clicking a session
 * opens its tab; clicking a header toggles that header's section).
 */
export function sessionTreeSectionExpanded(
  sectionID: string,
  expandedSidebarSectionIDs: ReadonlySet<string>,
): boolean {
  return expandedSidebarSectionIDs.has(sectionID);
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

export function mergeSidebarThreadSnapshots(
  cached: Thread[] | undefined,
  incoming: Thread[],
): Thread[] {
  let merged = cached ?? [];
  for (const thread of incoming) {
    merged = upsertThread(merged, thread);
  }
  return sortThreads(merged);
}

export function threadsForDesktopProject(
  threads: Thread[],
  project: DesktopProject,
): Thread[] {
  // Archived threads intentionally stay in `state.threads` so Settings → Archive
  // can list them; this helper feeds the sidebar surfaces and must hide them.
  return sortThreads(
    threads.filter(
      (thread) =>
        !thread.ephemeral &&
        !thread.archived &&
        threadBelongsToProject(thread, project),
    ),
  );
}

export function useSidebarProjectState({
  projects,
  threads,
  activeContext,
  activeProjectID,
  setStatus,
}: {
  projects: DesktopProject[];
  threads: Thread[];
  activeContext?: RuntimeContext;
  activeProjectID?: string;
  setStatus: (status: string) => void;
}): SidebarProjectStateController {
  const [collapsedSidebarSectionIDs, setCollapsedSidebarSectionIDs] =
    useState<Set<string>>(initialCollapsedSidebarSectionIDs);
  const [expandedSidebarSectionIDs, setExpandedSidebarSectionIDs] =
    useState<Set<string>>(initialExpandedSidebarSectionIDs);
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
  const loadingProjectThreadIDsRef = useRef(new Set<string>());
  const projectsByID = useMemo(
    () => new Map(projects.map((project) => [project.id, project])),
    [projects],
  );
  const loadingProjectThreadIDs = useMemo(() => {
    const loading = new Set(loadingProjectThreadIDsRef.current);
    for (const project of projects) {
      if (
        project.id !== activeProjectID &&
        sessionTreeSectionExpanded(project.id, expandedSidebarSectionIDs) &&
        !Object.prototype.hasOwnProperty.call(projectThreadsByProjectID, project.id)
      ) {
        loading.add(project.id);
      }
    }
    return loading;
  }, [activeProjectID, expandedSidebarSectionIDs, projectThreadsByProjectID, projects]);

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
      reconcileSidebarSectionOrder(
        current,
        validProjectIDs,
      ),
    );
  }, [projects]);

  useEffect(() => {
    const validProjectIDs = new Set(projects.map((project) => project.id));
    const validSectionIDs = new Set([
      ...validProjectIDs,
      SIDEBAR_SECTION_PINNED,
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
      const merged = mergeSidebarThreadSnapshots(
        current[activeProjectID],
        activeProjectThreads,
      );
      if (threadListsEquivalent(current[activeProjectID], merged)) {
        return current;
      }
      return { ...current, [activeProjectID]: merged };
    });
  }, [activeContext?.kind, activeProjectID, projectsByID, threads]);

  useEffect(() => {
    if (activeContext?.kind !== "no_project") {
      return;
    }
    const activeScratchThreads = sortThreads(
      threads.filter(
        (thread) => !thread.ephemeral && isScratchThread(thread, projects),
      ),
    );
    setCachedScratchThreads((current) =>
      threadListsEquivalent(current, activeScratchThreads)
        ? current
        : activeScratchThreads,
    );
  }, [activeContext?.kind, projects, threads]);

  useEffect(() => {
    for (const project of projects) {
      if (!sessionTreeSectionExpanded(project.id, expandedSidebarSectionIDs)) {
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
    expandedSidebarSectionIDs,
    projectThreadsByProjectID,
    projects,
  ]);

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
      setStatus(desktopApiErrorMessage(error, translateCurrent("project.threadsLoadFailed")));
    } finally {
      loadingProjectThreadIDsRef.current.delete(project.id);
    }
  }

  function cacheSidebarThreads(incoming: Thread[]): void {
    const scratchThreads = incoming.filter((thread) => isScratchThread(thread, projects));
    if (scratchThreads.length > 0) {
      setCachedScratchThreads((current) =>
        mergeSidebarThreadSnapshots(current, scratchThreads),
      );
    }
    setProjectThreadsByProjectID((current) => {
      let next = current;
      for (const project of projects) {
        const projectThreads = threadsForDesktopProject(incoming, project);
        if (projectThreads.length === 0) {
          continue;
        }
        if (next === current) {
          next = { ...current };
        }
        next[project.id] = mergeSidebarThreadSnapshots(
          current[project.id],
          projectThreads,
        );
      }
      return next;
    });
  }

  function updateCachedSidebarThread(thread: Thread): void {
    cacheSidebarThreads([thread]);
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
    // Fixed sections are pure manual sidebar sections:
    // expanded ⇔ !collapsedSidebarSectionIDs.has(id).
    if (
      sectionID === SIDEBAR_SECTION_PINNED ||
      sectionID === SIDEBAR_SECTION_COLLAB
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
    // 对话 / project tree sections: expanded ⇔ the id is in the expanded
    // set. The collapse motion lives inside SidebarSection (it keeps the
    // body mounted while animating), so the state flip is immediate here.
    const expanded = sessionTreeSectionExpanded(
      sectionID,
      expandedSidebarSectionIDs,
    );
    if (!expanded) {
      setCollapsedSidebarSectionIDs((current) => {
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
  }

  return {
    collapsedSidebarSectionIDs,
    expandedSidebarSectionIDs,
    loadingProjectThreadIDs,
    projectThreadsByProjectID,
    cachedScratchThreads,
    sidebarSectionOrder,
    setSidebarSectionOrder,
    loadProjectThreads,
    cacheSidebarThreads,
    updateCachedSidebarThread,
    removeCachedSidebarThread,
    toggleSidebarSectionCollapsed,
  };
}
