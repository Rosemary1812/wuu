import { act, createElement, type Dispatch, type SetStateAction } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import type {
  ConversationSubthread,
  Thread,
  ThreadItem,
  WuuDesktopApi,
} from "../shared/protocol";
import {
  emptyComposerDraft,
  type ComposerDraftState,
} from "./AppState";
import {
  useConversationSubthreadState,
  type ConversationSubthreadStateController,
} from "./ConversationSubthreadState";

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

function deferred<T>(): {
  promise: Promise<T>;
  resolve: (value: T) => void;
  reject: (reason: unknown) => void;
} {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((done, fail) => {
    resolve = done;
    reject = fail;
  });
  return { promise, resolve, reject };
}

function installWuuStub(overrides: Partial<WuuDesktopApi>): void {
  (window as unknown as { wuu: WuuDesktopApi }).wuu = {
    ...overrides,
  } as WuuDesktopApi;
}

function subthread(
  overrides: Partial<ConversationSubthread> = {},
): ConversationSubthread {
  return {
    id: "cth-1",
    thread_id: "group-1",
    anchor_item_id: "item-1",
    status: "open",
    created_at: "2026-01-01T00:00:00Z",
    reply_count: 1,
    turns: [],
    ...overrides,
  };
}

function groupThread(overrides: Partial<Thread> = {}): Thread {
  return {
    id: "group-1",
    group: true,
    title: "Group",
    preview: "Group",
    cwd: "/tmp/wuu",
    workspace_kind: "project",
    status: "idle",
    pinned: false,
    archived: false,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    turns: [],
    members: [
      { id: "lead-a", name: "Lead A", kind: "named" },
      { id: "lead-b", name: "Lead B", kind: "named" },
      { id: "human", name: "Human", kind: "human" },
    ],
    ...overrides,
  } as Thread;
}

function threadItem(overrides: Partial<ThreadItem> = {}): ThreadItem {
  return {
    id: "item-1",
    type: "agent_message",
    status: "completed",
    task: { name: "Investigate" },
    participant: { id: "lead-a", name: "Lead A", kind: "named" },
    ...overrides,
  } as ThreadItem;
}

async function renderConversationSubthreadState({
  activeThreadID = "group-1",
  draft = emptyComposerDraft(),
}: {
  activeThreadID?: string;
  draft?: ComposerDraftState;
} = {}): Promise<{
  get: () => ConversationSubthreadStateController;
  getDraft: () => ComposerDraftState;
  rerender: () => Promise<void>;
  setDraft: Dispatch<SetStateAction<ComposerDraftState>>;
  onOpenSubthreadPanel: ReturnType<typeof vi.fn>;
}> {
  let latest: ConversationSubthreadStateController | undefined;
  let currentActiveThreadID = activeThreadID;
  let currentDraft = draft;
  const onOpenSubthreadPanel = vi.fn();

  const setDraft: Dispatch<SetStateAction<ComposerDraftState>> = (update) => {
    currentDraft =
      typeof update === "function"
        ? update(currentDraft)
        : update;
  };

  function Probe(): null {
    latest = useConversationSubthreadState({
      activeThreadID: currentActiveThreadID,
      subthreadComposerDraft: currentDraft,
      setSubthreadComposerDraft: setDraft,
      onOpenSubthreadPanel,
    });
    return null;
  }

  const container = document.createElement("div");
  document.body.appendChild(container);
  const root = createRoot(container);
  mountedRoots.push(root);

  async function rerender(): Promise<void> {
    await act(async () => {
      root.render(createElement(Probe));
      await flushEffects();
    });
  }

  await rerender();

  return {
    get: () => {
      if (!latest) {
        throw new Error("conversation subthread state was not rendered");
      }
      return latest;
    },
    getDraft: () => currentDraft,
    rerender,
    setDraft,
    onOpenSubthreadPanel,
  };
}

