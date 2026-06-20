import { describe, expect, it } from "vitest";
import type { ManagedProcess, Thread } from "../shared/protocol";
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

function managedProcess(overrides: Partial<ManagedProcess>): ManagedProcess {
  return {
    id: "proc-1",
    owner_kind: "main_agent",
    owner_id: "test",
    lifecycle: "session",
    status: "running",
    pid: 123,
    command: "npm run dev",
    cwd: "/repo",
    started_at: "2026-01-01T00:00:00.000Z",
    updated_at: "2026-01-01T00:00:01.000Z",
    exit_code: -1,
    ...overrides
  };
}

describe("buildBackgroundProcessItems", () => {
  it("shows an in-progress bash background command before a process id exists", () => {
    const processes = buildBackgroundProcessItems(
      threadWithItems([
        {
          id: "tool-1",
          type: "tool_call",
          status: "in_progress",
          name: "bash",
          display: { capability: "command.background" },
          arguments: JSON.stringify({ action: "start_background", command: "npm run dev", lifecycle: "session" })
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

  it("shows an in-progress command.background item before a process id exists", () => {
    const processes = buildBackgroundProcessItems(
      threadWithItems([
        {
          id: "tool-1",
          type: "tool_call",
          status: "in_progress",
          name: "bash",
          display: { capability: "command.background" },
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
          name: "bash",
          result: JSON.stringify({ action: "start_background", ...started })
        },
        {
          id: "tool-2",
          type: "tool_call",
          status: "completed",
          name: "bash",
          result: JSON.stringify({ action: "read_background", process: started })
        },
        {
          id: "tool-3",
          type: "tool_call",
          status: "completed",
          name: "bash",
          result: JSON.stringify({ action: "stop_background", ...stopped })
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

  it("reads process observations from result action even when tool name changed", () => {
    const started = {
      action: "start_background",
      id: "proc-1",
      command: "npm run dev",
      cwd: "/repo",
      lifecycle: "session",
      status: "running",
      started_at: "2026-01-01T00:00:00.000Z",
      updated_at: "2026-01-01T00:00:01.000Z"
    };

    const processes = buildBackgroundProcessItems(
      threadWithItems([
        {
          id: "tool-1",
          type: "tool_call",
          status: "completed",
          name: "bash",
          result: JSON.stringify(started)
        }
      ])
    );

    expect(processes).toHaveLength(1);
    expect(processes[0]).toMatchObject({
      id: "proc-1",
      command: "npm run dev",
      cwd: "/repo",
      status: "running"
    });
  });

  it("reads processes from bash list_background results", () => {
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
          name: "bash",
          result: JSON.stringify({ action: "list_background", processes: [stopped, running] })
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

  it("uses managed process snapshots as the latest status", () => {
    const started = {
      id: "proc-1",
      command: "npm run dev",
      cwd: "/repo",
      lifecycle: "session",
      status: "running",
      started_at: "2026-01-01T00:00:00.000Z",
      updated_at: "2026-01-01T00:00:01.000Z"
    };

    const processes = buildBackgroundProcessItems(
      threadWithItems([
        {
          id: "tool-1",
          type: "tool_call",
          status: "completed",
          name: "bash",
          result: JSON.stringify({ action: "start_background", ...started })
        }
      ]),
      [
        managedProcess({
          status: "stopped",
          updated_at: "2026-01-01T00:00:03.000Z"
        })
      ]
    );

    expect(processes).toHaveLength(1);
    expect(processes[0]).toMatchObject({
      id: "proc-1",
      status: "stopped",
      updatedAt: "2026-01-01T00:00:03.000Z"
    });
  });

  it("carries preview urls from process metadata", () => {
    const started = {
      id: "proc-1",
      owner_id: "thread-1",
      command: "npm run dev",
      cwd: "/repo",
      lifecycle: "session",
      status: "running",
      preview_urls: ["http://localhost:5173/"],
      primary_preview_url: "http://localhost:5173/",
      started_at: "2026-01-01T00:00:00.000Z",
      updated_at: "2026-01-01T00:00:01.000Z"
    };

    const processes = buildBackgroundProcessItems(
      threadWithItems([
        {
          id: "tool-1",
          type: "tool_call",
          status: "completed",
          name: "bash",
          result: JSON.stringify({ action: "start_background", ...started })
        }
      ])
    );

    expect(processes[0]).toMatchObject({
      id: "proc-1",
      ownerID: "thread-1",
      primaryPreviewURL: "http://localhost:5173/",
      previewURLs: ["http://localhost:5173/"]
    });
  });
});
