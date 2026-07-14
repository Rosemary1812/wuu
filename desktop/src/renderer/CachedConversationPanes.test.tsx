import { act, createElement, type ComponentProps } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Thread } from "../shared/protocol";
import { CachedConversationPanes } from "./CachedConversationPanes";
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
  onOpenSubthread = vi.fn(),
): { container: HTMLElement; onOpenSubthread: ReturnType<typeof vi.fn> } {
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
    onOpenSubthread,
    onReact: () => {},
    onEditMessage: () => {},
    onCancelEditMessage: () => {},
    onSubmitEditMessage: () => {},
    onOpenFileDiff: () => {},
    turnStreamStatus: {},
    busyParticipantIDs: new Set(),
    activeThreadMarks: [],
    resolveParticipantName: (id) => id,
    chatReaderCount: 0,
    pendingChatMessagesByThread: {},
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
  return { container, onOpenSubthread };
}

describe("CachedConversationPanes Thread wiring", () => {
  it("does not expose Thread entry points in a DM", () => {
    const { container, onOpenSubthread } = renderPane(
      chatThread("dm-1", {
        workspace_kind: "dm",
        dm_participant_id: "prt-a",
      }),
    );

    expect(container.querySelector(".chat-bubble-toolbar-reply")).toBeNull();
    expect(container.querySelector(".chat-reply-badge")).toBeNull();
    expect(onOpenSubthread).not.toHaveBeenCalled();
  });

  it("wires the current group's named members as human Thread owner choices", () => {
    const group = chatThread("group-1", {
      group: true,
      members: [{ id: "prt-a", name: "Ada", kind: "named" }],
    });
    const { container, onOpenSubthread } = renderPane(group);

    act(() => {
      container
        .querySelector<HTMLButtonElement>(".chat-bubble-toolbar-reply")!
        .click();
    });

    expect(onOpenSubthread).toHaveBeenCalledWith(
      group,
      expect.objectContaining({ id: "human-1" }),
      "prt-a",
      undefined,
    );
  });
});

describe("CachedConversationPanes session switching", () => {
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
      onOpenSubthread: () => {},
      onReact: () => {},
      onEditMessage: () => {},
      onCancelEditMessage: () => {},
      onSubmitEditMessage: () => {},
      onOpenFileDiff: () => {},
      turnStreamStatus: {},
      busyParticipantIDs: new Set<string>(),
      activeThreadMarks: [],
      resolveParticipantName: (id: string) => id,
      chatReaderCount: 0,
      pendingChatMessagesByThread: {},
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
