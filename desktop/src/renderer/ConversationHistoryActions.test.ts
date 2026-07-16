import { afterEach, describe, expect, it, vi } from "vitest";
import type {
  GitStatusResult,
  RuntimeContext,
  Thread,
  ThreadItem,
  Turn,
} from "../shared/protocol";
import {
  createThreadSessionTab,
  initialSplitComposerDrafts,
  initialState,
  threadSessionTabID,
  type AppState,
} from "./AppState";
import type { ComposerFile, ComposerImage, QueuedComposerMessage } from "./ComposerMessages";
import {
  createConversationHistoryActions,
  type HistoryMessageEditState,
  type PendingForkState,
} from "./ConversationHistoryActions";
import * as TurnViewHelpers from "./TurnViewHelpers";
import { resolveLocalizedText, setActiveLocale } from "./i18n";

// `scrollToUserMessage` schedules retry timeouts that would otherwise leak
// across tests in jsdom (no DOM anchor is mounted, so the helper keeps
// retrying). Replace it with a synchronous stub that just records calls.
vi.spyOn(TurnViewHelpers, "scrollToUserMessage").mockImplementation(() => {});

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
  setActiveLocale("zh-CN");
});

function projectContext(): RuntimeContext {
  return { kind: "project", project_id: "project-1", cwd: "/tmp/project-1" };
}

function userItem(id = "item-1", text = "Hello"): ThreadItem {
  return {
    id,
    type: "user_message",
    role: "user",
    text,
  };
}

