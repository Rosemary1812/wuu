import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import type { GoalSnapshotResult, WuuDesktopApi } from "../shared/protocol";
import { WorkspaceGoalPanel } from "./WorkspaceGoalPanel";

let container: HTMLDivElement;
let root: Root | null = null;
let originalWuu: PropertyDescriptor | undefined;

beforeEach(() => {
  originalWuu = Object.getOwnPropertyDescriptor(window, "wuu");
  container = document.createElement("div");
  document.body.appendChild(container);
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  root = null;
  container.remove();
  if (originalWuu) {
    Object.defineProperty(window, "wuu", originalWuu);
  } else {
    delete (window as Partial<Window>).wuu;
  }
  vi.restoreAllMocks();
});

function installWuu(result: GoalSnapshotResult): ReturnType<typeof vi.fn> {
  const getGoalSnapshot = vi.fn(() => Promise.resolve(result));
  Object.defineProperty(window, "wuu", {
    configurable: true,
    value: { getGoalSnapshot } as Partial<WuuDesktopApi>,
  });
  return getGoalSnapshot;
}

async function render(open: boolean): Promise<void> {
  await act(async () => {
    root = createRoot(container);
    root.render(
      <WorkspaceGoalPanel
        activeContext={{ kind: "no_project", cwd: "/repo" }}
        threadId="thread-1"
        open={open}
      />
    );
  });
  await act(async () => {
    await Promise.resolve();
  });
}

describe("WorkspaceGoalPanel", () => {
  it("loads the unified goal snapshot for the active thread", async () => {
    const getGoalSnapshot = installWuu({
      snapshot: {
        generated_at: "2026-06-14T07:00:00Z",
        goals: [
          {
            id: "goal-1",
            goal: "release",
            status: "needs_human",
            current_step: "approval",
            needs_human: true,
            pending_approvals: [
              {
                id: "approval-1",
                goal_id: "goal-1",
                title: "Approve merge",
                requested_action: "merge worktree",
                status: "pending",
                created_at: "2026-06-14T07:00:00Z",
              },
            ],
          },
        ],
        approvals: [
          {
            id: "approval-1",
            goal_id: "goal-1",
            title: "Approve merge",
            requested_action: "merge worktree",
            status: "pending",
            created_at: "2026-06-14T07:00:00Z",
          },
        ],
        workflows: [
          {
            id: "workflow-1",
            definition_name: "release-check",
            status: "running",
            goal_id: "goal-1",
            phases: [{ id: "verify", name: "Verify", status: "running" }],
            agent_runs: [{ id: "agent-1", status: "completed" }],
            event_count: 3,
          },
        ],
        harness: {
          tasks: [
            {
              id: "task-1",
              name: "QA pass",
              role: "verifier",
              goal_id: "goal-1",
              status: "failed",
            },
          ],
          reports: [
            {
              id: "report-1",
              task_id: "task-1",
              outcome: "partial",
              summary: "tests still fail",
              verification: ["go test ./..."],
            },
          ],
        },
        attention: [
          {
            source: "harness",
            id: "task-1",
            status: "failed",
            message: "review failed",
          },
        ],
      },
    });

    await render(true);

    expect(getGoalSnapshot).toHaveBeenCalledWith("thread-1");
    expect(container.textContent).toContain("release-check");
    expect(container.textContent).toContain("QA pass");
    expect(container.textContent).toContain("Approve merge");
    expect(container.textContent).toContain("review failed");
    expect(container.textContent).toContain("tests still fail");
  });

  it("does not read goal state while closed", async () => {
    const getGoalSnapshot = installWuu({
      snapshot: {
        generated_at: "2026-06-14T07:00:00Z",
      },
    });

    await render(false);

    expect(getGoalSnapshot).not.toHaveBeenCalled();
  });
});
