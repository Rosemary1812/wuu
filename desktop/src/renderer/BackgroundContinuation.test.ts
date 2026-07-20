import { describe, expect, it } from "vitest";
import type { Thread, Turn } from "../shared/protocol";
import {
  backgroundContinuationState,
  hasFollowingBackgroundContinuation,
  isAutomaticBackgroundContinuationTurn,
} from "./BackgroundContinuation";
import { AGENT_NOTIFICATION_NAME } from "./AgentHandoff";
import { PROCESS_NOTIFICATION_NAME } from "./InternalUserNotification";

function turn(overrides: Partial<Turn> = {}): Turn {
  return {
    id: "turn-1",
    kind: "user",
    items: [],
    items_view: "full",
    status: "completed",
    ...overrides,
  };
}

function thread(
  turns: Turn[],
  childAgents: Thread["child_agents"] = [],
  backgroundWaiting?: boolean,
): Thread {
  return {
    id: "thread-1",
    preview: "preview",
    model_provider: "fake",
    model: "fake-model",
    cwd: "/repo",
    status: "idle",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    turns,
    child_agents: childAgents,
    background_waiting: backgroundWaiting,
  };
}

describe("background continuation presentation", () => {
  it("recognizes only internal turns carrying agent or process completion notifications", () => {
    const agentTurn = turn({
      kind: "internal",
      items: [
        {
          id: "agent-notification",
          type: "user_message",
          name: AGENT_NOTIFICATION_NAME,
          text: "notification",
        },
      ],
    });
    const processTurn = turn({
      kind: "internal",
      items: [
        {
          id: "process-notification",
          type: "user_message",
          name: PROCESS_NOTIFICATION_NAME,
          text: "notification",
        },
      ],
    });

    expect(isAutomaticBackgroundContinuationTurn(agentTurn)).toBe(true);
    expect(isAutomaticBackgroundContinuationTurn(processTurn)).toBe(true);
    expect(isAutomaticBackgroundContinuationTurn(turn({ kind: "internal" }))).toBe(false);
    expect(isAutomaticBackgroundContinuationTurn({ ...agentTurn, kind: "user" })).toBe(false);
    expect(hasFollowingBackgroundContinuation([turn(), agentTurn], 0)).toBe(true);
  });

  it("waits for a live child agent after the model turn has completed", () => {
    expect(
      backgroundContinuationState(
        thread([turn()], [{ id: "agent-1", status: "running" }]),
      ),
    ).toEqual({ waiting: true, kind: "agent" });
  });

  it("keeps waiting between a background spawn result and its internal completion turn", () => {
    const spawned = turn({
      items: [
        {
          id: "spawn-1",
          type: "collab_agent_tool_call",
          name: "spawn_agent",
          arguments: JSON.stringify({ run_in_background: true }),
          result: JSON.stringify({ status: "running", run_in_background: true }),
        },
      ],
    });
    expect(backgroundContinuationState(thread([spawned]))).toEqual({
      waiting: true,
      kind: "agent",
    });
  });

  it("waits for resume-mode managed commands but ignores detached processes", () => {
    const backgroundItem = {
      id: "bash-1",
      type: "tool_call" as const,
      name: "bash",
      arguments: JSON.stringify({ action: "start_background", command: "npm test" }),
      result: JSON.stringify({ action: "start_background", id: "proc-1", status: "running" }),
    };
    expect(
      backgroundContinuationState(thread([turn({ items: [backgroundItem] })])),
    ).toEqual({ waiting: true, kind: "process" });
    expect(
      backgroundContinuationState(
        thread([
          turn({
            items: [
              {
                ...backgroundItem,
                arguments: JSON.stringify({
                  action: "start_background",
                  command: "npm run dev",
                  completion_mode: "detached",
                }),
              },
            ],
          }),
        ]),
      ),
    ).toEqual({ waiting: false });
  });

  it("does not show a waiting state while a model turn is active or after a normal result", () => {
    expect(
      backgroundContinuationState(
        thread([turn({ status: "in_progress" })], [
          { id: "agent-1", status: "running" },
        ]),
      ),
    ).toEqual({ waiting: false });
    expect(backgroundContinuationState(thread([turn()]))).toEqual({ waiting: false });
  });

  it("uses exact server state across unrelated turns and after a process is stopped", () => {
    const completedFollowUp = turn({ id: "turn-2", items: [] });
    expect(backgroundContinuationState(thread([turn(), completedFollowUp], [], true))).toEqual({
      waiting: true,
      kind: "process",
    });

    const staleProcessStart = turn({
      items: [
        {
          id: "bash-stale",
          type: "tool_call",
          name: "bash",
          arguments: JSON.stringify({ action: "start_background", command: "npm test" }),
          result: JSON.stringify({ action: "start_background", id: "proc-1", status: "running" }),
        },
      ],
    });
    expect(backgroundContinuationState(thread([staleProcessStart], [], false))).toEqual({
      waiting: false,
    });
  });
});
