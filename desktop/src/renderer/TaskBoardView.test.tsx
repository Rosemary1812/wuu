/**
 * Tests for TaskBoardView — the per-group task-board tab. The board is the
 * only place anchorless (standalone) tasks are visible in the GUI, pins
 * review-status tasks (waiting for the human's 完成 Task click), and renders
 * each task's owner. Render-harness pattern mirroring
 * ConversationSubthreadPanel.test.tsx, plus a window.wuu stub for
 * listConversationSubthreads (SettingsView.test.tsx pattern).
 */
import { act } from "react";
import { createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ConversationSubthread, WuuDesktopApi } from "../shared/protocol";
import { TaskBoardView } from "./TaskBoardView";

let mountedRoots: Root[] = [];
let mountedContainers: HTMLElement[] = [];

async function mount(element: React.ReactElement): Promise<HTMLElement> {
  const container = document.createElement("div");
  document.body.appendChild(container);
  const root = createRoot(container);
  await act(async () => {
    root.render(element);
  });
  mountedRoots.push(root);
  mountedContainers.push(container);
  return container;
}

afterEach(() => {
  for (const root of mountedRoots) {
    act(() => {
      root.unmount();
    });
  }
  for (const container of mountedContainers) {
    container.remove();
  }
  mountedRoots = [];
  mountedContainers = [];
  delete (globalThis as { wuu?: WuuDesktopApi }).wuu;
});

function subthread(
  overrides: Partial<ConversationSubthread>,
): ConversationSubthread {
  return {
    id: "cth-x",
    thread_id: "group-1",
    anchor_item_id: "",
    status: "task",
    created_at: "2026-07-06T10:00:00Z",
    reply_count: 0,
    ...overrides,
  };
}

function stubListSubthreads(subthreads: ConversationSubthread[]): void {
  const stub = {
    listConversationSubthreads: vi.fn().mockResolvedValue({ subthreads }),
  };
  (globalThis as { wuu?: WuuDesktopApi }).wuu = stub as unknown as WuuDesktopApi;
  (window as { wuu?: WuuDesktopApi }).wuu = stub as unknown as WuuDesktopApi;
}

describe("TaskBoardView", () => {
  it("renders anchorless standalone tasks with owner and pins review on top", async () => {
    stubListSubthreads([
      // Standalone (no anchor message) — invisible in the chat view; the
      // board is its only GUI surface.
      subthread({
        id: "cth-standalone",
        title: "整理演练记录",
        status: "task",
        owner_participant_id: "prt-ada",
        created_at: "2026-07-06T11:00:00Z",
      }),
      // Review-status task waits for the human gate and must be pinned
      // above active tasks regardless of creation order.
      subthread({
        id: "cth-review",
        title: "修复登录抖动",
        status: "review",
        owner_participant_id: "prt-bea",
        anchor_item_id: "item-9",
        created_at: "2026-07-06T12:00:00Z",
      }),
      // Resolved/open subthreads are not board rows.
      subthread({ id: "cth-done", title: "旧任务", status: "resolved" }),
      subthread({ id: "cth-open", title: "闲聊讨论", status: "open" }),
    ]);
    const container = await mount(
      createElement(TaskBoardView, {
        threadID: "group-1",
        title: "任务 · 发布小组",
        refreshToken: 0,
        resolveParticipantName: (id: string) =>
          ({ "prt-ada": "Ada", "prt-bea": "Bea" })[id],
        onOpenTask: () => {},
      }),
    );

    const rows = Array.from(
      container.querySelectorAll<HTMLButtonElement>(".task-board-row"),
    );
    expect(rows.map((row) => row.textContent)).toHaveLength(2);
    // Review section renders before 进行中, so the review task is first.
    expect(rows[0]?.textContent).toContain("修复登录抖动");
    expect(rows[0]?.textContent).toContain("待验收");
    expect(rows[0]?.textContent).toContain("Bea");
    expect(rows[1]?.textContent).toContain("整理演练记录");
    expect(rows[1]?.textContent).toContain("Ada 认领");
    expect(container.textContent).toContain("1 待验收 · 1 进行中");
    expect(container.textContent).not.toContain("旧任务");
    expect(container.textContent).not.toContain("闲聊讨论");
  });

  it("marks unclaimed tasks and opens a task on row click", async () => {
    stubListSubthreads([
      subthread({ id: "cth-unclaimed", title: "无人认领的活", status: "task" }),
    ]);
    const onOpenTask = vi.fn();
    const container = await mount(
      createElement(TaskBoardView, {
        threadID: "group-1",
        refreshToken: 0,
        onOpenTask,
      }),
    );

    const row = container.querySelector<HTMLButtonElement>(".task-board-row");
    expect(row?.textContent).toContain("无人认领");
    await act(async () => {
      row?.click();
    });
    expect(onOpenTask).toHaveBeenCalledWith("cth-unclaimed");
  });

  it("shows the empty state when the board has no tasks", async () => {
    stubListSubthreads([subthread({ id: "cth-open", status: "open" })]);
    const container = await mount(
      createElement(TaskBoardView, {
        threadID: "group-1",
        refreshToken: 0,
        onOpenTask: () => {},
      }),
    );
    expect(container.textContent).toContain("板上没有任务");
  });
});
