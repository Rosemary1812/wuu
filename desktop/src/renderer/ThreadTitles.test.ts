import { describe, expect, it } from "vitest";
import type { Thread } from "../shared/protocol";
import { threadDisplayTitle } from "./ThreadTitles";

function thread(overrides: Partial<Thread>): Thread {
  return {
    id: "thread",
    preview: "Build the app",
    model_provider: "test",
    model: "test",
    cwd: "/tmp/project",
    status: "idle",
    created_at: "2026-05-28T00:00:00.000Z",
    updated_at: "2026-05-28T00:00:00.000Z",
    turns: [],
    ...overrides
  };
}

describe("threadDisplayTitle", () => {
  it("uses the preview for regular threads", () => {
    expect(threadDisplayTitle(thread({ preview: "Fix tabs" }))).toBe("Fix tabs");
  });

  it("labels forked threads from their source preview", () => {
    const source = thread({ id: "source", preview: "Fix tabs" });
    const fork = thread({ id: "fork", preview: "Fix tabs", forked_from_id: "source" });

    expect(threadDisplayTitle(fork, [source, fork])).toBe("Fix tabs · 分叉");
  });

  it("falls back to the fork preview when the source is unavailable", () => {
    const fork = thread({ id: "fork", preview: "Fix tabs", forked_from_id: "source" });

    expect(threadDisplayTitle(fork, [fork])).toBe("Fix tabs · 分叉");
  });
});
