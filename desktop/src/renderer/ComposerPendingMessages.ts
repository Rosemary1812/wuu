import type { QueuedComposerMessage } from "./ComposerMessages";

export type ThreadPendingComposerMessages = {
  queued: QueuedComposerMessage[];
  guides: QueuedComposerMessage[];
};

export type PendingComposerMessagesByThread = Record<
  string,
  ThreadPendingComposerMessages
>;

export type LocatedPendingComposerMessage = {
  threadID: string;
  index: number;
  message: QueuedComposerMessage;
};

export type PendingComposerMessageRemovalScope = "queue" | "guide" | "all";

export function emptyThreadPendingComposerMessages(): ThreadPendingComposerMessages {
  return { queued: [], guides: [] };
}

export function pendingComposerMessagesForThread(
  messagesByThread: PendingComposerMessagesByThread,
  threadID: string | undefined,
): ThreadPendingComposerMessages {
  if (!threadID) {
    return emptyThreadPendingComposerMessages();
  }
  return messagesByThread[threadID] ?? emptyThreadPendingComposerMessages();
}

export function threadPendingComposerMessagesIsEmpty(
  pending: ThreadPendingComposerMessages,
): boolean {
  return pending.queued.length === 0 && pending.guides.length === 0;
}

export function removePendingComposerMessagesByID(
  messagesByThread: PendingComposerMessagesByThread,
  threadID: string | undefined,
  id: string,
  scope: PendingComposerMessageRemovalScope = "all",
): PendingComposerMessagesByThread {
  if (!threadID || !id) {
    return messagesByThread;
  }
  const previous = messagesByThread[threadID];
  if (!previous) {
    return messagesByThread;
  }
  const next: ThreadPendingComposerMessages = {
    queued:
      scope === "guide"
        ? previous.queued
        : previous.queued.filter((message) => message.id !== id),
    guides:
      scope === "queue"
        ? previous.guides
        : previous.guides.filter((message) => message.id !== id),
  };
  if (
    next.queued.length === previous.queued.length &&
    next.guides.length === previous.guides.length
  ) {
    return messagesByThread;
  }
  const nextByThread = { ...messagesByThread };
  if (threadPendingComposerMessagesIsEmpty(next)) {
    delete nextByThread[threadID];
  } else {
    nextByThread[threadID] = next;
  }
  return nextByThread;
}

export function findPendingComposerMessage(
  messagesByThread: PendingComposerMessagesByThread,
  id: string,
  kind: "queue" | "guide",
  preferredThreadID?: string,
): LocatedPendingComposerMessage | undefined {
  const field = kind === "queue" ? "queued" : "guides";
  const threadIDs = [
    ...(preferredThreadID ? [preferredThreadID] : []),
    ...Object.keys(messagesByThread).filter(
      (threadID) => threadID !== preferredThreadID,
    ),
  ];
  for (const threadID of threadIDs) {
    const messages = messagesByThread[threadID]?.[field] ?? [];
    const index = messages.findIndex((message) => message.id === id);
    if (index >= 0) {
      const message = messages[index];
      return { threadID, index, message };
    }
  }
  return undefined;
}

export function pendingComposerMessageCount(
  pending: ThreadPendingComposerMessages,
): number {
  return pending.queued.length + pending.guides.length;
}
