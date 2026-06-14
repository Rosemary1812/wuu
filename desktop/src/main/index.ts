import {
  app,
  BrowserWindow,
  dialog,
  ipcMain,
  type OpenDialogOptions,
} from "electron";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import type {
  ConfigCodexModelsResult,
  ConfigModelUpdateResult,
  GitCommitParams,
  GitPullRequestParams,
  BuildInfoResult,
  CoreBuildInfo,
  DesktopBuildInfo,
  InputFile,
  InputImage,
  InitializeResult,
  LoopSnapshotResult,
  LoopWorktreeCleanupResult,
  LoopWorktreeMergeResult,
  LoopWorktreeRollbackResult,
  LoopWorktreeReviewResult,
  ManagedProcessListResult,
  ManagedProcessStopResult,
  ServerEvent,
  TerminalSessionStartParams,
  Thread,
  Turn,
} from "../shared/protocol";
import { AppServerClientPool } from "./appServerClients";
import { GitService } from "./gitService";
import { ProjectManager } from "./projects";
import {
  registerRenderableFileProtocol,
  registerRenderableFileScheme,
} from "./renderableFileProtocol";
import { TerminalSessionManager } from "./terminalSessions";
import { WorkspaceFileService } from "./workspaceFiles";

const __dirname = dirname(fileURLToPath(import.meta.url));
const MAIN_WINDOW_DEFAULT_WIDTH = 1280;
const MAIN_WINDOW_DEFAULT_HEIGHT = 920;
const MAIN_WINDOW_MIN_WIDTH = 980;
const MAIN_WINDOW_MIN_HEIGHT = 920;
registerRenderableFileScheme();

let mainWindow: BrowserWindow | null = null;
const projectManager = new ProjectManager();
const gitService = new GitService(() => projectManager.ensureRuntimeContext());
const workspaceFiles = new WorkspaceFileService(() =>
  projectManager.ensureRuntimeContext(),
);

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
  ipcMain.handle("wuu:file-tree-list", () => workspaceFiles.fileTreeList());
  ipcMain.handle("wuu:file-directory-list", (_event, path?: string) =>
    workspaceFiles.directoryList(path),
  );
  ipcMain.handle("wuu:file-read", (_event, path: string) =>
    workspaceFiles.readFile(path),
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
      toolPolicyProfile?: string,
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
        ...(toolPolicyProfile === undefined
          ? {}
          : { tool_policy_profile: toolPolicyProfile }),
      }),
  );
  ipcMain.handle("wuu:skill-list", () => appServerClientPool.request("skill/list"));
  ipcMain.handle("wuu:loop-snapshot", (_event, threadId?: string) =>
    appServerClientPool.request<LoopSnapshotResult>("loop/snapshot", {
      thread_id: threadId ?? "",
    }),
  );
  ipcMain.handle("wuu:loop-worktree-review", (_event, worktreePath: string) =>
    appServerClientPool.request<LoopWorktreeReviewResult>("loop/worktree/review", {
      worktree_path: worktreePath,
    }),
  );
  ipcMain.handle(
    "wuu:loop-worktree-cleanup",
    (
      _event,
      worktreePath: string,
      confirmUserApproved: boolean,
      confirmRemoveCleanWorktree: boolean,
    ) =>
      appServerClientPool.request<LoopWorktreeCleanupResult>("loop/worktree/cleanup", {
        worktree_path: worktreePath,
        confirm_user_approved: confirmUserApproved,
        confirm_remove_clean_worktree: confirmRemoveCleanWorktree,
      }),
  );
  ipcMain.handle(
    "wuu:loop-worktree-rollback",
    (
      _event,
      worktreePath: string,
      confirmUserApproved: boolean,
      confirmDiscardWorktreeChanges: boolean,
    ) =>
      appServerClientPool.request<LoopWorktreeRollbackResult>("loop/worktree/rollback", {
        worktree_path: worktreePath,
        confirm_user_approved: confirmUserApproved,
        confirm_discard_worktree_changes: confirmDiscardWorktreeChanges,
      }),
  );
  ipcMain.handle(
    "wuu:loop-worktree-merge",
    (
      _event,
      worktreePath: string,
      confirmUserApproved: boolean,
      confirmApplyWorktreeDiff: boolean,
      confirmTargetRepoMutation: boolean,
    ) =>
      appServerClientPool.request<LoopWorktreeMergeResult>("loop/worktree/merge", {
        worktree_path: worktreePath,
        confirm_user_approved: confirmUserApproved,
        confirm_apply_worktree_diff: confirmApplyWorktreeDiff,
        confirm_target_repo_mutation: confirmTargetRepoMutation,
      }),
  );
  ipcMain.handle("wuu:process-list", () =>
    appServerClientPool.request<ManagedProcessListResult>("process/list"),
  );
  ipcMain.handle("wuu:process-stop", (_event, processID: string) =>
    appServerClientPool.request<ManagedProcessStopResult>("process/stop", {
      process_id: processID,
    }),
  );
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
    (_event, threadId: string, prompt: string, images?: InputImage[], files?: InputFile[]) =>
      appServerClientPool.request<{ turn: Turn }>("turn/start", {
        thread_id: threadId,
        prompt,
        images: images ?? [],
        files: files ?? [],
      }),
  );
  ipcMain.handle(
    "wuu:turn-queue",
    (
      _event,
      threadId: string,
      prompt: string,
      images?: InputImage[],
      clientId?: string,
      files?: InputFile[],
    ) =>
      appServerClientPool.request("turn/queue", {
        thread_id: threadId,
        prompt,
        images: images ?? [],
        files: files ?? [],
        client_id: clientId,
      }),
  );
  ipcMain.handle(
    "wuu:turn-dequeue",
    (_event, threadId: string, queueId: string) =>
      appServerClientPool.request<{ ok: boolean }>("turn/dequeue", {
        thread_id: threadId,
        queue_id: queueId,
      }),
  );
  ipcMain.handle(
    "wuu:turn-steer",
    (
      _event,
      threadId: string,
      expectedTurnId: string,
      prompt: string,
      images?: InputImage[],
      clientId?: string,
      files?: InputFile[],
    ) =>
      appServerClientPool.request("turn/steer", {
        thread_id: threadId,
        expected_turn_id: expectedTurnId,
        prompt,
        images: images ?? [],
        files: files ?? [],
        client_id: clientId,
      }),
  );
  ipcMain.handle("wuu:turn-unsteer", (_event, threadId: string, steerId: string) =>
    appServerClientPool.request<{ ok: boolean }>("turn/unsteer", {
      thread_id: threadId,
      steer_id: steerId,
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
