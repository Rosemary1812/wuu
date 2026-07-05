import { describe, expect, it } from "vitest";
import type {
  ConversationSubthread,
  SubthreadUpdatedNotification,
  Turn,
} from "../shared/protocol";
import {
  applySubthreadUpdatedNotification,
  type OpenSubthreadPanel,
} from "./AppState";

// W1 realtime-refresh: a cth (reply-subthread) message emits a thread/subUpdated
// notification (no turn/item/thread notification of its own). The open split
// reply panel must patch in place so agent replies stream, and the patch must be
// scoped to the subthread actually being shown.

function turn(id: string): Turn {
  return { id, items_view: "full", status: "completed", items: [] } as Turn;
}

function subthread(overrides: Partial<ConversationSubthread> = {}): ConversationSubthread {
  return {
    id: "cth-1",
    thread_id: "group-1",
    anchor_item_id: "seq-3",
    status: "open",
    created_at: "2026-01-01T00:00:00Z",
    reply_count: 2,
    turns: [turn("t1"), turn("t2")],
    ...overrides,
  };
}

function note(
  overrides: Partial<SubthreadUpdatedNotification> = {},
): SubthreadUpdatedNotification {
  return {
    thread_id: "group-1",
    subthread_id: "cth-1",
    subthread: subthread(),
    ...overrides,
  };
}

function openPanel(overrides: Partial<OpenSubthreadPanel> = {}): OpenSubthreadPanel {
  return {
    threadID: "group-1",
    subthread: subthread({ reply_count: 1, turns: [turn("t1")] }),
    loading: false,
    ...overrides,
  };
}

describe("applySubthreadUpdatedNotification", () => {
  it("patches the open panel with the refreshed view when the subthread matches", () => {
    const next = applySubthreadUpdatedNotification(openPanel(), note());
    expect(next?.subthread?.turns).toHaveLength(2);
    expect(next?.subthread?.reply_count).toBe(2);
    expect(next?.loading).toBe(false);
    expect(next?.error).toBeUndefined();
  });

  it("clears a prior error when the fresh view arrives", () => {
    const next = applySubthreadUpdatedNotification(
      openPanel({ error: "stale", loading: true }),
      note(),
    );
    expect(next?.error).toBeUndefined();
    expect(next?.loading).toBe(false);
  });

  it("leaves a closed panel untouched", () => {
    expect(applySubthreadUpdatedNotification(undefined, note())).toBeUndefined();
  });

  it("ignores updates for a different subthread than the one shown", () => {
    const panel = openPanel();
    const next = applySubthreadUpdatedNotification(
      panel,
      note({ subthread_id: "cth-OTHER", subthread: subthread({ id: "cth-OTHER" }) }),
    );
    expect(next).toBe(panel);
  });

  it("does not patch when the payload carried no turns (minimal fallback)", () => {
    const panel = openPanel();
    const next = applySubthreadUpdatedNotification(panel, {
      thread_id: "group-1",
      subthread_id: "cth-1",
      // Minimal error-fallback payload: no embedded view. Badge refresh via nonce
      // still fires separately in App.tsx; the panel keeps its current turns.
      subthread: undefined as unknown as ConversationSubthread,
    });
    expect(next).toBe(panel);
  });
});
