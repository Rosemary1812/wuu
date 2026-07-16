import { randomUUID } from "node:crypto";
import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { EventEmitter } from "node:events";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { PassThrough } from "node:stream";
import { describe, expect, it, vi } from "vitest";

vi.mock("electron", () => ({
  app: {
    getAppPath: () => process.cwd(),
  },
}));

import {
  activityServerRequestRejection,
  AppServerClient,
  type AppServerClientEvent,
  appServerExitMessage,
  AppServerClientPool,
  type AppServerSpawn,
  cuaMacHelperEnvironment,
  updateStoppedActivityIDs,
} from "./appServerClients";

class FakeAppServerChild extends EventEmitter {
  readonly stdin = new PassThrough();
  readonly stdout = new PassThrough();
  readonly stderr = new PassThrough();
  killed = false;

  kill(): boolean {
    this.killed = true;
    return true;
  }

  asChildProcess(): ChildProcessWithoutNullStreams {
    return this as unknown as ChildProcessWithoutNullStreams;
  }
}

function makeClient(spawnAppServer: AppServerSpawn): {
  client: AppServerClient;
  events: AppServerClientEvent[];
  stateChanges: () => number;
} {
  const events: AppServerClientEvent[] = [];
  let stateChangeCount = 0;
  const client = new AppServerClient(
    tmpdir(),
    "",
    (_source, event) => events.push(event),
    () => {
      stateChangeCount += 1;
    },
    spawnAppServer,
    (_env, workdir) => ({ command: "test-wuu-core", args: [], cwd: workdir }),
  );
  return {
    client,
    events,
    stateChanges: () => stateChangeCount,
  };
}

function serverExitEvents(events: AppServerClientEvent[]): Extract<
  AppServerClientEvent,
  { kind: "server-exit" }
>[] {
  return events.filter(
    (event): event is Extract<AppServerClientEvent, { kind: "server-exit" }> =>
      event.kind === "server-exit",
  );
}

describe("appServerExitMessage", () => {
  it("preserves stderr and the exit code", () => {
    expect(appServerExitMessage(1, "parse config: unknown field")).toBe(
      "wuu core exited (code 1): parse config: unknown field",
    );
  });

  it("still reports an exit without stderr", () => {
    expect(appServerExitMessage(null, "")).toBe("wuu core exited");
  });

  it("rejects future plugin bridge requests after their Activity stops", () => {
    const stopped = new Set<string>();
    updateStoppedActivityIDs(stopped, {
      method: "activity/stopped",
      params: { id: "activity-1", thread_id: "thread-1" },
    });
    expect(
      activityServerRequestRejection(
        {
          id: "bridge-1",
          method: "official-plugin/browser-command",
          params: { activity_id: "activity-1", action: "click" },
        },
        stopped,
      ),
    ).toBe("activity activity-1 is stopped");
    expect(
      activityServerRequestRejection(
        {
          id: "bridge-2",
          method: "official-plugin/browser-command",
          params: { activity_id: "activity-2", action: "click" },
        },
        stopped,
      ),
    ).toBeUndefined();
  });
});

describe("cuaMacHelperEnvironment", () => {
  it("injects the packaged signed helper path on macOS", () => {
    const result = cuaMacHelperEnvironment(
      { HOME: "/Users/test" },
      "/source",
      "/Applications/wuu.app/Contents/Resources",
      "darwin",
      (path) => path === "/Applications/wuu.app/Contents/Resources/bin/wuu-cua-mac",
    );
    expect(result.WUU_CUA_MAC_HELPER).toBe(
      "/Applications/wuu.app/Contents/Resources/bin/wuu-cua-mac",
    );
  });

  it("uses the development helper without replacing an explicit override", () => {
    const discovered = cuaMacHelperEnvironment(
      {},
      "/source",
      undefined,
      "darwin",
      (path) => path === "/source/desktop/build/bin/wuu-cua-mac",
    );
    expect(discovered.WUU_CUA_MAC_HELPER).toBe(
      "/source/desktop/build/bin/wuu-cua-mac",
    );
    const overridden = cuaMacHelperEnvironment(
      { WUU_CUA_MAC_HELPER: "/custom/helper" },
      "/source",
      undefined,
      "darwin",
      () => true,
    );
    expect(overridden.WUU_CUA_MAC_HELPER).toBe("/custom/helper");
  });
});

describe("AppServerClientPool Activity routing", () => {
  it("does not create a new workspace client for an unknown Activity workdir", async () => {
    const pool = new AppServerClientPool(
      () => ({ kind: "no_project", cwd: "/active" }),
      () => "/active",
      () => undefined,
    );
    await expect(
      pool.requestForWorkdir("/missing", "activity/stop", {
        thread_id: "thread-1",
        activity_id: "activity-1",
      }),
    ).rejects.toThrow("activity workspace is no longer connected");
  });
});

