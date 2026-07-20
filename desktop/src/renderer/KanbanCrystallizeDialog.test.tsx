import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import type {
  KanbanCrystallizeResult,
  KanbanRun,
  KanbanTask,
  ParticipantProfile,
  WuuDesktopApi,
} from "../shared/protocol";
import { KanbanCrystallizeDialog } from "./KanbanCrystallizeDialog";

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

async function mountDialog(
  props: Partial<React.ComponentProps<typeof KanbanCrystallizeDialog>> = {},
): Promise<{ container: HTMLElement; root: Root }> {
  const container = document.createElement("div");
  document.body.appendChild(container);
  const root = createRoot(container);
  await act(async () => {
    root.render(
      createElement(KanbanCrystallizeDialog, {
        threadId: "thread-1",
        isOpen: true,
        pending: false,
        result: undefined,
        participants: [],
        onClose: () => {},
        onSwitchToBoard: () => {},
        ...props,
      }),
    );
  });
  mountedRoots.push(root);
  mountedContainers.push(container);
  return { container, root };
}

function createResult(subtasks: KanbanCrystallizeResult["subtasks"]): KanbanCrystallizeResult {
  const parent: KanbanTask = {
    id: "task-parent",
    session_id: "session-1",
    title: "Parent",
    brief: "Parent brief",
    status: "draft",
    sort_index: 0,
    created_at: 1_700_000_000_000,
    updated_at: 1_700_000_000_000,
  };
  return { task: parent, subtasks };
}

const participants: ParticipantProfile[] = [
  { id: "prt-ada", kind: "named", name: "Ada" },
  { id: "prt-bob", kind: "named", name: "Bob" },
];

describe("KanbanCrystallizeDialog", () => {
  it("shows pending state while crystallizing", async () => {
    const { container } = await mountDialog({ pending: true });
    expect(container.textContent).toContain("蒸馏中");
  });

  it("confirm-and-dispatch transitions parent and subtasks then dispatches", async () => {
    const transitionCalls: [string, KanbanTask["status"]][] = [];
    const dispatchCalls: KanbanRun[] = [];
    const result = createResult([
      {
        id: "task-sub-1",
        session_id: "session-1",
        title: "Sub 1",
        status: "draft",
        sort_index: 0,
        created_at: 1_700_000_000_000,
        updated_at: 1_700_000_000_000,
        suggested_target_id: "prt-ada",
      },
    ]);

    (window as { wuu?: WuuDesktopApi }).wuu = {
      kanbanTransitionTask: vi.fn(async (taskId, status) => {
        transitionCalls.push([taskId, status]);
        return { id: taskId, session_id: "session-1", title: "", status, sort_index: 0, created_at: 0, updated_at: 0 };
      }),
      kanbanDispatchRun: vi.fn(async (params) => {
        dispatchCalls.push(params as KanbanRun);
        return { ...params, id: "run-1", session_id: "session-1", status: "queued", created_at: 0 } as KanbanRun;
      }),
    } as unknown as WuuDesktopApi;

    const onSwitchToBoard = vi.fn();
    const { container } = await mountDialog({
      result,
      participants,
      onSwitchToBoard,
    });

    const confirmButton = [
      ...container.querySelectorAll(".kanban-crystallize-button"),
    ].find((button) => button.textContent?.includes("确认并派单"));
    expect(confirmButton).toBeDefined();
    await act(async () => (confirmButton! as HTMLElement).click());

    expect(transitionCalls).toEqual([
      ["task-parent", "ready"],
      ["task-sub-1", "ready"],
    ]);
    expect(dispatchCalls).toHaveLength(1);
    expect(dispatchCalls[0].target_id).toBe("prt-ada");
    expect(dispatchCalls[0].task_id).toBe("task-sub-1");
    expect(dispatchCalls[0].thread_id).toBe("thread-1");
    expect(onSwitchToBoard).toHaveBeenCalled();
  });

  it("confirm-only transitions but does not dispatch", async () => {
    const transitionCalls: [string, KanbanTask["status"]][] = [];
    const dispatchCalls: KanbanRun[] = [];
    const result = createResult([
      {
        id: "task-sub-1",
        session_id: "session-1",
        title: "Sub 1",
        status: "draft",
        sort_index: 0,
        created_at: 1_700_000_000_000,
        updated_at: 1_700_000_000_000,
      },
    ]);

    (window as { wuu?: WuuDesktopApi }).wuu = {
      kanbanTransitionTask: vi.fn(async (taskId, status) => {
        transitionCalls.push([taskId, status]);
        return { id: taskId, session_id: "session-1", title: "", status, sort_index: 0, created_at: 0, updated_at: 0 };
      }),
      kanbanDispatchRun: vi.fn(async (params) => {
        dispatchCalls.push(params as KanbanRun);
        return { ...params, id: "run-1", session_id: "session-1", status: "queued", created_at: 0 } as KanbanRun;
      }),
    } as unknown as WuuDesktopApi;

    const onSwitchToBoard = vi.fn();
    const { container } = await mountDialog({
      result,
      participants,
      onSwitchToBoard,
    });

    const confirmOnlyButton = [
      ...container.querySelectorAll(".kanban-crystallize-button"),
    ].find((button) => button.textContent?.includes("仅确认"));
    expect(confirmOnlyButton).toBeDefined();
    await act(async () => (confirmOnlyButton! as HTMLElement).click());

    expect(transitionCalls).toEqual([
      ["task-parent", "ready"],
      ["task-sub-1", "ready"],
    ]);
    expect(dispatchCalls).toHaveLength(0);
    expect(onSwitchToBoard).toHaveBeenCalled();
  });

  it("removes a subtask from the list", async () => {
    const result = createResult([
      {
        id: "task-sub-1",
        session_id: "session-1",
        title: "Sub 1",
        status: "draft",
        sort_index: 0,
        created_at: 1_700_000_000_000,
        updated_at: 1_700_000_000_000,
      },
    ]);
    const { container } = await mountDialog({ result, participants });
    const titleInput = container.querySelector<HTMLInputElement>(
      ".kanban-crystallize-subtask .kanban-crystallize-input",
    );
    expect(titleInput?.value).toBe("Sub 1");
    const removeButton = container.querySelector(".kanban-crystallize-subtask-remove");
    expect(removeButton).not.toBeNull();
    act(() => (removeButton! as HTMLElement).click());
    expect(container.querySelector(".kanban-crystallize-subtask")).toBeNull();
  });
});
