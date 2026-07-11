import { describe, expect, it } from "vitest";

import type { Thread, ThreadItem, Turn } from "../shared/protocol";
import {
  deriveActiveSessionHint,
  deriveActiveSessionHints,
  latestAgentMessageText,
} from "./activeSessionHint";

// Fixture helpers keep the tests focused on the selection logic instead of
// TypeScript boilerplate. Each builder drops a raw object so we can hand
// fields like `text: ""` to express placeholder items without TS noise.

function agentMessage(partial: Partial<ThreadItem> & { text: string }): ThreadItem {
  return {
    id: partial.id ?? "msg",
    type: "agent_message",
    phase: partial.phase,
    status: partial.status,
    text: partial.text,
  };
}

function turn(items: ThreadItem[], partial: Partial<Turn> = {}): Turn {
  return {
    id: partial.id ?? "turn",
    items,
    items_view: "full",
    status: partial.status ?? "completed",
    ...partial,
  };
}

function thread(partial: Partial<Thread> = {}): Thread {
  return {
    id: partial.id ?? "t",
    preview: partial.preview ?? "",
    title: partial.title ?? "对话",
    model_provider: partial.model_provider ?? "wuu",
    model: partial.model ?? "model",
    cwd: partial.cwd ?? "/tmp",
    status: partial.status ?? "idle",
    created_at: partial.created_at ?? "2026-01-01T00:00:00Z",
    updated_at: partial.updated_at ?? "2026-01-01T00:00:00Z",
    turns: partial.turns ?? [],
    ...partial,
  };
}

describe("latestAgentMessageText", () => {
  it("returns the freshest commentary in the latest turn", () => {
    const t = thread({
      turns: [
        turn([
          agentMessage({ id: "old", text: "更早的回复", phase: "commentary" }),
        ]),
        turn([
          agentMessage({ id: "m1", text: "分析问题中…", phase: "commentary", status: "in_progress" }),
          agentMessage({ id: "m2", text: "正在读取文件", phase: "commentary", status: "completed" }),
        ]),
      ],
    });
    expect(latestAgentMessageText(t)).toBe("正在读取文件");
  });

  it("skips empty placeholders so the bubble shows the next-newest text", () => {
    const t = thread({
      turns: [
        turn([
          agentMessage({ id: "stale", text: "实际内容", phase: "commentary" }),
          agentMessage({ id: "fresh", text: "", phase: "commentary", status: "in_progress" }),
        ]),
      ],
    });
    expect(latestAgentMessageText(t)).toBe("实际内容");
  });

  it("walks across turns when the latest one carries no agent messages yet", () => {
    const t = thread({
      turns: [
        turn([
          agentMessage({ id: "earlier-final", text: "上一轮结论", phase: "final_answer", status: "completed" }),
        ]),
        turn([
          // Tool calls only; no assistant text yet in this turn.
        ] as ThreadItem[]),
      ],
    });
    expect(latestAgentMessageText(t)).toBe("上一轮结论");
  });

  it("returns the latest final_answer once the model finishes a turn", () => {
    const t = thread({
      turns: [
        turn([
          agentMessage({ id: "commentary", text: "先写注释", phase: "commentary", status: "completed" }),
          agentMessage({ id: "final", text: "完成后的回答", phase: "final_answer", status: "completed" }),
        ]),
      ],
    });
    expect(latestAgentMessageText(t)).toBe("完成后的回答");
  });

  it("ignores non-agent_message items entirely", () => {
    const t = thread({
      turns: [
        turn([
          { id: "tool", type: "tool_call", text: "忽略我" } as ThreadItem,
          agentMessage({ id: "reply", text: "我是回复", phase: "commentary" }),
        ]),
      ],
    });
    expect(latestAgentMessageText(t)).toBe("我是回复");
  });

  it("returns null when no agent_message items exist anywhere", () => {
    const t = thread({
      turns: [
        turn([{ id: "tool", type: "tool_call", text: "tool only" } as ThreadItem]),
      ],
    });
    expect(latestAgentMessageText(t)).toBeNull();
  });

  it("returns null when there are no turns at all", () => {
    expect(latestAgentMessageText(thread({ turns: [] }))).toBeNull();
  });
});