describe("useConversationSubthreadState", () => {
  it("opens an anchored reply subthread and bumps the badge reload nonce", async () => {
    const opened = subthread();
    const openConversationSubthread = vi.fn().mockResolvedValue({
      subthread: opened,
    });
    installWuuStub({ openConversationSubthread });
    const hook = await renderConversationSubthreadState();

    await act(async () => {
      hook.get().openConversationSubthread(groupThread(), threadItem());
      await flushEffects();
    });

    expect(openConversationSubthread).toHaveBeenCalledWith("group-1", {
      subthreadId: undefined,
      anchorItemId: "item-1",
      title: "Investigate",
      threadOwnerParticipantId: "lead-a",
    });
    expect(hook.onOpenSubthreadPanel).toHaveBeenCalledTimes(1);
    expect(hook.get().openSubthreadPanel).toEqual({
      threadID: "group-1",
      subthread: opened,
      loading: false,
    });
    expect(hook.get().chatSubthreadsNonce).toBe(1);
  });

  it("does not open Thread from a DM", async () => {
    const openConversationSubthread = vi.fn();
    installWuuStub({ openConversationSubthread });
    const hook = await renderConversationSubthreadState();

    act(() => {
      hook.get().openConversationSubthread(
        groupThread({ group: false, workspace_kind: "dm" }),
        threadItem(),
      );
    });

    expect(openConversationSubthread).not.toHaveBeenCalled();
    expect(hook.onOpenSubthreadPanel).not.toHaveBeenCalled();
    expect(hook.get().openSubthreadPanel).toBeUndefined();
  });

  it("opens an existing ownerless legacy Thread by its durable id", async () => {
    const legacy = subthread({
      id: "cth-legacy",
      status: "resolved",
      thread_owner_participant_id: undefined,
    });
    const openConversationSubthread = vi.fn().mockResolvedValue({
      subthread: legacy,
    });
    installWuuStub({ openConversationSubthread });
    const hook = await renderConversationSubthreadState();

    await act(async () => {
      hook.get().openConversationSubthread(
        groupThread(),
        threadItem({ task: undefined, participant: undefined }),
        undefined,
        legacy.id,
      );
      await flushEffects();
    });

    expect(openConversationSubthread).toHaveBeenCalledWith("group-1", {
      subthreadId: legacy.id,
      anchorItemId: undefined,
      title: undefined,
      threadOwnerParticipantId: undefined,
    });
    expect(hook.get().openSubthreadPanel?.subthread?.id).toBe(legacy.id);
  });

  it("upgrades the same Thread without accepting a lead override", async () => {
    const escalated = subthread({ status: "task", thread_owner_participant_id: "lead-a" });
    const escalateConversationSubthread = vi.fn().mockResolvedValue({
      subthread: escalated,
    });
    installWuuStub({ escalateConversationSubthread });
    const hook = await renderConversationSubthreadState();
    act(() => {
      hook.get().setOpenSubthreadPanel({
        threadID: "group-1",
        subthread: subthread({
          title: "Investigate",
          thread_owner_participant_id: "lead-a",
        }),
        loading: false,
      });
    });

    await act(async () => {
      hook.get().escalateOpenConversationSubthread();
      await flushEffects();
    });

    expect(escalateConversationSubthread).toHaveBeenCalledWith(
      "group-1",
      "cth-1",
      { title: "Investigate" },
    );
    expect(hook.get().openSubthreadPanel?.subthread).toBe(escalated);
  });

  it("keeps the second Thread open when the first request resolves last", async () => {
    const first = deferred<{ subthread: ConversationSubthread }>();
    const second = deferred<{ subthread: ConversationSubthread }>();
    const openConversationSubthread = vi
      .fn()
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise);
    installWuuStub({ openConversationSubthread });
    const hook = await renderConversationSubthreadState();

    act(() => {
      hook.get().openConversationSubthreadByID("group-1", "cth-a");
      hook.get().openConversationSubthreadByID("group-1", "cth-b");
    });
    await act(async () => {
      second.resolve({ subthread: subthread({ id: "cth-b" }) });
      await flushEffects();
    });
    await act(async () => {
      first.resolve({ subthread: subthread({ id: "cth-a" }) });
      await flushEffects();
    });

    expect(hook.get().openSubthreadPanel?.subthread?.id).toBe("cth-b");
  });

  it("ignores a resolve result after the user opens another Thread", async () => {
    const resolving = deferred<{ subthread: ConversationSubthread }>();
    installWuuStub({
      resolveConversationSubthread: vi.fn().mockReturnValue(resolving.promise),
      openConversationSubthread: vi.fn().mockResolvedValue({
        subthread: subthread({ id: "cth-b" }),
      }),
    });
    const hook = await renderConversationSubthreadState();
    act(() => {
      hook.get().setOpenSubthreadPanel({
        threadID: "group-1",
        subthread: subthread({ id: "cth-a" }),
        loading: false,
      });
    });
    act(() => {
      hook.get().resolveOpenConversationSubthread(true);
      hook.get().openConversationSubthreadByID("group-1", "cth-b");
    });
    await act(async () => {
      await flushEffects();
      resolving.resolve({ subthread: subthread({ id: "cth-a", status: "resolved" }) });
      await flushEffects();
    });

    expect(hook.get().openSubthreadPanel?.subthread?.id).toBe("cth-b");
    expect(hook.get().openSubthreadPanel?.subthread?.status).toBe("open");
  });

  it("ignores a promotion result after the user opens another Thread", async () => {
    const escalating = deferred<{ subthread: ConversationSubthread }>();
    installWuuStub({
      escalateConversationSubthread: vi.fn().mockReturnValue(escalating.promise),
      openConversationSubthread: vi.fn().mockResolvedValue({
        subthread: subthread({ id: "cth-b" }),
      }),
    });
    const hook = await renderConversationSubthreadState();
    act(() => {
      hook.get().setOpenSubthreadPanel({
        threadID: "group-1",
        subthread: subthread({ id: "cth-a", title: "A" }),
        loading: false,
      });
    });
    act(() => {
      hook.get().escalateOpenConversationSubthread();
      hook.get().openConversationSubthreadByID("group-1", "cth-b");
    });
    await act(async () => {
      await flushEffects();
      escalating.resolve({ subthread: subthread({ id: "cth-a", status: "task" }) });
      await flushEffects();
    });

    expect(hook.get().openSubthreadPanel?.subthread?.id).toBe("cth-b");
    expect(hook.get().openSubthreadPanel?.subthread?.status).toBe("open");
  });

  it("restores the reply composer draft when sending fails", async () => {
    installWuuStub({
      postSubthreadMessage: vi.fn().mockRejectedValue(new Error("offline")),
    });
    const originalDraft: ComposerDraftState = {
      prompt: "Need one more detail",
      images: [],
      files: [],
    };
    const hook = await renderConversationSubthreadState();
    act(() => {
      hook.get().setOpenSubthreadPanel({
        threadID: "group-1",
        subthread: subthread(),
        loading: false,
      });
    });
    await flushEffects();
    hook.setDraft(originalDraft);
    await hook.rerender();

    await act(async () => {
      hook.get().sendOpenConversationSubthreadMessage();
      await flushEffects();
    });

    expect(hook.get().openSubthreadPanel?.error).toBe("offline");
    expect(hook.getDraft()).toEqual(originalDraft);
    expect(
      (window.wuu.postSubthreadMessage as ReturnType<typeof vi.fn>).mock
        .calls[0],
    ).toEqual(["group-1", "cth-1", "Need one more detail", [], []]);
  });

  it("does not restore an old draft or error into a newly opened Thread", async () => {
    const sending = deferred<{ subthread: ConversationSubthread }>();
    installWuuStub({
      postSubthreadMessage: vi.fn().mockReturnValue(sending.promise),
    });
    const hook = await renderConversationSubthreadState();
    act(() => {
      hook.get().setOpenSubthreadPanel({
        threadID: "group-1",
        subthread: subthread({ id: "cth-a" }),
        loading: false,
      });
    });
    await flushEffects();
    hook.setDraft({ prompt: "draft A", images: [], files: [] });
    await hook.rerender();
    act(() => {
      hook.get().sendOpenConversationSubthreadMessage();
      hook.get().setOpenSubthreadPanel({
        threadID: "group-1",
        subthread: subthread({ id: "cth-b" }),
        loading: false,
      });
    });
    hook.setDraft({ prompt: "draft B", images: [], files: [] });
    await hook.rerender();

    await act(async () => {
      sending.reject(new Error("A failed"));
      await flushEffects();
    });

    expect(hook.get().openSubthreadPanel?.subthread?.id).toBe("cth-b");
    expect(hook.get().openSubthreadPanel?.error).toBeUndefined();
    expect(hook.getDraft().prompt).toBe("draft B");
  });
});
