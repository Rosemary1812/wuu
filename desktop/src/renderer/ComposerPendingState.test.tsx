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
  sendComposerMessageToThread: ReturnType<typeof vi.fn>;
  setPrimaryDraft: (draft: ComposerDraftState) => void;
}> {
  let latest: ComposerPendingStateController | undefined;
  let currentAppState = appState;
  let currentPrimaryDraft = primaryDraft;
  const setStatus = vi.fn();
  const restorePrimaryComposerDraft = vi.fn((draft: ComposerDraftState) => {
    currentPrimaryDraft = draft;
  });
  const sendComposerMessageToThread = vi.fn();

  function Probe() {
    latest = useComposerPendingState({
      getAppState: () => currentAppState,
      getPrimaryComposerDraft: () => currentPrimaryDraft,
      restorePrimaryComposerDraft,
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
    sendComposerMessageToThread,
    setPrimaryDraft: (draft) => {
      currentPrimaryDraft = draft;
    },
  };
}

describe("useComposerPendingState", () => {
  it("enqueues messages per thread", async () => {
    const hook = await renderComposerPendingState();

    act(() => {
      hook.get().enqueueComposerMessage("thread-a", message("queue-1", "First"));
    });

    expect(
      hook.get().pendingComposerMessagesByThread["thread-a"]?.queued,
    ).toEqual([message("queue-1", "First")]);
  });

  it("syncs server events by removing materialized queue and guide messages", async () => {
    const hook = await renderComposerPendingState();

    act(() => {
      hook.get().enqueueComposerMessage("thread-a", message("queue-1", "First"));
      hook.get().updateThreadPendingComposerMessages("thread-a", (previous) => ({
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

    expect(hook.get().pendingComposerMessagesByThread["thread-a"]).toBeUndefined();
  });

  it("restores a queued message into the primary composer for editing", async () => {
    const hook = await renderComposerPendingState();

    act(() => {
      hook.get().enqueueComposerMessage("thread-a", message("queue-1", "Edit me"));
    });
    await act(async () => {
      await hook.get().editQueuedMessage("queue-1");
    });

    expect(hook.restorePrimaryComposerDraft).toHaveBeenCalledWith({
      prompt: "Edit me",
      images: [],
      files: [],
    });
    expect(hook.get().queuedMessageEditTarget).toEqual({
      threadID: "thread-a",
      queueID: "queue-1",
    });
    expect(hook.setStatus).toHaveBeenCalledWith(
      "正在编辑第 1 条排队消息，发送后会保存到原位置",
    );
  });

  it("refuses to edit pending messages while the primary composer has content", async () => {
    const hook = await renderComposerPendingState({
      primaryDraft: { prompt: "busy", images: [], files: [] },
    });

    act(() => {
      hook.get().enqueueComposerMessage("thread-a", message("queue-1", "Edit me"));
    });
    await act(async () => {
      await hook.get().editQueuedMessage("queue-1");
    });

    expect(hook.restorePrimaryComposerDraft).not.toHaveBeenCalled();
    expect(hook.get().queuedMessageEditTarget).toBeUndefined();
    expect(hook.setStatus).toHaveBeenCalledWith(
      "先发送或清空当前输入，再编辑排队消息",
    );
  });

  it("rolls back a queued message removal when dequeue fails", async () => {
    installWuuStub({
      dequeueTurn: vi.fn().mockRejectedValue(new Error("network down")),
    });
    const hook = await renderComposerPendingState();

    act(() => {
      hook.get().enqueueComposerMessage("thread-a", message("queue-1", "First"));
      hook.get().enqueueComposerMessage("thread-a", message("queue-2", "Second"));
    });
    let removed = true;
    await act(async () => {
      removed = await hook.get().removeQueuedMessage("queue-1");
    });

    expect(removed).toBe(false);
    expect(
      hook.get().pendingComposerMessagesByThread["thread-a"]?.queued.map(
        (item) => item.id,
      ),
    ).toEqual(["queue-1", "queue-2"]);
    expect(hook.setStatus).toHaveBeenCalledWith("network down");
  });
});
