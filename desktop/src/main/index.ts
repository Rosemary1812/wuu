import {
  app,
  BrowserWindow,
  dialog,
  ipcMain,
  session as electronSession,
  type OpenDialogOptions,
  shell,
} from "electron";
import { readdir, rm, stat } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import type {
  ComposerGoalSummary,
  ConfigAdvancedUpdateResult,
  ConfigGeneralUpdateResult,
  ConfigCodexModelsResult,
  ConfigModelUpdateResult,
  GitCommitParams,
  GitPullRequestParams,
  BuildInfoResult,
  CoreBuildInfo,
  DesktopBuildInfo,
  InputFile,
  CliAutoInstallResult,
  CliInstallStatus,
  InputImage,
  InitializeResult,
  InstructionsListResult,
  MCPListResult,
  MCPServerActionResult,
  ParticipantFeedbackResult,
  ParticipantListResult,
  ParticipantResetResult,
  ParticipantRetireResult,
  ParticipantSaveParams,
  ParticipantSaveResult,
  ParticipantStartParams,
  ParticipantStartResult,
  ServerEvent,
  RuntimeAdvancedSettingsUpdate,
  RuntimeGeneralSettingsUpdate,
  SettingsUsageQuery,
  SettingsUsageRange,
  SettingsUsageResponse,
  TerminalSessionStartParams,
  Thread,
  ThreadContextCompositionResult,
  ThreadEditMessageResult,
  ThreadForkResult,
  ThreadListSubResult,
  ThreadOpenSubResult,
  ThreadResolveSubResult,
  ThreadStartParams,
  Turn,
} from "../shared/protocol";
import { AppServerClientPool } from "./appServerClients";
import { autoInstallCli, getCliInstallStatus, installCli } from "./cliInstall";
import {
  getCliAutoInstallEnabled,
  getThemePreference,
  setCliAutoInstallEnabled,
  setThemePreference,
  type ThemePreference,
} from "./desktopSettings";
import { GitService } from "./gitService";
import { ProjectManager, wuuHomePath } from "./projects";
import {
  registerRenderableFileProtocol,
  registerRenderableFileScheme,
} from "./renderableFileProtocol";
import { TerminalSessionManager } from "./terminalSessions";
import { WorkspaceFileService } from "./workspaceFiles";

const __dirname = dirname(fileURLToPath(import.meta.url));
const MAIN_WINDOW_DEFAULT_WIDTH = 1280;
const MAIN_WINDOW_DEFAULT_HEIGHT = 920;
const DEV_CACHE_CLEANUP_THRESHOLD_BYTES = 512 * 1024 * 1024;
const DEV_CACHE_DIRECTORIES = ["Cache", "Code Cache", "GPUCache", "DawnCache"];
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
// Outcome of this session's startup CLI auto-install pass. Surfaced through
// wuu:cli-install-status so the settings page can show a one-time,
// non-blocking note when the pass actually installed or repaired the link.
let lastCliAutoInstall: CliAutoInstallResult | null = null;
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

async function clearOversizedDevCaches(): Promise<void> {
  if (app.isPackaged || process.env.WUU_DESKTOP_DISABLE_DEV_CACHE_CLEANUP === "1") {
    return;
  }
  const userData = app.getPath("userData");
  const totalBytes = await cacheDirectoriesSize(userData, DEV_CACHE_DIRECTORIES);
  if (totalBytes < DEV_CACHE_CLEANUP_THRESHOLD_BYTES) {
    return;
  }
  try {
    await Promise.all([
      electronSession.defaultSession.clearCache(),
      electronSession.fromPartition("persist:wuu-browser").clearCache(),
    ]);
    await Promise.all(
      DEV_CACHE_DIRECTORIES.map((dir) =>
        rm(join(userData, dir), { recursive: true, force: true }),
      ),
    );
    console.info(
      `[desktop] cleared oversized dev cache (${Math.round(totalBytes / 1024 / 1024)} MB)`,
    );
  } catch (error) {
    console.warn(
      `[desktop] failed to clear oversized dev cache: ${error instanceof Error ? error.message : String(error)}`,
    );
  }
}

