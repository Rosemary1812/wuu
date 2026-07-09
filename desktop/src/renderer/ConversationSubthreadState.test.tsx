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
    title: "Group",
    preview: "Group",
    cwd: "/tmp/wuu",
    workspace_kind: "group",
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
  threads = [groupThread()],
  activeThreadID = "group-1",
  draft = emptyComposerDraft(),
}: {
  threads?: Thread[];
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
  let currentThreads = threads;
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
      threads: currentThreads,
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
      createdBy: "lead-a",
    });
    expect(hook.onOpenSubthreadPanel).toHaveBeenCalledTimes(1);
    expect(hook.get().openSubthreadPanel).toEqual({
      threadID: "group-1",
      subthread: opened,
      loading: false,
    });
    expect(hook.get().chatSubthreadsNonce).toBe(1);
  });

  it("scopes task lead candidates to the open subthread participant subset", async () => {
    const hook = await renderConversationSubthreadState();

    act(() => {
      hook.get().setOpenSubthreadPanel({
        threadID: "group-1",
        subthread: subthread({ participants: ["lead-b"] }),
        loading: false,
      });
    });

    expect(hook.get().subthreadLeadCandidates.map((item) => item.id)).toEqual([
      "lead-b",
    ]);
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
});
