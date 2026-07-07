import { describe, expect, it } from "vitest";

import {
  RemoteChild,
  RemoteChildStream,
  RemoteHostEvent,
  RemoteHostManager,
  RemoteSpawn,
} from "./remoteControl";

class FakeStream implements RemoteChildStream {
  private listeners: Array<(chunk: string) => void> = [];

  setEncoding(): void {}

  on(_event: "data", listener: (chunk: string) => void): void {
    this.listeners.push(listener);
  }

  push(chunk: string): void {
    for (const listener of this.listeners) {
      listener(chunk);
    }
  }
}

class FakeChild implements RemoteChild {
  stdout = new FakeStream();
  stderr = new FakeStream();
  signals: string[] = [];
  exitOnKill = true;
  private exitListeners: Array<(code: number | null) => void> = [];
  private errorListeners: Array<(err: Error) => void> = [];
  private exited = false;

  constructor(
    readonly command: string,
    readonly args: string[],
  ) {}

  on(event: "exit" | "error", listener: ((code: number | null) => void) | ((err: Error) => void)): void {
    if (event === "exit") {
      this.exitListeners.push(listener as (code: number | null) => void);
    } else {
      this.errorListeners.push(listener as (err: Error) => void);
    }
  }

  kill(signal?: NodeJS.Signals): boolean {
    this.signals.push(signal ?? "SIGTERM");
    if (this.exitOnKill) {
      queueMicrotask(() => this.exit(0));
    }
    return true;
  }

  exit(code: number | null): void {
    if (this.exited) {
      return;
    }
    this.exited = true;
    for (const listener of this.exitListeners) {
      listener(code);
    }
  }

  fail(err: Error): void {
    for (const listener of this.errorListeners) {
      listener(err);
    }
  }
}

function makeManager(overrides: { onEvent?: (ev: RemoteHostEvent) => void } = {}) {
  const children: FakeChild[] = [];
  const spawn: RemoteSpawn = (command, args) => {
    const child = new FakeChild(command, args);
    children.push(child);
    return child;
  };
  const events: RemoteHostEvent[] = [];
  const manager = new RemoteHostManager({
    spawn,
    resolveCommand: (workdir) => ({ command: "wuu", args: [], cwd: workdir }),
    env: {},
    onEvent: (ev) => {
      events.push(ev);
      overrides.onEvent?.(ev);
    },
  });
  return { manager, children, events };
}

async function flush(): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, 0));
}

describe("RemoteHostManager.status", () => {
  it("parses the status JSON and passes the right argv", async () => {
    const { manager, children } = makeManager();
    const statusPromise = manager.status("/work/dir");
    await flush();
    const child = children[0];
    expect(child.args).toEqual(["remote", "status", "--json"]);
    child.stdout.push(
      JSON.stringify({
        fingerprint: "abc123",
        relay_url: "ws://relay/v1/connect",
        store: "/home/.wuu/remote.json",
        devices: [{ pub: "PUB", fingerprint: "fff", name: "phone", added_at: "2026-07-07T00:00:00Z" }],
      }),
    );
    child.exit(0);
    const status = await statusPromise;
    expect(status.fingerprint).toBe("abc123");
    expect(status.devices).toHaveLength(1);
    expect(status.devices[0].name).toBe("phone");
  });

  it("rejects with stderr when the CLI fails", async () => {
    const { manager, children } = makeManager();
    const statusPromise = manager.status("/work/dir");
    await flush();
    children[0].stderr.push("error: no relay configured\n");
    children[0].exit(1);
    await expect(statusPromise).rejects.toThrow(/no relay configured/);
  });
});

describe("RemoteHostManager host lifecycle", () => {
  it("captures the pairing URI and the paired event", async () => {
    const { manager, children, events } = makeManager();
    manager.startHost("/work/dir", { pair: true });
    const child = children[0];
    expect(child.args).toEqual(["remote", "host", "--workdir", "/work/dir", "--pair"]);
    expect(manager.isRunning()).toBe(true);
    expect(manager.currentPairUri()).toBeNull();

    child.stdout.push("pairing uri (scan or pass to `wuu remote phone pair --uri`):\n");
    child.stdout.push("wuu://pair?v=1&p=abc&k=KEY&h=HOST&r=ws%3A%2F%2Frelay\n");
    expect(manager.currentPairUri()).toBe("wuu://pair?v=1&p=abc&k=KEY&h=HOST&r=ws%3A%2F%2Frelay");
    expect(events.some((e) => e.kind === "pair-uri")).toBe(true);

    child.stdout.push("paired: my phone (2579e6ff1255)\n");
    expect(manager.currentPairUri()).toBeNull();
    const paired = events.find((e) => e.kind === "paired");
    expect(paired && paired.kind === "paired" ? paired.detail : "").toBe("my phone (2579e6ff1255)");
  });

  it("handles split lines across stdout chunks", () => {
    const { manager, children } = makeManager();
    manager.startHost("/w");
    const child = children[0];
    child.stdout.push("wuu://pair?v=1");
    expect(manager.currentPairUri()).toBeNull();
    child.stdout.push("&p=xyz\n");
    expect(manager.currentPairUri()).toBe("wuu://pair?v=1&p=xyz");
  });

  it("stopHost terminates the child and clears state", async () => {
    const { manager, children, events } = makeManager();
    manager.startHost("/w", { pair: true });
    children[0].stdout.push("wuu://pair?v=1&p=abc\n");
    await manager.stopHost();
    expect(children[0].signals).toContain("SIGTERM");
    expect(manager.isRunning()).toBe(false);
    expect(manager.currentPairUri()).toBeNull();
    expect(events.some((e) => e.kind === "host-exit")).toBe(true);
  });

  it("reports unexpected exits and clears the pairing window", async () => {
    const { manager, children, events } = makeManager();
    manager.startHost("/w", { pair: true });
    children[0].stdout.push("wuu://pair?v=1&p=abc\n");
    children[0].exit(1);
    await flush();
    expect(manager.isRunning()).toBe(false);
    expect(manager.currentPairUri()).toBeNull();
    const exit = events.find((e) => e.kind === "host-exit");
    expect(exit && exit.kind === "host-exit" ? exit.code : null).toBe(1);
  });

  it("startHost is idempotent while a host is running", () => {
    const { manager, children } = makeManager();
    manager.startHost("/w");
    manager.startHost("/w");
    expect(children).toHaveLength(1);
  });
});

describe("RemoteHostManager.removeDevice", () => {
  it("revokes and restarts a running host without reopening pairing", async () => {
    const { manager, children } = makeManager();
    manager.startHost("/work/dir", { pair: true });
    expect(children).toHaveLength(1);

    const removePromise = manager.removeDevice("/work/dir", "2579e6ff1255");
    await flush();
    const removeChild = children[1];
    expect(removeChild.args).toEqual(["remote", "devices", "remove", "2579e6ff1255"]);
    removeChild.exit(0);
    await removePromise;

    // Old host got SIGTERM'd; a fresh one is running without --pair.
    expect(children[0].signals).toContain("SIGTERM");
    expect(children).toHaveLength(3);
    expect(children[2].args).toEqual(["remote", "host", "--workdir", "/work/dir"]);
    expect(manager.isRunning()).toBe(true);
  });

  it("does not start a host when none was running", async () => {
    const { manager, children } = makeManager();
    const removePromise = manager.removeDevice("/w", "fp");
    await flush();
    children[0].exit(0);
    await removePromise;
    expect(children).toHaveLength(1);
    expect(manager.isRunning()).toBe(false);
  });
});
