import { app, BrowserWindow, dialog, ipcMain, net, protocol, type OpenDialogOptions } from "electron";
import { spawn, spawnSync, type ChildProcessWithoutNullStreams } from "node:child_process";
import {
  closeSync,
  existsSync,
  mkdirSync,
  openSync,
  readdirSync,
  readFileSync,
  readSync,
  realpathSync,
  statSync,
  writeFileSync,
  type Dirent
} from "node:fs";
import { basename, dirname, extname, isAbsolute, join, relative, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import type {
  AppServerNotification,
  AppServerRequest,
  AppServerResponse,
  ConfigModelUpdateResult,
  DesktopProject,
  FileTreeListResult,
  GitStatusResult,
  InitializeResult,
  ProjectListResult,
  RuntimeContext,
  ServerEvent,
  Thread,
  Turn,
  WorkspaceFileReadResult
} from "../shared/protocol";

const __dirname = dirname(fileURLToPath(import.meta.url));
const RENDERABLE_IMAGE_EXTENSIONS = new Set([".apng", ".avif", ".gif", ".jpeg", ".jpg", ".png", ".svg", ".webp"]);
const FILE_TREE_MAX_PATHS = 4000;
const FILE_PREVIEW_MAX_BYTES = 512 * 1024;
const FILE_TREE_IGNORED_DIRS = new Set([
  ".git",
  ".next",
  ".turbo",
  ".vite",
  "coverage",
  "dist",
  "node_modules",
  "out",
  "target"
]);
const FILE_TREE_IGNORED_FILES = new Set([".DS_Store"]);

protocol.registerSchemesAsPrivileged([
  {
    scheme: "wuu-file",
    privileges: {
      standard: true,
      secure: true,
      supportFetchAPI: false
    }
  }
]);

type PendingRequest = {
  resolve: (value: unknown) => void;
  reject: (reason?: unknown) => void;
};

class AppServerClient {
  private child: ChildProcessWithoutNullStreams | null = null;
  private pending = new Map<string, PendingRequest>();
  private nextRequestID = 1;
  private stdoutBuffer = "";
  private disposing = false;

  constructor(
    readonly workdir: string,
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

  dispose(): void {
    this.disposing = true;
    for (const pending of this.pending.values()) {
      pending.reject(new Error("app-server stopped"));
    }
    this.pending.clear();
    this.shutdown();
  }

  private ensureStarted(): void {
    if (this.child && !this.child.killed) {
      return;
    }
    const command = resolveWuuCommand(this.workdir);
    this.child = spawn(command.command, [...command.args, "app-server", "--workdir", this.workdir], {
      cwd: command.cwd,
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
      if (!this.disposing) {
        this.emit({ kind: "server-exit", code });
      }
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
  cwd: string;
};

function resolveWuuCommand(workdir: string): WuuCommand {
  if (process.env.WUU_BIN) {
    return { command: process.env.WUU_BIN, args: [], cwd: workdir };
  }
  const sourceRoot = wuuSourceRoot();
  if (sourceRoot && process.env.WUU_DESKTOP_USE_GO_RUN !== "0") {
    return { command: "go", args: ["run", "./cmd/wuu"], cwd: sourceRoot };
  }
  for (const candidate of [join(workdir, "bin", "wuu"), join(workdir, "wuu")]) {
    if (existsSync(candidate)) {
      return { command: candidate, args: [], cwd: workdir };
    }
  }
  return { command: "wuu", args: [], cwd: workdir };
}

function wuuSourceRoot(): string | undefined {
  const candidates = [
    process.env.WUU_SOURCE_ROOT,
    process.cwd(),
    resolve(process.cwd(), ".."),
    app.getAppPath(),
    resolve(app.getAppPath(), ".."),
    resolve(__dirname, "..", "..", "..")
  ].filter((candidate): candidate is string => Boolean(candidate));
  return candidates.find((candidate) => existsSync(join(candidate, "go.mod")) && existsSync(join(candidate, "cmd", "wuu")));
}

type ProjectStore = {
  projects: DesktopProject[];
  active_context?: RuntimeContext;
};

let mainWindow: BrowserWindow | null = null;
let client: AppServerClient | null = null;
let projectStore: ProjectStore = { projects: [] };

function projectStorePath(): string {
  return join(app.getPath("userData"), "projects.json");
}

function loadProjectStore(): ProjectStore {
  try {
    const parsed = JSON.parse(readFileSync(projectStorePath(), "utf8")) as Partial<ProjectStore> & {
      active_project_id?: unknown;
    };
    const projects = Array.isArray(parsed.projects)
      ? parsed.projects.filter((project): project is DesktopProject => isDesktopProject(project))
      : [];
    const activeContext = normalizeRuntimeContext(parsed.active_context, projects) ?? legacyProjectContext(parsed.active_project_id, projects);
    return {
      projects,
      active_context: activeContext
    };
  } catch {
    return { projects: [] };
  }
}

function isDesktopProject(value: unknown): value is DesktopProject {
  if (!value || typeof value !== "object") {
    return false;
  }
  const project = value as Partial<DesktopProject>;
  return (
    typeof project.id === "string" &&
    typeof project.name === "string" &&
    typeof project.path === "string" &&
    typeof project.created_at === "string" &&
    typeof project.updated_at === "string"
  );
}

function normalizeRuntimeContext(value: unknown, projects: DesktopProject[]): RuntimeContext | undefined {
  if (!value || typeof value !== "object") {
    return undefined;
  }
  const context = value as Partial<RuntimeContext>;
  if (context.kind === "project" && typeof context.project_id === "string") {
    const project = projects.find((candidate) => candidate.id === context.project_id);
    return project ? { kind: "project", project_id: project.id, cwd: project.path } : undefined;
  }
  if (context.kind === "no_project" && typeof context.cwd === "string") {
    const cwd = resolve(context.cwd);
    mkdirSync(cwd, { recursive: true });
    return { kind: "no_project", cwd };
  }
  return undefined;
}

function legacyProjectContext(value: unknown, projects: DesktopProject[]): RuntimeContext | undefined {
  if (typeof value !== "string") {
    return undefined;
  }
  const project = projects.find((candidate) => candidate.id === value);
  return project ? { kind: "project", project_id: project.id, cwd: project.path } : undefined;
}

function saveProjectStore(): void {
  mkdirSync(dirname(projectStorePath()), { recursive: true });
  writeFileSync(projectStorePath(), `${JSON.stringify(projectStore, null, 2)}\n`);
}

function projectListResult(): ProjectListResult {
  const context = ensureRuntimeContext();
  return {
    projects: projectStore.projects,
    active_context: context,
    active_project_id: context.kind === "project" ? context.project_id : undefined
  };
}

function gitStatusResult(): GitStatusResult {
  const context = ensureRuntimeContext();
  const insideWorkTree = gitOutput(context.cwd, ["rev-parse", "--is-inside-work-tree"]) === "true";
  if (!insideWorkTree) {
    return { is_repo: false, dirty_count: 0 };
  }
  const branch = gitOutput(context.cwd, ["branch", "--show-current"]) || gitOutput(context.cwd, ["rev-parse", "--short", "HEAD"]);
  const branches = gitOutput(context.cwd, ["for-each-ref", "--format=%(refname:short)", "refs/heads"])
    ?.split("\n")
    .map((item) => item.trim())
    .filter(Boolean);
  const porcelain = gitOutput(context.cwd, ["status", "--porcelain"]);
  const dirtyCount = porcelain ? porcelain.split("\n").filter((line) => line.trim()).length : 0;
  return {
    is_repo: true,
    branch,
    branches,
    dirty_count: dirtyCount
  };
}

function checkoutGitBranch(branch: string): GitStatusResult {
  const context = ensureRuntimeContext();
  const current = gitStatusResult();
  const target = branch.trim();
  if (!current.is_repo) {
    throw new Error("current workspace is not a git repository");
  }
  if (!target || !current.branches?.includes(target)) {
    throw new Error("branch not found");
  }
  const result = spawnSync("git", ["-C", context.cwd, "checkout", target], {
    cwd: context.cwd,
    encoding: "utf8",
    env: process.env
  });
  if (result.status !== 0) {
    throw new Error(result.stderr.trim() || `failed to checkout ${target}`);
  }
  return gitStatusResult();
}

function gitOutput(cwd: string, args: string[]): string | undefined {
  const result = spawnSync("git", ["-C", cwd, ...args], {
    cwd,
    encoding: "utf8",
    env: process.env
  });
  if (result.status !== 0) {
    return undefined;
  }
  return result.stdout.trim() || undefined;
}

function fileTreeListResult(): FileTreeListResult {
  const context = ensureRuntimeContext();
  const paths: string[] = [];
  const truncated = collectFileTreePaths(context.cwd, "", paths);
  return { root: context.cwd, paths, truncated };
}

function readWorkspaceFileResult(path: string): WorkspaceFileReadResult {
  const context = ensureRuntimeContext();
  const relativeFilePath = normalizeWorkspaceRelativePath(path);
  const absolutePath = resolveWorkspacePath(context.cwd, relativeFilePath);
  const stats = statSync(absolutePath);
  if (!stats.isFile()) {
    throw new Error("selected path is not a file");
  }

  const readLimit = Math.min(stats.size, FILE_PREVIEW_MAX_BYTES + 1);
  const buffer = Buffer.alloc(readLimit);
  const descriptor = openSync(absolutePath, "r");
  let bytesRead = 0;
  try {
    bytesRead = readSync(descriptor, buffer, 0, readLimit, 0);
  } finally {
    closeSync(descriptor);
  }
  const truncated = stats.size > FILE_PREVIEW_MAX_BYTES;
  const previewBuffer = buffer.subarray(0, truncated ? FILE_PREVIEW_MAX_BYTES : bytesRead);
  const binary = previewBuffer.includes(0);

  return {
    root: context.cwd,
    path: relativeFilePath,
    absolute_path: absolutePath,
    size_bytes: stats.size,
    binary,
    truncated,
    text: binary ? undefined : previewBuffer.toString("utf8")
  };
}

function normalizeWorkspaceRelativePath(path: string): string {
  const value = path.trim().replace(/\\/g, "/").replace(/^\/+/, "").replace(/\/+$/, "");
  if (!value || value.includes("\0") || value.split("/").some((segment) => segment === "..")) {
    throw new Error("invalid workspace file path");
  }
  return value;
}

function resolveWorkspacePath(root: string, relativeFilePath: string): string {
  const absolutePath = resolve(root, relativeFilePath);
  const relativeToRoot = relative(root, absolutePath);
  if (!relativeToRoot || relativeToRoot.startsWith("..") || isAbsolute(relativeToRoot)) {
    throw new Error("file is outside the current workspace");
  }

  const realRoot = realpathSync(root);
  const realFile = realpathSync(absolutePath);
  const realRelative = relative(realRoot, realFile);
  if (!realRelative || realRelative.startsWith("..") || isAbsolute(realRelative)) {
    throw new Error("file is outside the current workspace");
  }
  return absolutePath;
}

function collectFileTreePaths(root: string, relativeDirectory: string, paths: string[]): boolean {
  if (paths.length >= FILE_TREE_MAX_PATHS) {
    return true;
  }

  const directory = relativeDirectory ? join(root, relativeDirectory) : root;
  let entries: Dirent[];
  try {
    entries = readdirSync(directory, { withFileTypes: true });
  } catch {
    return false;
  }

  entries.sort((left, right) => {
    const leftDirectory = left.isDirectory();
    const rightDirectory = right.isDirectory();
    if (leftDirectory !== rightDirectory) {
      return leftDirectory ? -1 : 1;
    }
    return left.name.localeCompare(right.name, undefined, { sensitivity: "base" });
  });

  for (const entry of entries) {
    if (FILE_TREE_IGNORED_FILES.has(entry.name)) {
      continue;
    }

    const relativePath = relativeDirectory ? `${relativeDirectory}/${entry.name}` : entry.name;
    if (entry.isDirectory()) {
      if (FILE_TREE_IGNORED_DIRS.has(entry.name)) {
        continue;
      }
      paths.push(`${relativePath}/`);
      if (collectFileTreePaths(root, relativePath, paths)) {
        return true;
      }
      continue;
    }

    if (entry.isFile() || entry.isSymbolicLink()) {
      paths.push(relativePath);
    }

    if (paths.length >= FILE_TREE_MAX_PATHS) {
      return true;
    }
  }

  return false;
}

function ensureRuntimeContext(): RuntimeContext {
  const activeContext = projectStore.active_context;
  if (activeContext?.kind === "project") {
    const project = projectStore.projects.find((candidate) => candidate.id === activeContext.project_id);
    if (project) {
      projectStore.active_context = { kind: "project", project_id: project.id, cwd: project.path };
      return projectStore.active_context;
    }
  }
  if (projectStore.active_context?.kind === "no_project") {
    mkdirSync(projectStore.active_context.cwd, { recursive: true });
    return projectStore.active_context;
  }
  projectStore.active_context = createNoProjectContext();
  saveProjectStore();
  return projectStore.active_context;
}

function createNoProjectContext(): RuntimeContext {
  return { kind: "no_project", cwd: allocateNoProjectCwd() };
}

function allocateNoProjectCwd(): string {
  const baseDir = join(app.getPath("documents"), "Wuu", formatLocalDate(new Date()));
  mkdirSync(baseDir, { recursive: true });
  for (let index = 0; index < 1000; index += 1) {
    const name = index === 0 ? "new-chat" : `new-chat-${index + 1}`;
    const candidate = join(baseDir, name);
    if (existsSync(candidate)) {
      continue;
    }
    mkdirSync(candidate, { recursive: true });
    return candidate;
  }
  throw new Error(`failed to allocate no-project workspace under ${baseDir}`);
}

function formatLocalDate(date: Date): string {
  const year = String(date.getFullYear());
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}

function projectID(projectPath: string): string {
  return Buffer.from(resolve(projectPath)).toString("base64url");
}

function projectName(projectPath: string): string {
  return basename(projectPath) || projectPath;
}

function isDirectory(projectPath: string): boolean {
  try {
    return statSync(projectPath).isDirectory();
  } catch {
    return false;
  }
}

function addProject(projectPath: string): ProjectListResult {
  const resolvedPath = resolve(projectPath);
  if (!isDirectory(resolvedPath)) {
    throw new Error("selected project is not a directory");
  }
  const now = new Date().toISOString();
  const id = projectID(resolvedPath);
  const existingIndex = projectStore.projects.findIndex((project) => project.id === id);
  const project: DesktopProject = {
    id,
    name: projectName(resolvedPath),
    path: resolvedPath,
    created_at: existingIndex >= 0 ? projectStore.projects[existingIndex].created_at : now,
    updated_at: now
  };
  if (existingIndex >= 0) {
    projectStore.projects[existingIndex] = project;
  } else {
    projectStore.projects = [project, ...projectStore.projects];
  }
  projectStore.active_context = { kind: "project", project_id: id, cwd: resolvedPath };
  resetClient();
  saveProjectStore();
  return projectListResult();
}

function selectProject(projectIDToSelect: string): ProjectListResult {
  const project = projectStore.projects.find((candidate) => candidate.id === projectIDToSelect);
  if (!project) {
    throw new Error("project not found");
  }
  projectStore.active_context = { kind: "project", project_id: project.id, cwd: project.path };
  resetClient();
  saveProjectStore();
  return projectListResult();
}

function selectNoProject(fresh: boolean): ProjectListResult {
  if (fresh || projectStore.active_context?.kind !== "no_project") {
    projectStore.active_context = createNoProjectContext();
  }
  resetClient();
  saveProjectStore();
  return projectListResult();
}

function resetClient(): void {
  client?.dispose();
  client = null;
}

async function showProjectDirectoryDialog(options: OpenDialogOptions): Promise<string | undefined> {
  const result = mainWindow ? await dialog.showOpenDialog(mainWindow, options) : await dialog.showOpenDialog(options);
  if (result.canceled) {
    return undefined;
  }
  return result.filePaths[0];
}

function serverClient(): AppServerClient {
  const context = ensureRuntimeContext();
  if (!client || client.workdir !== context.cwd) {
    resetClient();
    client = new AppServerClient(context.cwd, emitServerEvent);
  }
  return client;
}

function emitServerEvent(event: ServerEvent): void {
  if (!mainWindow || mainWindow.isDestroyed() || mainWindow.webContents.isDestroyed()) {
    return;
  }
  mainWindow.webContents.send("wuu:server-event", event);
}

function createWindow(): void {
  mainWindow = new BrowserWindow({
    width: 1280,
    height: 860,
    minWidth: 980,
    minHeight: 460,
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

function registerRenderableFileProtocol(): void {
  protocol.handle("wuu-file", (request) => {
    const filePath = filePathFromRenderableURL(request.url);
    if (!filePath || !isRenderableImageFile(filePath)) {
      return new Response("Not found", { status: 404 });
    }
    return net.fetch(pathToFileURL(filePath).toString());
  });
}

function filePathFromRenderableURL(rawURL: string): string | undefined {
  try {
    const url = new URL(rawURL);
    if (url.hostname !== "local") {
      return undefined;
    }
    const encodedPath = url.pathname.replace(/^\/+/, "");
    if (!encodedPath) {
      return undefined;
    }
    return Buffer.from(encodedPath, "base64url").toString("utf8");
  } catch {
    return undefined;
  }
}

function isRenderableImageFile(filePath: string): boolean {
  try {
    return statSync(filePath).isFile() && RENDERABLE_IMAGE_EXTENSIONS.has(extname(filePath).toLowerCase());
  } catch {
    return false;
  }
}

app.whenReady().then(() => {
  projectStore = loadProjectStore();
  registerRenderableFileProtocol();

  ipcMain.handle("wuu:project-list", () => projectListResult());
  ipcMain.handle("wuu:project-select", (_event, projectIDToSelect: string) => selectProject(projectIDToSelect));
  ipcMain.handle("wuu:project-select-none", (_event, fresh?: boolean) => selectNoProject(Boolean(fresh)));
  ipcMain.handle("wuu:git-status", () => gitStatusResult());
  ipcMain.handle("wuu:git-checkout-branch", (_event, branch: string) => checkoutGitBranch(branch));
  ipcMain.handle("wuu:file-tree-list", () => fileTreeListResult());
  ipcMain.handle("wuu:file-read", (_event, path: string) => readWorkspaceFileResult(path));
  ipcMain.handle("wuu:project-choose-folder", async () => {
    const projectPath = await showProjectDirectoryDialog({
      title: "使用现有文件夹",
      buttonLabel: "使用文件夹",
      properties: ["openDirectory"]
    });
    if (!projectPath) {
      return projectListResult();
    }
    return addProject(projectPath);
  });
  ipcMain.handle("wuu:project-create-blank", async () => {
    const projectPath = await showProjectDirectoryDialog({
      title: "新建空白项目",
      buttonLabel: "创建项目",
      properties: ["openDirectory", "createDirectory"]
    });
    if (!projectPath) {
      return projectListResult();
    }
    return addProject(projectPath);
  });
  ipcMain.handle("wuu:initialize", () => serverClient().request<InitializeResult>("initialize"));
  ipcMain.handle("wuu:config-model-update", (_event, provider: string, model: string) =>
    serverClient().request<ConfigModelUpdateResult>("config/model/update", { provider, model })
  );
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