describe("AppServerClient child lifecycle", () => {
  it("tracks the cwd of running threads and clears it on completion", async () => {
    const child = new FakeAppServerChild();
    const { client } = makeClient(() => child.asChildProcess());
    const listed = client.request("thread/list");
    child.stdout.write(
      `${JSON.stringify({
        id: "client-1",
        result: {
          threads: [
            {
              id: "thread-1",
              cwd: "/repo/.wuu/worktrees/thread-1",
              status: "in_progress",
            },
          ],
        },
      })}\n`,
    );
    await listed;

    expect(client.runningThreadCwds()).toEqual([
      "/repo/.wuu/worktrees/thread-1",
    ]);

    child.stdout.write(
      `${JSON.stringify({
        method: "turn/completed",
        params: { thread_id: "thread-1" },
      })}\n`,
    );
    expect(client.runningThreadCwds()).toEqual([]);
  });

  it("retains an idle worktree cwd when its next turn starts", async () => {
    const child = new FakeAppServerChild();
    const { client } = makeClient(() => child.asChildProcess());
    const listed = client.request("thread/list");
    child.stdout.write(
      `${JSON.stringify({
        id: "client-1",
        result: {
          threads: [
            {
              id: "thread-1",
              cwd: "/repo/.wuu/worktrees/thread-1",
              status: "idle",
            },
          ],
        },
      })}\n`,
    );
    await listed;
    expect(client.runningThreadCwds()).toEqual([]);

    const started = client.request("turn/start", { thread_id: "thread-1" });
    expect(client.runningThreadCwds()).toEqual([
      "/repo/.wuu/worktrees/thread-1",
    ]);
    child.stdout.write(
      `${JSON.stringify({
        id: "client-2",
        result: { turn: { status: "in_progress" } },
      })}\n`,
    );
    await started;
    expect(client.runningThreadCwds()).toEqual([
      "/repo/.wuu/worktrees/thread-1",
    ]);
  });

  it("finalizes a real ENOENT spawn error and its later close exactly once", async () => {
    const missingBinary = join(tmpdir(), `wuu-missing-${randomUUID()}`);
    let closePromise: Promise<void> | undefined;
    const spawnMissing: AppServerSpawn = (_command, _args, options) => {
      const child = spawn(missingBinary, [], options);
      closePromise = new Promise((resolve) => {
        child.once("close", () => resolve());
      });
      return child;
    };
    const { client, events, stateChanges } = makeClient(spawnMissing);

    await expect(client.request("initialize")).rejects.toThrow(/ENOENT/);
    await closePromise;

    expect(serverExitEvents(events)).toHaveLength(1);
    expect(serverExitEvents(events)[0]?.message).toMatch(/ENOENT/);
    expect(stateChanges()).toBe(1);
    expect(client.isBusy()).toBe(false);
  });

  it("routes a synchronous stdin write failure through finalization", async () => {
    const child = new FakeAppServerChild();
    child.stdin.write = (() => {
      throw Object.assign(new Error("write EPIPE"), { code: "EPIPE" });
    }) as typeof child.stdin.write;
    const { client, events, stateChanges } = makeClient(
      () => child.asChildProcess(),
    );

    await expect(client.request("initialize")).rejects.toThrow(
      /stdin write failed: write EPIPE/,
    );
    child.emit("exit", 1, null);
    child.emit("close", 1, null);

    expect(serverExitEvents(events)).toHaveLength(1);
    expect(stateChanges()).toBe(1);
    expect(client.isBusy()).toBe(false);
    expect(child.killed).toBe(true);
  });

  it("clears pending and running state on EPIPE without letting stale child events clear its replacement", async () => {
    const first = new FakeAppServerChild();
    const second = new FakeAppServerChild();
    const children = [first, second];
    let spawnIndex = 0;
    const { client, events, stateChanges } = makeClient(() => {
      const child = children[spawnIndex];
      spawnIndex += 1;
      if (!child) {
        throw new Error("unexpected extra app-server spawn");
      }
      return child.asChildProcess();
    });

    const started = client.request("turn/start", { thread_id: "thread-1" });
    expect(client.runningThreadCwds()).toEqual([tmpdir()]);
    first.stdout.write(
      `${JSON.stringify({
        id: "client-1",
        result: { turn: { status: "in_progress" } },
      })}\n`,
    );
    await expect(started).resolves.toEqual({
      turn: { status: "in_progress" },
    });
    expect(client.isBusy()).toBe(true);

    const pending = client.request("thread/list");
    const pendingRejection = expect(pending).rejects.toThrow(
      /stdin failed: write EPIPE/,
    );
    first.stdin.emit(
      "error",
      Object.assign(new Error("write EPIPE"), { code: "EPIPE" }),
    );
    await pendingRejection;
    expect(client.isBusy()).toBe(false);
    expect(serverExitEvents(events)).toHaveLength(1);
    expect(first.killed).toBe(true);

    const replacement = client.request("initialize");
    expect(spawnIndex).toBe(2);
    expect(client.isBusy()).toBe(true);
    const changesBeforeStaleEvents = stateChanges();

    first.stderr.write("late stderr from old child\n");
    first.stdout.write(
      `${JSON.stringify({ id: "client-3", result: { stale: true } })}\n`,
    );
    first.emit("exit", 1, null);
    first.emit("close", 1, null);

    expect(serverExitEvents(events)).toHaveLength(1);
    expect(stateChanges()).toBe(changesBeforeStaleEvents);
    expect(client.isBusy()).toBe(true);

    second.stdout.write(
      `${JSON.stringify({ id: "client-3", result: { ready: true } })}\n`,
    );
    await expect(replacement).resolves.toEqual({ ready: true });
    expect(client.isBusy()).toBe(false);
  });
});