describe("deriveActiveSessionHint", () => {
  it("returns null when no candidate threads exist", () => {
    expect(
      deriveActiveSessionHint({ threads: [] }),
    ).toBeNull();
  });

  it("filters out archived threads", () => {
    const visible = thread({
      id: "live",
      updated_at: "2026-05-02T00:00:00Z",
      turns: [turn([agentMessage({ text: "可见" })])],
    });
    const archived = thread({
      id: "dead",
      archived: true,
      updated_at: "2026-05-01T00:00:00Z",
      turns: [turn([agentMessage({ text: "不应被选中" })])],
    });
    const hint = deriveActiveSessionHint({ threads: [archived, visible] });
    expect(hint?.thread_id).toBe("live");
    expect(hint?.preview).toBe("可见");
  });

  it("uses the latest agent message as preview and never falls back to thread.preview", () => {
    // The bubble must surface the latest stable agent_message text only.
    // The legacy fallback to Thread.preview (which typically carries the
    // first turn's user query) is intentionally removed: surfacing that
    // as "latest commentary" reads as stale/early text and is exactly the
    // failure mode the pet bubble should avoid. When a thread has no
    // agent_message anywhere, the preview is empty so the pet window
    // hides its bubble card entirely rather than render a stale summary.
    const withItems = thread({
      id: "items",
      turns: [turn([agentMessage({ text: "实时文本" })])],
      // thread.preview is the denormalized field — must be ignored now.
      preview: "聚合后的简介",
    });
    const withStaticOnly = thread({
      id: "static",
      turns: [turn([] as ThreadItem[])],
      // No agent_message anywhere; thread.preview is the only string on
      // the thread and used to surface here as a fallback. Under the
      // new contract it must NOT appear in the hint preview.
      preview: "降级到 thread.preview",
    });
    expect(deriveActiveSessionHint({ threads: [withItems] })?.preview).toBe(
      "实时文本",
    );
    expect(deriveActiveSessionHint({ threads: [withStaticOnly] })?.preview).toBe(
      "",
    );
  });

  it("ranks failed > needs_review > running > unread > idle", () => {
    const failed = thread({
      id: "failed",
      status: "idle",
      title: "失败",
      updated_at: "2026-05-05T00:00:00Z",
      turns: [turn([agentMessage({ text: "出错了" })])],
    });
    const review = thread({
      id: "review",
      status: "idle",
      updated_at: "2026-05-04T00:00:00Z",
      turns: [turn([agentMessage({ text: "需要批准权限" })])],
    });
    const running = thread({
      id: "running",
      status: "in_progress",
      updated_at: "2026-05-03T00:00:00Z",
      turns: [turn([agentMessage({ text: "正在执行" })])],
    });
    const unread = thread({
      id: "unread",
      status: "idle",
      updated_at: "2026-05-02T00:00:00Z",
      turns: [turn([agentMessage({ text: "未读消息" })])],
    });
    const idle = thread({
      id: "idle",
      status: "idle",
      updated_at: "2026-05-01T00:00:00Z",
      turns: [turn([agentMessage({ text: "空闲" })])],
    });
    const all = [idle, unread, running, review, failed];
    expect(deriveActiveSessionHint({ threads: all })?.thread_id).toBe("failed");

    const withoutFailures = all.filter((t) => t.id !== "failed");
    expect(deriveActiveSessionHint({ threads: withoutFailures })?.thread_id).toBe(
      "review",
    );
    const withoutReview = withoutFailures.filter((t) => t.id !== "review");
    expect(deriveActiveSessionHint({ threads: withoutReview })?.thread_id).toBe(
      "running",
    );
    const withoutRunning = withoutReview.filter((t) => t.id !== "running");
    // Plain idle threads outrank each other only by updated_at, so without an
    // unread set the first non-running leftover wins on recency.
    expect(
      deriveActiveSessionHint({ threads: withoutRunning })?.thread_id,
    ).toBe("unread");

    // Same candidates but flagged as unread → the unread idle outranks the
    // plain idle.
    expect(
      deriveActiveSessionHint({
        threads: withoutRunning,
        unreadThreadIDs: new Set(["unread"]),
      })?.thread_id,
    ).toBe("unread");
    expect(
      deriveActiveSessionHint({
        threads: [unread, idle],
        unreadThreadIDs: new Set(["idle"]),
      })?.thread_id,
    ).toBe("idle");
  });

  it("breaks priority ties with updated_at desc", () => {
    const newer = thread({
      id: "newer",
      updated_at: "2026-05-02T00:00:00Z",
      turns: [turn([agentMessage({ text: "新的" })])],
    });
    const older = thread({
      id: "older",
      updated_at: "2026-05-01T00:00:00Z",
      turns: [turn([agentMessage({ text: "旧的" })])],
    });
    expect(
      deriveActiveSessionHint({ threads: [older, newer] })?.thread_id,
    ).toBe("newer");
  });

  it("lets the focused thread win ties via the +0.5 priority bump", () => {
    const focused = thread({
      id: "focused",
      updated_at: "2026-05-01T00:00:00Z",
      turns: [turn([agentMessage({ text: "f" })])],
    });
    const background = thread({
      id: "background",
      updated_at: "2026-05-02T00:00:00Z", // strictly newer
      turns: [turn([agentMessage({ text: "b" })])],
    });
    expect(
      deriveActiveSessionHint({
        threads: [focused, background],
        thread: focused,
      })?.thread_id,
    ).toBe("focused");
    // Without the focus bump, recency would pick background.
    expect(
      deriveActiveSessionHint({ threads: [focused, background] })?.thread_id,
    ).toBe("background");
  });

  it("only flags needs_review once the thread has settled", () => {
    const running = thread({
      id: "running-with-permission-text",
      status: "in_progress",
      turns: [turn([agentMessage({ text: "正在请求权限批准" })])],
    });
    const hint = deriveActiveSessionHint({ threads: [running] });
    expect(hint?.status).toBe("running");
    expect(hint?.attention).toBe(false);
  });
});

