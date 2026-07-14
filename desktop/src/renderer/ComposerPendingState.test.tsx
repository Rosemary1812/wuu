import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ServerEvent, Thread, WuuDesktopApi } from "../shared/protocol";
import {
  emptyComposerDraft,
  initialState,
  type AppState,
  type ComposerDraftState,
} from "./AppState";
import type { QueuedComposerMessage } from "./ComposerMessages";
import {
  useComposerPendingState,
  type ComposerPendingStateController,
} from "./ComposerPendingState";

let mountedRoots: Root[] = [];

afterEach(() => {
  act(() => {
    for (const root of mountedRoots) root.unmount();
  });
  mountedRoots = [];
  document.body.innerHTML = "";
  delete (window as unknown as { wuu?: unknown }).wuu;
  vi.restoreAllMocks();
});

async function flushEffects(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
}

function message(id: string, text: string): QueuedComposerMessage {
  return { id, text, images: [], files: [] };
}

function thread(id = "thread-a", running = false): Thread {
  return {
    id,
    status: running ? "in_progress" : "completed",
    turns: running
      ? [{ id: "turn-running", status: "in_progress", items: [] }]
      : [],
  } as unknown as Thread;
}

function installWuuStub(overrides: Partial<WuuDesktopApi>): void {
  (window as unknown as { wuu: WuuDesktopApi }).wuu = {
    ...overrides,
  } as WuuDesktopApi;
}

async function renderComposerPendingState({
  appState = {
    ...initialState,
    thread: thread(),
    threads: [thread()],
  },
  primaryDraft = emptyComposerDraft(),
}: {
  appState?: AppState;
  primaryDraft?: ComposerDraftState;
} = {}): Promise<{
  get: () => ComposerPendingStateController;
  setStatus: ReturnType<typeof vi.fn>;
  restorePrimaryComposerDraft: ReturnType<typeof vi.fn>;
  restoreComposerDraftForThread: ReturnType<typeof vi.fn>;
  sendComposerMessageToThread: ReturnType<typeof vi.fn>;
  setPrimaryDraft: (draft: ComposerDraftState) => void;
  setAppState: (state: AppState) => void;
}> {
  let latest: ComposerPendingStateController | undefined;
  let currentAppState = appState;
  let currentPrimaryDraft = primaryDraft;
  const setStatus = vi.fn();
  const restorePrimaryComposerDraft = vi.fn((draft: ComposerDraftState) => {
    currentPrimaryDraft = draft;
  });
  const restoreComposerDraftForThread = vi.fn(
    (_threadID: string, draft: ComposerDraftState) =>
      restorePrimaryComposerDraft(draft),
  );
  const sendComposerMessageToThread = vi.fn();

  function Probe() {
    latest = useComposerPendingState({
      getAppState: () => currentAppState,
      getPrimaryComposerDraft: () => currentPrimaryDraft,
      restoreComposerDraftForThread,
      setStatus,
      sendComposerMessageToThread,
    });
    return null;
  }

  const container = document.createElement("div");
  document.body.appendChild(container);
  const root = createRoot(container);
  mountedRoots.push(root);

  await act(async () => {
    root.render(createElement(Probe));
    await flushEffects();
  });

  return {
    get: () => {
      if (!latest) {
        throw new Error("composer pending state was not rendered");
      }
      return latest;
    },
    setStatus,
    restorePrimaryComposerDraft,
    restoreComposerDraftForThread,
    sendComposerMessageToThread,
    setPrimaryDraft: (draft) => {
      currentPrimaryDraft = draft;
    },
    setAppState: (state) => {
      currentAppState = state;
    },
  };
}

