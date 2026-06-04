import {
  app,
  BrowserWindow,
  dialog,
  ipcMain,
  type OpenDialogOptions,
} from "electron";
import {
  closeSync,
  openSync,
  readdirSync,
  readSync,
  realpathSync,
  statSync,
  type Dirent,
} from "node:fs";
import {
  dirname,
  isAbsolute,
  join,
  relative,
  resolve,
} from "node:path";
import { fileURLToPath } from "node:url";
import type {
  ConfigCodexModelsResult,
  ConfigModelUpdateResult,
  GitCommitParams,
  GitPullRequestParams,
  FileTreeListResult,
  BuildInfoResult,
  CoreBuildInfo,
  DesktopBuildInfo,
  InputImage,
  InitializeResult,
  ServerEvent,
  TerminalSessionStartParams,
  Thread,
  Turn,
  WorkspaceDirectoryListResult,
  WorkspaceFileReadResult,
} from "../shared/protocol";
import { AppServerClientPool } from "./appServerClients";
import { GitService } from "./gitService";
import { ProjectManager } from "./projects";
import {
  registerRenderableFileProtocol,
  registerRenderableFileScheme,
} from "./renderableFileProtocol";
import { TerminalSessionManager } from "./terminalSessions";

const __dirname = dirname(fileURLToPath(import.meta.url));
const FILE_TREE_MAX_PATHS = 4000;
const FILE_PREVIEW_MAX_BYTES = 512 * 1024;
const MAIN_WINDOW_DEFAULT_WIDTH = 1280;
const MAIN_WINDOW_DEFAULT_HEIGHT = 920;
const MAIN_WINDOW_MIN_WIDTH = 980;
const MAIN_WINDOW_MIN_HEIGHT = 920;
const FILE_TREE_IGNORED_DIRS = new Set([
  ".git",
  ".next",
  ".turbo",
  ".vite",
  "coverage",
  "dist",
  "node_modules",
  "out",
  "target",
]);
const FILE_TREE_IGNORED_FILES = new Set([".DS_Store"]);

registerRenderableFileScheme();

let mainWindow: BrowserWindow | null = null;
const projectManager = new ProjectManager();
const gitService = new GitService(() => projectManager.ensureRuntimeContext());

// Build-time globals injected by electron.vite.config.ts. TypeScript
// doesn't know about them by default; declare them so we can reference
// them inside main. The "undefined" type keeps the same declaration
// valid for unit-test contexts where the define hasn't run.
declare const __DESKTOP_VERSION__: string | undefined;
declare const __DESKTOP_BUILD_DATE__: string | undefined;

const DESKTOP_BUILD_INFO: DesktopBuildInfo = {
  version:
    typeof __DESKTOP_VERSION__ === "string"
      ? __DESKTOP_VERSION__
      : "0.0.0-test",
  date:
    typeof __DESKTOP_BUILD_DATE__ === "string"
      ? __DESKTOP_BUILD_DATE__
      : "1970-01-01T00:00:00Z",
};

// Cached core build info. Populated on the first wuu:initialize call so
// the renderer can ask for build identity via wuu:build-info without
// racing the first initialize.
let cachedCoreBuildInfo: CoreBuildInfo | undefined;
let windowResizeEndTimer: NodeJS.Timeout | undefined;
let windowResizeState = false;
const appServerClientPool = new AppServerClientPool(
  () => projectManager.ensureRuntimeContext(),
  () => projectManager.activeWorkdir(),
  (event) => emitServerEvent(event),
);
const terminalSessionManager = new TerminalSessionManager(
  () => projectManager.ensureRuntimeContext(),
  (event) => emitTerminalEvent(event),
);

function fileTreeListResult(): FileTreeListResult {
  const context = projectManager.ensureRuntimeContext();
  const paths: string[] = [];
  const truncated = collectFileTreePaths(context.cwd, "", paths);
  return { root: context.cwd, paths, truncated };
}

