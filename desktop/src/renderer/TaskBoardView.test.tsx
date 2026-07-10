import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ConversationSubthread, WuuDesktopApi } from "../shared/protocol";
import { TaskBoardView } from "./TaskBoardView";

let mountedRoots: Root[] = [];
let mountedContainers: HTMLElement[] = [];

afterEach(() => {
  for (const root of mountedRoots) {
    act(() => root.unmount());
  }
  for (const container of mountedContainers) {
    container.remove();
  }
  mountedRoots = [];
  mountedContainers = [];
  delete (globalThis as { wuu?: WuuDesktopApi }).wuu;
  vi.restoreAllMocks();
});

function subthread(
  overrides: Partial<ConversationSubthread>,
): ConversationSubthread {
  return {
    id: "cth-x",
    thread_id: "group-1",
    anchor_item_id: "item-1",
    status: "task",
    created_at: "2026-07-06T10:00:00Z",
    reply_count: 0,
    thread_owner_participant_id: "prt-ada",
    lead_participant_id: "prt-ada",
    ...overrides,
  };
}

function installListStub(
  implementation: WuuDesktopApi["listConversationSubthreads"],
): void {
  const stub = { listConversationSubthreads: vi.fn(implementation) };
  (globalThis as { wuu?: WuuDesktopApi }).wuu =
    stub as unknown as WuuDesktopApi;
  (window as { wuu?: WuuDesktopApi }).wuu = stub as unknown as WuuDesktopApi;
}

async function mountBoard(
  props: Partial<React.ComponentProps<typeof TaskBoardView>> = {},
): Promise<{ container: HTMLElement; root: Root }> {
  const container = document.createElement("div");
  document.body.appendChild(container);
  const root = createRoot(container);
  await act(async () => {
    root.render(
      createElement(TaskBoardView, {
        threadID: "group-1",
        refreshToken: 0,
        onOpenTask: () => {},
        ...props,
      }),
    );
  });
  mountedRoots.push(root);
  mountedContainers.push(container);
  return { container, root };
}

function deferred<T>(): {
  promise: Promise<T>;
  resolve: (value: T) => void;
} {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

describe("TaskBoardView", () => {
  it("projects lead, execution state, progress, active workers, and completed Tasks", async () => {
    installListStub(async () => ({
      subthreads: [
        subthread({
          id: "cth-review",
          title: "验收发布结果",
          exec_state: "awaiting_lead",
          plan: [
            { id: "one", title: "实现", status: "done", assignee: "prt-bea" },
            { id: "old", title: "旧范围", status: "cancelled", assignee: "prt-cai" },
          ],
        }),
        subthread({
          id: "cth-running",
          title: "修复登录抖动",
          exec_state: "executing",
          plan: [
            { id: "one", title: "定位", status: "done", assignee: "prt-bea" },
            { id: "two", title: "修复", status: "active", assignee: "prt-cai" },
          ],
        }),
        subthread({
          id: "cth-done",
          title: "整理演练记录",
          status: "resolved",
          exec_state: "completed",
          task: { id: "task-done", status: "completed" },
        }),
        subthread({
          id: "cth-open",
          title: "闲聊讨论",
          status: "open",
          lead_participant_id: undefined,
          exec_state: undefined,
          task: undefined,
        }),
      ],
    }));
    const { container } = await mountBoard({
      title: "任务 · 发布小组",
      resolveParticipantName: (id: string) =>
        ({ "prt-ada": "Ada", "prt-bea": "Bea", "prt-cai": "Cai" })[id],
    });

    const rows = Array.from(
      container.querySelectorAll<HTMLButtonElement>(".task-board-row"),
    );
    expect(rows).toHaveLength(3);
    expect(rows[0]?.textContent).toContain("验收发布结果");
    expect(rows[0]?.textContent).toContain("等待 Lead");
    expect(rows[0]?.textContent).toContain("1/1 完成");
    expect(rows[0]?.textContent).toContain("1 已取消");
    expect(rows[1]?.textContent).toContain("修复登录抖动");
    expect(rows[1]?.textContent).toContain("Lead · Ada");
    expect(rows[1]?.textContent).toContain("执行中");
    expect(rows[1]?.textContent).toContain("1/2 完成");
    expect(rows[1]?.textContent).toContain("执行 · Cai");
    expect(rows[2]?.textContent).toContain("整理演练记录");
    expect(rows[2]?.textContent).toContain("已完成");
    expect(container.textContent).toContain("1 待处理 · 1 执行中 · 1 已完成");
    expect(container.textContent).toContain("需要处理");
    expect(container.textContent).not.toContain("认领");
    expect(container.textContent).not.toContain("无人认领");
    expect(container.textContent).not.toContain("闲聊讨论");
  });

  it("opens the same Task Thread from its board row", async () => {
    installListStub(async () => ({
      subthreads: [subthread({ id: "cth-task", title: "验证发布" })],
    }));
    const onOpenTask = vi.fn();
    const { container } = await mountBoard({ onOpenTask });

    act(() => {
      container.querySelector<HTMLButtonElement>(".task-board-row")!.click();
    });
    expect(onOpenTask).toHaveBeenCalledWith("cth-task");
  });

  it("shows the new empty state when the group has no escalated Tasks", async () => {
    installListStub(async () => ({
      subthreads: [
        subthread({
          id: "cth-open",
          status: "open",
          lead_participant_id: undefined,
          exec_state: undefined,
          task: undefined,
        }),
      ],
    }));
    const { container } = await mountBoard();
    expect(container.textContent).toContain("还没有 Task");
  });

  it("clears the previous group and ignores a slower response after thread switch", async () => {
    const groupA = deferred<{ subthreads: ConversationSubthread[] }>();
    const groupB = deferred<{ subthreads: ConversationSubthread[] }>();
    installListStub((threadID) =>
      threadID === "group-a" ? groupA.promise : groupB.promise,
    );
    const { container, root } = await mountBoard({ threadID: "group-a" });

    await act(async () => {
      root.render(
        createElement(TaskBoardView, {
          threadID: "group-b",
          refreshToken: 0,
          onOpenTask: () => {},
        }),
      );
    });
    expect(container.textContent).toContain("加载任务");
    await act(async () => {
      groupB.resolve({
        subthreads: [subthread({ id: "b", title: "Group B Task" })],
      });
    });
    expect(container.textContent).toContain("Group B Task");
    await act(async () => {
      groupA.resolve({
        subthreads: [subthread({ id: "a", title: "Stale Group A Task" })],
      });
    });
    expect(container.textContent).toContain("Group B Task");
    expect(container.textContent).not.toContain("Stale Group A Task");
  });

  it("does not leave stale rows visible when a refresh fails", async () => {
    let calls = 0;
    installListStub(async () => {
      calls += 1;
      if (calls === 1) {
        return {
          subthreads: [subthread({ id: "old", title: "Old Task" })],
        };
      }
      throw new Error("offline");
    });
    const { container, root } = await mountBoard();
    expect(container.textContent).toContain("Old Task");

    await act(async () => {
      root.render(
        createElement(TaskBoardView, {
          threadID: "group-1",
          refreshToken: 1,
          onOpenTask: () => {},
        }),
      );
    });

    expect(container.textContent).toContain("offline");
    expect(container.textContent).not.toContain("Old Task");
  });
});
