import type { SetStateAction } from "react";
import { arrayMove } from "@dnd-kit/sortable";
import {
  activeSessionTab,
  cloneSessionTabDraft,
  createThreadSessionTab,
  draftSessionTabForContext,
  ensureSessionTab,
  isThreadRunning,
  persistActiveSessionTabDraft,
  requireThread,
  runtimeContextKey,
  sameRuntimeContext,
  threadNeedsResumeOnReselect,
  threadSessionTabID,
  upsertThread,
  type AppState,
  type ComposerDraftState,
  type SessionTab,
} from "./AppState";
import {
  loadRuntime as defaultLoadRuntime,
  selectRuntimeContext as defaultSelectRuntimeContext,
} from "./RuntimeLoadState";

type SetAppState = (update: SetStateAction<AppState>) => void;
type ViewSwitchKind = "thread" | "project" | "runtime";
type MutableSetRef = { current: Set<string> };

export type SessionTabActionsDeps = {
  getAppState: () => AppState;
  setAppState: SetAppState;
  getPrimaryComposerDraft: () => ComposerDraftState;
  restorePrimaryComposerDraft: (draft: ComposerDraftState) => void;
  clearPrimaryComposerDraft: () => void;
  resetSplitComposerDrafts: () => void;
  nextDraftSessionTab: (
    context: NonNullable<AppState["activeContext"]>,
  ) => SessionTab;
  selectThread: (threadID: string) => Promise<void>;
  
  poppingOutTabIDsRef: MutableSetRef;
  beginViewSwitch: (kind: ViewSwitchKind, targetID: string) => number;
  finishViewSwitch: (requestID: number) => boolean;
  cancelViewSwitch: () => void;
  loadRuntime?: typeof defaultLoadRuntime;
  selectRuntimeContext?: typeof defaultSelectRuntimeContext;
};

export type SessionTabActions = {
  selectSessionTab: (tabID: string) => Promise<void>;
  closeSessionTab: (tabID: string) => Promise<void>;
  closeSessionTabs: (tabIDs: string[]) => Promise<void>;
  startNewThread: () => Promise<void>;
  reorderSessionTabs: (activeID: string, overID: string) => void;
  popOutSessionTab: (tabID: string) => Promise<void>;
};