function workspaceDirectoryListResult(
  path?: string,
): WorkspaceDirectoryListResult {
  const context = projectManager.ensureRuntimeContext();
  const relativeDirectoryPath = normalizeWorkspaceDirectoryPath(path ?? "");
  const absoluteDirectoryPath = resolveWorkspaceDirectoryPath(
    context.cwd,
    relativeDirectoryPath,
  );
  const stats = statSync(absoluteDirectoryPath);
  if (!stats.isDirectory()) {
    throw new Error("selected path is not a directory");
  }

  let entries: Dirent[];
  try {
    entries = readdirSync(absoluteDirectoryPath, { withFileTypes: true });
  } catch {
    entries = [];
  }

  entries.sort(compareFileTreeEntries);

  const visibleEntries = [];
  let truncated = false;
  for (const entry of entries) {
    if (FILE_TREE_IGNORED_FILES.has(entry.name)) {
      continue;
    }

    const relativePath = relativeDirectoryPath
      ? `${relativeDirectoryPath}/${entry.name}`
      : entry.name;
    if (entry.isDirectory()) {
      if (FILE_TREE_IGNORED_DIRS.has(entry.name)) {
        continue;
      }
      visibleEntries.push({
        name: entry.name,
        path: `${relativePath}/`,
        kind: "directory" as const,
      });
    } else if (entry.isFile() || entry.isSymbolicLink()) {
      visibleEntries.push({
        name: entry.name,
        path: relativePath,
        kind: "file" as const,
      });
    }

    if (visibleEntries.length >= FILE_TREE_MAX_PATHS) {
      truncated = true;
      break;
    }
  }

  return {
    root: context.cwd,
    path: relativeDirectoryPath,
    entries: visibleEntries,
    truncated,
  };
}

function readWorkspaceFileResult(path: string): WorkspaceFileReadResult {
  const context = projectManager.ensureRuntimeContext();
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
  const previewBuffer = buffer.subarray(
    0,
    truncated ? FILE_PREVIEW_MAX_BYTES : bytesRead,
  );
  const binary = previewBuffer.includes(0);

  return {
    root: context.cwd,
    path: relativeFilePath,
    absolute_path: absolutePath,
    size_bytes: stats.size,
    binary,
    truncated,
    text: binary ? undefined : previewBuffer.toString("utf8"),
  };
}

function normalizeWorkspaceRelativePath(path: string): string {
  const value = path
    .trim()
    .replace(/\\/g, "/")
    .replace(/^\/+/, "")
    .replace(/\/+$/, "");
  if (
    !value ||
    value.includes("\0") ||
    value.split("/").some((segment) => segment === "..")
  ) {
    throw new Error("invalid workspace file path");
  }
  return value;
}

function normalizeWorkspaceDirectoryPath(path: string): string {
  const value = path
    .trim()
    .replace(/\\/g, "/")
    .replace(/^\/+/, "")
    .replace(/\/+$/, "");
  if (
    value.includes("\0") ||
    value.split("/").some((segment) => segment === "..")
  ) {
    throw new Error("invalid workspace directory path");
  }
  return value;
}

function resolveWorkspacePath(root: string, relativeFilePath: string): string {
  const absolutePath = resolve(root, relativeFilePath);
  const relativeToRoot = relative(root, absolutePath);
  if (
    !relativeToRoot ||
    relativeToRoot.startsWith("..") ||
    isAbsolute(relativeToRoot)
  ) {
    throw new Error("file is outside the current workspace");
  }

  const realRoot = realpathSync(root);
  const realFile = realpathSync(absolutePath);
  const realRelative = relative(realRoot, realFile);
  if (
    !realRelative ||
    realRelative.startsWith("..") ||
    isAbsolute(realRelative)
  ) {
    throw new Error("file is outside the current workspace");
  }
  return absolutePath;
}

function resolveWorkspaceDirectoryPath(
  root: string,
  relativeDirectoryPath: string,
): string {
  const absolutePath = relativeDirectoryPath
    ? resolve(root, relativeDirectoryPath)
    : root;
  const relativeToRoot = relative(root, absolutePath);
  if (
    relativeToRoot &&
    (relativeToRoot.startsWith("..") || isAbsolute(relativeToRoot))
  ) {
    throw new Error("directory is outside the current workspace");
  }

  const realRoot = realpathSync(root);
  const realDirectory = realpathSync(absolutePath);
  const realRelative = relative(realRoot, realDirectory);
  if (
    realRelative &&
    (realRelative.startsWith("..") || isAbsolute(realRelative))
  ) {
    throw new Error("directory is outside the current workspace");
  }
  return absolutePath;
}

