import type { Dispatch, MutableRefObject, SetStateAction } from "react";
import type { RuntimeContext, Thread } from "../shared/protocol";
import {
  activeThreadForState,
  createSkillsSessionTab,
  createThreadSessionTab,
  ensureSessionTab,
  initialSplitComposerDrafts,
  isThreadRunning,
  persistActiveSessionTabDraft,
  sameRuntimeContext,
  sessionTabDraftForThread,
  threadSessionTabID,
  upsertThread,
  type AppState,
  type ComposerDraftState,
} from "./AppState";
import type { ComposerFile, ComposerImage } from "./ComposerMessages";
import type { ContextCompositionEntry } from "./ContextCompositionCard";
import type { InstructionFilesEntry } from "./InstructionFilesCard";
import { desktopApiErrorMessage } from "./WorkspaceReviewHelpers";
import type { SettingsPage } from "./SettingsView";
import { localizedText, translateCurrent } from "./i18n";

type SetAppState = (update: SetStateAction<AppState>) => void;

export type CollaborationActionsDeps = {
  getAppState: () => AppState;
  setAppState: SetAppState;
  getActiveTitle: () => string;
  getPrimaryComposerDraft: () => ComposerDraftState;
  setSplitComposerDrafts: Dispatch<
    SetStateAction<Record<"primary" | "secondary", ComposerDraftState>>
  >;
  setPrompt: Dispatch<SetStateAction<string>>;
  setComposerImages: Dispatch<SetStateAction<ComposerImage[]>>;
  setComposerFiles: Dispatch<SetStateAction<ComposerFile[]>>;
  
  cancelViewSwitch: () => void;
  activateThread: (threadID: string) => Promise<void>;
  setContextCompositionEntries: Dispatch<
    SetStateAction<ContextCompositionEntry[]>
  >;
  setInstructionFilesEntries: Dispatch<
    SetStateAction<InstructionFilesEntry[]>
  >;
  scheduleStreamScroll: () => void;
  openingCollaborationRef: MutableRefObject<boolean>;
  getCollaborationThreadID: () => string | undefined;
  setCollaborationThreadID: (threadID: string) => void;
  closeProjectMenus: () => void;
  setSettingsMemoryFocusID: (participantID: string | undefined) => void;
  setSettingsInitialPage: (page: SettingsPage) => void;
  setSettingsOpen: (open: boolean) => void;
};

export type CollaborationActions = {
  openSkillsTab: () => void;
  dismissContextCompositionEntry: (id: string) => void;
  dismissInstructionFilesEntry: (id: string) => void;
  openInstructions: () => void;
  openContextComposition: () => void;
  openCollaborationIntake: () => Promise<void>;
  openMemorySettings: (participantID?: string) => void;
};

function createContextCompositionEntryID(): string {
  return `context-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
}

function createInstructionFilesEntryID(): string {
  return `instructions-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
}

