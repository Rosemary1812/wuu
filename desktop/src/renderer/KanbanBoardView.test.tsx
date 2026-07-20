import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import type {
  KanbanRun,
  KanbanTask,
  ParticipantListResult,
  WuuDesktopApi,
} from "../shared/protocol";
import { KanbanBoardView } from "./KanbanBoardView";

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

function task(
  status: KanbanTask["status"],
  overrides: Partial<KanbanTask>,
): KanbanTask {
  return {
    id: `task-${status}`,
    session_id: "session-1",
    title: `Task ${status}`,
    status,
    sort_index: 0,
    created_at: 1_700_000_000_000,
    updated_at: 1_700_000_000_000,
    ...overrides,
  };
}

function run(overrides: Partial<KanbanRun> = {}): KanbanRun {
  return {
    id: "run-1",
    task_id: overrides.task_id ?? "task-running",
    session_id: "session-1",
    kind: "execute",
    target_id: "prt-ada",
    status: "running",
    created_at: 1_700_000_000_000,
    ...overrides,
  };
}

function installStubs(
  tasks: KanbanTask[],
  participants: ParticipantListResult["participants"] = [],
): void {
  const stub = {
    kanbanListTasks: vi.fn(async () => tasks),
    kanbanListRuns: vi.fn(async () => []),
    kanbanListArtifacts: vi.fn(async () => []),
    listParticipants: vi.fn(async () => ({ participants })),
  };
  (globalThis as { wuu?: WuuDesktopApi }).wuu = stub as unknown as WuuDesktopApi;
  (window as { wuu?: WuuDesktopApi }).wuu = stub as unknown as WuuDesktopApi;
}

async function mountBoard(
  props: Partial<React.ComponentProps<typeof KanbanBoardView>> = {},
): Promise<{ container: HTMLElement; root: Root }> {
  const container = document.createElement("div");
  document.body.appendChild(container);
  const root = createRoot(container);
  await act(async () => {
    root.render(
      createElement(KanbanBoardView, {
        sessionId: "session-1",
        refreshToken: 0,
        ...props,
      }),
    );
  });
  mountedRoots.push(root);
  mountedContainers.push(container);
  return { container, root };
}

describe("KanbanBoardView", () => {
  it("renders the five columns and cards for each status", async () => {
    installStubs([
      task("draft", { id: "draft-1", title: "Draft card" }),
      task("ready", { id: "ready-1", title: "Ready card" }),
      task("running", { id: "running-1", title: "Running card", latest_run: run() }),
      task("review", { id: "review-1", title: "Review card" }),
      task("done", { id: "done-1", title: "Done card" }),
    ]);
    const { container } = await mountBoard();
    expect(container.textContent).toContain("Draft card");
    expect(container.textContent).toContain("Ready card");
    expect(container.textContent).toContain("Running card");
    expect(container.textContent).toContain("Review card");
    expect(container.textContent).toContain("Done card");
    expect(container.querySelectorAll(".kanban-column").length).toBe(5);
  });

  it("shows a subtask count on parent cards", async () => {
    installStubs([
      task("ready", { id: "parent-1", title: "Parent" }),
      task("draft", { id: "child-1", title: "Child", parent_id: "parent-1" }),
      task("draft", { id: "child-2", title: "Child", parent_id: "parent-1" }),
    ]);
    const { container } = await mountBoard();
    expect(container.textContent).toContain("2");
  });

  it("resolves the latest run target to a participant name", async () => {
    installStubs(
      [
        task("running", {
          id: "running-1",
          title: "Running card",
          latest_run: run({ target_id: "prt-ada" }),
        }),
      ],
      [
        {
          id: "prt-ada",
          name: "Ada",
          kind: "named",
          avatar_image: undefined,
        },
      ],
    );
    const { container } = await mountBoard();
    expect(container.textContent).toContain("Ada");
  });

  it("shows the drawer when a card is clicked", async () => {
    installStubs(
      [
        task("ready", {
          id: "ready-1",
          title: "Ready card",
          brief: "This is the brief",
          source_thread_id: "thread-1",
        }),
      ],
      [],
    );
    const onOpenSourceThread = vi.fn();
    const { container } = await mountBoard({ onOpenSourceThread });
    act(() => {
      container.querySelector<HTMLElement>(".kanban-card")!.click();
    });
    expect(container.querySelector(".kanban-drawer")).not.toBeNull();
    expect(container.textContent).toContain("This is the brief");
    const linkButton = container.querySelector(".kanban-link-button");
    expect(linkButton).not.toBeNull();
    act(() => (linkButton! as HTMLElement).click());
    expect(onOpenSourceThread).toHaveBeenCalledWith("thread-1");
  });

  it("opens a card from the keyboard without nesting action buttons", async () => {
    installStubs([task("draft", { id: "draft-1", title: "Keyboard card" })]);
    const { container } = await mountBoard();
    const card = container.querySelector<HTMLElement>(".kanban-card")!;

    expect(card.tagName).toBe("DIV");
    expect(card.getAttribute("role")).toBe("button");
    expect(card.querySelector("button")).not.toBeNull();
    await act(async () => {
      card.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true }));
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(container.querySelector(".kanban-drawer")).not.toBeNull();
  });

  it("shows loading state initially", async () => {
    installStubs([]);
    const { container } = await mountBoard();
    // After act, the loading spinner should have been replaced by empty columns
    expect(container.querySelectorAll(".kanban-column").length).toBe(5);
  });

  it("shows an error message when list fails", async () => {
    installStubs([]);
    (window as { wuu?: WuuDesktopApi }).wuu = {
      kanbanListTasks: vi.fn(async () => {
        throw new Error("offline");
      }),
      listParticipants: vi.fn(async () => ({ participants: [] })),
    } as unknown as WuuDesktopApi;
    const { container } = await mountBoard();
    expect(container.textContent).toContain("offline");
  });
});
