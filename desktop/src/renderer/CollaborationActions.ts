import type { Dispatch, MutableRefObject, SetStateAction } from "react";
import type {
  ParticipantProfile,
  RuntimeContext,
  Thread,
} from "../shared/protocol";
import {
  activeThreadForState,
  createBoardSessionTab,
  createSkillsSessionTab,
  createThreadSessionTab,
  ensureSessionTab,
  findDMThread,
  initialSplitComposerDrafts,
  isThreadRunning,
  persistActiveSessionTabDraft,
  sameRuntimeContext,
  sessionTabDraftForThread,
  sessionTabForParticipant,
  threadSessionTabID,
  upsertThread,
  type AppState,
  type ComposerDraftState,
  type SessionTab,
} from "./AppState";
import type { ComposerFile, ComposerImage } from "./ComposerMessages";
import type { ContextCompositionEntry } from "./ContextCompositionCard";
import type { InstructionFilesEntry } from "./InstructionFilesCard";
import type { WorkspacePanelView } from "./WorkspacePanels";
import { desktopApiErrorMessage } from "./WorkspaceReviewHelpers";
import type { CloseConversationSearchOptions } from "./ConversationSearchState";
import type { SettingsPage } from "./SettingsView";

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
  setArchiveConfirmThreadID: (
    update: SetStateAction<string | undefined>,
  ) => void;
  setWorkspaceMode: (mode: WorkspacePanelView | undefined) => void;
  cancelViewSwitch: () => void;
  activateThread: (threadID: string) => Promise<void>;
  selectThread: (threadID: string) => Promise<void>;
  selectSessionTab: (tabID: string) => Promise<void>;
  closeConversationSearch: (
    options?: CloseConversationSearchOptions,
  ) => void;
  closeEnvironmentPanel: (options?: { dismissed?: boolean }) => void;
  setOpenSubthreadPanel: (value: undefined) => void;
  setRightPanelOpenWithMotion: (open: boolean) => void;
  openParticipantProfilePanel: (participant: ParticipantProfile) => void;
  setContextCompositionEntries: Dispatch<
    SetStateAction<ContextCompositionEntry[]>
  >;
  setInstructionFilesEntries: Dispatch<
    SetStateAction<InstructionFilesEntry[]>
  >;
  scheduleStreamScroll: () => void;
  openingDMParticipantIDRef: MutableRefObject<string | undefined>;
  openConversationSubthreadByID: (
    threadID: string,
    subthreadID: string,
  ) => void;
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
  openParticipantProfile: (participant: ParticipantProfile) => void;
  openParticipantDM: (participant: ParticipantProfile) => Promise<void>;
  createGroupThread: (title: string) => Promise<void>;
  openTaskBoardTab: (thread: Thread) => void;
  openTaskFromBoard: (
    threadID: string,
    subthreadID: string,
  ) => Promise<void>;
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
    deps.setArchiveConfirmThreadID(undefined);
    deps.setWorkspaceMode(undefined);
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
        status: "没有当前对话",
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
                  error: desktopApiErrorMessage(error, "无法读取指令文件"),
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
        status: "没有当前对话",
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
                  error: desktopApiErrorMessage(error, "无法读取上下文组成"),
                }
              : entry,
          ),
        );
      }
    })();
  }

  function openParticipantProfile(participant: ParticipantProfile): void {
    deps.closeConversationSearch({ immediate: true });
    deps.closeEnvironmentPanel({ dismissed: true });
    deps.setOpenSubthreadPanel(undefined);
    deps.setRightPanelOpenWithMotion(false);
    deps.openParticipantProfilePanel(participant);
  }

  async function openParticipantDM(
    participant: ParticipantProfile,
  ): Promise<void> {
    const currentState = deps.getAppState();
    if (!currentState.activeContext || !currentState.initialized) {
      return;
    }
    if (deps.openingDMParticipantIDRef.current === participant.id) {
      return;
    }
    deps.openingDMParticipantIDRef.current = participant.id;
    try {
      resetComposerForThreadActivation();
      const existing = findDMThread(currentState.threads, participant.id);
      if (existing) {
        await deps.activateThread(existing.id);
        return;
      }
      const existingTab = sessionTabForParticipant(
        currentState.sessionTabs,
        currentState.threads,
        participant.id,
      );
      if (existingTab) {
        await deps.activateThread(existingTab.threadID);
        return;
      }
      try {
        const { thread } = await window.wuu.startThread({
          dm_participant_id: participant.id,
        });
        if (
          !sameRuntimeContext(
            deps.getAppState().activeContext,
            currentState.activeContext,
          )
        ) {
          return;
        }
        selectFreshThread(thread, deps.getAppState().activeContext);
      } catch (error) {
        deps.setAppState((current) => ({
          ...current,
          status: error instanceof Error ? error.message : "无法创建 Agent 对话",
        }));
      }
    } finally {
      deps.openingDMParticipantIDRef.current = undefined;
    }
  }

  async function createGroupThread(title: string): Promise<void> {
    const currentState = deps.getAppState();
    if (!currentState.activeContext || !currentState.initialized) {
      return;
    }
    resetComposerForThreadActivation();
    try {
      const { thread } = await window.wuu.startThread({ group: true, title });
      if (
        !sameRuntimeContext(
          deps.getAppState().activeContext,
          currentState.activeContext,
        )
      ) {
        return;
      }
      selectFreshThread(thread, deps.getAppState().activeContext);
    } catch (error) {
      deps.setAppState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "无法创建群聊",
      }));
    }
  }

  function openTaskBoardTab(thread: Thread): void {
    const context = deps.getAppState().activeContext;
    if (!context) {
      return;
    }
    const tab = createBoardSessionTab(thread, context);
    deps.setAppState((current) => ({
      ...current,
      sessionTabs: ensureSessionTab(current.sessionTabs, tab),
      activeSessionTabID: tab.id,
    }));
  }

  async function openTaskFromBoard(
    threadID: string,
    subthreadID: string,
  ): Promise<void> {
    const tabs = deps.getAppState().sessionTabs;
    if (tabs.some((tab) => tab.id === threadSessionTabID(threadID))) {
      await deps.selectSessionTab(threadSessionTabID(threadID));
    } else {
      await deps.selectThread(threadID);
    }
    deps.openConversationSubthreadByID(threadID, subthreadID);
  }

  function openMemorySettings(participantID?: string): void {
    deps.closeProjectMenus();
    deps.setSettingsMemoryFocusID(participantID);
    deps.setSettingsInitialPage("memory");
    deps.setSettingsOpen(true);
  }

  function resetComposerForThreadActivation(): void {
    deps.cancelViewSwitch();
    deps.setArchiveConfirmThreadID(undefined);
    deps.setWorkspaceMode(undefined);
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
    openParticipantProfile,
    openParticipantDM,
    createGroupThread,
    openTaskBoardTab,
    openTaskFromBoard,
    openMemorySettings,
  };
}

function activeThreadForActions(state: AppState): Thread | undefined {
  return activeThreadForState(state);
}