describe("useComposerPendingState", () => {
  it("enqueues messages per thread", async () => {
    const hook = await renderComposerPendingState();

    act(() => {
      hook
        .get()
        .enqueueComposerMessage("thread-a", message("queue-1", "First"));
    });

    expect(
      hook.get().pendingComposerMessagesByThread["thread-a"]?.queued,
    ).toEqual([message("queue-1", "First")]);
  });

  it("syncs server events by removing materialized queue and guide messages", async () => {
    const hook = await renderComposerPendingState();

    act(() => {
      hook
        .get()
        .enqueueComposerMessage("thread-a", message("queue-1", "First"));
      hook
        .get()
        .updateThreadPendingComposerMessages("thread-a", (previous) => ({
          ...previous,
          guides: [message("guide-1", "Guide")],
        }));
      hook.get().syncPendingComposerMessagesFromServerEvent({
        kind: "notification",
        message: {
          method: "turn/started",
          params: { thread_id: "thread-a", queue_id: "queue-1" },
        },
      } as ServerEvent);
      hook.get().syncPendingComposerMessagesFromServerEvent({
        kind: "notification",
        message: {
          method: "item/completed",
          params: {
            thread_id: "thread-a",
            item: {
              id: "item-1",
              type: "user_message",
              source_id: "guide-1",
            },
          },
        },
      } as ServerEvent);
    });

    expect(
      hook.get().pendingComposerMessagesByThread["thread-a"],
    ).toBeUndefined();
  });

  it("restores a queued message into the primary composer for editing", async () => {
    const dequeueTurn = vi.fn().mockResolvedValue({ ok: true });
    installWuuStub({ dequeueTurn });
    const hook = await renderComposerPendingState();

    act(() => {
      hook
        .get()
        .enqueueComposerMessage("thread-a", message("queue-1", "Edit me"));
    });
    await act(async () => {
      await hook.get().editQueuedMessage("queue-1");
    });

    expect(hook.restorePrimaryComposerDraft).toHaveBeenCalledWith({
      prompt: "Edit me",
      images: [],
      files: [],
    });
    expect(dequeueTurn).toHaveBeenCalledWith("thread-a", "queue-1");
    expect(
      hook.get().pendingComposerMessagesByThread["thread-a"],
    ).toBeUndefined();
    expect(hook.setStatus).toHaveBeenCalledWith(
      "已撤回排队消息，可编辑后重新发送",
    );
  });

  it("refuses to edit pending messages while the primary composer has content", async () => {
    const hook = await renderComposerPendingState({
      primaryDraft: { prompt: "busy", images: [], files: [] },
    });

    act(() => {
      hook
        .get()
        .enqueueComposerMessage("thread-a", message("queue-1", "Edit me"));
    });
    await act(async () => {
      await hook.get().editQueuedMessage("queue-1");
    });

    expect(hook.restorePrimaryComposerDraft).not.toHaveBeenCalled();
    expect(hook.setStatus).toHaveBeenCalledWith(
      "先发送或清空当前输入，再编辑排队消息",
    );
  });

  it("does not restore a queued message that already started", async () => {
    installWuuStub({
      dequeueTurn: vi.fn().mockResolvedValue({ ok: false }),
    });
    const hook = await renderComposerPendingState();

    act(() => {
      hook
        .get()
        .enqueueComposerMessage("thread-a", message("queue-1", "Too late"));
    });
    await act(async () => {
      await hook.get().editQueuedMessage("queue-1");
    });

    expect(hook.restorePrimaryComposerDraft).not.toHaveBeenCalled();
    expect(
      hook.get().pendingComposerMessagesByThread["thread-a"],
    ).toBeUndefined();
    expect(hook.setStatus).toHaveBeenCalledWith("排队消息已被处理，无法取消");
  });

  it("restores an edited draft to its originating thread after a tab switch", async () => {
    let finishDequeue: ((value: { ok: boolean }) => void) | undefined;
    installWuuStub({
      dequeueTurn: vi.fn().mockImplementation(
        () =>
          new Promise<{ ok: boolean }>((resolve) => {
            finishDequeue = resolve;
          }),
      ),
    });
    const hook = await renderComposerPendingState();

    act(() => {
      hook
        .get()
        .enqueueComposerMessage("thread-a", message("queue-1", "Keep me"));
    });
    let editing: Promise<void> | undefined;
    await act(async () => {
      editing = hook.get().editQueuedMessage("queue-1");
      await Promise.resolve();
    });
    hook.setAppState({
      ...initialState,
      thread: thread("thread-b"),
      threads: [thread("thread-a"), thread("thread-b")],
    });
    await act(async () => {
      finishDequeue?.({ ok: true });
      await editing;
    });

    expect(hook.restoreComposerDraftForThread).toHaveBeenCalledWith(
      "thread-a",
      { prompt: "Keep me", images: [], files: [] },
    );
  });

  it("dequeues before sending a queued message whose active turn ended", async () => {
    const dequeueTurn = vi.fn().mockResolvedValue({ ok: true });
    installWuuStub({ dequeueTurn });
    const hook = await renderComposerPendingState();
    hook.sendComposerMessageToThread.mockResolvedValue(true);

    act(() => {
      hook
        .get()
        .enqueueComposerMessage("thread-a", message("queue-1", "Send now"));
    });
    await act(async () => {
      await hook.get().guideQueuedMessage("queue-1");
    });

    expect(dequeueTurn).toHaveBeenCalledWith("thread-a", "queue-1");
    expect(hook.sendComposerMessageToThread).toHaveBeenCalledWith(
      message("queue-1", "Send now"),
      expect.objectContaining({ id: "thread-a" }),
    );
    expect(
      hook.get().pendingComposerMessagesByThread["thread-a"],
    ).toBeUndefined();
  });

  it("restores a dequeued message when immediate sending fails", async () => {
    installWuuStub({
      dequeueTurn: vi.fn().mockResolvedValue({ ok: true }),
    });
    const hook = await renderComposerPendingState();
    hook.sendComposerMessageToThread.mockResolvedValue(false);

    act(() => {
      hook
        .get()
        .enqueueComposerMessage("thread-a", message("queue-1", "Retry me"));
    });
    await act(async () => {
      await hook.get().guideQueuedMessage("queue-1");
    });

    expect(hook.restorePrimaryComposerDraft).toHaveBeenCalledWith({
      prompt: "Retry me",
      images: [],
      files: [],
    });
  });

  it("rolls back a queued message removal when dequeue fails", async () => {
    installWuuStub({
      dequeueTurn: vi.fn().mockRejectedValue(new Error("network down")),
    });
    const hook = await renderComposerPendingState();

    act(() => {
      hook
        .get()
        .enqueueComposerMessage("thread-a", message("queue-1", "First"));
      hook
        .get()
        .enqueueComposerMessage("thread-a", message("queue-2", "Second"));
    });
    let removed = true;
    await act(async () => {
      removed = await hook.get().removeQueuedMessage("queue-1");
    });

    expect(removed).toBe(false);
    expect(
      hook
        .get()
        .pendingComposerMessagesByThread["thread-a"]?.queued.map(
          (item) => item.id,
        ),
    ).toEqual(["queue-1", "queue-2"]);
    expect(hook.setStatus).toHaveBeenCalledWith("network down");
  });
});