async function cacheDirectoriesSize(root: string, names: string[]): Promise<number> {
  let total = 0;
  for (const name of names) {
    total += await directorySize(join(root, name));
  }
  return total;
}

async function directorySize(path: string): Promise<number> {
  let info;
  try {
    info = await stat(path);
  } catch {
    return 0;
  }
  if (!info.isDirectory()) {
    return info.size;
  }
  let total = 0;
  let entries;
  try {
    entries = await readdir(path, { withFileTypes: true });
  } catch {
    return 0;
  }
  for (const entry of entries) {
    total += await directorySize(join(path, entry.name));
  }
  return total;
}

app.whenReady().then(async () => {
  await clearOversizedDevCaches();
  projectManager.load();
  registerRenderableFileProtocol();

  // Startup CLI install pass (default on, toggleable in settings). Runs in
  // the background so it never delays window creation; autoInstallCli is
  // idempotent, self-repairs dangling links after an app move/update, and
  // never overwrites a wuu binary the desktop does not own. Windows returns
  // "unsupported" without touching the filesystem.
  if (getCliAutoInstallEnabled()) {
    void autoInstallCli()
      .then((result) => {
        lastCliAutoInstall = result;
      })
      .catch(() => {
        // autoInstallCli reports failures as outcomes; this is a last-resort
        // guard so startup can never be affected.
      });
  }

  ipcMain.handle("wuu:project-list", () => projectManager.list());
  ipcMain.handle("wuu:project-select", (_event, projectIDToSelect: string) =>
    projectManager.select(projectIDToSelect),
  );
  ipcMain.handle("wuu:project-remove", (_event, projectIDToRemove: string) =>
    projectManager.remove(projectIDToRemove),
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
  ipcMain.handle("wuu:file-tree-list", (_event, root?: string) =>
    workspaceFiles.fileTreeList(root),
  );
  ipcMain.handle(
    "wuu:file-directory-list",
    (_event, path?: string, root?: string) =>
      workspaceFiles.directoryList(path, root),
  );
  ipcMain.handle("wuu:file-read", (_event, path: string, root?: string) =>
    workspaceFiles.readFile(path, root),
  );
  ipcMain.handle(
    "wuu:file-reference-resolve",
    (_event, reference: string, root?: string) =>
      workspaceFiles.resolveFileReference(reference, root),
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
  ipcMain.handle("wuu:open-external", async (_event, url: string) => {
    // Hand the URL to the OS default browser. We only accept http(s) to
    // avoid the renderer escalating arbitrary protocols (file://,
    // custom-scheme deeplinks, etc.) through this channel. Anything
    // non-conforming is silently dropped — the renderer is the
    // producer of all URLs (collectTurnSources parses them from
    // tool_call results), so a malformed value is a bug, not a user
    // decision; failing silently keeps the assistant turn UI clean.
    if (typeof url !== "string" || url.length === 0) return;
    if (!/^https?:\/\//i.test(url)) return;
    await shell.openExternal(url);
  });
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
        auth_token?: string;
        type?: string;
        create_provider?: boolean;
      },
      variant?: string,
      permissionMode?: string,
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
        ...(connection?.auth_token === undefined
          ? {}
          : { auth_token: connection.auth_token }),
        ...(connection?.type === undefined ? {} : { type: connection.type }),
        ...(connection?.create_provider ? { create_provider: true } : {}),
        ...(effort === undefined ? {} : { effort }),
        ...(variant === undefined ? {} : { variant }),
        ...(permissionMode === undefined
          ? {}
          : { permission_mode: permissionMode }),
      }),
  );
  ipcMain.handle(
    "wuu:config-advanced-update",
    (_event, settings: RuntimeAdvancedSettingsUpdate) =>
      appServerClientPool.request<ConfigAdvancedUpdateResult>(
        "config/advanced/update",
        settings ?? {},
      ),
  );
  ipcMain.handle(
    "wuu:config-general-update",
    (_event, settings: RuntimeGeneralSettingsUpdate) =>
      appServerClientPool.request<ConfigGeneralUpdateResult>(
        "config/general/update",
        settings ?? {},
      ),
  );
  ipcMain.handle(
    "wuu:config-provider-remove",
    (
      _event,
      provider: string,
      options?: { fallbackProvider?: string; fallbackModel?: string },
    ) =>
      appServerClientPool.request<ConfigModelUpdateResult>(
        "config/provider/remove",
        {
          provider,
          ...(options?.fallbackProvider
            ? { fallback_provider: options.fallbackProvider }
            : {}),
          ...(options?.fallbackModel
            ? { fallback_model: options.fallbackModel }
            : {}),
        },
      ),
  );
  ipcMain.handle("wuu:skill-list", () => appServerClientPool.request("skill/list"));
  ipcMain.handle(
    "wuu:settings-usage",
    (_event, range?: SettingsUsageRange) =>
      appServerClientPool.request<SettingsUsageResponse>(
        "settings/usage",
        { range } satisfies SettingsUsageQuery,
      ),
  );
  ipcMain.handle("wuu:mcp-list", () =>
    appServerClientPool.request<MCPListResult>("mcp/list"),
  );
  ipcMain.handle("wuu:mcp-connect", (_event, name: string) =>
    appServerClientPool.request<MCPServerActionResult>("mcp/connect", { name }),
  );
  ipcMain.handle("wuu:mcp-disconnect", (_event, name: string) =>
    appServerClientPool.request<MCPServerActionResult>("mcp/disconnect", { name }),
  );
  ipcMain.handle("wuu:mcp-refresh", (_event, name: string) =>
    appServerClientPool.request<MCPServerActionResult>("mcp/refresh", { name }),
  );
  ipcMain.handle(
    "wuu:thread-start",
    (_event, params?: ThreadStartParams) =>
      appServerClientPool.request<{ thread: Thread }>("thread/start", params ?? {}),
  );
  ipcMain.handle("wuu:thread-resume", (_event, sessionId?: string) =>
    appServerClientPool.request<{ thread: Thread }>("thread/resume", {
      session_id: sessionId ?? "",
    }),
  );
  ipcMain.handle("wuu:participant-start", (_event, params: ParticipantStartParams) =>
    appServerClientPool.request<ParticipantStartResult>("participant/start", params),
  );
  ipcMain.handle(
    "wuu:thread-fork",
    (
      _event,
      threadId: string,
      turnId?: string,
      itemId?: string,
      mode?: "local" | "worktree",
    ) =>
      appServerClientPool.request<ThreadForkResult>("thread/fork", {
        thread_id: threadId,
        turn_id: turnId ?? "",
        item_id: itemId ?? "",
        ...(mode ? { mode } : {}),
      }),
  );
  ipcMain.handle(
    "wuu:thread-edit-message",
    (_event, threadId: string, turnId: string, itemId: string) =>
      appServerClientPool.request<ThreadEditMessageResult>("thread/edit-message", {
        thread_id: threadId,
        turn_id: turnId,
        item_id: itemId,
      }),
  );
  ipcMain.handle("wuu:thread-context-composition", (_event, threadId: string) =>
    appServerClientPool.request<ThreadContextCompositionResult>("thread/context-composition", {
      thread_id: threadId,
    }),
  );
  ipcMain.handle("wuu:instructions-list", () =>
    appServerClientPool.request<InstructionsListResult>("instructions/list"),
  );
  ipcMain.handle(
    "wuu:cli-install-status",
    async (): Promise<CliInstallStatus> => ({
      ...(await getCliInstallStatus()),
      auto_install_enabled: getCliAutoInstallEnabled(),
      last_auto_install: lastCliAutoInstall,
    }),
  );
  ipcMain.handle("wuu:cli-install", (_event, overwrite?: boolean) =>
    installCli({ overwrite: Boolean(overwrite) }),
  );
  ipcMain.handle("wuu:cli-auto-install-set", (_event, enabled: boolean) => {
    setCliAutoInstallEnabled(Boolean(enabled));
    return { ok: true, enabled: Boolean(enabled) };
  });
  ipcMain.handle("wuu:theme-preference-get", () => getThemePreference());
  // Synchronous variant used by the preload script so the first paint
  // already carries the persisted theme (no light-mode flash on boot).
  ipcMain.on("wuu:theme-preference-get-sync", (event) => {
    event.returnValue = getThemePreference();
  });
  ipcMain.handle("wuu:theme-preference-set", (_event, theme: ThemePreference) => {
    const valid: ThemePreference[] = ["system", "light", "dark"];
    const next = valid.includes(theme) ? theme : "system";
    setThemePreference(next);
    return { ok: true, theme: next };
  });
  ipcMain.handle("wuu:participant-list", () =>
    appServerClientPool.request<ParticipantListResult>("participant/list"),
  );
  ipcMain.handle("wuu:participant-save", (_event, params: ParticipantSaveParams) =>
    appServerClientPool.request<ParticipantSaveResult>("participant/save", params),
  );
  ipcMain.handle(
    "wuu:participant-feedback",
    (
      _event,
      participantId: string,
      text: string,
      taskId?: string,
      messageId?: string,
    ) =>
      appServerClientPool.request<ParticipantFeedbackResult>("participant/feedback", {
        participant_id: participantId,
        text,
        task_id: taskId ?? "",
        message_id: messageId ?? "",
      }),
  );
  ipcMain.handle(
    "wuu:participant-reset",
    (_event, participantId: string, scope: "restart" | "session" | "full") =>
      appServerClientPool.request<ParticipantResetResult>("participant/reset", {
        participant_id: participantId,
        scope,
      }),
  );
  ipcMain.handle("wuu:participant-retire", (_event, participantId: string) =>
    appServerClientPool.request<ParticipantRetireResult>("participant/retire", {
      participant_id: participantId,
    }),
  );
  ipcMain.handle("wuu:thread-list-sub", (_event, threadId: string) =>
    appServerClientPool.request<ThreadListSubResult>("thread/listSub", {
      thread_id: threadId,
    }),
  );
  ipcMain.handle(
    "wuu:thread-open-sub",
    (
      _event,
      threadId: string,
      options?: { subthreadId?: string; anchorItemId?: string; title?: string; createdBy?: string },
    ) =>
      appServerClientPool.request<ThreadOpenSubResult>("thread/openSub", {
        thread_id: threadId,
        subthread_id: options?.subthreadId ?? "",
        anchor_item_id: options?.anchorItemId ?? "",
        title: options?.title ?? "",
        created_by: options?.createdBy ?? "",
      }),
  );
  ipcMain.handle(
    "wuu:thread-resolve-sub",
    (_event, threadId: string, subthreadId: string, resolved: boolean) =>
      appServerClientPool.request<ThreadResolveSubResult>("thread/resolveSub", {
        thread_id: threadId,
        subthread_id: subthreadId,
        resolved,
      }),
  );
  ipcMain.handle("wuu:thread-list", (_event, cwd?: string) =>
    appServerClientPool.request<{ threads: Thread[] }>(
      "thread/list",
      typeof cwd === "string" && cwd.length > 0 ? { cwd } : undefined,
    ),
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
    "wuu:thread-members-remove",
    (_event, threadId: string, participantId: string) =>
      appServerClientPool.request<{ thread: Thread }>("thread/members/remove", {
        thread_id: threadId,
        participant_id: participantId,
      }),
  );
  ipcMain.handle(
    "wuu:thread-rename",
    (_event, threadId: string, title: string) =>
      appServerClientPool.request<{ thread: Thread }>("thread/rename", {
        thread_id: threadId,
        title,
      }),
  );
  ipcMain.handle("wuu:reveal-session", (_event, _threadId: string) => {
    // The session's data lives in the user-level sessions dir (a single
    // shared SQLite file). Reveal that dir in the OS file browser so
    // the user can inspect the database.
    return shell.openPath(join(wuuHomePath(), "sessions"));
  });
  ipcMain.handle(
    "wuu:turn-start",
    (
      _event,
      threadId: string,
      prompt: string,
      images?: InputImage[],
      files?: InputFile[],
      permissionMode?: string,
      mentions?: string[],
      focusWorkspace?: string,
    ) =>
      appServerClientPool.request<{ turn: Turn }>("turn/start", {
        thread_id: threadId,
        prompt,
        images: images ?? [],
        files: files ?? [],
        ...(permissionMode === undefined ? {} : { permission_mode: permissionMode }),
        // Attached only when non-empty: the server rejects unknown params
        // fields, so plain sends stay compatible with backends that have
        // not landed mentions support yet.
        ...(mentions && mentions.length > 0 ? { mentions } : {}),
        // Attached only when the renderer computed an explicit chat-focus
        // change for this send (see focusWorkspaceSendValue in
        // AppState.ts) — omitted entirely otherwise, same compatibility
        // reasoning as mentions above.
        ...(focusWorkspace === undefined ? {} : { focus_workspace: focusWorkspace }),
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
      permissionMode?: string,
    ) =>
      appServerClientPool.request("turn/queue", {
        thread_id: threadId,
        prompt,
        images: images ?? [],
        files: files ?? [],
        client_id: clientId,
        ...(permissionMode === undefined ? {} : { permission_mode: permissionMode }),
      }),
  );
  ipcMain.handle(
    "wuu:turn-update-queued",
    (
      _event,
      threadId: string,
      queueId: string,
      prompt: string,
      images?: InputImage[],
      files?: InputFile[],
    ) =>
      appServerClientPool.request("turn/update-queued", {
        thread_id: threadId,
        queue_id: queueId,
        prompt,
        images: images ?? [],
        files: files ?? [],
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
  // Composer goal banner surface. The renderer only needs a lightweight
  // summary plus explicit runtime controls; the full GoalSnapshot and
  // workflow/agent run detail stay on the agent tool loop.
  ipcMain.handle("wuu:goal-active-summary", async (_event, threadID?: string) => {
    const result = await appServerClientPool.request<{
      summary?: ComposerGoalSummary | null;
    }>("goal/active-summary", { thread_id: threadID });
    return result.summary ?? null;
  });
  ipcMain.handle("wuu:goal-pause", (_event, goalID: string, threadID?: string) =>
    appServerClientPool.request<{ ok: boolean }>("goal/pause", {
      goal_id: goalID,
      thread_id: threadID,
      confirm_user_approved: true,
    }),
  );
  ipcMain.handle("wuu:goal-resume", (_event, goalID: string, threadID?: string) =>
    appServerClientPool.request<{ ok: boolean }>("goal/resume", {
      goal_id: goalID,
      thread_id: threadID,
      confirm_user_approved: true,
    }),
  );
  ipcMain.handle("wuu:goal-clear", (_event, goalID: string, threadID?: string) =>
    appServerClientPool.request<{ ok: boolean }>("goal/clear", {
      goal_id: goalID,
      thread_id: threadID,
      confirm_user_approved: true,
    }),
  );
  ipcMain.handle(
    "wuu:goal-update-text",
    (_event, goalID: string, text: string, threadID?: string) =>
      appServerClientPool.request<{ ok: boolean }>("goal/update-text", {
        goal_id: goalID,
        thread_id: threadID,
        text,
        confirm_user_approved: true,
      }),
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
