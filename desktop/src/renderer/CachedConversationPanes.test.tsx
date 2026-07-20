import { act, createElement, type ComponentProps } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Thread } from "../shared/protocol";
import type { TurnStreamStatus } from "./AppState";
import { CachedConversationPanes } from "./CachedConversationPanes";
import { OPTIMISTIC_TURN_ID_PREFIX } from "./ComposerMessages";
import { ImagePreviewProvider } from "./ImagePreview";

const turnListRenders = vi.hoisted(() => new Map<string, number>());

vi.mock("./ConversationTurnList", () => ({
  ConversationTurnList: ({ threadID }: { threadID: string }): JSX.Element => {
    turnListRenders.set(threadID, (turnListRenders.get(threadID) ?? 0) + 1);
    return <div data-testid={`turn-list-${threadID}`} />;
  },
}));

let roots: Root[] = [];

afterEach(() => {
  for (const root of roots) {
    act(() => root.unmount());
  }
  vi.restoreAllMocks();
  roots = [];
  turnListRenders.clear();
  document.body.innerHTML = "";
});

function chatThread(
  id: string,
  overrides: Partial<Thread>,
): Thread {
  return {
    id,
    preview: id,
    title: id,
    model_provider: "test",
    model: "test",
    cwd: "/tmp/wuu",
    status: "idle",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    turns: [
      {
        id: "turn-1",
        status: "completed",
        items_view: "full",
        items: [
          {
            id: "human-1",
            type: "user_message",
            text: "把这个提案收敛一下",
          },
        ],
      },
    ],
    ...overrides,
  } as Thread;
}

function renderPane(
  thread: Thread,
  turnStreamStatus: Record<string, TurnStreamStatus> = {},
): { container: HTMLElement } {
  const container = document.createElement("div");
  document.body.appendChild(container);
  const root = createRoot(container);
  roots.push(root);
  const props: ComponentProps<typeof CachedConversationPanes> = {
    threadIDs: [thread.id],
    threadsByID: new Map([[thread.id, thread]]),
    activeThreadID: thread.id,
    conversationGridVisible: false,
    contextCompositionEntries: [],
    instructionFilesEntries: [],
    onStreamFrame: () => {},
    onCollapseComplete: () => {},
    onDismissContextComposition: () => {},
    onDismissInstructions: () => {},
    canEditThreadMessage: () => false,
    onForkMessage: () => {},
    onOpenAgent: () => {},
    onEditMessage: () => {},
    onCancelEditMessage: () => {},
    onSubmitEditMessage: () => {},
    onOpenFileDiff: () => {},
    turnStreamStatus,
  };
  act(() => {
    root.render(
      createElement(
        ImagePreviewProvider,
        null,
        createElement(CachedConversationPanes, props),
      ),
    );
  });
  return { container };
}

