import { describe, expect, it } from "vitest";
import type { Thread } from "../shared/protocol";
import { buildBackgroundProcessItems } from "./EnvironmentPanel";

function threadWithItems(items: unknown[]): Thread {
  return {
    id: "thread-1",
    preview: "",
    model_provider: "test",
    model: "test",
    cwd: "/repo",
    status: "idle",
    created_at: "2026-01-01T00:00:00.000Z",
    updated_at: "2026-01-01T00:00:00.000Z",
    turns: [
      {
        id: "turn-1",
        items: items as Thread["turns"][number]["items"],
        items_view: "full",
        status: "completed"
      }
    ]
  };
}

describe("buildBackgroundProcessItems", () => {
  it("shows an in-progress start_process before a process id exists", () => {
    const processes = buildBackgroundProcessItems(
      threadWithItems([
        {
          id: "tool-1",
          type: "tool_call",
          status: "in_progress",
          name: "start_process",
          arguments: JSON.stringify({ command: "npm run dev", lifecycle: "session" })
        }
      ])
    );

    expect(processes).toEqual([
      {
        id: "tool-1",
        command: "npm run dev",
        cwd: "",
        lifecycle: "session",
        status: "starting"
      }
    ]);
  });

  it("merges later process observations by process id", () => {
    const started = {
      id: "proc-1",
      command: "npm run dev",
      cwd: "/repo",
      lifecycle: "session",
      status: "running",
      started_at: "2026-01-01T00:00:00.000Z",
      updated_at: "2026-01-01T00:00:01.000Z"
    };
    const stopped = {
      ...started,
      status: "stopped",
      updated_at: "2026-01-01T00:00:02.000Z"
    };

    const processes = buildBackgroundProcessItems(
      threadWithItems([
        {
          id: "tool-1",
          type: "tool_call",
          status: "completed",
          name: "start_process",
          result: JSON.stringify(started)
        },
        {
          id: "tool-2",
          type: "tool_call",
          status: "completed",
          name: "read_process_output",
          result: JSON.stringify({ process: started })
        },
        {
          id: "tool-3",
          type: "tool_call",
          status: "completed",
          name: "stop_process",
          result: JSON.stringify(stopped)
        }
      ])
    );

    expect(processes).toHaveLength(1);
    expect(processes[0]).toMatchObject({
      id: "proc-1",
      command: "npm run dev",
      cwd: "/repo",
      lifecycle: "session",
      status: "stopped",
      updatedAt: "2026-01-01T00:00:02.000Z"
    });
  });

  it("reads processes from list_processes results", () => {
    const running = {
      id: "proc-1",
      command: "npm run dev",
      cwd: "/repo",
      lifecycle: "managed",
      status: "running",
      started_at: "2026-01-01T00:00:00.000Z",
      updated_at: "2026-01-01T00:00:02.000Z"
    };
    const stopped = {
      id: "proc-2",
      command: "npm run watch",
      cwd: "/repo",
      lifecycle: "session",
      status: "stopped",
      started_at: "2026-01-01T00:00:00.000Z",
      updated_at: "2026-01-01T00:00:03.000Z"
    };

    const processes = buildBackgroundProcessItems(
      threadWithItems([
        {
          id: "tool-1",
          type: "tool_call",
          status: "completed",
          name: "list_processes",
          result: JSON.stringify({ action: "list_processes", processes: [stopped, running] })
        }
      ])
    );

    expect(processes.map((process) => process.id)).toEqual(["proc-1", "proc-2"]);
    expect(processes[0]).toMatchObject({
      command: "npm run dev",
      lifecycle: "managed",
      status: "running"
    });
  });
});
