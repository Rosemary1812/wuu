import { afterEach, describe, expect, it, vi } from "vitest";
import type {
  InitializeResult,
  ParticipantProfile,
  RuntimeContext,
  Thread,
} from "../shared/protocol";
import {
  createThreadSessionTab,
  initialSplitComposerDrafts,
  initialState,
  threadSessionTabID,
  type AppState,
  type ComposerDraftState,
} from "./AppState";
import { createCollaborationActions } from "./CollaborationActions";
import type { ComposerFile, ComposerImage } from "./ComposerMessages";
import type { ContextCompositionEntry } from "./ContextCompositionCard";
import type { InstructionFilesEntry } from "./InstructionFilesCard";
import type { SettingsPage } from "./SettingsView";
import type { WorkspacePanelView } from "./WorkspacePanels";

const originalWuu = (window as unknown as { wuu?: unknown }).wuu;

function restoreWuu(): void {
  if (originalWuu === undefined) {
    delete (window as unknown as { wuu?: unknown }).wuu;
    return;
  }
  Object.defineProperty(window, "wuu", {
    configurable: true,
    value: originalWuu,
  });
}

afterEach(() => {
  restoreWuu();
});

function projectContext(): RuntimeContext {
  return { kind: "project", project_id: "project-1", cwd: "/tmp/project-1" };
}

function initialized(): InitializeResult {
  return {
    protocol_version: "1",
    provider: "codex",
    model: "gpt-5",
    workspace_root: "/tmp/project-1",
  };
}

function thread(id = "thread-1", patch: Partial<Thread> = {}): Thread {
  return {
    id,
    title: id,
    preview: id,
    model_provider: "codex",
    model: "gpt-5",
    cwd: "/tmp/project-1",
    status: "idle",
    pinned: false,
    archived: false,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    turns: [],
    ...patch,
  };
}

function participant(id = "agent-1"): ParticipantProfile {
  return { id, kind: "agent", name: id };
}

function installWuuApi(startThreadResult = thread("created-thread")): {
  startThread: ReturnType<typeof vi.fn>;
  listInstructionFiles: ReturnType<typeof vi.fn>;
  getThreadContextComposition: ReturnType<typeof vi.fn>;
} {
  const startThread = vi.fn().mockResolvedValue({ thread: startThreadResult });
  const listInstructionFiles = vi.fn().mockResolvedValue({
    files: [
      {
        path: "/tmp/project-1/AGENTS.md",
        name: "AGENTS.md",
        source: "AGENTS.md",
        scope: "project",
        bytes: 12,
        content: "instructions",
      },
    ],
  });
  const getThreadContextComposition = vi.fn().mockResolvedValue({
    available: true,
    context_window_tokens: 1000,
    retained_tokens: 100,
    categories: [],
  });
  Object.defineProperty(window, "wuu", {
    configurable: true,
    value: {
      startThread,
      listInstructionFiles,
      getThreadContextComposition,
    },
  });
  return { startThread, listInstructionFiles, getThreadContextComposition };
}

async function flushAsyncActions(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
}