function turn(id: string, items: ThreadItem[] = [userItem()]): Turn {
  return {
    id,
    items_view: "full",
    status: "completed",
    items,
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

function gitStatus(isRepo = true): GitStatusResult {
  return { is_repo: isRepo, branch: "main", dirty_count: 0 };
}

function installWuuApi({
  forkThreadResult = thread("fork-thread"),
  editThreadResult = thread("edited-thread"),
  gitStatusResult = gitStatus(true),
}: {
  forkThreadResult?: Thread;
  editThreadResult?: Thread;
  gitStatusResult?: GitStatusResult;
} = {}): {
  forkThread: ReturnType<typeof vi.fn>;
  editThreadMessage: ReturnType<typeof vi.fn>;
  gitStatus: ReturnType<typeof vi.fn>;
} {
  const forkThread = vi.fn().mockResolvedValue({ thread: forkThreadResult });
  const editThreadMessage = vi.fn().mockResolvedValue({
    thread: editThreadResult,
  });
  const gitStatusMock = vi.fn().mockResolvedValue(gitStatusResult);
  Object.defineProperty(window, "wuu", {
    configurable: true,
    value: {
      forkThread,
      editThreadMessage,
      gitStatus: gitStatusMock,
    },
  });
  return { forkThread, editThreadMessage, gitStatus: gitStatusMock };
}

function buildActions({
  initial,
  pendingFork,
  sendResult = true,
  hasPendingMessages = false,
}: {
  initial?: AppState;
  pendingFork?: PendingForkState;
  sendResult?: boolean;
  hasPendingMessages?: boolean;
} = {}) {
  let appState =
    initial ??
    ({
      ...initialState,
      activeContext: projectContext(),
      thread: thread(),
      status: "ready",
    } as AppState);
  const appStateRef = { current: appState };
  let pendingForkState = pendingFork;
  let historyMessageEdit: HistoryMessageEditState | undefined;
  let prompt = "draft prompt";
  let composerImages: ComposerImage[] = [
    { id: "image-1", media_type: "image/png", data: "img" },
  ];
  let composerFiles: ComposerFile[] = [
    {
      id: "file-1",
      media_type: "application/pdf",
      data: "pdf",
      filename: "file.pdf",
    },
  ];
  let splitDrafts = initialSplitComposerDrafts();
  
  const localDemoThreadsRef = { current: new Map<string, Thread>() };
  const closeConversationSearch = vi.fn();
  const clearEnvironmentDialog = vi.fn();
  const scheduleGitStatusRefresh = vi.fn();
  const disableConversationAutoFollow = vi.fn();
  const enableConversationAutoFollow = vi.fn();
  const rememberConversationScrollForEdit = vi.fn();
  const restoreConversationScrollForEdit = vi.fn();
  const restorePrimaryComposerDraft = vi.fn();
  const sendComposerMessageToThread = vi
    .fn()
    .mockImplementation(
      async (message: QueuedComposerMessage, targetThread: Thread) => {
        if (sendResult) {
          const sentTurn = turn("sent-turn", [
            { id: "sent-item", type: "user_message", text: message.text },
          ]);
          appStateRef.current = {
            ...appStateRef.current,
            thread:
              appStateRef.current.thread?.id === targetThread.id
                ? {
                    ...targetThread,
                    turns: [...targetThread.turns, sentTurn],
                  }
                : appStateRef.current.thread,
          };
          appState = appStateRef.current;
        }
        return sendResult;
      },
    );
  const actions = createConversationHistoryActions({
    appStateRef,
    setAppState: (update) => {
      appState = typeof update === "function" ? update(appState) : update;
      appStateRef.current = appState;
    },
    localDemoThreadsRef,
    getPendingFork: () => pendingForkState,
    setPendingFork: (update) => {
      pendingForkState =
        typeof update === "function" ? update(pendingForkState) : update;
    },
    setHistoryMessageEdit: (update) => {
      historyMessageEdit =
        typeof update === "function" ? update(historyMessageEdit) : update;
    },
    
    getPrompt: () => prompt,
    getComposerImages: () => composerImages,
    getComposerFiles: () => composerFiles,
    getSplitComposerDrafts: () => splitDrafts,
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
    setSplitComposerDrafts: (update) => {
      splitDrafts =
        typeof update === "function" ? update(splitDrafts) : update;
    },
    restorePrimaryComposerDraft,
    closeConversationSearch,
    clearEnvironmentDialog,
    scheduleGitStatusRefresh,
    disableConversationAutoFollow,
    enableConversationAutoFollow,
    rememberConversationScrollForEdit,
    restoreConversationScrollForEdit,
    threadHasPendingComposerMessages: () => hasPendingMessages,
    sendComposerMessageToThread,
    worktreeForkNonGitReason: "当前工作目录不是 git 仓库，不能创建 git worktree",
  });
  return {
    actions,
    appStateRef,
    getAppState: () => appState,
    getPendingFork: () => pendingForkState,
    getHistoryMessageEdit: () => historyMessageEdit,
    getComposerState: () => ({
      prompt,
      composerImages,
      composerFiles,
      splitDrafts,
      
    }),
    localDemoThreadsRef,
    closeConversationSearch,
    clearEnvironmentDialog,
    scheduleGitStatusRefresh,
    disableConversationAutoFollow,
    enableConversationAutoFollow,
    rememberConversationScrollForEdit,
    restoreConversationScrollForEdit,
    restorePrimaryComposerDraft,
    sendComposerMessageToThread,
  };
}

describe("createConversationHistoryActions", () => {
  it("localizes history action status in English", () => {
    setActiveLocale("en-US");
    const source = thread();
    const harness = buildActions();
    harness.localDemoThreadsRef.current.set(source.id, source);

    harness.actions.startEditingThreadMessageFromHistory(
      source,
      "turn-1",
      userItem(),
    );

    expect(resolveLocalizedText(harness.getAppState().status)).toBe(
      "History cannot be edited in demo conversations",
    );
  });

  it("forks a pending conversation and clears the pending dialog", async () => {
    const source = thread("source-thread");
    const fork = thread("fork-thread");
    const api = installWuuApi({ forkThreadResult: fork });
    const harness = buildActions({
      initial: {
        ...initialState,
        activeContext: projectContext(),
        thread: source,
        sessionTabs: [createThreadSessionTab(source, projectContext())],
        activeSessionTabID: threadSessionTabID(source.id),
      },
      pendingFork: { sourceThread: source, turnID: "turn-1", itemID: "item-1" },
    });

    await harness.actions.choosePendingFork("local");

    expect(api.forkThread).toHaveBeenCalledWith(
      "source-thread",
      "turn-1",
      "item-1",
      "local",
    );
    expect(harness.enableConversationAutoFollow).toHaveBeenCalled();
    expect(harness.getPendingFork()).toBeUndefined();
    expect(harness.getComposerState()).toMatchObject({
      prompt: "",
      composerImages: [],
      composerFiles: [],
      
    });
    expect(harness.getAppState().thread?.id).toBe("fork-thread");
  });

  it("keeps the fork dialog open when worktree fork is unavailable", async () => {
    const source = thread("source-thread");
    const api = installWuuApi({ gitStatusResult: gitStatus(false) });
    const pending = { sourceThread: source, turnID: "turn-1", itemID: "item-1" };
    const harness = buildActions({ pendingFork: pending });

    await harness.actions.choosePendingFork("worktree");

    expect(api.gitStatus).toHaveBeenCalled();
    expect(api.forkThread).not.toHaveBeenCalled();
    expect(harness.getPendingFork()).toBe(pending);
    expect(harness.getAppState().status).toBe(
      "当前工作目录不是 git 仓库，不能创建 git worktree",
    );
  });

  it("opens the fork picker for non-latest user messages", async () => {
    installWuuApi();
    const oldItem = userItem("old-item");
    const latestItem = userItem("latest-item");
    const source = thread("source-thread", {
      turns: [turn("old-turn", [oldItem]), turn("latest-turn", [latestItem])],
    });
    const harness = buildActions();

    await harness.actions.forkThreadFromMessage(
      source,
      "old-turn",
      "old-item",
    );

    expect(harness.closeConversationSearch).toHaveBeenCalledWith({
      immediate: true,
    });
    expect(harness.clearEnvironmentDialog).toHaveBeenCalled();
    expect(harness.scheduleGitStatusRefresh).toHaveBeenCalledWith(0);
    expect(harness.getPendingFork()).toEqual({
      sourceThread: source,
      turnID: "old-turn",
      itemID: "old-item",
    });
  });

  it("starts and cancels a history message edit", () => {
    installWuuApi();
    const item = userItem("item-1");
    const source = thread("source-thread");
    const harness = buildActions();

    harness.actions.startEditingThreadMessageFromHistory(
      source,
      "turn-1",
      item,
      "secondary",
    );

    expect(harness.getHistoryMessageEdit()).toEqual({
      threadID: "source-thread",
      turnID: "turn-1",
      itemID: "item-1",
      pane: "secondary",
      submitting: false,
    });
    // Capture happens *before* anything that would mutate the viewport,
    // so the snapshot reflects the user's true pre-edit scrollTop +
    // auto-follow state — the data that cancel needs to put them back.
    expect(harness.rememberConversationScrollForEdit).toHaveBeenCalledTimes(1);
    // The bubble-to-editor swap makes the conversation taller; without
    // disarming auto-follow first, the reflow would be read as "user
    // scrolled away from latest" and the "跳到最新" pill would pop up.
    expect(harness.disableConversationAutoFollow).toHaveBeenCalledTimes(1);
    // The editor also needs to be visible — the previous flow relied on
    // the browser's default focus-scroll, which both moved the scroll
    // unexpectedly and triggered the auto-follow disarm above.
    expect(TurnViewHelpers.scrollToUserMessage).toHaveBeenCalledWith(
      "turn-1",
      "item-1",
    );

    harness.actions.cancelEditingThreadMessage();
    expect(harness.getHistoryMessageEdit()).toBeUndefined();
    // Cancel restores the captured scrollTop + auto-follow snapshot
    // instead of blanket-enabling auto-follow (which would let the
    // resize observer drop the user at the bottom even when they had
    // been scrolled up before clicking edit).
    expect(harness.restoreConversationScrollForEdit).toHaveBeenCalledTimes(1);
    expect(harness.enableConversationAutoFollow).not.toHaveBeenCalled();
  });

  it("blocks history edit while the thread has pending composer messages", () => {
    installWuuApi();
    const harness = buildActions({ hasPendingMessages: true });

    harness.actions.startEditingThreadMessageFromHistory(
      thread("source-thread"),
      "turn-1",
      userItem("item-1"),
    );

    expect(harness.getHistoryMessageEdit()).toBeUndefined();
    expect(resolveLocalizedText(harness.getAppState().status)).toBe(
      "先处理待发送消息，再编辑历史",
    );
  });

  it("submits an edited history message and sends the replacement text", async () => {
    const item = userItem("item-1");
    const source = thread("source-thread", { turns: [turn("turn-1", [item])] });
    const edited = thread("source-thread", { turns: [] });
    const api = installWuuApi({ editThreadResult: edited });
    const harness = buildActions({
      initial: {
        ...initialState,
        activeContext: projectContext(),
        thread: source,
      },
    });
    harness.actions.startEditingThreadMessageFromHistory(
      source,
      "turn-1",
      item,
    );

    await harness.actions.submitEditedThreadMessageFromHistory(
      source,
      "turn-1",
      item,
      "Edited text",
      [{ media_type: "image/png", data: "img" }],
      [{ media_type: "application/pdf", data: "pdf", filename: "file.pdf" }],
    );

    expect(api.editThreadMessage).toHaveBeenCalledWith(
      "source-thread",
      "turn-1",
      "item-1",
    );
    expect(harness.sendComposerMessageToThread).toHaveBeenCalled();
    const sent = harness.sendComposerMessageToThread.mock.calls[0][0];
    expect(sent).toMatchObject({
      text: "Edited text",
      images: [{ media_type: "image/png", data: "img" }],
      files: [
        {
          media_type: "application/pdf",
          data: "pdf",
          filename: "file.pdf",
        },
      ],
    });
    expect(harness.getHistoryMessageEdit()).toBeUndefined();
  });

  it("restores the edited draft when replacement sending fails", async () => {
    const item = userItem("item-1");
    const source = thread("source-thread", { turns: [turn("turn-1", [item])] });
    installWuuApi({ editThreadResult: thread("source-thread") });
    const harness = buildActions({ sendResult: false });

    await harness.actions.submitEditedThreadMessageFromHistory(
      source,
      "turn-1",
      item,
      "Edited text",
      [],
      [],
    );

    expect(harness.restorePrimaryComposerDraft).toHaveBeenCalledWith({
      prompt: "Edited text",
      images: [],
      files: [],
    });
  });
});
