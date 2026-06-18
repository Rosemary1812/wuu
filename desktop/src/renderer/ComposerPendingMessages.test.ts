import { describe, expect, it } from "vitest";
import type { QueuedComposerMessage } from "./ComposerMessages";
import {
  findPendingComposerMessage,
  pendingComposerMessageCount,
  pendingComposerMessagesForThread,
  type PendingComposerMessagesByThread,
} from "./ComposerPendingMessages";

function message(id: string, text: string): QueuedComposerMessage {
  return { id, text, images: [], files: [] };
}

describe("composer pending messages", () => {
  it("returns only the pending messages for the requested thread", () => {
    const byThread: PendingComposerMessagesByThread = {
      "thread-a": {
        queued: [message("queue-a", "A queued")],
        guides: [message("guide-a", "A guide")],
      },
      "thread-b": {
        queued: [message("queue-b", "B queued")],
        guides: [],
      },
    };

    const threadA = pendingComposerMessagesForThread(byThread, "thread-a");
    const threadB = pendingComposerMessagesForThread(byThread, "thread-b");
    const missing = pendingComposerMessagesForThread(byThread, "missing");

    expect(threadA.queued.map((item) => item.id)).toEqual(["queue-a"]);
    expect(threadA.guides.map((item) => item.id)).toEqual(["guide-a"]);
    expect(threadB.queued.map((item) => item.id)).toEqual(["queue-b"]);
    expect(pendingComposerMessageCount(missing)).toBe(0);
  });

  it("finds the original thread for a pending message before mutating it", () => {
    const byThread: PendingComposerMessagesByThread = {
      "thread-a": {
        queued: [message("shared-id", "A queued")],
        guides: [],
      },
      "thread-b": {
        queued: [message("other-id", "B queued")],
        guides: [message("guide-b", "B guide")],
      },
    };

    expect(
      findPendingComposerMessage(byThread, "shared-id", "queue", "thread-b"),
    ).toMatchObject({
      threadID: "thread-a",
      index: 0,
      message: { id: "shared-id" },
    });
    expect(
      findPendingComposerMessage(byThread, "guide-b", "guide", "thread-b"),
    ).toMatchObject({
      threadID: "thread-b",
      index: 0,
      message: { id: "guide-b" },
    });
  });
});