function buildActions({
  initial = {
    ...initialState,
    activeContext: projectContext(),
    initialized: initialized(),
    thread: thread(),
    status: "ready",
  },
}: {
  initial?: AppState;
} = {}) {
  let appState = initial;
  let splitDrafts = initialSplitComposerDrafts();
  let prompt = "draft";
  let composerImages: ComposerImage[] = [
    { id: "image-1", media_type: "image/png", data: "" },
  ];
  let composerFiles: ComposerFile[] = [
    {
      id: "file-1",
      media_type: "application/pdf",
      data: "",
      filename: "file.pdf",
    },
  ];
  let archiveConfirmThreadID: string | undefined = "thread-1";
  let workspaceMode: WorkspacePanelView | undefined = "files";
  let contextCompositionEntries: ContextCompositionEntry[] = [];
  let instructionFilesEntries: InstructionFilesEntry[] = [];
  let settingsMemoryFocusID: string | undefined;
  let settingsInitialPage: SettingsPage = "general";
  let settingsOpen = false;
  const primaryDraft: ComposerDraftState = {
    prompt: "persist me",
    images: [],
    files: [],
  };
  const cancelViewSwitch = vi.fn();
  const activateThread = vi.fn().mockResolvedValue(undefined);
  const selectThread = vi.fn().mockResolvedValue(undefined);
  const selectSessionTab = vi.fn().mockResolvedValue(undefined);
  const closeConversationSearch = vi.fn();
  const closeEnvironmentPanel = vi.fn();
  const setOpenSubthreadPanel = vi.fn();
  const setRightPanelOpenWithMotion = vi.fn();
  const openParticipantProfilePanel = vi.fn();
  const scheduleStreamScroll = vi.fn();
  const openConversationSubthreadByID = vi.fn();
  const closeProjectMenus = vi.fn();
  const openingDMParticipantIDRef = { current: undefined as string | undefined };
  const actions = createCollaborationActions({
    getAppState: () => appState,
    setAppState: (update) => {
      appState = typeof update === "function" ? update(appState) : update;
    },
    getActiveTitle: () => "Active title",
    getPrimaryComposerDraft: () => primaryDraft,
    setSplitComposerDrafts: (update) => {
      splitDrafts =
        typeof update === "function" ? update(splitDrafts) : update;
    },
    setPrompt: (update) => {
      prompt = typeof update === "function" ? update(prompt) : update;
    },
    setComposerImages: (update) => {
      composerImages =
        typeof update === "function" ? update(composerImages) : update;
    },
    setComposerFiles: (update) => {
      composerFiles =
        typeof update === "function" ? update(composerFiles) : update;
    },
    setArchiveConfirmThreadID: (update) => {
      archiveConfirmThreadID =
        typeof update === "function" ? update(archiveConfirmThreadID) : update;
    },
    setWorkspaceMode: (mode) => {
      workspaceMode = mode;
    },
    cancelViewSwitch,
    activateThread,
    selectThread,
    selectSessionTab,
    closeConversationSearch,
    closeEnvironmentPanel,
    setOpenSubthreadPanel,
    setRightPanelOpenWithMotion,
    openParticipantProfilePanel,
    setContextCompositionEntries: (update) => {
      contextCompositionEntries =
        typeof update === "function"
          ? update(contextCompositionEntries)
          : update;
    },
    setInstructionFilesEntries: (update) => {
      instructionFilesEntries =
        typeof update === "function" ? update(instructionFilesEntries) : update;
    },
    scheduleStreamScroll,
    openingDMParticipantIDRef,
    openConversationSubthreadByID,
    closeProjectMenus,
    setSettingsMemoryFocusID: (participantID) => {
      settingsMemoryFocusID = participantID;
    },
    setSettingsInitialPage: (page) => {
      settingsInitialPage = page;
    },
    setSettingsOpen: (open) => {
      settingsOpen = open;
    },
  });
  return {
    actions,
    getAppState: () => appState,
    getComposerState: () => ({
      splitDrafts,
      prompt,
      composerImages,
      composerFiles,
      archiveConfirmThreadID,
      workspaceMode,
    }),
    getContextEntries: () => contextCompositionEntries,
    getInstructionEntries: () => instructionFilesEntries,
    getSettings: () => ({
      settingsMemoryFocusID,
      settingsInitialPage,
      settingsOpen,
    }),
    cancelViewSwitch,
    activateThread,
    selectThread,
    selectSessionTab,
    closeConversationSearch,
    closeEnvironmentPanel,
    setOpenSubthreadPanel,
    setRightPanelOpenWithMotion,
    openParticipantProfilePanel,
    scheduleStreamScroll,
    openingDMParticipantIDRef,
    openConversationSubthreadByID,
    closeProjectMenus,
  };
}