function compareFileTreeEntries(left: Dirent, right: Dirent): number {
  const leftDirectory = left.isDirectory();
  const rightDirectory = right.isDirectory();
  if (leftDirectory !== rightDirectory) {
    return leftDirectory ? -1 : 1;
  }
  return left.name.localeCompare(right.name, undefined, {
    sensitivity: "base",
  });
}

function collectFileTreePaths(
  root: string,
  relativeDirectory: string,
  paths: string[],
): boolean {
  if (paths.length >= FILE_TREE_MAX_PATHS) {
    return true;
  }

  const directoriesToRead = [relativeDirectory];
  for (let index = 0; index < directoriesToRead.length; index += 1) {
    const currentRelativeDirectory = directoriesToRead[index];
    const directory = currentRelativeDirectory
      ? join(root, currentRelativeDirectory)
      : root;
    let entries: Dirent[];
    try {
      entries = readdirSync(directory, { withFileTypes: true });
    } catch {
      continue;
    }

    entries.sort(compareFileTreeEntries);

    for (const entry of entries) {
      if (FILE_TREE_IGNORED_FILES.has(entry.name)) {
        continue;
      }

      const relativePath = currentRelativeDirectory
        ? `${currentRelativeDirectory}/${entry.name}`
        : entry.name;
      if (entry.isDirectory()) {
        if (FILE_TREE_IGNORED_DIRS.has(entry.name)) {
          continue;
        }
        paths.push(`${relativePath}/`);
        directoriesToRead.push(relativePath);
      } else if (entry.isFile() || entry.isSymbolicLink()) {
        paths.push(relativePath);
      }

      if (paths.length >= FILE_TREE_MAX_PATHS) {
        return true;
      }
    }
  }

  return false;
}

async function showProjectDirectoryDialog(
  options: OpenDialogOptions,
): Promise<string | undefined> {
  const result = mainWindow
    ? await dialog.showOpenDialog(mainWindow, options)
    : await dialog.showOpenDialog(options);
  if (result.canceled) {
    return undefined;
  }
  return result.filePaths[0];
}

function emitServerEvent(event: ServerEvent): void {
  if (
    !mainWindow ||
    mainWindow.isDestroyed() ||
    mainWindow.webContents.isDestroyed()
  ) {
    return;
  }
  mainWindow.webContents.send("wuu:server-event", event);
}

function emitTerminalEvent(
  event: Parameters<TerminalSessionManager["emit"]>[0],
): void {
  if (
    !mainWindow ||
    mainWindow.isDestroyed() ||
    mainWindow.webContents.isDestroyed()
  ) {
    return;
  }
  mainWindow.webContents.send("wuu:terminal-event", event);
}

function setWindowResizeState(resizing: boolean): void {
  if (windowResizeState === resizing) {
    return;
  }
  windowResizeState = resizing;
  if (
    !mainWindow ||
    mainWindow.isDestroyed() ||
    mainWindow.webContents.isDestroyed()
  ) {
    return;
  }
  mainWindow.webContents.send("wuu:window-resize-state", { resizing });
}

function scheduleWindowResizeEnd(delay = 140): void {
  if (windowResizeEndTimer) {
    clearTimeout(windowResizeEndTimer);
  }
  windowResizeEndTimer = setTimeout(() => {
    windowResizeEndTimer = undefined;
    setWindowResizeState(false);
  }, delay);
}