export function createSessionTabActions(
  deps: SessionTabActionsDeps,
): SessionTabActions {
  const loadRuntime = deps.loadRuntime ?? defaultLoadRuntime;
  const selectRuntimeContext =
    deps.selectRuntimeContext ?? defaultSelectRuntimeContext;

  function setStatus(status: string): void {
    deps.setAppState((current) => ({
      ...current,
      status,
    }));
  }

  async function selectSessionTab(tabID: string): Promise<void> {
    const currentState = deps.getAppState();
    const tab = currentState.sessionTabs.find((item) => item.id === tabID);
    if (!tab) {
      return;
    }
    if (tabID === currentState.activeSessionTabID) {
      if (
        tab.kind === "thread" &&
        threadNeedsResumeOnReselect(currentState, tab.threadID)
      ) {
        await deps.selectThread(tab.threadID);
      }
      return;
    }
    
    const sameContext = sameRuntimeContext(
      tab.context,
      currentState.activeContext,
    );
    if (tab.kind === "skills" || tab.kind === "board") {
      const outgoingDraft = deps.getPrimaryComposerDraft();
      const requestID = sameContext
        ? undefined
        : deps.beginViewSwitch("runtime", runtimeContextKey(tab.context));
      try {
        const loadedState = sameContext
          ? undefined
          : await loadRuntime(await selectRuntimeContext(tab.context), {
              resumeLatestThread: false,
            });
        if (requestID !== undefined && !deps.finishViewSwitch(requestID)) {
          return;
        }
        if (requestID === undefined) {
          deps.cancelViewSwitch();
        }
        deps.resetSplitComposerDrafts();
        deps.setAppState((current) => {
          const withDraft = persistActiveSessionTabDraft(
            current,
            outgoingDraft,
          );
          const next = loadedState
            ? { ...withDraft, ...loadedState }
            : withDraft;
          return {
            ...next,
            secondaryThread: undefined,
            activePane: "primary",
            sessionTabs: ensureSessionTab(next.sessionTabs, tab),
            activeSessionTabID: tab.id,
            allowThreadAutoActivation: false,
            running: false,
            status: "ready",
          };
        });
      } catch (error) {
        if (requestID !== undefined && !deps.finishViewSwitch(requestID)) {
          return;
        }
        setStatus(error instanceof Error ? error.message : "load failed");
      }
      return;
    }
    if (tab.kind === "draft") {
      const outgoingDraft = deps.getPrimaryComposerDraft();
      const requestID = sameContext
        ? undefined
        : deps.beginViewSwitch("runtime", runtimeContextKey(tab.context));
      try {
        const loadedState = sameContext
          ? undefined
          : await loadRuntime(await selectRuntimeContext(tab.context), {
              resumeLatestThread: false,
            });
        if (requestID !== undefined && !deps.finishViewSwitch(requestID)) {
          return;
        }
        if (requestID === undefined) {
          deps.cancelViewSwitch();
        }
        deps.restorePrimaryComposerDraft(cloneSessionTabDraft(tab));
        deps.resetSplitComposerDrafts();
        deps.setAppState((current) => {
          const withDraft = persistActiveSessionTabDraft(
            current,
            outgoingDraft,
          );
          const next = loadedState
            ? { ...withDraft, ...loadedState }
            : withDraft;
          return {
            ...next,
            thread: undefined,
            secondaryThread: undefined,
            activePane: "primary",
            sessionTabs: ensureSessionTab(next.sessionTabs, tab),
            activeSessionTabID: tab.id,
            allowThreadAutoActivation: false,
            running: false,
            status: "ready",
          };
        });
      } catch (error) {
        if (requestID !== undefined && !deps.finishViewSwitch(requestID)) {
          return;
        }
        setStatus(error instanceof Error ? error.message : "load failed");
      }
      return;
    }
    if (sameContext) {
      await deps.selectThread(tab.threadID);
      return;
    }
    const outgoingDraft = deps.getPrimaryComposerDraft();
    const targetDraft = cloneSessionTabDraft(tab);
    const requestID = deps.beginViewSwitch("thread", tab.threadID);
    try {
      const projectState = await selectRuntimeContext(tab.context);
      const loadedState = await loadRuntime(projectState, {
        resumeLatestThread: false,
      });
      const thread = requireThread(
        await window.wuu.resumeThread(tab.threadID),
        "resume did not return a thread",
      );
      if (!deps.finishViewSwitch(requestID)) {
        return;
      }
      deps.restorePrimaryComposerDraft(targetDraft);
      deps.resetSplitComposerDrafts();
      deps.setAppState((current) => {
        const withDraft = persistActiveSessionTabDraft(current, outgoingDraft);
        const next = { ...withDraft, ...loadedState };
        return {
          ...next,
          thread,
          secondaryThread: undefined,
          activePane: "primary",
          allowThreadAutoActivation: true,
          sessionTabs: ensureSessionTab(
            next.sessionTabs,
            createThreadSessionTab(thread, tab.context, targetDraft),
          ),
          activeSessionTabID: threadSessionTabID(thread.id),
          threads: upsertThread(next.threads, thread),
          running: isThreadRunning(thread),
          status: "ready",
        };
      });
    } catch (error) {
      if (!deps.finishViewSwitch(requestID)) {
        return;
      }
      setStatus(error instanceof Error ? error.message : "load failed");
    }
  }

  async function closeSessionTab(tabID: string): Promise<void> {
    const currentState = deps.getAppState();
    const tabIndex = currentState.sessionTabs.findIndex(
      (tab) => tab.id === tabID,
    );
    if (tabIndex < 0) {
      return;
    }
    const closingActive = currentState.activeSessionTabID === tabID;
    const nextTabs = currentState.sessionTabs.filter((tab) => tab.id !== tabID);
    const closedTab = currentState.sessionTabs[tabIndex];
    if (!closingActive) {
      deps.setAppState((current) => ({
        ...current,
        sessionTabs: current.sessionTabs.filter((tab) => tab.id !== tabID),
      }));
      return;
    }

    const fallbackTab =
      nextTabs[Math.min(tabIndex, Math.max(nextTabs.length - 1, 0))] ??
      deps.nextDraftSessionTab(closedTab.context);
    const tabsWithFallback = nextTabs.length > 0 ? nextTabs : [fallbackTab];
    
    if (fallbackTab.kind === "skills" || fallbackTab.kind === "board") {
      const sameContext = sameRuntimeContext(
        fallbackTab.context,
        currentState.activeContext,
      );
      const requestID = sameContext
        ? undefined
        : deps.beginViewSwitch("runtime", runtimeContextKey(fallbackTab.context));
      try {
        const loadedState = sameContext
          ? undefined
          : await loadRuntime(await selectRuntimeContext(fallbackTab.context), {
              resumeLatestThread: false,
            });
        if (requestID !== undefined && !deps.finishViewSwitch(requestID)) {
          return;
        }
        if (requestID === undefined) {
          deps.cancelViewSwitch();
        }
        deps.resetSplitComposerDrafts();
        deps.setAppState((current) => {
          const next = loadedState ? { ...current, ...loadedState } : current;
          return {
            ...next,
            sessionTabs: tabsWithFallback,
            activeSessionTabID: fallbackTab.id,
            secondaryThread: undefined,
            activePane: "primary",
            allowThreadAutoActivation: false,
            running: false,
            status: "ready",
          };
        });
      } catch (error) {
        if (requestID !== undefined && !deps.finishViewSwitch(requestID)) {
          return;
        }
        setStatus(error instanceof Error ? error.message : "load failed");
      }
      return;
    }
    if (fallbackTab.kind === "draft") {
      const sameContext = sameRuntimeContext(
        fallbackTab.context,
        currentState.activeContext,
      );
      const requestID = sameContext
        ? undefined
        : deps.beginViewSwitch("runtime", runtimeContextKey(fallbackTab.context));
      try {
        const loadedState = sameContext
          ? undefined
          : await loadRuntime(await selectRuntimeContext(fallbackTab.context), {
              resumeLatestThread: false,
            });
        if (requestID !== undefined && !deps.finishViewSwitch(requestID)) {
          return;
        }
        if (requestID === undefined) {
          deps.cancelViewSwitch();
        }
        deps.restorePrimaryComposerDraft(cloneSessionTabDraft(fallbackTab));
        deps.resetSplitComposerDrafts();
        deps.setAppState((current) => {
          const next = loadedState ? { ...current, ...loadedState } : current;
          return {
            ...next,
            sessionTabs: tabsWithFallback,
            activeSessionTabID: fallbackTab.id,
            thread: undefined,
            secondaryThread: undefined,
            activePane: "primary",
            allowThreadAutoActivation: false,
            running: false,
            status: "ready",
          };
        });
      } catch (error) {
        if (requestID !== undefined && !deps.finishViewSwitch(requestID)) {
          return;
        }
        setStatus(error instanceof Error ? error.message : "load failed");
      }
      return;
    }

    const restoredDraft = cloneSessionTabDraft(fallbackTab);
    const sameContext = sameRuntimeContext(
      fallbackTab.context,
      currentState.activeContext,
    );
    const requestID = deps.beginViewSwitch("thread", fallbackTab.threadID);
    try {
      const loadedState = sameContext
        ? undefined
        : await loadRuntime(await selectRuntimeContext(fallbackTab.context), {
            resumeLatestThread: false,
          });
      const thread = requireThread(
        await window.wuu.resumeThread(fallbackTab.threadID),
        "resume did not return a thread",
      );
      if (!deps.finishViewSwitch(requestID)) {
        return;
      }
      deps.restorePrimaryComposerDraft(restoredDraft);
      deps.resetSplitComposerDrafts();
      deps.setAppState((current) => {
        const next = loadedState ? { ...current, ...loadedState } : current;
        return {
          ...next,
          thread,
          secondaryThread: undefined,
          activePane: "primary",
          allowThreadAutoActivation: true,
          sessionTabs: ensureSessionTab(
            tabsWithFallback,
            createThreadSessionTab(thread, fallbackTab.context, restoredDraft),
          ),
          activeSessionTabID: threadSessionTabID(thread.id),
          threads: upsertThread(next.threads, thread),
          running: isThreadRunning(thread),
          status: "ready",
        };
      });
    } catch (error) {
      if (!deps.finishViewSwitch(requestID)) {
        return;
      }
      setStatus(error instanceof Error ? error.message : "load failed");
    }
  }

  async function closeSessionTabs(tabIDs: string[]): Promise<void> {
    if (tabIDs.length === 0) {
      return;
    }
    const activeID = deps.getAppState().activeSessionTabID;
    const orderedIDs = tabIDs.includes(activeID)
      ? [...tabIDs.filter((id) => id !== activeID), activeID]
      : tabIDs;
    for (const tabID of orderedIDs) {
      await closeSessionTab(tabID);
    }
  }

  function reorderSessionTabs(activeID: string, overID: string): void {
    deps.setAppState((current) => {
      const sourceIndex = current.sessionTabs.findIndex(
        (tab) => tab.id === activeID,
      );
      const targetIndex = current.sessionTabs.findIndex(
        (tab) => tab.id === overID,
      );
      if (sourceIndex < 0 || targetIndex < 0) {
        return current;
      }
      return {
        ...current,
        sessionTabs: arrayMove(current.sessionTabs, sourceIndex, targetIndex),
      };
    });
  }

  async function popOutSessionTab(tabID: string): Promise<void> {
    const currentState = deps.getAppState();
    const tab = currentState.sessionTabs.find((item) => item.id === tabID);
    if (!tab || (tab.kind !== "thread" && tab.kind !== "draft")) {
      return;
    }
    if (deps.poppingOutTabIDsRef.current.has(tabID)) {
      return;
    }
    deps.poppingOutTabIDsRef.current.add(tabID);
    try {
      await window.wuu.popOutSession(
        tab.kind === "thread"
          ? {
              kind: "thread",
              threadID: tab.threadID,
              context: tab.context,
            }
          : {
              kind: "draft",
              context: tab.context,
            },
      );
      await closeSessionTab(tabID);
    } catch (error) {
      setStatus(
        error instanceof Error ? error.message : "open detached window failed",
      );
    } finally {
      deps.poppingOutTabIDsRef.current.delete(tabID);
    }
  }

  async function startNewThread(): Promise<void> {
    const currentState = deps.getAppState();
    if (!currentState.activeContext) {
      return;
    }
    const existingDraft = draftSessionTabForContext(
      currentState.sessionTabs,
      currentState.activeContext,
    );
    if (existingDraft) {
      await selectSessionTab(existingDraft.id);
      return;
    }
    deps.cancelViewSwitch();
    
    const outgoingDraft = deps.getPrimaryComposerDraft();
    deps.clearPrimaryComposerDraft();
    const nextTab =
      activeSessionTab(currentState)?.kind === "draft" &&
      !outgoingDraft.prompt.trim() &&
      outgoingDraft.images.length === 0 &&
      outgoingDraft.files.length === 0
        ? activeSessionTab(currentState)
        : deps.nextDraftSessionTab(currentState.activeContext);
    if (!nextTab) {
      return;
    }
    deps.setAppState((current) => ({
      ...persistActiveSessionTabDraft(current, outgoingDraft),
      thread: undefined,
      secondaryThread: undefined,
      activePane: "primary",
      sessionTabs: ensureSessionTab(current.sessionTabs, nextTab),
      activeSessionTabID: nextTab.id,
      allowThreadAutoActivation: false,
      running: false,
      status: "ready",
    }));
  }

  return {
    selectSessionTab,
    closeSessionTab,
    closeSessionTabs,
    startNewThread,
    reorderSessionTabs,
    popOutSessionTab,
  };
}
