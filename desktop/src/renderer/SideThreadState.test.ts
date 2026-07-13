import { describe, expect, it } from "vitest";
import type {
  SideThreadMessage,
  SideThreadSummary
} from "../shared/protocol";
import {
  SIDE_THREAD_DEFAULT_WIDTH,
  SIDE_THREAD_MAX_WIDTH,
  SIDE_THREAD_MIN_WIDTH,
  clampSideThreadWidth,
  createEmptySideThreadEntry,
  createInitialSideThreadStore,
  ensureSideThreadEntry,
  getSideThreadEntry,
  reduceSideThreadStore
} from "./SideThreadState";

function summary(overrides: Partial<SideThreadSummary> = {}): SideThreadSummary {
  return {
    side_thread_id: "side-1",
    main_thread_id: "main-1",
    status: "idle",
    created_at: "2026-01-01T00:00:00.000Z",
    updated_at: "2026-01-01T00:00:00.000Z",
    ...overrides
  };
}

function message(overrides: Partial<SideThreadMessage> = {}): SideThreadMessage {
  return {
    id: "m-1",
    side_thread_id: "side-1",
    role: "user",
    text: "hello",
    created_at: "2026-01-01T00:00:00.000Z",
    ...overrides
  };
}

describe("SideThreadState", () => {
  describe("createInitialSideThreadStore", () => {
    it("creates an empty store with default width", () => {
      const store = createInitialSideThreadStore();
      expect(store.byThread).toEqual({});
      expect(store.sideIdToMain).toEqual({});
      expect(store.width).toBe(SIDE_THREAD_DEFAULT_WIDTH);
    });

    it("clamps out-of-range width values to the allowed band", () => {
      const tooSmall = createInitialSideThreadStore(100);
      const tooBig = createInitialSideThreadStore(10_000);
      expect(tooSmall.width).toBe(SIDE_THREAD_MIN_WIDTH);
      expect(tooBig.width).toBe(SIDE_THREAD_MAX_WIDTH);
    });

    it("treats non-finite width as default", () => {
      expect(createInitialSideThreadStore(NaN).width).toBe(SIDE_THREAD_DEFAULT_WIDTH);
      expect(createInitialSideThreadStore(Infinity).width).toBe(SIDE_THREAD_DEFAULT_WIDTH);
    });
  });

  describe("clampSideThreadWidth", () => {
    it("passes through values inside the band", () => {
      expect(clampSideThreadWidth(450)).toBe(450);
    });
    it("clamps below the minimum", () => {
      expect(clampSideThreadWidth(200)).toBe(SIDE_THREAD_MIN_WIDTH);
    });
    it("clamps above the maximum", () => {
      expect(clampSideThreadWidth(900)).toBe(SIDE_THREAD_MAX_WIDTH);
    });
    it("rounds fractional values", () => {
      expect(clampSideThreadWidth(421.6)).toBe(422);
    });
  });

  describe("ensureSideThreadEntry / getSideThreadEntry", () => {
    it("returns undefined for an unseen main thread", () => {
      const store = createInitialSideThreadStore();
      expect(getSideThreadEntry(store, "main-x")).toBeUndefined();
    });

    it("creates an empty entry on demand without mutating the original store", () => {
      const store = createInitialSideThreadStore();
      const { store: nextStore, entry } = ensureSideThreadEntry(store, "main-x");
      expect(entry).toEqual(createEmptySideThreadEntry());
      expect(nextStore).not.toBe(store);
      expect(getSideThreadEntry(store, "main-x")).toBeUndefined();
      expect(getSideThreadEntry(nextStore, "main-x")).toEqual(entry);
    });
  });

  describe("open / close / toggle", () => {
    it("open creates the entry if missing and marks it open", () => {
      const store = createInitialSideThreadStore();
      const next = reduceSideThreadStore(store, {
        type: "open",
        mainThreadId: "main-1"
      });
      expect(next.byThread["main-1"]?.open).toBe(true);
    });

    it("close keeps the entry but marks it closed", () => {
      let store = createInitialSideThreadStore();
      store = reduceSideThreadStore(store, { type: "open", mainThreadId: "main-1" });
      const next = reduceSideThreadStore(store, {
        type: "close",
        mainThreadId: "main-1"
      });
      expect(next.byThread["main-1"]?.open).toBe(false);
      // closing preserves identity, history is not cleared
      expect(next.byThread["main-1"]).toBeDefined();
    });

    it("toggle flips the open flag both ways", () => {
      let store = createInitialSideThreadStore();
      store = reduceSideThreadStore(store, {
        type: "toggle",
        mainThreadId: "main-1"
      });
      expect(store.byThread["main-1"]?.open).toBe(true);
      store = reduceSideThreadStore(store, {
        type: "toggle",
        mainThreadId: "main-1"
      });
      expect(store.byThread["main-1"]?.open).toBe(false);
    });
  });

  describe("setDraft", () => {
    it("updates draft text for the targeted main thread", () => {
      const store = createInitialSideThreadStore();
      const next = reduceSideThreadStore(store, {
        type: "setDraft",
        mainThreadId: "main-1",
        draft: "现在做到哪了？"
      });
      expect(next.byThread["main-1"]?.draft).toBe("现在做到哪了？");
    });
  });

  describe("setSummary", () => {
    it("stores summary and populates sideIdToMain reverse index", () => {
      const store = createInitialSideThreadStore();
      const next = reduceSideThreadStore(store, {
        type: "setSummary",
        mainThreadId: "main-1",
        summary: summary()
      });
      expect(next.byThread["main-1"]?.summary).toEqual(summary());
      expect(next.sideIdToMain["side-1"]).toBe("main-1");
    });

    it("clears summary while keeping the reverse index intact when null", () => {
      let store = createInitialSideThreadStore();
      store = reduceSideThreadStore(store, {
        type: "setSummary",
        mainThreadId: "main-1",
        summary: summary()
      });
      const next = reduceSideThreadStore(store, {
        type: "setSummary",
        mainThreadId: "main-1",
        summary: null
      });
      expect(next.byThread["main-1"]?.summary).toBeNull();
      // null summary keeps the index so previously streamed events
      // can still be routed back to this main thread.
      expect(next.sideIdToMain["side-1"]).toBe("main-1");
    });
  });

  describe("messages", () => {
    it("appendMessage adds to the end without mutating prior messages", () => {
      let store = createInitialSideThreadStore();
      store = reduceSideThreadStore(store, {
        type: "appendMessage",
        mainThreadId: "main-1",
        message: message({ id: "m-1", text: "first" })
      });
      store = reduceSideThreadStore(store, {
        type: "appendMessage",
        mainThreadId: "main-1",
        message: message({ id: "m-2", text: "second", role: "assistant" })
      });
      expect(store.byThread["main-1"]?.messages.map((m) => m.id)).toEqual([
        "m-1",
        "m-2"
      ]);
    });

    it("updateMessage applies a patch to a single message", () => {
      let store = createInitialSideThreadStore();
      store = reduceSideThreadStore(store, {
        type: "appendMessage",
        mainThreadId: "main-1",
        message: message({ id: "m-1", text: "draft" })
      });
      const next = reduceSideThreadStore(store, {
        type: "updateMessage",
        mainThreadId: "main-1",
        messageId: "m-1",
        patch: { status: "completed" }
      });
      expect(next.byThread["main-1"]?.messages[0]?.status).toBe("completed");
      expect(next.byThread["main-1"]?.messages[0]?.text).toBe("draft");
    });
  });

  describe("setStreaming / setError", () => {
    it("setStreaming toggles the streaming flag", () => {
      let store = createInitialSideThreadStore();
      store = reduceSideThreadStore(store, {
        type: "setStreaming",
        mainThreadId: "main-1",
        streaming: true
      });
      expect(store.byThread["main-1"]?.streaming).toBe(true);
    });

    it("setError records an error and clears with undefined", () => {
      let store = createInitialSideThreadStore();
      store = reduceSideThreadStore(store, {
        type: "setError",
        mainThreadId: "main-1",
        error: "boom"
      });
      expect(store.byThread["main-1"]?.lastError).toBe("boom");
      store = reduceSideThreadStore(store, {
        type: "setError",
        mainThreadId: "main-1",
        error: undefined
      });
      expect(store.byThread["main-1"]?.lastError).toBeUndefined();
    });
  });

  describe("applyEvent", () => {
    it("status event updates streaming flag and summary status", () => {
      let store = createInitialSideThreadStore();
      store = reduceSideThreadStore(store, {
        type: "setSummary",
        mainThreadId: "main-1",
        summary: summary()
      });
      const next = reduceSideThreadStore(store, {
        type: "applyEvent",
        event: {
          type: "status",
          side_thread_id: "side-1",
          main_thread_id: "main-1",
          status: "running"
        }
      });
      expect(next.byThread["main-1"]?.streaming).toBe(true);
      expect(next.byThread["main-1"]?.summary?.status).toBe("running");
      expect(next.byThread["main-1"]?.lastError).toBeUndefined();
    });

    it("delta event appends to an unknown message id and streams", () => {
      const store = createInitialSideThreadStore();
      const next = reduceSideThreadStore(store, {
        type: "applyEvent",
        event: {
          type: "delta",
          side_thread_id: "side-1",
          main_thread_id: "main-1",
          message_id: "m-a",
          text_delta: "你好"
        }
      });
      const messages = next.byThread["main-1"]?.messages ?? [];
      expect(messages).toHaveLength(1);
      expect(messages[0]).toMatchObject({
        id: "m-a",
        role: "assistant",
        text: "你好",
        status: "streaming"
      });
      expect(next.byThread["main-1"]?.streaming).toBe(true);
    });

    it("delta event concatenates into the existing message", () => {
      let store = createInitialSideThreadStore();
      store = reduceSideThreadStore(store, {
        type: "applyEvent",
        event: {
          type: "delta",
          side_thread_id: "side-1",
          main_thread_id: "main-1",
          message_id: "m-a",
          text_delta: "你好"
        }
      });
      store = reduceSideThreadStore(store, {
        type: "applyEvent",
        event: {
          type: "delta",
          side_thread_id: "side-1",
          main_thread_id: "main-1",
          message_id: "m-a",
          text_delta: "世界"
        }
      });
      const messages = store.byThread["main-1"]?.messages ?? [];
      expect(messages).toHaveLength(1);
      expect(messages[0]?.text).toBe("你好世界");
    });

    it("message event finalizes a streamed message and clears streaming", () => {
      let store = createInitialSideThreadStore();
      store = reduceSideThreadStore(store, {
        type: "applyEvent",
        event: {
          type: "delta",
          side_thread_id: "side-1",
          main_thread_id: "main-1",
          message_id: "m-a",
          text_delta: "你好"
        }
      });
      const next = reduceSideThreadStore(store, {
        type: "applyEvent",
        event: {
          type: "message",
          side_thread_id: "side-1",
          main_thread_id: "main-1",
          message: message({
            id: "m-a",
            role: "assistant",
            text: "你好世界",
            status: "completed"
          })
        }
      });
      const messages = next.byThread["main-1"]?.messages ?? [];
      expect(messages[0]).toMatchObject({
        id: "m-a",
        text: "你好世界",
        status: "completed"
      });
      expect(next.byThread["main-1"]?.streaming).toBe(false);
    });

    it("error event marks the targeted message failed and records lastError", () => {
      let store = createInitialSideThreadStore();
      store = reduceSideThreadStore(store, {
        type: "applyEvent",
        event: {
          type: "delta",
          side_thread_id: "side-1",
          main_thread_id: "main-1",
          message_id: "m-a",
          text_delta: "中"
        }
      });
      const next = reduceSideThreadStore(store, {
        type: "applyEvent",
        event: {
          type: "error",
          side_thread_id: "side-1",
          main_thread_id: "main-1",
          message_id: "m-a",
          error_message: "rate limited"
        }
      });
      const messages = next.byThread["main-1"]?.messages ?? [];
      expect(messages[0]).toMatchObject({
        id: "m-a",
        status: "failed",
        error_message: "rate limited"
      });
      expect(next.byThread["main-1"]?.lastError).toBe("rate limited");
      expect(next.byThread["main-1"]?.streaming).toBe(false);
    });

    it("populates sideIdToMain from a delta event even before setSummary", () => {
      const store = createInitialSideThreadStore();
      const next = reduceSideThreadStore(store, {
        type: "applyEvent",
        event: {
          type: "delta",
          side_thread_id: "side-x",
          main_thread_id: "main-x",
          message_id: "m-1",
          text_delta: "hi"
        }
      });
      expect(next.sideIdToMain["side-x"]).toBe("main-x");
    });
  });

  describe("dropThread", () => {
    it("removes the entry and the reverse index entry", () => {
      let store = createInitialSideThreadStore();
      store = reduceSideThreadStore(store, {
        type: "setSummary",
        mainThreadId: "main-1",
        summary: summary()
      });
      store = reduceSideThreadStore(store, {
        type: "appendMessage",
        mainThreadId: "main-1",
        message: message()
      });
      const next = reduceSideThreadStore(store, {
        type: "dropThread",
        mainThreadId: "main-1"
      });
      expect(next.byThread["main-1"]).toBeUndefined();
      expect(next.sideIdToMain["side-1"]).toBeUndefined();
    });

    it("is a no-op when the main thread has no side entry", () => {
      const store = createInitialSideThreadStore();
      const next = reduceSideThreadStore(store, {
        type: "dropThread",
        mainThreadId: "main-x"
      });
      expect(next).toBe(store);
    });
  });

  it("drops the targeted entry through the reducer", () => {
    let store = createInitialSideThreadStore();
    store = reduceSideThreadStore(store, {
      type: "setSummary",
      mainThreadId: "main-1",
      summary: summary()
    });
    const next = reduceSideThreadStore(store, {
      type: "dropThread",
      mainThreadId: "main-1"
    });
    expect(next.byThread["main-1"]).toBeUndefined();
    expect(next.sideIdToMain["side-1"]).toBeUndefined();
  });
});