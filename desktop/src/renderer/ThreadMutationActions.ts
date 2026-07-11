import type { MutableRefObject, SetStateAction } from "react";
import type { Agent, Thread } from "../shared/protocol";
import {
  activeThreadIDForState,
  ensureSessionTab,
  isDMThread,
  isGroupThread,
  isThreadRunning,
  removeSessionTab,
  threadSessionTabID,
  upsertThread,
  type AppState,
  type SessionTab,
  type ThreadSummary,
} from "./AppState";
import { desktopApiErrorMessage } from "./WorkspaceReviewHelpers";

type SetAppState = (update: SetStateAction<AppState>) => void;

export type ThreadMutationActionsDeps = {
  getAppState: () => AppState;
  setAppState: SetAppState;
  getActiveThreadID: () => string | undefined;
  getArchiveConfirmSubagentID: () => string | undefined;
  setArchiveConfirmSubagentID: (
    update: SetStateAction<string | undefined>,
  ) => void;
  localDemoThreadsRef: MutableRefObject<Map<string, Thread>>;
  nextDraftSessionTab: (context: NonNullable<AppState["activeContext"]>) => SessionTab;
  clearPrimaryComposerDraft: () => void;
  resetSplitComposerDrafts: () => void;
  updateCachedSidebarThread: (thread: Thread) => void;
  removeCachedSidebarThread: (threadID: string) => void;
  clearThreadPendingComposerMessages: (threadID: string) => void;
};

export type ThreadMutationActions = {
  toggleThreadPinned: (thread: ThreadSummary) => Promise<void>;
  renameThread: (thread: ThreadSummary, title: string) => Promise<void>;
  removeThreadMemberByID: (
    threadID: string,
    participantID: string,
  ) => Promise<void>;
  addThreadMemberByID: (
    threadID: string,
    participantID: string,
  ) => Promise<void>;
  archiveThread: (thread: ThreadSummary) => Promise<void>;
  unarchiveThread: (thread: Pick<ThreadSummary, "id">) => Promise<void>;
  deleteThread: (thread: ThreadSummary) => Promise<void>;
  toggleSubagentPinned: (agent: Agent) => Promise<void>;
  archiveSubagent: (agent: Agent) => Promise<void>;
};

function patchChildAgentInThread(
  thread: Thread | undefined,
  agentID: string,
  patch: Partial<Agent>,
): Thread | undefined {
  if (!thread || !thread.child_agents) {
    return thread;
  }
  const index = thread.child_agents.findIndex((agent) => agent.id === agentID);
  if (index === -1) {
    return thread;
  }
  return {
    ...thread,
    child_agents: thread.child_agents.map((agent, i) =>
      i === index ? { ...agent, ...patch } : agent,
    ),
  };
}

