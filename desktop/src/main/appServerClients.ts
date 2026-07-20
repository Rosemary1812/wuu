import { app } from "electron";
import {
  spawn as spawnChild,
  type ChildProcessWithoutNullStreams,
} from "node:child_process";
import { existsSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { resolveWuuCommand } from "./wuuCommand";
import type {
  AppServerNotification,
  AppServerRequest,
  AppServerResponse,
  RuntimeContext,
  ServerEvent,
} from "../shared/protocol";

const __dirname = dirname(fileURLToPath(import.meta.url));
const MAX_APP_SERVER_CLIENTS = 3;

type AppServerSpawnOptions = {
  cwd: string;
  env: NodeJS.ProcessEnv;
  stdio: ["pipe", "pipe", "pipe"];
};

export type AppServerSpawn = (
  command: string,
  args: readonly string[],
  options: AppServerSpawnOptions,
) => ChildProcessWithoutNullStreams;

const defaultSpawnAppServer: AppServerSpawn = (command, args, options) =>
  spawnChild(command, args, options);

type PendingRequest = {
  method: string;
  params?: unknown;
  resolve: (value: unknown) => void;
  reject: (reason?: unknown) => void;
};

export type AppServerClientEvent =
  | { kind: "notification"; message: AppServerNotification }
  | { kind: "server-request"; message: Required<AppServerRequest> }
  | { kind: "server-error"; message: string }
  | { kind: "server-exit"; code: number | null; message: string };

type ServerRequestRoute = {
  client: AppServerClient;
  serverID: string;
};

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export class AppServerClientPool {
  private clients = new Map<string, AppServerClient>();
  private nextServerRequestRouteID = 1;
  private serverRequestRoutes = new Map<string, ServerRequestRoute>();
  // Optional cross-cutting hooks wired after construction (index.ts) so the
  // embedded browser coordinator can (a) veto idle-eviction of a workdir that
  // still owns agent tabs and (b) tear down that workdir's views on dispose.
  private isWorkdirPinned?: (workdir: string) => boolean;
  private clientTorndownHandler?: (workdir: string) => void;

  constructor(
    private readonly getRuntimeContext: () => RuntimeContext,
    private readonly getActiveWorkdir: () => string | undefined,
    private readonly emitToRenderer: (event: ServerEvent) => void,
  ) {}

  // A workdir the check pins (e.g. it owns a live agent browser tab) is treated
  // as busy and never idle-evicted, so a page can't vanish mid user-takeover.
  setWorkdirPinnedCheck(check: (workdir: string) => boolean): void {
    this.isWorkdirPinned = check;
  }

  // Fired whenever a client is disposed (idle-evict, workdir removal, shutdown).
  // This is the authoritative teardown signal for view recycling: the disposing
  // flag suppresses the server-exit event on these paths, so a server-exit
  // listener alone would miss them.
  setClientTorndownHandler(handler: (workdir: string) => void): void {
    this.clientTorndownHandler = handler;
  }

  request<T>(method: string, params?: unknown): Promise<T> {
    return this.client().request<T>(method, params);
  }

  requestInContext<T>(
    context: RuntimeContext,
    method: string,
    params?: unknown,
  ): Promise<T> {
    return this.clientForContext(context).request<T>(method, params);
  }

  requestForWorkdir<T>(workdir: string, method: string, params?: unknown): Promise<T> {
    const client = this.clients.get(resolve(workdir));
    if (!client) {
      return Promise.reject(new Error("activity workspace is no longer connected"));
    }
    return client.request<T>(method, params);
  }

  runningThreadCwds(): string[] {
    const cwds = new Set<string>();
    for (const client of this.clients.values()) {
      for (const cwd of client.runningThreadCwds()) {
        cwds.add(cwd);
      }
    }
    return [...cwds];
  }

  threadCwdsForWorkdir(workdir: string): string[] {
    return this.clients.get(resolve(workdir))?.knownThreadCwds() ?? [];
  }

  respondToServerRequest(id: string, result: unknown): void {
    const route = this.serverRequestRoutes.get(id);
    if (!route) {
      throw new Error("server request is no longer active");
    }
    this.serverRequestRoutes.delete(id);
    route.client.respond(route.serverID, result);
  }

  rejectServerRequest(id: string, message: string): void {
    const route = this.serverRequestRoutes.get(id);
    if (!route) {
      throw new Error("server request is no longer active");
    }
    this.serverRequestRoutes.delete(id);
    route.client.reject(route.serverID, message);
  }

  shutdown(): void {
    for (const client of this.clients.values()) {
      client.dispose();
      this.clientTorndownHandler?.(client.workdir);
    }
    this.clients.clear();
    this.serverRequestRoutes.clear();
  }

  /**
   * Dispose the pooled app server (if any) that runs in the given workdir.
   * Used when a project is removed and its local state is about to be
   * cleaned up: the removed workspace's server must not stay alive and
   * recreate runtime state mid-cleanup.
   */
  disposeWorkdirClient(workdir: string): void {
    const client = this.clients.get(resolve(workdir));
    if (client) {
      this.disposeClient(client);
    }
  }

  private client(): AppServerClient {
    return this.clientForContext(this.getRuntimeContext());
  }

  private clientForContext(context: RuntimeContext): AppServerClient {
    const workdir = resolve(context.cwd);
    // Only registered projects carry a stable id; the no-project (对话)
    // workspace stays path-keyed (its scratch dir never moves).
    const workspaceId = context.kind === "project" ? context.project_id : "";
    let client = this.clients.get(workdir);
    if (!client) {
      client = new AppServerClient(
        workdir,
        workspaceId,
        (source, event) => this.emitServerEvent(source, event),
        () => this.evictIdleClients(),
      );
      this.clients.set(workdir, client);
    }
    client.touch();
    this.evictIdleClients();
    return client;
  }

  private emitServerEvent(
    client: AppServerClient,
    event: AppServerClientEvent,
  ): void {
    this.emitToRenderer(this.routeServerEvent(client, event));
  }

  private routeServerEvent(
    client: AppServerClient,
    event: AppServerClientEvent,
  ): ServerEvent {
    if (event.kind !== "server-request") {
      return { ...event, workdir: client.workdir };
    }
    const publicID = `server-request-${this.nextServerRequestRouteID++}`;
    this.serverRequestRoutes.set(publicID, {
      client,
      serverID: event.message.id,
    });
    return {
      ...event,
      workdir: client.workdir,
      message: {
        ...event.message,
        id: publicID,
      },
    };
  }

  private evictIdleClients(): void {
    if (this.clients.size <= MAX_APP_SERVER_CLIENTS) {
      return;
    }
    const activeWorkdir = this.getActiveWorkdir();
    const idleClients = [...this.clients.values()]
      .filter(
        (client) =>
          client.workdir !== activeWorkdir &&
          !client.isBusy() &&
          !this.isWorkdirPinned?.(client.workdir),
      )
      .sort((a, b) => a.lastUsed() - b.lastUsed());
    for (const client of idleClients) {
      if (this.clients.size <= MAX_APP_SERVER_CLIENTS) {
        return;
      }
      this.disposeClient(client);
    }
  }

  private disposeClient(client: AppServerClient): void {
    this.clients.delete(client.workdir);
    this.dropServerRequestRoutesForClient(client);
    client.dispose();
    // After the routes are dropped so any view-recycle broadcast the handler
    // fires can't collide with an in-flight reply for this client.
    this.clientTorndownHandler?.(client.workdir);
  }

  private dropServerRequestRoutesForClient(client: AppServerClient): void {
    for (const [id, route] of this.serverRequestRoutes) {
      if (route.client === client) {
        this.serverRequestRoutes.delete(id);
      }
    }
  }
}

export class AppServerClient {
  private child: ChildProcessWithoutNullStreams | null = null;
  private pending = new Map<string, PendingRequest>();
  private threadCwdsByID = new Map<string, string>();
  private runningThreadIDs = new Set<string>();
  private nextRequestID = 1;
  private stdoutBuffer = "";
  private disposing = false;
  private lastUsedAt = Date.now();
  private lastStderr = "";
  private stoppedActivityIDs = new Set<string>();

  constructor(
    readonly workdir: string,
    // Stable workspace identity for a registered project, forwarded to the
    // app-server so its state dir and session listing survive the project
    // moving on disk. Empty for the no-project (对话) workspace, which stays
    // keyed by its (fixed) path.
    readonly workspaceId: string,
    private readonly emit: (
      client: AppServerClient,
      event: AppServerClientEvent,
    ) => void,
    private readonly onStateChange: () => void,
    private readonly spawnAppServer: AppServerSpawn = defaultSpawnAppServer,
    private readonly resolveCommand: typeof resolveWuuCommand = resolveWuuCommand,
  ) {}

  request<T>(method: string, params?: unknown): Promise<T> {
    this.touch();
    this.ensureStarted();
    const id = `client-${this.nextRequestID++}`;
    const payload: AppServerRequest = { id, method, params };
    return new Promise<T>((resolveRequest, rejectRequest) => {
      this.pending.set(JSON.stringify(id), {
        method,
        params,
        resolve: (value) => resolveRequest(value as T),
        reject: rejectRequest,
      });
      this.write(payload);
    });
  }

  respond(id: string, result: unknown): void {
    this.touch();
    this.ensureStarted();
    this.write({ id, result });
  }

  reject(id: string, message: string): void {
    this.touch();
    this.ensureStarted();
    this.write({
      id,
      error: {
        code: "error",
        message,
      },
    });
  }

  shutdown(): void {
    const child = this.child;
    if (!child) {
      return;
    }
    try {
      this.write({ id: "shutdown", method: "shutdown" });
    } catch {
      child.kill();
    }
  }

  dispose(): void {
    this.disposing = true;
    for (const pending of this.pending.values()) {
      pending.reject(new Error("app-server stopped"));
    }
    this.pending.clear();
    this.shutdown();
  }

  touch(): void {
    this.lastUsedAt = Date.now();
  }

  lastUsed(): number {
    return this.lastUsedAt;
  }

  isBusy(): boolean {
    return this.pending.size > 0 || this.runningThreadIDs.size > 0;
  }

  knownThreadCwds(): string[] {
    return [...new Set([this.workdir, ...this.threadCwdsByID.values()])];
  }

  runningThreadCwds(): string[] {
    const cwds = new Set(
      [...this.runningThreadIDs].map(
        (threadID) => this.threadCwdsByID.get(threadID) ?? this.workdir,
      ),
    );
    for (const pending of this.pending.values()) {
      if (
        pending.method !== "turn/start" &&
        pending.method !== "thread/start" &&
        pending.method !== "thread/resume" &&
        pending.method !== "thread/fork" &&
        pending.method !== "thread/edit-message"
      ) {
        continue;
      }
      const params = isRecord(pending.params) ? pending.params : undefined;
      const threadID =
        typeof params?.thread_id === "string"
          ? params.thread_id
          : typeof params?.session_id === "string"
            ? params.session_id
            : undefined;
      cwds.add(
        (threadID && this.threadCwdsByID.get(threadID)) ?? this.workdir,
      );
    }
    return [...cwds];
  }

  private ensureStarted(): void {
    if (this.child) {
      if (!this.child.killed) {
        return;
      }
      this.finalizeChild(
        this.child,
        null,
        "wuu core process was terminated before it exited",
      );
    }
    const sourceRoot = wuuSourceRoot();
    const resourcesPath = (process as { resourcesPath?: string }).resourcesPath;
    const command = this.resolveCommand(
      process.env,
      this.workdir,
      sourceRoot,
      resourcesPath,
    );
    this.stoppedActivityIDs.clear();
    const appServerArgs = [
      ...command.args,
      "app-server",
      "--workdir",
      this.workdir,
    ];
    if (this.workspaceId.trim() !== "") {
      appServerArgs.push("--workspace-id", this.workspaceId);
    }
    const child = this.spawnAppServer(command.command, appServerArgs, {
      cwd: command.cwd,
      env: cuaMacHelperEnvironment(
        process.env,
        sourceRoot,
        resourcesPath,
        process.platform,
      ),
      stdio: ["pipe", "pipe", "pipe"],
    });
    this.child = child;
    this.stdoutBuffer = "";
    this.lastStderr = "";

    child.stdout.setEncoding("utf8");
    child.stdout.on("data", (chunk: string) => {
      if (this.child === child) {
        this.readStdout(chunk);
      }
    });
    child.stdout.on("error", (error) => {
      this.finalizeChild(
        child,
        null,
        appServerProcessError("stdout failed", error),
        true,
      );
    });
    child.stderr.setEncoding("utf8");
    child.stderr.on("data", (chunk: string) => {
      if (this.child !== child) {
        return;
      }
      const message = chunk.trim();
      if (message) {
        this.lastStderr = `${this.lastStderr}\n${message}`.trim().slice(-4000);
        this.emit(this, { kind: "server-error", message });
      }
    });
    child.stderr.on("error", (error) => {
      this.finalizeChild(
        child,
        null,
        appServerProcessError("stderr failed", error),
        true,
      );
    });
    child.stdin.on("error", (error) => {
      this.finalizeChild(
        child,
        null,
        appServerProcessError("stdin failed", error),
        true,
      );
    });
    child.on("error", (error) => {
      this.finalizeChild(
        child,
        null,
        appServerProcessError("process failed", error),
        true,
      );
    });
    child.on("exit", (code) => {
      this.finalizeChild(child, code);
    });
    child.on("close", (code) => {
      this.finalizeChild(child, code);
    });
  }

  private write(payload: unknown): void {
    const child = this.child;
    if (!child) {
      throw new Error("app-server is not running");
    }
    try {
      child.stdin.write(`${JSON.stringify(payload)}\n`, (error) => {
        if (error) {
          this.finalizeChild(
            child,
            null,
            appServerProcessError("stdin write failed", error),
            true,
          );
        }
      });
    } catch (error) {
      this.finalizeChild(
        child,
        null,
        appServerProcessError("stdin write failed", error),
        true,
      );
      throw error;
    }
  }

  private finalizeChild(
    child: ChildProcessWithoutNullStreams,
    code: number | null,
    failureDetail = "",
    terminateChild = false,
  ): void {
    if (this.child !== child) {
      return;
    }

    const detail = [this.lastStderr.trim(), failureDetail.trim()]
      .filter(Boolean)
      .join("\n");
    const message = appServerExitMessage(code, detail);
    const pending = [...this.pending.values()];

    // Clear process-owned state before invoking callbacks. A rejection or
    // renderer event may immediately start a replacement process; late
    // error/exit/close events from this child must not touch that new child.
    this.child = null;
    this.stdoutBuffer = "";
    this.lastStderr = "";
    this.pending.clear();
    this.threadCwdsByID.clear();
    this.runningThreadIDs.clear();

    if (terminateChild && !child.killed) {
      try {
        child.kill();
      } catch {
        // The process may already be gone (for example ENOENT). Its state is
        // still finalized locally, and late child events are identity-guarded.
      }
    }

    for (const request of pending) {
      request.reject(new Error(message));
    }
    if (!this.disposing) {
      this.emit(this, { kind: "server-exit", code, message });
    }
    this.onStateChange();
  }

  private readStdout(chunk: string): void {
    this.stdoutBuffer += chunk;
    for (;;) {
      const index = this.stdoutBuffer.indexOf("\n");
      if (index < 0) {
        return;
      }
      const line = this.stdoutBuffer.slice(0, index).trim();
      this.stdoutBuffer = this.stdoutBuffer.slice(index + 1);
      if (line) {
        this.handleLine(line);
      }
    }
  }

  private handleLine(line: string): void {
    let message:
      AppServerResponse | AppServerNotification | Required<AppServerRequest>;
    try {
      message = JSON.parse(line);
    } catch {
      this.emit(this, {
        kind: "server-error",
        message: `Invalid app-server JSON: ${line}`,
      });
      return;
    }

    const maybeRequest = message as Required<AppServerRequest>;
    if (maybeRequest.method && maybeRequest.id !== undefined) {
      const rejection = activityServerRequestRejection(
        maybeRequest,
        this.stoppedActivityIDs,
      );
      if (rejection) {
        this.reject(maybeRequest.id, rejection);
        return;
      }
      this.emit(this, { kind: "server-request", message: maybeRequest });
      return;
    }

    const maybeNotification = message as AppServerNotification;
    if (maybeNotification.method) {
      updateStoppedActivityIDs(this.stoppedActivityIDs, maybeNotification);
      this.updateRunningFromNotification(maybeNotification);
      this.emit(this, { kind: "notification", message: maybeNotification });
      this.onStateChange();
      return;
    }

    const response = message as AppServerResponse;
    const key = JSON.stringify(response.id);
    const pending = this.pending.get(key);
    if (!pending) {
      return;
    }
    this.pending.delete(key);
    this.updateRunningFromResponse(
      pending.method,
      pending.params,
      response.result,
    );
    this.onStateChange();
    if (response.error) {
      pending.reject(new Error(response.error.message));
      return;
    }
    pending.resolve(response.result);
  }

  private updateRunningFromNotification(message: AppServerNotification): void {
    const params = isRecord(message.params) ? message.params : undefined;
    const threadID =
      typeof params?.thread_id === "string" ? params.thread_id : undefined;
    switch (message.method) {
      case "turn/started":
        if (threadID) {
          this.runningThreadIDs.add(threadID);
        }
        return;
      case "turn/completed":
      case "turn/error":
        if (threadID) {
          this.runningThreadIDs.delete(threadID);
        }
        return;
      case "thread/started":
      case "thread/resumed":
        this.updateRunningFromThread(params?.thread);
        return;
    }
  }

  private updateRunningFromResponse(
    method: string,
    params: unknown,
    result: unknown,
  ): void {
    if (
      method === "thread/list" &&
      isRecord(result) &&
      Array.isArray(result.threads)
    ) {
      for (const thread of result.threads) {
        this.updateRunningFromThread(thread);
      }
      return;
    }
    if (
      (method === "thread/start" ||
        method === "thread/resume" ||
        method === "thread/fork" ||
        method === "thread/edit-message") &&
      isRecord(result)
    ) {
      this.updateRunningFromThread(result.thread);
      return;
    }
    if (
      method === "turn/start" &&
      isRecord(params) &&
      typeof params.thread_id === "string"
    ) {
      const turn = isRecord(result) ? result.turn : undefined;
      if (isRecord(turn) && turn.status === "in_progress") {
        this.runningThreadIDs.add(params.thread_id);
      }
    }
  }

  private updateRunningFromThread(value: unknown): void {
    if (!isRecord(value) || typeof value.id !== "string") {
      return;
    }
    if (typeof value.cwd === "string" && value.cwd !== "") {
      this.threadCwdsByID.set(value.id, value.cwd);
    }
    if (value.status === "in_progress") {
      this.runningThreadIDs.add(value.id);
    } else {
      this.runningThreadIDs.delete(value.id);
    }
  }
}

export function cuaMacHelperEnvironment(
  env: NodeJS.ProcessEnv,
  sourceRoot: string | undefined,
  resourcesPath: string | undefined,
  platform: NodeJS.Platform,
  exists: (path: string) => boolean = existsSync,
): NodeJS.ProcessEnv {
  const result = { ...env };
  if (platform !== "darwin" || result.WUU_CUA_MAC_HELPER) {
    return result;
  }
  const candidates = [
    resourcesPath ? join(resourcesPath, "bin", "wuu-cua-mac") : undefined,
    sourceRoot ? join(sourceRoot, "desktop", "build", "bin", "wuu-cua-mac") : undefined,
  ].filter((candidate): candidate is string => Boolean(candidate));
  const helper = candidates.find(exists);
  if (helper) {
    result.WUU_CUA_MAC_HELPER = helper;
  }
  return result;
}

export function updateStoppedActivityIDs(
  stoppedActivityIDs: Set<string>,
  notification: AppServerNotification,
): void {
  if (notification.method !== "activity/stopped" || !isRecord(notification.params)) {
    return;
  }
  const activityID = notification.params.id;
  if (typeof activityID === "string" && activityID.trim() !== "") {
    stoppedActivityIDs.add(activityID);
  }
}

export function activityServerRequestRejection(
  request: Required<AppServerRequest>,
  stoppedActivityIDs: ReadonlySet<string>,
): string | undefined {
  if (!isRecord(request.params)) {
    return undefined;
  }
  const activityID = request.params.activity_id;
  if (typeof activityID !== "string" || !stoppedActivityIDs.has(activityID)) {
    return undefined;
  }
  return `activity ${activityID} is stopped`;
}

export function appServerExitMessage(
  code: number | null,
  stderr: string,
): string {
  const detail = stderr.trim();
  const prefix = `wuu core exited${code === null ? "" : ` (code ${code})`}`;
  return detail ? `${prefix}: ${detail}` : prefix;
}

function appServerProcessError(context: string, error: unknown): string {
  const message = error instanceof Error ? error.message : String(error);
  return `${context}: ${message}`;
}

function wuuSourceRoot(): string | undefined {
  const candidates = [
    process.env.WUU_SOURCE_ROOT,
    process.cwd(),
    resolve(process.cwd(), ".."),
    app.getAppPath(),
    resolve(app.getAppPath(), ".."),
    resolve(__dirname, "..", "..", ".."),
  ].filter((candidate): candidate is string => Boolean(candidate));
  return candidates.find(
    (candidate) =>
      existsSync(join(candidate, "go.mod")) &&
      existsSync(join(candidate, "cmd", "wuu")),
  );
}