function createWindow(): void {
  mainWindow = new BrowserWindow({
    width: MAIN_WINDOW_DEFAULT_WIDTH,
    height: MAIN_WINDOW_DEFAULT_HEIGHT,
    minWidth: MAIN_WINDOW_MIN_WIDTH,
    minHeight: MAIN_WINDOW_MIN_HEIGHT,
    titleBarStyle: "hiddenInset",
    trafficLightPosition: { x: 18, y: 16 },
    backgroundColor: "#f6f6f4",
    webPreferences: {
      preload: join(__dirname, "../preload/index.cjs"),
      contextIsolation: true,
      nodeIntegration: false,
      webviewTag: true,
    },
  });

  mainWindow.on("will-resize", () => {
    setWindowResizeState(true);
    scheduleWindowResizeEnd();
  });
  mainWindow.on("resize", () => {
    setWindowResizeState(true);
    scheduleWindowResizeEnd();
  });
  mainWindow.on("resized", () => {
    scheduleWindowResizeEnd(40);
  });
  mainWindow.on("closed", () => {
    if (windowResizeEndTimer) {
      clearTimeout(windowResizeEndTimer);
      windowResizeEndTimer = undefined;
    }
    windowResizeState = false;
    mainWindow = null;
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
  projectManager.load();
  registerRenderableFileProtocol();

  ipcMain.handle("wuu:project-list", () => projectManager.list());
  ipcMain.handle("wuu:project-select", (_event, projectIDToSelect: string) =>
    projectManager.select(projectIDToSelect),
  );
  ipcMain.handle(
    "wuu:project-select-none",
    (_event, fresh?: boolean, cwd?: string) =>
      projectManager.selectNoProject(Boolean(fresh), cwd),
  );
  ipcMain.handle("wuu:git-status", () => gitService.status());
  ipcMain.handle("wuu:git-changes", () => gitService.changes());
  ipcMain.handle("wuu:git-file-diff", (_event, path: string) =>
    gitService.fileDiff(path),
  );
  ipcMain.handle("wuu:git-checkout-branch", (_event, branch: string) =>
    gitService.checkoutBranch(branch),
  );
  ipcMain.handle("wuu:git-create-checkout-branch", (_event, branch: string) =>
    gitService.createCheckoutBranch(branch),
  );
  ipcMain.handle("wuu:git-commit", (_event, params: GitCommitParams) =>
    gitService.commit(params ?? {}),
  );
  ipcMain.handle("wuu:git-create-pr", (_event, params: GitPullRequestParams) =>
    gitService.createPullRequest(params ?? {}),
  );
  ipcMain.handle("wuu:file-tree-list", () => fileTreeListResult());
  ipcMain.handle("wuu:file-directory-list", (_event, path?: string) =>
    workspaceDirectoryListResult(path),
  );
  ipcMain.handle("wuu:file-read", (_event, path: string) =>
    readWorkspaceFileResult(path),
  );
  ipcMain.handle(
    "wuu:terminal-start",
    (_event, params?: TerminalSessionStartParams) =>
      terminalSessionManager.start(params),
  );
  ipcMain.handle("wuu:terminal-write", (_event, id: string, data: string) =>
    terminalSessionManager.write(id, data),
  );
  ipcMain.handle(
    "wuu:terminal-resize",
    (_event, id: string, cols: number, rows: number) =>
      terminalSessionManager.resize(id, cols, rows),
  );
  ipcMain.handle("wuu:terminal-stop", (_event, id: string) =>
    terminalSessionManager.stop(id),
  );
  ipcMain.handle("wuu:project-choose-folder", async () => {
    const projectPath = await showProjectDirectoryDialog({
      title: "使用现有文件夹",
      buttonLabel: "使用文件夹",
      properties: ["openDirectory"],
    });
    if (!projectPath) {
      return projectManager.list();
    }
    return projectManager.add(projectPath);
  });
  ipcMain.handle("wuu:project-create-blank", async () => {
    const projectPath = await showProjectDirectoryDialog({
      title: "新建空白项目",
      buttonLabel: "创建项目",
      properties: ["openDirectory", "createDirectory"],
    });
    if (!projectPath) {
      return projectManager.list();
    }
    return projectManager.add(projectPath);
  });
  ipcMain.handle("wuu:initialize", async () => {
    const result = await appServerClientPool.request<InitializeResult>("initialize");
    if (result.core) {
      cachedCoreBuildInfo = result.core;
    }
    return result;
  });
  ipcMain.handle("wuu:build-info", (): BuildInfoResult => ({
    core: cachedCoreBuildInfo,
    desktop: DESKTOP_BUILD_INFO,
  }));
  ipcMain.handle("wuu:config-codex-models", (_event, provider?: string) =>
    appServerClientPool.request<ConfigCodexModelsResult>("config/codex/models", {
      provider: provider ?? "",
    }),
  );
  ipcMain.handle(
    "wuu:config-model-update",
    (
      _event,
      provider: string,
      model: string,
      effort?: string,
      connection?: {
        base_url?: string;
        api_key?: string;
        create_provider?: boolean;
      },
      variant?: string,
    ) =>
      appServerClientPool.request<ConfigModelUpdateResult>("config/model/update", {
        provider,
        model,
        ...(connection?.base_url === undefined
          ? {}
          : { base_url: connection.base_url }),
        ...(connection?.api_key === undefined
          ? {}
          : { api_key: connection.api_key }),
        ...(connection?.create_provider ? { create_provider: true } : {}),
        ...(effort === undefined ? {} : { effort }),
        ...(variant === undefined ? {} : { variant }),
      }),
  );
  ipcMain.handle("wuu:skill-list", () => appServerClientPool.request("skill/list"));
  ipcMain.handle("wuu:thread-start", () =>
    appServerClientPool.request<{ thread: Thread }>("thread/start"),
  );
  ipcMain.handle("wuu:thread-resume", (_event, sessionId?: string) =>
    appServerClientPool.request<{ thread: Thread }>("thread/resume", {
      session_id: sessionId ?? "",
    }),
  );
  ipcMain.handle(
    "wuu:thread-fork",
    (_event, threadId: string, turnId?: string, itemId?: string) =>
      appServerClientPool.request<{ thread: Thread }>("thread/fork", {
        thread_id: threadId,
        turn_id: turnId ?? "",
        item_id: itemId ?? "",
      }),
  );
  ipcMain.handle("wuu:thread-list", () =>
    appServerClientPool.request<{ threads: Thread[] }>("thread/list"),
  );
  ipcMain.handle("wuu:thread-search", (_event, query: string, limit?: number) =>
    appServerClientPool.request("thread/search", {
      query: query ?? "",
      limit: typeof limit === "number" ? limit : undefined,
    }),
  );
  ipcMain.handle(
    "wuu:thread-pin",
    (_event, threadId: string, pinned: boolean) =>
      appServerClientPool.request<{ thread: Thread }>("thread/pin", {
        thread_id: threadId,
        pinned,
      }),
  );
  ipcMain.handle(
    "wuu:thread-archive",
    (_event, threadId: string, archived: boolean) =>
      appServerClientPool.request<{ thread: Thread }>("thread/archive", {
        thread_id: threadId,
        archived,
      }),
  );
  ipcMain.handle(
    "wuu:turn-start",
    (_event, threadId: string, prompt: string, images?: InputImage[]) =>
      appServerClientPool.request<{ turn: Turn }>("turn/start", {
        thread_id: threadId,
        prompt,
        images: images ?? [],
      }),
  );
  ipcMain.handle("wuu:turn-interrupt", (_event, threadId: string) =>
    appServerClientPool.request<{ ok: boolean }>("turn/interrupt", {
      thread_id: threadId,
    }),
  );
  ipcMain.handle(
    "wuu:respond-server-request",
    (_event, id: string, result: unknown) => {
      appServerClientPool.respondToServerRequest(id, result);
    },
  );
  ipcMain.handle(
    "wuu:reject-server-request",
    (_event, id: string, message: string) => {
      appServerClientPool.rejectServerRequest(id, message);
    },
  );

  createWindow();

  app.on("activate", () => {
    if (BrowserWindow.getAllWindows().length === 0) {
      createWindow();
    }
  });
});

app.on("before-quit", () => {
  terminalSessionManager.cleanup();
  appServerClientPool.shutdown();
});

app.on("window-all-closed", () => {
  if (process.platform !== "darwin") {
    app.quit();
  }
});