describe("CachedConversationPanes session switching", () => {
  it("keeps an optimistic first query visible during the new-thread handoff", () => {
    const thread = chatThread("thread-a", {
      turns: [
        {
          id: `${OPTIMISTIC_TURN_ID_PREFIX}1`,
          status: "in_progress",
          items_view: "full",
          items: [],
        },
      ],
    });

    const { container } = renderPane(thread);
    const pane = container.querySelector<HTMLElement>(".cached-conversation-pane");

    expect(pane?.hasAttribute("data-layout-settled")).toBe(true);
  });

  it("waits until the mounted message DOM height is stable before entering", () => {
    const frameCallbacks: FrameRequestCallback[] = [];
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
      frameCallbacks.push(callback);
      return frameCallbacks.length;
    });
    vi.spyOn(window, "cancelAnimationFrame").mockImplementation(() => {});
    let paneHeight = 100;
    vi.spyOn(HTMLElement.prototype, "scrollHeight", "get").mockImplementation(
      function scrollHeight(this: HTMLElement): number {
        return this.classList.contains("cached-conversation-pane") ? paneHeight : 0;
      },
    );

    const { container } = renderPane(chatThread("thread-a", {}));
    const pane = container.querySelector<HTMLElement>(".cached-conversation-pane");

    expect(pane?.hasAttribute("data-layout-settled")).toBe(false);
    expect(frameCallbacks).toHaveLength(1);

    act(() => {
      frameCallbacks.shift()?.(0);
    });
    expect(pane?.hasAttribute("data-layout-settled")).toBe(false);
    expect(frameCallbacks).toHaveLength(1);

    paneHeight = 240;
    act(() => {
      frameCallbacks.shift()?.(16);
    });
    expect(pane?.hasAttribute("data-layout-settled")).toBe(false);
    expect(frameCallbacks).toHaveLength(1);

    act(() => {
      frameCallbacks.shift()?.(32);
    });
    expect(pane?.hasAttribute("data-layout-settled")).toBe(false);
    expect(frameCallbacks).toHaveLength(1);

    act(() => {
      frameCallbacks.shift()?.(48);
    });
    expect(pane?.hasAttribute("data-layout-settled")).toBe(true);
  });

  it("reuses a settled hidden pane until its layout content changes", () => {
    const frameCallbacks: FrameRequestCallback[] = [];
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
      frameCallbacks.push(callback);
      return frameCallbacks.length;
    });
    vi.spyOn(window, "cancelAnimationFrame").mockImplementation(() => {});

    const threadA = chatThread("thread-a", {});
    const threadB = chatThread("thread-b", {});
    let threadsByID = new Map([
      [threadA.id, threadA],
      [threadB.id, threadB],
    ]);
    let turnStreamStatus: Record<string, TurnStreamStatus> = {};
    let contextCompositionEntries: ComponentProps<
      typeof CachedConversationPanes
    >["contextCompositionEntries"] = [];
    let instructionFilesEntries: ComponentProps<
      typeof CachedConversationPanes
    >["instructionFilesEntries"] = [];
    const container = document.createElement("div");
    document.body.appendChild(container);
    const root = createRoot(container);
    roots.push(root);
    const stableProps = {
      threadIDs: [threadA.id, threadB.id],
      conversationGridVisible: false,
      contextCompositionEntries: [],
      instructionFilesEntries: [],
      onStreamFrame: () => {},
      onCollapseComplete: () => {},
      onDismissContextComposition: () => {},
      onDismissInstructions: () => {},
      canEditThreadMessage: () => false,
      onForkMessage: () => {},
      onOpenAgent: () => {},
      onEditMessage: () => {},
      onCancelEditMessage: () => {},
      onSubmitEditMessage: () => {},
      onOpenFileDiff: () => {},
      turnStreamStatus: {},
    } satisfies Omit<
      ComponentProps<typeof CachedConversationPanes>,
      "activeThreadID" | "threadsByID"
    >;
    const renderActiveThread = (activeThreadID: string): void => {
      act(() => {
        root.render(
          <ImagePreviewProvider>
            <CachedConversationPanes
              {...stableProps}
              activeThreadID={activeThreadID}
              contextCompositionEntries={contextCompositionEntries}
              instructionFilesEntries={instructionFilesEntries}
              threadsByID={threadsByID}
              turnStreamStatus={turnStreamStatus}
            />
          </ImagePreviewProvider>,
        );
      });
    };
    const runTwoStableFrames = (): void => {
      act(() => {
        frameCallbacks.shift()?.(0);
      });
      act(() => {
        frameCallbacks.shift()?.(16);
      });
    };

    renderActiveThread(threadA.id);
    runTwoStableFrames();
    renderActiveThread(threadB.id);
    runTwoStableFrames();

    const paneA = container.querySelector<HTMLElement>(
      '[data-thread-id="thread-a"]',
    );
    const paneB = container.querySelector<HTMLElement>(
      '[data-thread-id="thread-b"]',
    );
    expect(paneA?.hasAttribute("data-layout-settled")).toBe(true);
    expect(paneA?.hasAttribute("inert")).toBe(true);
    expect(paneA?.getAttribute("aria-hidden")).toBe("true");
    expect(paneA?.style.display).toBe("");

    renderActiveThread(threadA.id);
    expect(frameCallbacks).toHaveLength(0);
    expect(paneA?.getAttribute("data-active")).toBe("true");
    expect(paneA?.hasAttribute("inert")).toBe(false);

    const changedThreadB = {
      ...threadB,
      turns: [
        ...threadB.turns,
        {
          id: "turn-2",
          status: "completed" as const,
          items_view: "full" as const,
          items: [
            {
              id: "assistant-2",
              type: "agent_message" as const,
              text: "后台新增的消息",
            },
          ],
        },
      ],
    };
    threadsByID = new Map([
      [threadA.id, threadA],
      [changedThreadB.id, changedThreadB],
    ]);
    renderActiveThread(threadA.id);
    expect(paneB?.hasAttribute("data-layout-settled")).toBe(false);

    renderActiveThread(threadB.id);
    expect(frameCallbacks).toHaveLength(1);
    expect(paneB?.getAttribute("data-active")).toBe("true");
    const settleThreadBAndHide = (): void => {
      runTwoStableFrames();
      renderActiveThread(threadA.id);
      expect(frameCallbacks).toHaveLength(0);
    };

    settleThreadBAndHide();
    turnStreamStatus = {
      "turn-2": { text: "消息流暂时中断", liveProgress: true },
    };
    renderActiveThread(threadA.id);
    expect(paneB?.hasAttribute("data-layout-settled")).toBe(false);

    renderActiveThread(threadB.id);
    settleThreadBAndHide();
    turnStreamStatus = {
      "turn-2": { text: "约 2 秒后继续", liveProgress: true },
    };
    renderActiveThread(threadA.id);
    expect(paneB?.hasAttribute("data-layout-settled")).toBe(false);

    renderActiveThread(threadB.id);
    settleThreadBAndHide();
    turnStreamStatus = {};
    renderActiveThread(threadA.id);
    expect(paneB?.hasAttribute("data-layout-settled")).toBe(false);

    renderActiveThread(threadB.id);
    settleThreadBAndHide();
    contextCompositionEntries = [
      { id: "context-b", threadID: threadB.id, loading: true },
    ];
    renderActiveThread(threadA.id);
    expect(paneB?.hasAttribute("data-layout-settled")).toBe(false);

    renderActiveThread(threadB.id);
    settleThreadBAndHide();
    instructionFilesEntries = [
      { id: "instructions-b", threadID: threadB.id, loading: true },
    ];
    renderActiveThread(threadA.id);
    expect(paneB?.hasAttribute("data-layout-settled")).toBe(false);

    renderActiveThread(threadB.id);
    settleThreadBAndHide();
    const forkedThreadB = {
      ...changedThreadB,
      forked_from_id: "thread-source",
      worktree: { path: "/tmp/wuu/thread-b" },
    };
    threadsByID = new Map([
      [threadA.id, threadA],
      [forkedThreadB.id, forkedThreadB],
    ]);
    renderActiveThread(threadA.id);
    expect(paneB?.hasAttribute("data-layout-settled")).toBe(false);
  });

  it("does not re-render an unrelated hidden conversation", () => {
    const threads = [
      chatThread("thread-a", {}),
      chatThread("thread-b", {}),
      chatThread("thread-c", {}),
    ];
    const threadsByID = new Map(threads.map((thread) => [thread.id, thread]));
    const container = document.createElement("div");
    document.body.appendChild(container);
    const root = createRoot(container);
    roots.push(root);
    const stableProps = {
      threadIDs: threads.map((thread) => thread.id),
      threadsByID,
      conversationGridVisible: false,
      contextCompositionEntries: [],
      instructionFilesEntries: [],
      onStreamFrame: () => {},
      onCollapseComplete: () => {},
      onDismissContextComposition: () => {},
      onDismissInstructions: () => {},
      canEditThreadMessage: () => false,
      onForkMessage: () => {},
      onOpenAgent: () => {},
      onEditMessage: () => {},
      onCancelEditMessage: () => {},
      onSubmitEditMessage: () => {},
      onOpenFileDiff: () => {},
      turnStreamStatus: {},
    } satisfies Omit<
      ComponentProps<typeof CachedConversationPanes>,
      "activeThreadID"
    >;

    act(() => {
      root.render(
        <ImagePreviewProvider>
          <CachedConversationPanes {...stableProps} activeThreadID="thread-a" />
        </ImagePreviewProvider>,
      );
    });

    expect(turnListRenders.get("thread-a")).toBe(1);
    expect(turnListRenders.get("thread-b")).toBe(1);
    expect(turnListRenders.get("thread-c")).toBe(1);

    act(() => {
      root.render(
        <ImagePreviewProvider>
          <CachedConversationPanes {...stableProps} activeThreadID="thread-b" />
        </ImagePreviewProvider>,
      );
    });

    expect(turnListRenders.get("thread-a")).toBe(2);
    expect(turnListRenders.get("thread-b")).toBe(2);
    expect(turnListRenders.get("thread-c")).toBe(1);
  });
});