export function createThreadMutationActions(
  deps: ThreadMutationActionsDeps,
): ThreadMutationActions {
  function setStatus(status: string): void {
    deps.setAppState((current) => ({
      ...current,
      status,
    }));
  }

  function upsertLocalDemoThread(thread: Thread): void {
    deps.localDemoThreadsRef.current = new Map([
      ...deps.localDemoThreadsRef.current,
      [thread.id, thread],
    ]);
  }

  function removeLocalDemoThread(threadID: string): void {
    deps.localDemoThreadsRef.current = new Map(
      [...deps.localDemoThreadsRef.current].filter(([id]) => id !== threadID),
    );
  }

  function clearActiveComposer(): void {
    deps.clearPrimaryComposerDraft();
    deps.resetSplitComposerDrafts();
  }

  async function toggleThreadPinned(thread: ThreadSummary): Promise<void> {
    if (!deps.getAppState().activeContext) {
      return;
    }
    const localDemoThread = deps.localDemoThreadsRef.current.get(thread.id);
    if (localDemoThread) {
      const nextThread = { ...localDemoThread, pinned: !thread.pinned };
      upsertLocalDemoThread(nextThread);
      deps.setAppState((current) => ({
        ...current,
        thread: current.thread?.id === thread.id ? nextThread : current.thread,
        secondaryThread:
          current.secondaryThread?.id === thread.id
            ? nextThread
            : current.secondaryThread,
        threads: upsertThread(current.threads, nextThread),
        status: current.status === "ready" ? "ready" : current.status,
      }));
      return;
    }
    try {
      const result = await window.wuu.pinThread(thread.id, !thread.pinned);
      deps.updateCachedSidebarThread(result.thread);
      deps.setAppState((current) => ({
        ...current,
        thread:
          current.thread?.id === thread.id ? result.thread : current.thread,
        secondaryThread:
          current.secondaryThread?.id === thread.id
            ? result.thread
            : current.secondaryThread,
        threads:
          current.activeContext?.cwd === result.thread.cwd ||
          isDMThread(result.thread) ||
          isGroupThread(result.thread)
            ? upsertThread(current.threads, result.thread)
            : current.threads,
        status: current.status === "ready" ? "ready" : current.status,
      }));
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "pin thread failed");
    }
  }

  async function renameThread(
    thread: ThreadSummary,
    title: string,
  ): Promise<void> {
    const trimmed = title.trim();
    if (!trimmed) {
      return;
    }
    const localDemoThread = deps.localDemoThreadsRef.current.get(thread.id);
    if (localDemoThread) {
      const nextThread = { ...localDemoThread, title: trimmed };
      upsertLocalDemoThread(nextThread);
      deps.setAppState((current) => ({
        ...current,
        thread: current.thread?.id === thread.id ? nextThread : current.thread,
        secondaryThread:
          current.secondaryThread?.id === thread.id
            ? nextThread
            : current.secondaryThread,
        threads: upsertThread(current.threads, nextThread),
        status: current.status === "ready" ? "ready" : current.status,
      }));
      return;
    }
    try {
      const result = await window.wuu.renameThread(thread.id, trimmed);
      deps.updateCachedSidebarThread(result.thread);
      deps.setAppState((current) => ({
        ...current,
        thread:
          current.thread?.id === thread.id ? result.thread : current.thread,
        secondaryThread:
          current.secondaryThread?.id === thread.id
            ? result.thread
            : current.secondaryThread,
        threads:
          current.threads.some((item) => item.id === result.thread.id) ||
          current.activeContext?.cwd === result.thread.cwd ||
          isDMThread(result.thread) ||
          isGroupThread(result.thread)
            ? upsertThread(current.threads, result.thread)
            : current.threads,
        status: current.status === "ready" ? "ready" : current.status,
      }));
    } catch (error) {
      setStatus(desktopApiErrorMessage(error, "重命名对话失败"));
    }
  }

  async function removeThreadMemberByID(
    threadID: string,
    participantID: string,
  ): Promise<void> {
    try {
      const result = await window.wuu.removeThreadMember(threadID, participantID);
      deps.updateCachedSidebarThread(result.thread);
      deps.setAppState((current) => ({
        ...current,
        thread: current.thread?.id === threadID ? result.thread : current.thread,
        secondaryThread:
          current.secondaryThread?.id === threadID
            ? result.thread
            : current.secondaryThread,
        threads: upsertThread(current.threads, result.thread),
        status: current.status === "ready" ? "ready" : current.status,
      }));
    } catch (error) {
      setStatus(
        error instanceof Error ? error.message : "remove thread member failed",
      );
    }
  }

  async function addThreadMemberByID(
    threadID: string,
    participantID: string,
  ): Promise<void> {
    try {
      const result = await window.wuu.addThreadMember(threadID, participantID);
      deps.updateCachedSidebarThread(result.thread);
      deps.setAppState((current) => ({
        ...current,
        thread: current.thread?.id === threadID ? result.thread : current.thread,
        secondaryThread:
          current.secondaryThread?.id === threadID
            ? result.thread
            : current.secondaryThread,
        threads: upsertThread(current.threads, result.thread),
        status: current.status === "ready" ? "ready" : current.status,
      }));
    } catch (error) {
      setStatus(
        error instanceof Error ? error.message : "add thread member failed",
      );
    }
  }

  async function archiveThread(thread: ThreadSummary): Promise<void> {
    const currentState = deps.getAppState();
    const isLocalDemoThread = deps.localDemoThreadsRef.current.has(thread.id);
    if (
      !currentState.activeContext ||
      (!isLocalDemoThread && isThreadRunning(thread))
    ) {
      return;
    }
    deps.clearThreadPendingComposerMessages(thread.id);
    const archivedActiveThread = thread.id === deps.getActiveThreadID();
    const fallbackDraft = archivedActiveThread
      ? deps.nextDraftSessionTab(currentState.activeContext)
      : undefined;
    if (archivedActiveThread) {
      clearActiveComposer();
    }
    if (isLocalDemoThread) {
      removeLocalDemoThread(thread.id);
      deps.setAppState((current) => {
        const nextTabs = removeSessionTab(
          current.sessionTabs,
          threadSessionTabID(thread.id),
        );
        return archiveMarkThreadState(current, thread.id, true, nextTabs, fallbackDraft);
      });
      return;
    }
    try {
      const result = await window.wuu.archiveThread(thread.id, true);
      deps.updateCachedSidebarThread(result.thread);
      deps.setAppState((current) => {
        const nextTabs = removeSessionTab(
          current.sessionTabs,
          threadSessionTabID(thread.id),
        );
        return archiveMarkThreadState(
          current,
          result.thread.id,
          true,
          nextTabs,
          fallbackDraft,
        );
      });
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "archive thread failed");
    }
  }

  async function unarchiveThread(thread: Pick<ThreadSummary, "id">): Promise<void> {
    const isLocalDemoThread = deps.localDemoThreadsRef.current.has(thread.id);
    if (isLocalDemoThread) {
      const localDemoThread = deps.localDemoThreadsRef.current.get(thread.id);
      if (!localDemoThread) {
        return;
      }
      const nextThread = { ...localDemoThread, archived: false };
      upsertLocalDemoThread(nextThread);
      deps.setAppState((current) => ({
        ...current,
        thread:
          current.thread?.id === thread.id ? nextThread : current.thread,
        secondaryThread:
          current.secondaryThread?.id === thread.id
            ? nextThread
            : current.secondaryThread,
        threads: upsertThread(current.threads, nextThread),
        status: current.status === "ready" ? "ready" : current.status,
      }));
      return;
    }
    try {
      const result = await window.wuu.archiveThread(thread.id, false);
      deps.updateCachedSidebarThread(result.thread);
      deps.setAppState((current) => ({
        ...current,
        thread:
          current.thread?.id === thread.id ? result.thread : current.thread,
        secondaryThread:
          current.secondaryThread?.id === thread.id
            ? result.thread
            : current.secondaryThread,
        threads: upsertThread(current.threads, result.thread),
        status: current.status === "ready" ? "ready" : current.status,
      }));
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "unarchive thread failed");
    }
  }

  async function deleteThread(thread: ThreadSummary): Promise<void> {
    const currentState = deps.getAppState();
    const isLocalDemoThread = deps.localDemoThreadsRef.current.has(thread.id);
    if (
      !currentState.activeContext ||
      (!isLocalDemoThread && isThreadRunning(thread))
    ) {
      return;
    }
    deps.clearThreadPendingComposerMessages(thread.id);
    const deletedActiveThread = thread.id === deps.getActiveThreadID();
    const fallbackDraft = deletedActiveThread
      ? deps.nextDraftSessionTab(currentState.activeContext)
      : undefined;
    if (deletedActiveThread) {
      clearActiveComposer();
    }
    try {
      if (isLocalDemoThread) {
        removeLocalDemoThread(thread.id);
      } else {
        await window.wuu.deleteThread(thread.id);
      }
      deps.removeCachedSidebarThread(thread.id);
      deps.setAppState((current) => {
        const nextTabs = removeSessionTab(
          current.sessionTabs,
          threadSessionTabID(thread.id),
        );
        return archiveMarkThreadState(current, thread.id, false, nextTabs, fallbackDraft);
      });
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "delete thread failed");
    }
  }

  function archiveMarkThreadState(
    current: AppState,
    threadID: string,
    archived: boolean,
    nextTabs: AppState["sessionTabs"],
    fallbackDraft: SessionTab | undefined,
  ): AppState {
    return {
      ...current,
      thread: current.thread?.id === threadID ? undefined : current.thread,
      secondaryThread:
        current.secondaryThread?.id === threadID
          ? undefined
          : current.secondaryThread,
      activePane:
        current.activePane === "secondary" &&
        current.secondaryThread?.id === threadID
          ? "primary"
          : current.activePane,
      sessionTabs: fallbackDraft ? ensureSessionTab(nextTabs, fallbackDraft) : nextTabs,
      activeSessionTabID:
        current.activeSessionTabID === threadSessionTabID(threadID) &&
        fallbackDraft
          ? fallbackDraft.id
          : current.activeSessionTabID,
      // Keep the thread in `threads` when archiving so the Settings → Archive
      // page can list every archived session; the sidebar already filters by
      // `!archived`. Deletion (`archived === false` from deleteThread) still
      // drops the record entirely.
      threads:
        archived === false
          ? current.threads.filter((candidate) => candidate.id !== threadID)
          : current.threads.map((candidate) =>
              candidate.id === threadID
                ? { ...candidate, archived: true }
                : candidate,
            ),
      running: activeThreadIDForState(current) === threadID ? false : current.running,
      status: "ready",
    };
  }

  async function toggleSubagentPinned(agent: Agent): Promise<void> {
    if (!deps.getAppState().activeContext) {
      return;
    }
    deps.setArchiveConfirmSubagentID(undefined);
    try {
      const result = await window.wuu.pinThread(agent.id, !agent.pinned);
      deps.setAppState((current) => ({
        ...current,
        thread: patchChildAgentInThread(current.thread, agent.id, {
          pinned: result.thread.pinned,
        }),
        secondaryThread: patchChildAgentInThread(
          current.secondaryThread,
          agent.id,
          { pinned: result.thread.pinned },
        ),
        threads: current.threads.map((thread) =>
          patchChildAgentInThread(thread, agent.id, {
            pinned: result.thread.pinned,
          }) ?? thread,
        ),
        status: current.status === "ready" ? "ready" : current.status,
      }));
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "pin subagent failed");
    }
  }

  async function archiveSubagent(agent: Agent): Promise<void> {
    if (deps.getArchiveConfirmSubagentID() !== agent.id) {
      deps.setArchiveConfirmSubagentID(agent.id);
      return;
    }
    deps.setArchiveConfirmSubagentID(undefined);
    try {
      await window.wuu.archiveThread(agent.id, true);
      deps.setAppState((current) => ({
        ...current,
        thread: patchChildAgentInThread(current.thread, agent.id, {
          archived: true,
        }),
        secondaryThread: patchChildAgentInThread(
          current.secondaryThread,
          agent.id,
          { archived: true },
        ),
        threads: current.threads.map((thread) =>
          patchChildAgentInThread(thread, agent.id, { archived: true }) ??
            thread,
        ),
        status: current.status === "ready" ? "ready" : current.status,
      }));
    } catch (error) {
      setStatus(
        error instanceof Error ? error.message : "archive subagent failed",
      );
    }
  }

  return {
    toggleThreadPinned,
    renameThread,
    removeThreadMemberByID,
    addThreadMemberByID,
    archiveThread,
    unarchiveThread,
    deleteThread,
    toggleSubagentPinned,
    archiveSubagent,
  };
}