describe("deriveActiveSessionHints", () => {
  it("returns the ranked top threads capped at three", () => {
    const make = (id: string, updatedAt: string, text: string, status: Thread["status"] = "idle") =>
      thread({
        id,
        status,
        updated_at: updatedAt,
        turns: [turn([agentMessage({ text })])],
      });
    const threads = [
      make("a", "2026-05-01T00:00:00Z", "一"),
      make("b", "2026-05-02T00:00:00Z", "二"),
      make("c", "2026-05-03T00:00:00Z", "三"),
      make("d", "2026-05-04T00:00:00Z", "四"),
      make("running", "2026-05-01T00:00:00Z", "跑着", "in_progress"),
    ];
    const hints = deriveActiveSessionHints({ threads });
    // Cap at 3; the running thread outranks all idles, then recency.
    expect(hints.map((h) => h.thread_id)).toEqual(["running", "d", "c"]);
  });

  it("omits threads without commentary instead of pushing empty rows", () => {
    const silent = thread({
      id: "silent",
      status: "in_progress",
      updated_at: "2026-05-09T00:00:00Z",
      turns: [],
    });
    const talking = thread({
      id: "talking",
      updated_at: "2026-05-01T00:00:00Z",
      turns: [turn([agentMessage({ text: "有话说" })])],
    });
    // The silent running thread would win the single-hint ranking (and
    // does, as the hide-the-bubble signal there), but the multi-row feed
    // simply skips it: rows exist to show commentary.
    expect(deriveActiveSessionHint({ threads: [silent, talking] })?.thread_id).toBe(
      "silent",
    );
    const hints = deriveActiveSessionHints({ threads: [silent, talking] });
    expect(hints.map((h) => h.thread_id)).toEqual(["talking"]);
  });

  it("returns an empty list when nothing has commentary", () => {
    const silent = thread({ id: "s", turns: [] });
    expect(deriveActiveSessionHints({ threads: [silent] })).toEqual([]);
    expect(deriveActiveSessionHints({ threads: [] })).toEqual([]);
  });
});
