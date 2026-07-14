import type { Dispatch, MutableRefObject, SetStateAction } from "react";
import type { Thread } from "../shared/protocol";
import {
  createAgentTreeDemo,
  createConversationFixture,
  type ConversationFixtureKind,
} from "./ConversationFixtures";
import {
  createDraftSessionTab,
  createThreadSessionTab,
  ensureSessionTab,
  isThreadRunning,
  threadSessionTabID,
  upsertThread,
  type AppState,
  type ComposerDraftState,
  type ConversationPaneID,
} from "./AppState";
import type { ComposerFile, ComposerImage } from "./ComposerMessages";
import type { EnvironmentPanelMenu } from "./EnvironmentPanel";

type SetAppState = (update: SetStateAction<AppState>) => void;

export type ConversationDemoPaneActionsDeps = {
  getAppState: () => AppState;
  setAppState: SetAppState;
  localDemoThreadsRef: MutableRefObject<Map<string, Thread>>;
  cancelViewSwitch: () => void;
  
  setPrompt: Dispatch<SetStateAction<string>>;
  setComposerImages: Dispatch<SetStateAction<ComposerImage[]>>;
  setComposerFiles: Dispatch<SetStateAction<ComposerFile[]>>;
  setSplitComposerDrafts: Dispatch<
    SetStateAction<Record<ConversationPaneID, ComposerDraftState>>
  >;
  moveSplitDraftToGlobalComposer: (pane: ConversationPaneID) => void;
  setRunDebugOpen: (open: boolean) => void;
  setEnvironmentPanelOpen: (open: boolean) => void;
  setEnvironmentPanelDismissed: (dismissed: boolean) => void;
  setEnvironmentPanelMenu: (menu: EnvironmentPanelMenu) => void;
};

export type ConversationDemoPaneActions = {
  seedAgentTreeDemo: () => void;
  seedConversationFixture: (kind: ConversationFixtureKind) => void;
  seedPlanPanelDebug: () => void;
  activateConversationPane: (pane: ConversationPaneID) => void;
  closeConversationPane: (pane: ConversationPaneID) => void;
};

export function createConversationDemoPaneActions(
  deps: ConversationDemoPaneActionsDeps,
): ConversationDemoPaneActions {
  function resetComposerForDemo(): void {
    deps.cancelViewSwitch();
    
    deps.setPrompt("");
    deps.setComposerImages([]);
    deps.setComposerFiles([]);
  }

  function seedAgentTreeDemo(): void {
    const state = deps.getAppState();
    if (!state.activeContext || !state.initialized) {
      return;
    }
    const activeContext = state.activeContext;
    resetComposerForDemo();
    const demo = createAgentTreeDemo(activeContext.cwd, state.initialized);
    const demoThreads = [demo.parent, ...demo.children];
    deps.localDemoThreadsRef.current = new Map([
      ...deps.localDemoThreadsRef.current,
      ...demoThreads.map((thread): [string, Thread] => [thread.id, thread]),
    ]);
    deps.setAppState((current) => ({
      ...current,
      thread: demo.parent,
      secondaryThread: undefined,
      activePane: "primary",
      allowThreadAutoActivation: true,
      sessionTabs: ensureSessionTab(
        current.sessionTabs,
        createThreadSessionTab(demo.parent, activeContext),
      ),
      activeSessionTabID: threadSessionTabID(demo.parent.id),
      threads: upsertThread(current.threads, demo.parent),
      running: false,
      status: "ready",
    }));
  }

  function seedConversationFixture(kind: ConversationFixtureKind): void {
    const state = deps.getAppState();
    if (!state.activeContext || !state.initialized) {
      return;
    }
    const activeContext = state.activeContext;
    resetComposerForDemo();
    const thread = createConversationFixture(
      kind,
      activeContext.cwd,
      state.initialized,
    );
    deps.localDemoThreadsRef.current = new Map([
      ...deps.localDemoThreadsRef.current,
      [thread.id, thread],
    ]);
    deps.setAppState((current) => ({
      ...current,
      thread,
      secondaryThread: undefined,
      activePane: "primary",
      allowThreadAutoActivation: true,
      sessionTabs: ensureSessionTab(
        current.sessionTabs,
        createThreadSessionTab(thread, activeContext),
      ),
      activeSessionTabID: threadSessionTabID(thread.id),
      threads: upsertThread(current.threads, thread),
      running: isThreadRunning(thread),
      status: "ready",
    }));
  }

  function seedPlanPanelDebug(): void {
    const state = deps.getAppState();
    if (!state.activeContext || !state.initialized) {
      return;
    }
    seedConversationFixture("plan");
    deps.setRunDebugOpen(false);
    deps.setEnvironmentPanelOpen(true);
    deps.setEnvironmentPanelDismissed(false);
    deps.setEnvironmentPanelMenu(null);
  }

  function activateConversationPane(pane: ConversationPaneID): void {
    deps.setAppState((current) => {
      if (pane === "secondary" && !current.secondaryThread) {
        return current;
      }
      const thread =
        pane === "secondary" ? current.secondaryThread : current.thread;
      return {
        ...current,
        activePane: pane,
        activeSessionTabID: thread
          ? threadSessionTabID(thread.id)
          : current.activeSessionTabID,
        running: isThreadRunning(thread),
      };
    });
  }

  function closeConversationPane(pane: ConversationPaneID): void {
    deps.moveSplitDraftToGlobalComposer(
      pane === "secondary" ? "primary" : "secondary",
    );
    deps.setAppState((current) => {
      if (pane === "secondary") {
        return {
          ...current,
          secondaryThread: undefined,
          activePane: "primary",
          activeSessionTabID: current.thread
            ? threadSessionTabID(current.thread.id)
            : current.activeSessionTabID,
          running: isThreadRunning(current.thread),
          status: "ready",
        };
      }
      if (current.secondaryThread) {
        return {
          ...current,
          thread: current.secondaryThread,
          secondaryThread: undefined,
          activePane: "primary",
          activeSessionTabID: threadSessionTabID(current.secondaryThread.id),
          running: isThreadRunning(current.secondaryThread),
          status: "ready",
        };
      }
      if (!current.activeContext) {
        return current;
      }
      const nextTab = createDraftSessionTab(
        `draft:closed:${Date.now()}`,
        current.activeContext,
      );
      return {
        ...current,
        thread: undefined,
        activePane: "primary",
        sessionTabs: ensureSessionTab(current.sessionTabs, nextTab),
        activeSessionTabID: nextTab.id,
        running: false,
        status: "ready",
      };
    });
  }

  return {
    seedAgentTreeDemo,
    seedConversationFixture,
    seedPlanPanelDebug,
    activateConversationPane,
    closeConversationPane,
  };
}