export function createCollaborationActions(
  deps: CollaborationActionsDeps,
): CollaborationActions {
  function openSkillsTab(): void {
    const state = deps.getAppState();
    if (!state.activeContext) {
      return;
    }
    const tab = createSkillsSessionTab(state.activeContext);
    
    deps.setSplitComposerDrafts(initialSplitComposerDrafts());
    deps.setAppState((current) => ({
      ...persistActiveSessionTabDraft(current, deps.getPrimaryComposerDraft()),
      secondaryThread: undefined,
      activePane: "primary",
      sessionTabs: ensureSessionTab(current.sessionTabs, tab),
      activeSessionTabID: tab.id,
      allowThreadAutoActivation: false,
      running: false,
      status: "ready",
    }));
  }

  function dismissContextCompositionEntry(id: string): void {
    deps.setContextCompositionEntries((entries) =>
      entries.filter((entry) => entry.id !== id),
    );
  }

  function dismissInstructionFilesEntry(id: string): void {
    deps.setInstructionFilesEntries((entries) =>
      entries.filter((entry) => entry.id !== id),
    );
  }

  function openInstructions(): void {
    const activeThread = activeThreadForActions(deps.getAppState());
    if (!activeThread) {
      deps.setAppState((current) => ({
        ...current,
        status: localizedText("collaboration.noCurrentConversation"),
      }));
      return;
    }
    const threadID = activeThread.id;
    const title = activeThread.preview || deps.getActiveTitle();
    const entryID = createInstructionFilesEntryID();
    deps.setInstructionFilesEntries((entries) => [
      ...entries,
      {
        id: entryID,
        threadID,
        title,
        loading: true,
      },
    ]);
    deps.scheduleStreamScroll();
    void (async () => {
      try {
        const result = await window.wuu.listInstructionFiles();
        deps.setInstructionFilesEntries((entries) =>
          entries.map((entry) =>
            entry.id === entryID
              ? { ...entry, loading: false, result, error: undefined }
              : entry,
          ),
        );
        deps.scheduleStreamScroll();
      } catch (error) {
        deps.setInstructionFilesEntries((entries) =>
          entries.map((entry) =>
            entry.id === entryID
              ? {
                  ...entry,
                  loading: false,
                  error: desktopApiErrorMessage(error, translateCurrent("collaboration.instructionsReadFailed")),
                }
              : entry,
          ),
        );
      }
    })();
  }

  function openContextComposition(): void {
    const activeThread = activeThreadForActions(deps.getAppState());
    if (!activeThread) {
      deps.setAppState((current) => ({
        ...current,
        status: localizedText("collaboration.noCurrentConversation"),
      }));
      return;
    }
    const threadID = activeThread.id;
    const title = activeThread.preview || deps.getActiveTitle();
    const entryID = createContextCompositionEntryID();
    const afterTurnID = activeThread.turns.at(-1)?.id;
    deps.setContextCompositionEntries((entries) => [
      ...entries,
      {
        id: entryID,
        threadID,
        afterTurnID,
        title,
        loading: true,
      },
    ]);
    deps.scheduleStreamScroll();
    void (async () => {
      try {
        const result = await window.wuu.getThreadContextComposition(threadID);
        deps.setContextCompositionEntries((entries) =>
          entries.map((entry) =>
            entry.id === entryID
              ? {
                  ...entry,
                  loading: false,
                  result,
                  error: undefined,
                }
              : entry,
          ),
        );
        deps.scheduleStreamScroll();
      } catch (error) {
        deps.setContextCompositionEntries((entries) =>
          entries.map((entry) =>
            entry.id === entryID
              ? {
                  ...entry,
                  loading: false,
                  error: desktopApiErrorMessage(error, translateCurrent("collaboration.contextReadFailed")),
                }
              : entry,
          ),
        );
      }
    })();
  }

  async function openCollaborationIntake(): Promise<void> {
    const currentState = deps.getAppState();
    if (!currentState.activeContext || !currentState.initialized) {
      return;
    }
    if (deps.openingCollaborationRef.current) {
      return;
    }
    deps.openingCollaborationRef.current = true;
    try {
      const existingThreadID = deps.getCollaborationThreadID();
      if (existingThreadID) {
        try {
          await deps.activateThread(existingThreadID);
          return;
        } catch {
          // The remembered thread may have been deleted outside this view.
        }
      }
      resetComposerForThreadActivation();
      try {
        const { thread } = await window.wuu.startThread({ collaboration: true });
        if (
          !sameRuntimeContext(
            deps.getAppState().activeContext,
            currentState.activeContext,
          )
        ) {
          return;
        }
        deps.setCollaborationThreadID(thread.id);
        selectFreshThread(thread, deps.getAppState().activeContext);
      } catch (error) {
        deps.setAppState((current) => ({
          ...current,
          status:
            error instanceof Error
              ? error.message
              : translateCurrent("collaboration.intakeCreateFailed"),
        }));
      }
    } finally {
      deps.openingCollaborationRef.current = false;
    }
  }

  function openMemorySettings(participantID?: string): void {
    deps.closeProjectMenus();
    deps.setSettingsMemoryFocusID(participantID);
    deps.setSettingsInitialPage("memory");
    deps.setSettingsOpen(true);
  }

  function resetComposerForThreadActivation(): void {
    deps.cancelViewSwitch();
    
    deps.setPrompt("");
    deps.setComposerImages([]);
    deps.setComposerFiles([]);
  }

  function selectFreshThread(
    thread: Thread,
    activeContext: RuntimeContext | undefined,
  ): void {
    if (!activeContext) {
      return;
    }
    const targetDraft = sessionTabDraftForThread(deps.getAppState(), thread.id);
    deps.setSplitComposerDrafts(initialSplitComposerDrafts());
    deps.setAppState((current) => {
      const withDraft = persistActiveSessionTabDraft(
        current,
        deps.getPrimaryComposerDraft(),
      );
      return {
        ...withDraft,
        thread,
        secondaryThread: undefined,
        activePane: "primary",
        allowThreadAutoActivation: true,
        sessionTabs: ensureSessionTab(
          withDraft.sessionTabs,
          createThreadSessionTab(thread, activeContext, targetDraft),
        ),
        activeSessionTabID: threadSessionTabID(thread.id),
        threads: upsertThread(current.threads, thread),
        running: isThreadRunning(thread),
        status: "ready",
      };
    });
  }

  return {
    openSkillsTab,
    dismissContextCompositionEntry,
    dismissInstructionFilesEntry,
    openInstructions,
    openContextComposition,
    openCollaborationIntake,
    openMemorySettings,
  };
}

function activeThreadForActions(state: AppState): Thread | undefined {
  return activeThreadForState(state);
}
