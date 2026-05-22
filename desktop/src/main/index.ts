import { app, BrowserWindow, ipcMain } from "electron";
import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { existsSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import type {
  AppServerNotification,
  AppServerRequest,
  AppServerResponse,
  InitializeResult,
  ServerEvent,
  Thread,
  Turn
} from "../shared/protocol";

const __dirname = dirname(fileURLToPath(import.meta.url));

type PendingRequest = {
  resolve: (value: unknown) => void;
  reject: (reason?: unknown) => void;
};

class AppServerClient {
  private child: ChildProcessWithoutNullStreams | null = null;
  private pending = new Map<string, PendingRequest>();
  private nextRequestID = 1;
  private stdoutBuffer = "";

  constructor(
    private readonly workdir: string,
    private readonly emit: (event: ServerEvent) => void
  ) {}

  request<T>(method: string, params?: unknown): Promise<T> {
    this.ensureStarted();
    const id = `client-${this.nextRequestID++}`;
    const payload: AppServerRequest = { id, method, params };
    return new Promise<T>((resolveRequest, rejectRequest) => {
      this.pending.set(JSON.stringify(id), {
        resolve: (value) => resolveRequest(value as T),
        reject: rejectRequest
      });
      this.write(payload);
    });
  }

  respond(id: string, result: unknown): void {
    this.ensureStarted();
    this.write({ id, result });
  }

  reject(id: string, message: string): void {
    this.ensureStarted();
    this.write({
      id,
      error: {
        code: "error",
        message
      }
    });
  }

  shutdown(): void {
    if (!this.child) {
      return;
    }
    try {
      this.write({ id: "shutdown", method: "shutdown" });
    } catch {
      this.child.kill();
    }
  }

  private ensureStarted(): void {
    if (this.child && !this.child.killed) {
      return;
    }
    const command = resolveWuuCommand(this.workdir);
    this.child = spawn(command.command, [...command.args, "app-server", "--workdir", this.workdir], {
      cwd: this.workdir,
      env: process.env,
      stdio: ["pipe", "pipe", "pipe"]
    });

    this.child.stdout.setEncoding("utf8");
    this.child.stdout.on("data", (chunk: string) => this.readStdout(chunk));
    this.child.stderr.setEncoding("utf8");
    this.child.stderr.on("data", (chunk: string) => {
      const message = chunk.trim();
      if (message) {
        this.emit({ kind: "server-error", message });
      }
    });
    this.child.on("exit", (code) => {
      for (const pending of this.pending.values()) {
        pending.reject(new Error("app-server exited"));
      }
      this.pending.clear();
      this.emit({ kind: "server-exit", code });
      this.child = null;
    });
  }

  private write(payload: unknown): void {
    if (!this.child) {
      throw new Error("app-server is not running");
    }
    this.child.stdin.write(`${JSON.stringify(payload)}\n`);
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
    let message: AppServerResponse | AppServerNotification | Required<AppServerRequest>;
    try {
      message = JSON.parse(line);
    } catch {
      this.emit({ kind: "server-error", message: `Invalid app-server JSON: ${line}` });
      return;
    }

    const maybeRequest = message as Required<AppServerRequest>;
    if (maybeRequest.method && maybeRequest.id !== undefined) {
      this.emit({ kind: "server-request", message: maybeRequest });
      return;
    }

    const maybeNotification = message as AppServerNotification;
    if (maybeNotification.method) {
      this.emit({ kind: "notification", message: maybeNotification });
      return;
    }

    const response = message as AppServerResponse;
    const key = JSON.stringify(response.id);
    const pending = this.pending.get(key);
    if (!pending) {
      return;
    }
    this.pending.delete(key);
    if (response.error) {
      pending.reject(new Error(response.error.message));
      return;
    }
    pending.resolve(response.result);
  }
}

type WuuCommand = {
  command: string;
  args: string[];
};

function resolveWuuCommand(workdir: string): WuuCommand {
  if (process.env.WUU_BIN) {
    return { command: process.env.WUU_BIN, args: [] };
  }
  if (existsSync(join(workdir, "go.mod")) && process.env.WUU_DESKTOP_USE_GO_RUN !== "0") {
    return { command: "go", args: ["run", "./cmd/wuu"] };
  }
  for (const candidate of [join(workdir, "bin", "wuu"), join(workdir, "wuu")]) {
    if (existsSync(candidate)) {
      return { command: candidate, args: [] };
    }
  }
  return { command: "wuu", args: [] };
}

function defaultWorkdir(): string {
  if (process.env.WUU_WORKDIR) {
    return resolve(process.env.WUU_WORKDIR);
  }
  const candidates = [
    process.cwd(),
    resolve(process.cwd(), ".."),
    app.getAppPath(),
    resolve(app.getAppPath(), ".."),
    resolve(__dirname, "..", "..", "..")
  ];
  for (const candidate of candidates) {
    if (existsSync(join(candidate, "go.mod"))) {
      return candidate;
    }
  }
  return process.cwd();
}

let mainWindow: BrowserWindow | null = null;
let client: AppServerClient | null = null;

function serverClient(): AppServerClient {
  if (!client) {
    client = new AppServerClient(defaultWorkdir(), (event) => {
      mainWindow?.webContents.send("wuu:server-event", event);
    });
  }
  return client;
}

function createWindow(): void {
  mainWindow = new BrowserWindow({
    width: 1280,
    height: 860,
    minWidth: 980,
    minHeight: 680,
    titleBarStyle: "hiddenInset",
    trafficLightPosition: { x: 18, y: 18 },
    backgroundColor: "#f6f6f4",
    webPreferences: {
      preload: join(__dirname, "../preload/index.cjs"),
      contextIsolation: true,
      nodeIntegration: false
    }
  });

  if (!app.isPackaged) {
    mainWindow.webContents.on("console-message", (_event, _level, message) => {
      if (message) {
        console.error(`[renderer] ${message}`);
      }
    });
    mainWindow.webContents.on("preload-error", (_event, preloadPath, error) => {
      console.error(`[preload] ${preloadPath}: ${error.message}`);
    });
  }

  if (!app.isPackaged && process.env.ELECTRON_RENDERER_URL) {
    mainWindow.loadURL(process.env.ELECTRON_RENDERER_URL);
  } else {
    mainWindow.loadFile(join(__dirname, "../renderer/index.html"));
  }
}

app.whenReady().then(() => {
  ipcMain.handle("wuu:initialize", () => serverClient().request<InitializeResult>("initialize"));
  ipcMain.handle("wuu:thread-start", () => serverClient().request<{ thread: Thread }>("thread/start"));
  ipcMain.handle("wuu:thread-resume", (_event, sessionId?: string) =>
    serverClient().request<{ thread: Thread }>("thread/resume", { session_id: sessionId ?? "" })
  );
  ipcMain.handle("wuu:thread-list", () => serverClient().request<{ threads: Thread[] }>("thread/list"));
  ipcMain.handle("wuu:turn-start", (_event, threadId: string, prompt: string) =>
    serverClient().request<{ turn: Turn }>("turn/start", { thread_id: threadId, prompt })
  );
  ipcMain.handle("wuu:turn-interrupt", (_event, threadId: string) =>
    serverClient().request<{ ok: boolean }>("turn/interrupt", { thread_id: threadId })
  );
  ipcMain.handle("wuu:respond-server-request", (_event, id: string, result: unknown) => {
    serverClient().respond(id, result);
  });
  ipcMain.handle("wuu:reject-server-request", (_event, id: string, message: string) => {
    serverClient().reject(id, message);
  });

  createWindow();

  app.on("activate", () => {
    if (BrowserWindow.getAllWindows().length === 0) {
      createWindow();
    }
  });
});

app.on("before-quit", () => {
  client?.shutdown();
});

app.on("window-all-closed", () => {
  if (process.platform !== "darwin") {
    app.quit();
  }
});
