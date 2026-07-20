import { afterEach, describe, expect, it, vi } from "vitest";
import type {
  InitializeResult,
  ParticipantProfile,
  RuntimeContext,
  Thread,
} from "../shared/protocol";
import {
  initialSplitComposerDrafts,
  initialState,
  type AppState,
  type ComposerDraftState,
} from "./AppState";
import { createCollaborationActions } from "./CollaborationActions";
import type { ComposerFile, ComposerImage } from "./ComposerMessages";
import type { ContextCompositionEntry } from "./ContextCompositionCard";
import type { InstructionFilesEntry } from "./InstructionFilesCard";
import type { SettingsPage } from "./SettingsView";

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
  const scheduleStreamScroll = vi.fn();
  const closeProjectMenus = vi.fn();
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
    cancelViewSwitch,
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
    }),
    getContextEntries: () => contextCompositionEntries,
    getInstructionEntries: () => instructionFilesEntries,
    getSettings: () => ({
      settingsMemoryFocusID,
      settingsInitialPage,
      settingsOpen,
    }),
    cancelViewSwitch,
    scheduleStreamScroll,
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

  it("opens participant memory settings", () => {
    installWuuApi();
    const profile = participant();
    const harness = buildActions();

    harness.actions.openMemorySettings(profile.id);

    expect(harness.closeProjectMenus).toHaveBeenCalled();
    expect(harness.getSettings()).toEqual({
      settingsMemoryFocusID: "agent-1",
      settingsInitialPage: "memory",
      settingsOpen: true,
    });
  });

  it("closes project menus when opening collaboration intake", () => {
    installWuuApi();
    const harness = buildActions();

    harness.actions.openCollaborationIntake();

    expect(harness.closeProjectMenus).toHaveBeenCalled();
  });

});