describe("createCollaborationActions", () => {
  it("opens the skills tab and persists the active composer draft", () => {
    installWuuApi();
    const harness = buildActions();

    harness.actions.openSkillsTab();

    expect(harness.getAppState().activeSessionTabID).toContain("skills:");
    expect(harness.getAppState().sessionTabs[0]?.kind).toBe("skills");
    expect(harness.getAppState().status).toBe("ready");
    expect(harness.getComposerState().archiveConfirmThreadID).toBeUndefined();
    expect(harness.getComposerState().workspaceMode).toBeUndefined();
  });

  it("adds instruction and context cards, then fills them from the desktop API", async () => {
    const api = installWuuApi();
    const baseThread = thread("thread-1", {
      turns: [
        {
          id: "turn-1",
          items_view: "full",
          status: "completed",
          items: [],
        },
      ],
    });
    const harness = buildActions({
      initial: {
        ...initialState,
        activeContext: projectContext(),
        initialized: initialized(),
        thread: baseThread,
      },
    });

    harness.actions.openInstructions();
    harness.actions.openContextComposition();

    expect(harness.getInstructionEntries()[0]).toMatchObject({
      threadID: "thread-1",
      title: "thread-1",
      loading: true,
    });
    expect(harness.getContextEntries()[0]).toMatchObject({
      threadID: "thread-1",
      afterTurnID: "turn-1",
      loading: true,
    });

    await flushAsyncActions();

    expect(api.listInstructionFiles).toHaveBeenCalled();
    expect(api.getThreadContextComposition).toHaveBeenCalledWith("thread-1");
    expect(harness.getInstructionEntries()[0]?.loading).toBe(false);
    expect(harness.getInstructionEntries()[0]?.result?.files[0]?.name).toBe(
      "AGENTS.md",
    );
    expect(harness.getContextEntries()[0]?.loading).toBe(false);
    expect(harness.getContextEntries()[0]?.result?.available).toBe(true);
    expect(harness.scheduleStreamScroll).toHaveBeenCalledTimes(4);
  });

  it("opens participant profile surfaces and memory settings", () => {
    installWuuApi();
    const profile = participant();
    const harness = buildActions();

    harness.actions.openParticipantProfile(profile);
    harness.actions.openMemorySettings(profile.id);

    expect(harness.closeConversationSearch).toHaveBeenCalledWith({
      immediate: true,
    });
    expect(harness.closeEnvironmentPanel).toHaveBeenCalledWith({
      dismissed: true,
    });
    expect(harness.setOpenSubthreadPanel).toHaveBeenCalledWith(undefined);
    expect(harness.setRightPanelOpenWithMotion).toHaveBeenCalledWith(false);
    expect(harness.openParticipantProfilePanel).toHaveBeenCalledWith(profile);
    expect(harness.closeProjectMenus).toHaveBeenCalled();
    expect(harness.getSettings()).toEqual({
      settingsMemoryFocusID: "agent-1",
      settingsInitialPage: "memory",
      settingsOpen: true,
    });
  });

  it("focuses an existing DM thread instead of starting a duplicate", async () => {
    const api = installWuuApi();
    const existing = thread("dm-thread", {
      dm_participant_id: "agent-1",
      workspace_kind: "dm",
    });
    const harness = buildActions({
      initial: {
        ...initialState,
        activeContext: projectContext(),
        initialized: initialized(),
        threads: [existing],
      },
    });

    await harness.actions.openParticipantDM(participant());

    expect(harness.activateThread).toHaveBeenCalledWith("dm-thread");
    expect(api.startThread).not.toHaveBeenCalled();
    expect(harness.openingDMParticipantIDRef.current).toBeUndefined();
  });

  it("starts a new DM thread and makes it the active primary tab", async () => {
    const created = thread("new-dm", {
      dm_participant_id: "agent-1",
      status: "in_progress",
    });
    const api = installWuuApi(created);
    const harness = buildActions();

    await harness.actions.openParticipantDM(participant());

    expect(api.startThread).toHaveBeenCalledWith({
      dm_participant_id: "agent-1",
    });
    expect(harness.cancelViewSwitch).toHaveBeenCalled();
    expect(harness.getComposerState()).toMatchObject({
      prompt: "",
      composerImages: [],
      composerFiles: [],
      archiveConfirmThreadID: undefined,
      workspaceMode: undefined,
    });
    expect(harness.getAppState().thread?.id).toBe("new-dm");
    expect(harness.getAppState().activePane).toBe("primary");
    expect(harness.getAppState().activeSessionTabID).toBe(
      threadSessionTabID("new-dm"),
    );
    expect(harness.getAppState().running).toBe(true);
  });

  it("starts a group thread and opens task board tabs", async () => {
    const group = thread("group-thread", { group: true });
    const api = installWuuApi(group);
    const harness = buildActions();

    await harness.actions.createGroupThread("Core team");
    harness.actions.openTaskBoardTab(group);

    expect(api.startThread).toHaveBeenCalledWith({
      group: true,
      title: "Core team",
    });
    expect(harness.getAppState().thread?.id).toBe("group-thread");
    expect(harness.getAppState().activeSessionTabID).toContain("board:");
  });

  it("opens a board task through an existing group tab before opening the subthread", async () => {
    installWuuApi();
    const group = thread("group-thread", { group: true });
    const context = projectContext();
    const harness = buildActions({
      initial: {
        ...initialState,
        activeContext: context,
        initialized: initialized(),
        sessionTabs: [createThreadSessionTab(group, context)],
      },
    });

    await harness.actions.openTaskFromBoard("group-thread", "subthread-1");

    expect(harness.selectSessionTab).toHaveBeenCalledWith(
      threadSessionTabID("group-thread"),
    );
    expect(harness.selectThread).not.toHaveBeenCalled();
    expect(harness.openConversationSubthreadByID).toHaveBeenCalledWith(
      "group-thread",
      "subthread-1",
    );
  });
});
