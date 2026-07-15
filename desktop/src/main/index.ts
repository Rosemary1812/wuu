import {
  app,
  BrowserWindow,
  type BrowserWindowConstructorOptions,
  dialog,
  ipcMain,
  type IpcMainInvokeEvent,
  screen,
  session as electronSession,
  type OpenDialogOptions,
  shell,
} from "electron";
import { readdir, rm, stat } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { MESSAGE_FLOW_FONT_SIZE_RANGE } from "../shared/protocol";
import type {
  ComposerGoalSummary,
  ConfigAdvancedUpdateResult,
  ConfigGeneralUpdateResult,
  ConfigCodexModelsResult,
  ConfigModelUpdateResult,
  GitCommitParams,
  GitPullRequestParams,
  BuildInfoResult,
  ActivityActionResult,
  ActivityListResult,
  ActivityReleaseResult,
  ActivitySession,
  CoreBuildInfo,
  DesktopBuildInfo,
  InputFile,
  CliAutoInstallResult,
  CliInstallStatus,
  CodexPetSettingsUpdate,
  InputImage,
  InitializeResult,
  InstructionsListResult,
  MCPListResult,
  MCPAuthFinishResult,
  MCPAuthRemoveResult,
  MCPAuthStartResult,
  MCPAuthStatusResult,
  MCPServerActionResult,
  MemoryChatParams,
  MemoryChatResult,
  MemoryOverviewParams,
  MemoryOverviewResult,
  MemoryReadParams,
  MemoryReadResult,
  ParticipantFeedbackResult,
  ParticipantListResult,
  ParticipantResetResult,
  ParticipantRetireResult,
  ParticipantSaveParams,
  ParticipantSaveResult,
  ParticipantStartParams,
  ParticipantStartResult,
  RemoteControlSnapshot,
  RemoteControlStatus,
  ServerEvent,
  RuntimeContext,
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
  ThreadEscalateSubResult,
  ThreadListSubResult,
  ThreadMarksResult,
  MessageReactResult,
  MessagePostSubthreadResult,
  ThreadOpenSubResult,
  ThreadResolveSubResult,
  ThreadTaskEventsResult,
  ThreadStartParams,
  Turn,
  PopOutInitResult,
  PopOutSessionParams,
  WorkspaceFileSaveParams,
  CodexPetHint,
  SideThreadOpenResult,
  SideThreadHistoryResult,
  SideThreadSendParams,
  SideThreadSendResult,
} from "../shared/protocol";
import { AppServerClientPool } from "./appServerClients";
import {
  CUAObservationCoordinator,
} from "./cuaActivityWindows";
import { autoInstallCli, getCliInstallStatus, installCli } from "./cliInstall";
import {
  defaultCodexPetsDir,
  ensureCodexPetsDir,
  legacyCodexPetsDir,
  loadCodexPetsSnapshot,
} from "./codexPets";
import { CodexPetWindowManager, type CodexPetRuntime } from "./codexPetWindow";
import { RemoteHostManager } from "./remoteControl";
import {
  getCodexPetSettings,
  getCodexPetScale,
  getCodexPetSize,
  getCliAutoInstallEnabled,
  getMessageFlowFontSize,
  getThemePreference,
  setCodexPetSettings,
  setCliAutoInstallEnabled,
  setMessageFlowFontSize,
  setThemePreference,
  type MessageFlowFontSize,
  type ThemePreference,
} from "./desktopSettings";
import { GitService } from "./gitService";
import { openExternalURL, wireExternalNavigationGuards } from "./externalNavigation";
import { ProjectManager, wuuHomePath } from "./projects";
import { sideThreadEventFromServerEvent } from "./sideThreadEvents";
import {
  registerRenderableFileProtocol,
  registerRenderableFileScheme,
} from "./renderableFileProtocol";
import { TerminalSessionManager } from "./terminalSessions";
import { WorkspaceFileService } from "./workspaceFiles";

import { createWindowRegistry, type WindowRegistry } from "./windowRegistry";
import {
  computeDefaultMainWindowBounds,
  loadMainWindowBounds,
  saveMainWindowBounds,
} from "./windowState";
const __dirname = dirname(fileURLToPath(import.meta.url));
const DEV_CACHE_CLEANUP_THRESHOLD_BYTES = 512 * 1024 * 1024;
const DEV_CACHE_DIRECTORIES = ["Cache", "Code Cache", "GPUCache", "DawnCache"];
const DEFAULT_WINDOW_BACKGROUND = "#f6f6f4";
registerRenderableFileScheme();

let mainWindow: BrowserWindow | null = null;
const windowRegistry: WindowRegistry = createWindowRegistry();
const projectManager = new ProjectManager();

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
// Debounce timer for persisting the main window bounds on resize end. The
// 200ms delay matches windowResizeEndTimer so the two callbacks fire
// together — a single "user finished resizing" tick writes once.
let mainWindowBoundsSaveTimer: NodeJS.Timeout | undefined;
// Outcome of this session's startup CLI auto-install pass. Surfaced through
// wuu:cli-install-status so the settings page can show a one-time,
// non-blocking note when the pass actually installed or repaired the link.
let lastCliAutoInstall: CliAutoInstallResult | null = null;
const appServerClientPool = new AppServerClientPool(
  () => projectManager.ensureRuntimeContext(),
  () => projectManager.activeWorkdir(),
  (event) => emitServerEvent(event),
);
const cuaObservationCoordinator = new CUAObservationCoordinator(
  windowRegistry,
  async (threadID): Promise<ActivitySession[]> => {
    const workdir = projectManager.activeWorkdir();
    if (!workdir) return [];
    const result = await appServerClientPool.requestForWorkdir<ActivityListResult>(
      workdir,
      "activity/list",
      { thread_id: threadID },
    );
    return result.activities ?? [];
  },
);
// The pet is a standalone always-on-top window owned by the main process, so
// it stays on the desktop when the main window is hidden or minimized. Its
// right-click menu disables the setting, which also tears the window down.
// The jump callback fires when the user clicks the pet's session bubble:
// the main window is brought forward and the renderer is told to switch
// to that thread via the `wuu:codex-pet-jump` broadcast.
const codexPetWindowManager = new CodexPetWindowManager(
  () => {
    updateCodexPetSettings({ enabled: false });
  },
  (hint) => {
    if (mainWindow && !mainWindow.isDestroyed()) {
      if (mainWindow.isMinimized()) mainWindow.restore();
      mainWindow.show();
      mainWindow.focus();
    }
    broadcastToAll("wuu:codex-pet-jump", { thread_id: hint.thread_id });
  },
  (size) => {
    // Context-menu size change — push the choice into desktop-settings.json
    // and refresh the snapshot so the next sync re-stages the window with
    // the new size (CodexPetWindowManager already called applyView).
    updateCodexPetSettings({ size });
  },
  (scale) => {
    // Continuous-resize drag ended (commit=1) — persist the raw scale so
    // the pet reopens at exactly the size the user left it. Intermediate
    // per-frame scale updates never reach this callback.
    updateCodexPetSettings({ scale });
  },
);
const terminalSessionManager = new TerminalSessionManager(
  (windowID, event) => emitTerminalEvent(windowID, event),
);

async function showProjectDirectoryDialog(
  options: OpenDialogOptions,
): Promise<string | undefined> {
  const focusedWindow = BrowserWindow.getFocusedWindow();
  const result = focusedWindow && !focusedWindow.isDestroyed()
    ? await dialog.showOpenDialog(focusedWindow, options)
    : await dialog.showOpenDialog(options);
  if (result.canceled) {
    return undefined;
  }
  return result.filePaths[0];
}

function runtimeContextForEvent(event: IpcMainInvokeEvent): RuntimeContext {
  return (
    windowRegistry.runtimeContextForWindow(event.sender.id) ??
    projectManager.ensureRuntimeContext()
  );
}

function appServerRequest<T>(
  event: IpcMainInvokeEvent,
  method: string,
  params?: unknown,
): Promise<T> {
  const context = windowRegistry.runtimeContextForWindow(event.sender.id);
  return context
    ? appServerClientPool.requestInContext<T>(context, method, params)
    : appServerClientPool.request<T>(method, params);
}

function codexPetDirs(): string[] {
  const primaryPetsDir = defaultCodexPetsDir(wuuHomePath());
  ensureCodexPetsDir(primaryPetsDir);
  return [primaryPetsDir, legacyCodexPetsDir()];
}

function codexPetsSnapshot() {
  return loadCodexPetsSnapshot({
    petsDirs: codexPetDirs(),
    settings: getCodexPetSettings(),
  });
}

function updateCodexPetSettings(update: CodexPetSettingsUpdate) {
  const current = getCodexPetSettings();
  const requested = {
    enabled: update.enabled ?? current.enabled,
    selected_id: update.selected_id ?? current.selected_id,
    size: update.size ?? current.size,
    // 显式选 size 档位意味着放弃之前拖出来的连续 scale——两者同时在场时
    // scale 优先，所以这里必须把 scale 清掉档位才生效。
    scale: update.size !== undefined ? undefined : update.scale ?? current.scale,
  };
  const snapshot = loadCodexPetsSnapshot({
    petsDirs: codexPetDirs(),
    settings: requested,
  });
  setCodexPetSettings({
    enabled: snapshot.enabled,
    selected_id: snapshot.selected_id,
    size: requested.size,
    scale: requested.scale,
  });
  codexPetWindowManager.sync(snapshot);
  return snapshot;
}

function gitServiceForEvent(event: IpcMainInvokeEvent): GitService {
  return new GitService(() => runtimeContextForEvent(event));
}

function workspaceFilesForEvent(event: IpcMainInvokeEvent): WorkspaceFileService {
  return new WorkspaceFileService(() => runtimeContextForEvent(event));
}

function emitServerEvent(event: ServerEvent): void {
  cuaObservationCoordinator.handleServerEvent(event);
  const sideThreadEvent = sideThreadEventFromServerEvent(event);
  if (sideThreadEvent) {
    broadcastToAll("wuu:side-thread-event", sideThreadEvent);
  }
  broadcastToAll("wuu:server-event", event);
}

function broadcastToAll(channel: string, payload: unknown): void {
  for (const window of windowRegistry.allWindows()) {
    if (window.isDestroyed() || window.webContents.isDestroyed()) {
      continue;
    }
    window.webContents.send(channel, payload);
  }
}

function emitTerminalEvent(
  windowID: number,
  event: Parameters<TerminalSessionManager["emit"]>[1],
): void {
  const window = windowRegistry.windowForID(windowID);
  if (
    !window ||
    window.isDestroyed() ||
    window.webContents.isDestroyed()
  ) {
    return;
  }
  window.webContents.send("wuu:terminal-event", event);
}

function unregisterWindow(windowID: number): void {
  terminalSessionManager.stopForOwner(windowID);
  windowRegistry.unregisterWindow(windowID);
}

// Remote-control host: one machine-global daemon serving paired phones,
// independent of the per-workdir app-server pool. Events (pairing URI,
// paired, exit) fan out to every window; the settings panel re-pulls its
// snapshot on each one.
const remoteHostManager = new RemoteHostManager({
  onEvent: (event) => broadcastToAll("wuu:remote-event", event),
});

async function remoteControlSnapshot(workdir: string): Promise<RemoteControlSnapshot> {
  let status: RemoteControlStatus | null = null;
  let statusError = "";
  try {
    status = await remoteHostManager.status(workdir);
  } catch (err) {
    statusError = err instanceof Error ? err.message : String(err);
  }
  return {
    status,
    status_error: statusError || undefined,
    host_running: remoteHostManager.isRunning(),
    pair_uri: remoteHostManager.currentPairUri(),
  };
}

function setWindowResizeState(resizing: boolean): void {
  if (windowResizeState === resizing) {
    return;
  }
  windowResizeState = resizing;
  broadcastToAll("wuu:window-resize-state", { resizing });
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

function scheduleMainWindowBoundsSave(win: BrowserWindow, delay = 200): void {
  if (mainWindowBoundsSaveTimer) {
    clearTimeout(mainWindowBoundsSaveTimer);
  }
  mainWindowBoundsSaveTimer = setTimeout(() => {
    mainWindowBoundsSaveTimer = undefined;
    persistMainWindowBoundsNow(win);
  }, delay);
}

function persistMainWindowBoundsNow(win: BrowserWindow): void {
  if (win.isDestroyed()) return;
  // Maximised / fullscreen bounds are full-rect; restoring to them on the
  // next launch would skip the user's normal layout. Skip the write so the
  // last non-maximised bounds stay saved.
  if (win.isMinimized()) return;
  if (win.isMaximized() || win.isFullScreen()) return;
  saveMainWindowBounds(win.getBounds());
}

function loadRenderer(window: BrowserWindow): void {
  if (!app.isPackaged) {
    window.webContents.on("console-message", (_event, _level, message) => {
      if (message) {
        console.error(`[renderer] ${message}`);
      }
    });
    window.webContents.on("preload-error", (_event, preloadPath, error) => {
      console.error(`[preload] ${preloadPath}: ${error.message}`);
    });
  }

  const devRendererURL = !app.isPackaged ? process.env.ELECTRON_RENDERER_URL : undefined;
  const rendererPath = join(__dirname, "../renderer/index.html");
  const rendererURL = devRendererURL ?? pathToFileURL(rendererPath).toString();
  wireExternalNavigationGuards(window, {
    rendererURL,
    openExternal: openExternalNavigation,
  });

  if (devRendererURL) {
    window.loadURL(devRendererURL);
  } else {
    window.loadFile(rendererPath);
  }
}

function openExternalNavigation(rawURL: unknown): Promise<boolean> {
  return openExternalURL(rawURL, (url) => shell.openExternal(url));
}

function mainWindowMaterialOptions(): Pick<
  BrowserWindowConstructorOptions,
  "backgroundColor" | "transparent" | "vibrancy" | "visualEffectState"
> {
  if (process.platform !== "darwin") {
    return { backgroundColor: DEFAULT_WINDOW_BACKGROUND };
  }
  return {
    backgroundColor: "#00000000",
    transparent: true,
    vibrancy: "sidebar",
    visualEffectState: "active",
  };
}

type PopOutWindowParams =
  | {
      kind: "thread";
      threadID: string;
      context: RuntimeContext;
      sourceWindow?: BrowserWindow | null;
    }
  | {
      kind: "draft";
      context: RuntimeContext;
      sourceWindow?: BrowserWindow | null;
    }
  | {
      kind: "subthread";
      threadID: string;
      subthreadID: string;
      context: RuntimeContext;
      sourceWindow?: BrowserWindow | null;
    };

function createPopOutWindow(params: PopOutWindowParams): BrowserWindow {
  // Plan §2.2 #1 (vs Reviewer S1): cursor position is read by main
  // process via `screen.getCursorScreenPoint()` so the preload bridge
  // never sees the `screen` Electron API. Combined with `getBounds()`
  // we compute the new window position without exposing platform APIs
  // to the renderer.
  // Cast: some Electron typings bundle cursor methods on the namespace
  // value rather than the `Screen` interface; both exist at runtime.
  const cursor = (screen as unknown as { getCursorScreenPoint(): { x: number; y: number } }).getCursorScreenPoint();
  const display = (screen as unknown as { getDisplayNearestPoint(point: { x: number; y: number }): { workArea: { x: number; y: number; width: number; height: number } } }).getDisplayNearestPoint(cursor);
  const workArea = display.workArea;
  const sourceBounds = params.sourceWindow?.isDestroyed()
    ? undefined
    : params.sourceWindow?.getBounds();
  const winWidth = Math.max(
    720,
    Math.min(sourceBounds?.width ?? 800, workArea.width),
  );
  const winHeight = Math.max(
    560,
    Math.min(sourceBounds?.height ?? 600, workArea.height),
  );
  const x = Math.max(
    workArea.x,
    Math.min(cursor.x - winWidth / 2, workArea.x + workArea.width - winWidth),
  );
  const y = Math.max(
    workArea.y,
    Math.min(cursor.y - 20, workArea.y + workArea.height - winHeight),
  );

  const placeholderTitle =
    params.kind === "thread"
      ? `wuu · ${params.threadID.slice(0, 8)}`
      : params.kind === "subthread"
        ? `wuu · Thread ${params.subthreadID.slice(0, 8)}`
        : "wuu · 对话";
  const win = new BrowserWindow({
    width: winWidth,
    height: winHeight,
    x,
    y,
    titleBarStyle: "hiddenInset",
    trafficLightPosition: { x: 18, y: 16 },
    backgroundColor: DEFAULT_WINDOW_BACKGROUND,
    title: placeholderTitle,
    webPreferences: {
      preload: join(__dirname, "../preload/index.cjs"),
      contextIsolation: true,
      nodeIntegration: false,
      webviewTag: true,
    },
  });
  const windowID = win.webContents.id;

  windowRegistry.registerWindow(win, "popped-out", {
    workdir: params.context.cwd,
    runtimeContext: params.context,
    // A subthread window stores its PARENT threadID too so window-routed
    // app-server calls (escalate/postSubthread/react/bubble) resolve against
    // the right thread; the cth identity rides in subthreadID.
    threadID:
      params.kind === "thread" || params.kind === "subthread"
        ? params.threadID
        : undefined,
    subthreadID: params.kind === "subthread" ? params.subthreadID : undefined,
  });
  windowRegistry.attachResizeHandlers(win, () => {
    setWindowResizeState(true);
    scheduleWindowResizeEnd();
  });
  if (params.kind === "thread") {
    windowRegistry.setThreadWindow(params.threadID, windowID);
  } else if (params.kind === "subthread") {
    // NOT setThreadWindow — that would clobber the parent group thread's own
    // pop-out dedup mapping. cth windows dedup via the separate subthread map.
    windowRegistry.setSubthreadWindow(params.subthreadID, windowID);
  }
  win.on("closed", () => {
    unregisterWindow(windowID);
  });

  if (params.kind === "thread") {
    // Async title refresh. We only need the title here; the renderer
    // hydrates the actual thread through the window-routed app-server path.
    // Failures are intentionally silent: the placeholder remains usable and
    // window creation should not be blocked by a title lookup.
    void appServerClientPool
      .requestInContext<{ threads: Thread[] }>(params.context, "thread/list")
      .then((result) => {
        if (win.isDestroyed()) return;
        const threads = Array.isArray(result?.threads) ? result.threads : [];
        const match = threads.find((t) => t.id === params.threadID);
        const title = typeof match?.title === "string" ? match.title : "";
        if (title.length > 0) {
          win.setTitle(`wuu · ${title}`);
        }
      })
      .catch(() => {
        if (win.isDestroyed()) return;
      });
  }
  loadRenderer(win);
  return win;
}

function createWindow(): void {
  const primaryDisplay = screen.getPrimaryDisplay();
  const restoredBounds = loadMainWindowBounds(screen.getAllDisplays());
  const defaultSize = computeDefaultMainWindowBounds(primaryDisplay.workArea);
  const width = restoredBounds?.width ?? defaultSize.width;
  const height = restoredBounds?.height ?? defaultSize.height;

  const windowOptions: BrowserWindowConstructorOptions = {
    width,
    height,
    titleBarStyle: "hiddenInset",
    trafficLightPosition: { x: 18, y: 16 },
    ...mainWindowMaterialOptions(),
    webPreferences: {
      preload: join(__dirname, "../preload/index.cjs"),
      contextIsolation: true,
      nodeIntegration: false,
      webviewTag: true,
    },
  };
  if (restoredBounds) {
    windowOptions.x = restoredBounds.x;
    windowOptions.y = restoredBounds.y;
  }

  mainWindow = new BrowserWindow(windowOptions);

  windowRegistry.registerWindow(mainWindow, "main");
  const win = mainWindow;
  const windowID = win.webContents.id;

  windowRegistry.attachResizeHandlers(win, () => {
    setWindowResizeState(true);
    scheduleWindowResizeEnd();
    scheduleMainWindowBoundsSave(win);
  });
  win.on("close", () => {
    // Last-write-wins: cancel any pending debounce and persist synchronously
    // before the window is destroyed.
    if (mainWindowBoundsSaveTimer) {
      clearTimeout(mainWindowBoundsSaveTimer);
      mainWindowBoundsSaveTimer = undefined;
    }
    persistMainWindowBoundsNow(win);
  });
  win.on("closed", () => {
    if (windowResizeEndTimer) {
      clearTimeout(windowResizeEndTimer);
      windowResizeEndTimer = undefined;
    }
    if (mainWindowBoundsSaveTimer) {
      clearTimeout(mainWindowBoundsSaveTimer);
      mainWindowBoundsSaveTimer = undefined;
    }
    windowResizeState = false;
    cuaObservationCoordinator.setActiveThread(undefined);
    unregisterWindow(windowID);
    mainWindow = null;
  });
  loadRenderer(win);
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
  // Pick up the user's chosen size before the pet window is created, so the
  // initial BrowserWindow and the inline data: URL are sized right the first
  // time. setSize is a no-op when the persisted size equals the default. A
  // persisted continuous scale (edge-drag resize) overrides the preset — it
  // must be applied after setSize, which clears any scale override.
  codexPetWindowManager.setSize(getCodexPetSize());
  const persistedPetScale = getCodexPetScale();
  if (persistedPetScale !== undefined) {
    codexPetWindowManager.setScale(persistedPetScale);
  }
  codexPetWindowManager.sync(codexPetsSnapshot());

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

  ipcMain.handle(
    "wuu:pop-out-session",
    (event, params: PopOutSessionParams) => {
      const context = params?.context ?? runtimeContextForEvent(event);
      const sourceWindow = BrowserWindow.fromWebContents(event.sender);
      if (params?.kind === "draft") {
        const win = createPopOutWindow({
          kind: "draft",
          context,
          sourceWindow,
        });
        return { windowID: win.webContents.id };
      }
      if (params?.kind === "subthread") {
        const parentThreadID =
          typeof params.threadID === "string" ? params.threadID.trim() : "";
        const subthreadID =
          typeof params.subthreadID === "string"
            ? params.subthreadID.trim()
            : "";
        if (!parentThreadID || !subthreadID) {
          throw new Error("threadID and subthreadID are required");
        }
        const existingWindowID =
          windowRegistry.subthreadHostWindowID(subthreadID);
        const existing = windowRegistry.popOutWindowForSubthread(subthreadID);
        if (
          existing &&
          !existing.isDestroyed() &&
          !existing.webContents.isDestroyed()
        ) {
          if (existing.isMinimized()) {
            existing.restore();
          }
          existing.show();
          existing.focus();
          return { windowID: existing.webContents.id };
        }
        if (existing) {
          windowRegistry.clearSubthreadWindow(subthreadID);
          if (existingWindowID !== undefined) {
            unregisterWindow(existingWindowID);
          }
        }
        const win = createPopOutWindow({
          kind: "subthread",
          threadID: parentThreadID,
          subthreadID,
          context,
          sourceWindow,
        });
        return { windowID: win.webContents.id };
      }
      const threadID =
        typeof params?.threadID === "string" ? params.threadID.trim() : "";
      if (!threadID) {
        throw new Error("threadID is required");
      }
      const existingWindowID = windowRegistry.threadHostWindowID(threadID);
      const existing = windowRegistry.popOutWindowForThread(threadID);
      if (existing && !existing.isDestroyed() && !existing.webContents.isDestroyed()) {
        if (existing.isMinimized()) {
          existing.restore();
        }
        existing.show();
        existing.focus();
        return { windowID: existing.webContents.id };
      }
      if (existing) {
        windowRegistry.clearThreadWindow(threadID);
        if (existingWindowID !== undefined) {
          unregisterWindow(existingWindowID);
        }
      }
      const win = createPopOutWindow({
        kind: "thread",
        threadID,
        context,
        sourceWindow,
      });
      return { windowID: win.webContents.id };
    },
  );
  ipcMain.handle(
    "wuu:pop-out-closed",
    (_event, params: { threadID: string }) => {
      const win = windowRegistry.popOutWindowForThread(params.threadID);
      windowRegistry.clearThreadWindow(params.threadID);
      if (win && !win.isDestroyed()) win.close();
      return { ok: true };
    },
  );
  // Sync bootstrap for popped-out windows. This returns only the
  // main-process-owned window identity; conversation data loads through
  // normal async IPC after React starts. M1 parity: keep this channel
  // mirrored in preload.ts and always set event.returnValue.
  ipcMain.on(
    "wuu:pop-out-init",
    (event) => {
      const threadID = windowRegistry.threadForWindow(event.sender.id);
      const subthreadID = windowRegistry.subthreadForWindow(event.sender.id);
      const context = windowRegistry.runtimeContextForWindow(event.sender.id);
      // Check subthreadID BEFORE threadID: a subthread window intentionally
      // stores the parent threadID too (for runtime routing), so ordering it
      // first would misreport kind as "thread".
      event.returnValue = {
        kind: context
          ? subthreadID
            ? "subthread"
            : threadID
              ? "thread"
              : "draft"
          : null,
        threadID: threadID ?? null,
        subthreadID: subthreadID ?? null,
        context: context ?? null,
      } satisfies PopOutInitResult;
    },
  );
  ipcMain.handle("wuu:cua-active-thread", (event, threadID?: string) => {
    const senderWindow = BrowserWindow.fromWebContents(event.sender);
    if (!senderWindow || senderWindow.isDestroyed()) return;
    // Renderer focus and React mount are independent. Rejecting the initial
    // thread sync while focus is still settling permanently suppresses PiP for
    // that session because no later focus event is guaranteed.
    cuaObservationCoordinator.setActiveThread(threadID);
  });
  ipcMain.handle("wuu:project-list", () => projectManager.list());
  ipcMain.handle("wuu:project-select", (_event, projectIDToSelect: string) =>
    projectManager.select(projectIDToSelect),
  );
  ipcMain.handle("wuu:project-remove", (_event, projectIDToRemove: string) =>
    projectManager.remove(projectIDToRemove),
  );
  ipcMain.handle(
    "wuu:project-cleanup-state",
    (_event, projectId: string, projectPath: string) => {
      // The removed workspace may still have a pooled app server; dispose
      // it first so nothing recreates runtime state during the cleanup.
      if (typeof projectPath === "string" && projectPath.trim() !== "") {
        appServerClientPool.disposeWorkdirClient(projectPath);
      }
      return appServerClientPool.request<{
        state_dir: string;
        removed: boolean;
        memory_archived: boolean;
      }>("workspace/state/cleanup", {
        workspace_id: projectId,
      });
    },
  );
  ipcMain.handle(
    "wuu:project-select-none",
    (_event, fresh?: boolean, cwd?: string) =>
      projectManager.selectNoProject(Boolean(fresh), cwd),
  );
  ipcMain.handle("wuu:git-status", (event) =>
    gitServiceForEvent(event).status(),
  );
  ipcMain.handle("wuu:git-changes", (event) =>
    gitServiceForEvent(event).changes(),
  );
  ipcMain.handle("wuu:git-file-diff", (event, path: string, root?: string) =>
    gitServiceForEvent(event).fileDiff(path, root),
  );
  ipcMain.handle("wuu:git-checkout-branch", (event, branch: string) =>
    gitServiceForEvent(event).checkoutBranch(branch),
  );
  ipcMain.handle("wuu:git-create-checkout-branch", (event, branch: string) =>
    gitServiceForEvent(event).createCheckoutBranch(branch),
  );
  ipcMain.handle("wuu:git-commit", (event, params: GitCommitParams) =>
    gitServiceForEvent(event).commit(params ?? {}),
  );
  ipcMain.handle("wuu:git-create-pr", (event, params: GitPullRequestParams) =>
    gitServiceForEvent(event).createPullRequest(params ?? {}),
  );
  ipcMain.handle("wuu:file-tree-list", (event, root?: string) =>
    workspaceFilesForEvent(event).fileTreeList(root),
  );
  ipcMain.handle(
    "wuu:file-directory-list",
    (event, path?: string, root?: string) =>
      workspaceFilesForEvent(event).directoryList(path, root),
  );
  ipcMain.handle("wuu:file-read", (event, path: string, root?: string) =>
    workspaceFilesForEvent(event).readFile(path, root),
  );
  ipcMain.handle(
    "wuu:file-write",
    (event, params: WorkspaceFileSaveParams, root?: string) =>
      workspaceFilesForEvent(event).writeFile(params, root),
  );
  ipcMain.handle(
    "wuu:file-reference-resolve",
    (event, reference: string, root?: string) =>
      workspaceFilesForEvent(event).resolveFileReference(reference, root),
  );
  ipcMain.handle(
    "wuu:terminal-start",
    (event, params?: TerminalSessionStartParams) =>
      terminalSessionManager.startInContext(
        runtimeContextForEvent(event),
        params,
        event.sender.id,
      ),
  );
  ipcMain.handle("wuu:terminal-write", (event, id: string, data: string) =>
    terminalSessionManager.write(id, data, event.sender.id),
  );
  ipcMain.handle(
    "wuu:terminal-resize",
    (event, id: string, cols: number, rows: number) =>
      terminalSessionManager.resize(id, cols, rows, event.sender.id),
  );
  ipcMain.handle("wuu:terminal-stop", (event, id: string) =>
    terminalSessionManager.stop(id, event.sender.id),
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
  ipcMain.handle(
    "wuu:project-relocate",
    async (_event, projectIDToRelocate: string) => {
      const projectPath = await showProjectDirectoryDialog({
        title: "重新定位工作区",
        buttonLabel: "定位到此文件夹",
        properties: ["openDirectory"],
      });
      if (!projectPath) {
        return projectManager.list();
      }
      return projectManager.relocate(projectIDToRelocate, projectPath);
    },
  );
  ipcMain.handle("wuu:initialize", async (event) => {
    const result = await appServerRequest<InitializeResult>(event, "initialize");
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
    await openExternalNavigation(url);
  });
  ipcMain.handle("wuu:config-codex-models", (event, provider?: string) =>
    appServerRequest<ConfigCodexModelsResult>(event, "config/codex/models", {
      provider: provider ?? "",
    }),
  );
  ipcMain.handle(
    "wuu:config-model-update",
    (
      event,
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
      appServerRequest<ConfigModelUpdateResult>(event, "config/model/update", {
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
  ipcMain.handle("wuu:config-ultra-update", (event, enabled: boolean) =>
    appServerRequest<ConfigModelUpdateResult>(event, "config/model/update", {
      ultra: enabled,
    }),
  );
  ipcMain.handle(
    "wuu:config-advanced-update",
    (event, settings: RuntimeAdvancedSettingsUpdate) =>
      appServerRequest<ConfigAdvancedUpdateResult>(
        event,
        "config/advanced/update",
        settings ?? {},
      ),
  );
  ipcMain.handle(
    "wuu:config-general-update",
    (event, settings: RuntimeGeneralSettingsUpdate) =>
      appServerRequest<ConfigGeneralUpdateResult>(
        event,
        "config/general/update",
        settings ?? {},
      ),
  );
  ipcMain.handle(
    "wuu:config-provider-remove",
    (
      event,
      provider: string,
      options?: { fallbackProvider?: string; fallbackModel?: string },
    ) =>
      appServerRequest<ConfigModelUpdateResult>(
        event,
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
  ipcMain.handle("wuu:skill-list", (event) =>
    appServerRequest(event, "skill/list"),
  );
  ipcMain.handle("wuu:agent-template-list", (event) =>
    appServerRequest(event, "agent-template/list"),
  );
  ipcMain.handle("wuu:codex-pets-list", () => {
    const snapshot = codexPetsSnapshot();
    // Sync the pet window from the renderer-side list call so a failed
    // startup sync (e.g. data: URL ready-to-show that never fires) can
    // be recovered by re-opening the snapshot via SettingsView, not only
    // by toggling the enabled flag. `sync` is idempotent: if the window
    // already shows the same pet, applyView short-circuits.
    codexPetWindowManager.sync(snapshot);
    return snapshot;
  });
  ipcMain.handle(
    "wuu:codex-pets-update",
    (_event, settings: CodexPetSettingsUpdate) =>
      updateCodexPetSettings(settings ?? {}),
  );
  ipcMain.handle(
    "wuu:codex-pet-runtime",
    (_event, runtime: CodexPetRuntime) =>
      codexPetWindowManager.setRuntime(
        runtime ?? { running: false, status: "" },
      ),
  );
  ipcMain.handle(
    "wuu:codex-pet-hints",
    (_event, hints: CodexPetHint[] | null) =>
      codexPetWindowManager.setHints(hints ?? []),
  );
  ipcMain.handle(
    "wuu:settings-usage",
    (event, range?: SettingsUsageRange) =>
      appServerRequest<SettingsUsageResponse>(
        event,
        "settings/usage",
        { range } satisfies SettingsUsageQuery,
      ),
  );
  ipcMain.handle("wuu:mcp-list", (event) =>
    appServerRequest<MCPListResult>(event, "mcp/list"),
  );
  ipcMain.handle("wuu:mcp-connect", (event, name: string) =>
    appServerRequest<MCPServerActionResult>(event, "mcp/connect", { name }),
  );
  ipcMain.handle("wuu:mcp-disconnect", (event, name: string) =>
    appServerRequest<MCPServerActionResult>(event, "mcp/disconnect", { name }),
  );
  ipcMain.handle("wuu:mcp-refresh", (event, name: string) =>
    appServerRequest<MCPServerActionResult>(event, "mcp/refresh", { name }),
  );
  ipcMain.handle("wuu:mcp-auth-start", (event, name: string) =>
    appServerRequest<MCPAuthStartResult>(event, "mcp/auth/start", { name }),
  );
  ipcMain.handle("wuu:mcp-auth-status", (event, name: string) =>
    appServerRequest<MCPAuthStatusResult>(event, "mcp/auth/status", { name }),
  );
  ipcMain.handle("wuu:mcp-auth-finish", (event, name: string, state: string, code: string) =>
    appServerRequest<MCPAuthFinishResult>(event, "mcp/auth/finish", { name, state, code }),
  );
  ipcMain.handle("wuu:mcp-auth-remove", (event, name: string) =>
    appServerRequest<MCPAuthRemoveResult>(event, "mcp/auth/remove", { name }),
  );
  ipcMain.handle("wuu:activity-list", (event, threadId: string) =>
    appServerRequest<ActivityListResult>(event, "activity/list", { thread_id: threadId }),
  );
  ipcMain.handle("wuu:activity-takeover", (event, threadId: string, activityId: string) =>
    appServerRequest<ActivityActionResult>(event, "activity/takeover", { thread_id: threadId, activity_id: activityId }),
  );
  ipcMain.handle("wuu:activity-release", (event, threadId: string, activityId: string) =>
    appServerRequest<ActivityReleaseResult>(event, "activity/release", { thread_id: threadId, activity_id: activityId }),
  );
  ipcMain.handle("wuu:activity-stop", (event, threadId: string, activityId: string) =>
    appServerRequest<ActivityActionResult>(event, "activity/stop", { thread_id: threadId, activity_id: activityId }),
  );
  ipcMain.handle("wuu:side-thread-open", (event, mainThreadId: string) =>
    appServerRequest<SideThreadOpenResult>(event, "sideThread/open", { main_thread_id: mainThreadId }),
  );
  ipcMain.handle("wuu:side-thread-history", (event, mainThreadId: string) =>
    appServerRequest<SideThreadHistoryResult>(event, "sideThread/getHistory", { main_thread_id: mainThreadId }),
  );
  ipcMain.handle(
    "wuu:side-thread-send",
    (event, params: SideThreadSendParams) =>
      appServerRequest<SideThreadSendResult>(event, "sideThread/sendMessage", params),
  );
  ipcMain.handle("wuu:side-thread-reset", (event, mainThreadId: string) =>
    appServerRequest<{ ok: boolean }>(event, "sideThread/reset", { main_thread_id: mainThreadId }),
  );
  ipcMain.handle("wuu:side-thread-interrupt", (event, mainThreadId: string) =>
    appServerRequest<{ ok: boolean }>(event, "sideThread/interrupt", { main_thread_id: mainThreadId }),
  );
  ipcMain.handle(
    "wuu:thread-start",
    (event, params?: ThreadStartParams) =>
      appServerRequest<{ thread: Thread }>(event, "thread/start", params ?? {}),
  );
  ipcMain.handle("wuu:thread-resume", (event, sessionId?: string) =>
    appServerRequest<{ thread: Thread }>(event, "thread/resume", {
      session_id: sessionId ?? "",
    }),
  );
  ipcMain.handle("wuu:participant-start", (event, params: ParticipantStartParams) =>
    appServerRequest<ParticipantStartResult>(event, "participant/start", params),
  );
  ipcMain.handle(
    "wuu:thread-fork",
    (
      event,
      threadId: string,
      turnId?: string,
      itemId?: string,
      mode?: "local" | "worktree",
    ) =>
      appServerRequest<ThreadForkResult>(event, "thread/fork", {
        thread_id: threadId,
        turn_id: turnId ?? "",
        item_id: itemId ?? "",
        ...(mode ? { mode } : {}),
      }),
  );
  ipcMain.handle(
    "wuu:thread-edit-message",
    (event, threadId: string, turnId: string, itemId: string) =>
      appServerRequest<ThreadEditMessageResult>(event, "thread/edit-message", {
        thread_id: threadId,
        turn_id: turnId,
        item_id: itemId,
      }),
  );
  ipcMain.handle("wuu:thread-context-composition", (event, threadId: string) =>
    appServerRequest<ThreadContextCompositionResult>(event, "thread/context-composition", {
      thread_id: threadId,
    }),
  );
  ipcMain.handle("wuu:instructions-list", (event) =>
    appServerRequest<InstructionsListResult>(event, "instructions/list"),
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
  ipcMain.handle("wuu:remote-snapshot", (event) =>
    remoteControlSnapshot(runtimeContextForEvent(event).cwd),
  );
  ipcMain.handle("wuu:remote-relay-set", async (event, relayUrl: string) => {
    const workdir = runtimeContextForEvent(event).cwd;
    await remoteHostManager.setRelay(workdir, String(relayUrl));
    return remoteControlSnapshot(workdir);
  });
  ipcMain.handle("wuu:remote-host-set", async (event, enabled: boolean) => {
    const workdir = runtimeContextForEvent(event).cwd;
    if (enabled) {
      remoteHostManager.startHost(workdir);
    } else {
      await remoteHostManager.stopHost();
    }
    return remoteControlSnapshot(workdir);
  });
  // Opening a pairing window needs a host started with --pair; restart the
  // running one so the window applies without a manual toggle cycle.
  ipcMain.handle("wuu:remote-pairing-start", async (event) => {
    const workdir = runtimeContextForEvent(event).cwd;
    await remoteHostManager.stopHost();
    remoteHostManager.startHost(workdir, { pair: true });
    return remoteControlSnapshot(workdir);
  });
  ipcMain.handle("wuu:remote-device-remove", async (event, fingerprintOrPub: string) => {
    const workdir = runtimeContextForEvent(event).cwd;
    await remoteHostManager.removeDevice(workdir, String(fingerprintOrPub));
    return remoteControlSnapshot(workdir);
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
  ipcMain.on("wuu:message-flow-font-size-get-sync", (event) => {
    // Sync partner of getMessageFlowFontSize — preload needs the value
    // before first paint to stamp --conversation-message-font-size on
    // <html> and avoid a flash of the default size.
    event.returnValue = getMessageFlowFontSize();
  });
  ipcMain.handle("wuu:message-flow-font-size-get", () =>
    getMessageFlowFontSize(),
  );
  ipcMain.handle(
    "wuu:message-flow-font-size-set",
    (_event, fontSize: unknown) => {
      // Mirror the renderer's clamp: keep the IPC boundary honest about
      // the same 13-20 window so a corrupted renderer can't widen it.
      const valid =
        typeof fontSize === "number" &&
        Number.isFinite(fontSize) &&
        fontSize >= MESSAGE_FLOW_FONT_SIZE_RANGE.min &&
        fontSize <= MESSAGE_FLOW_FONT_SIZE_RANGE.max;
      const next: MessageFlowFontSize = valid
        ? (fontSize as MessageFlowFontSize)
        : MESSAGE_FLOW_FONT_SIZE_RANGE.default;
      setMessageFlowFontSize(next);
      return { ok: true, fontSize: next };
    },
  );
  ipcMain.handle("wuu:participant-list", (event) =>
    appServerRequest<ParticipantListResult>(event, "participant/list"),
  );
  ipcMain.handle("wuu:participant-save", (event, params: ParticipantSaveParams) =>
    appServerRequest<ParticipantSaveResult>(event, "participant/save", params),
  );
  ipcMain.handle(
    "wuu:participant-feedback",
    (
      event,
      participantId: string,
      text: string,
      taskId?: string,
      messageId?: string,
    ) =>
      appServerRequest<ParticipantFeedbackResult>(event, "participant/feedback", {
        participant_id: participantId,
        text,
        task_id: taskId ?? "",
        message_id: messageId ?? "",
      }),
  );
  ipcMain.handle(
    "wuu:participant-reset",
    (event, participantId: string, scope: "restart" | "session" | "full") =>
      appServerRequest<ParticipantResetResult>(event, "participant/reset", {
        participant_id: participantId,
        scope,
      }),
  );
  ipcMain.handle("wuu:participant-retire", (event, participantId: string) =>
    appServerRequest<ParticipantRetireResult>(event, "participant/retire", {
      participant_id: participantId,
    }),
  );
  // 记忆面板 RPC（memory-redesign.md §8.2）。participant_id 只在
  // participant scope 下附带，避免后端 DisallowUnknownFields 之外的
  // 空字段歧义；user scope 的请求只带 scope。
  ipcMain.handle("wuu:memory-overview", (event, params: MemoryOverviewParams) =>
    appServerRequest<MemoryOverviewResult>(event, "memory/overview", {
      scope: params.scope,
      ...(params.participant_id ? { participant_id: params.participant_id } : {}),
      ...(params.force_refresh ? { force_refresh: true } : {}),
    }),
  );
  ipcMain.handle("wuu:memory-chat", (event, params: MemoryChatParams) =>
    appServerRequest<MemoryChatResult>(event, "memory/chat", {
      scope: params.scope,
      message: params.message,
      ...(params.participant_id ? { participant_id: params.participant_id } : {}),
    }),
  );
  ipcMain.handle("wuu:memory-read", (event, params: MemoryReadParams) =>
    appServerRequest<MemoryReadResult>(event, "memory/read", {
      scope: params.scope,
      ...(params.participant_id ? { participant_id: params.participant_id } : {}),
    }),
  );
  ipcMain.handle("wuu:thread-list-sub", (event, threadId: string) =>
    appServerRequest<ThreadListSubResult>(event, "thread/listSub", {
      thread_id: threadId,
    }),
  );
  ipcMain.handle(
    "wuu:thread-open-sub",
    (
      event,
      threadId: string,
      options?: {
        subthreadId?: string;
        anchorItemId?: string;
        parentSeq?: number;
        title?: string;
        threadOwnerParticipantId?: string;
      },
    ) =>
      appServerRequest<ThreadOpenSubResult>(event, "thread/openSub", {
        thread_id: threadId,
        subthread_id: options?.subthreadId ?? "",
        anchor_item_id: options?.anchorItemId ?? "",
        parent_seq: options?.parentSeq ?? 0,
        title: options?.title ?? "",
        thread_owner_participant_id: options?.threadOwnerParticipantId ?? "",
      }),
  );
  ipcMain.handle(
    "wuu:thread-resolve-sub",
    (event, threadId: string, subthreadId: string, resolved: boolean) =>
      appServerRequest<ThreadResolveSubResult>(event, "thread/resolveSub", {
        thread_id: threadId,
        subthread_id: subthreadId,
        resolved,
      }),
  );
  ipcMain.handle(
    "wuu:thread-escalate-sub",
    (
      event,
      threadId: string,
      subthreadId: string,
      options?: { title?: string },
    ) =>
      appServerRequest<ThreadEscalateSubResult>(event, "thread/escalateSub", {
        thread_id: threadId,
        subthread_id: subthreadId,
        title: options?.title ?? "",
      }),
  );
  ipcMain.handle(
    "wuu:thread-task-events",
    (event, threadId: string, subthreadId: string) =>
      appServerRequest<ThreadTaskEventsResult>(event, "thread/taskEvents", {
        thread_id: threadId,
        subthread_id: subthreadId,
      }),
  );
  ipcMain.handle("wuu:thread-list", (event, cwd?: string) =>
    appServerRequest<{ threads: Thread[] }>(
      event,
      "thread/list",
      typeof cwd === "string" && cwd.length > 0 ? { cwd } : undefined,
    ),
  );
  ipcMain.handle("wuu:thread-list-archived", (event) =>
    appServerRequest<{ threads: Thread[] }>(event, "thread/listArchived"),
  );
  ipcMain.handle("wuu:thread-search", (event, query: string, limit?: number) =>
    appServerRequest(event, "thread/search", {
      query: query ?? "",
      limit: typeof limit === "number" ? limit : undefined,
    }),
  );
  ipcMain.handle(
    "wuu:thread-preview",
    (event, threadId: string, limit?: number) =>
      appServerRequest(event, "thread/preview", {
        thread_id: threadId,
        limit: typeof limit === "number" ? limit : undefined,
      }),
  );
  ipcMain.handle(
    "wuu:thread-pin",
    (event, threadId: string, pinned: boolean) =>
      appServerRequest<{ thread: Thread }>(event, "thread/pin", {
        thread_id: threadId,
        pinned,
      }),
  );
  ipcMain.handle(
    "wuu:thread-archive",
    (event, threadId: string, archived: boolean) =>
      appServerRequest<{ thread: Thread }>(event, "thread/archive", {
        thread_id: threadId,
        archived,
      }),
  );
  ipcMain.handle("wuu:thread-delete", (event, threadId: string) =>
    appServerRequest<{ thread_id: string }>(event, "thread/delete", {
      thread_id: threadId,
    }),
  );
  ipcMain.handle("wuu:thread-compact-start", (event, threadId: string) =>
    appServerRequest<{ turn: Turn }>(event, "thread/compact/start", {
      thread_id: threadId,
    }),
  );
  ipcMain.handle(
    "wuu:thread-members-add",
    (event, threadId: string, participantId: string) =>
      appServerRequest<{ thread: Thread }>(event, "thread/members/add", {
        thread_id: threadId,
        participant_id: participantId,
      }),
  );
  ipcMain.handle(
    "wuu:thread-members-remove",
    (event, threadId: string, participantId: string) =>
      appServerRequest<{ thread: Thread }>(event, "thread/members/remove", {
        thread_id: threadId,
        participant_id: participantId,
      }),
  );
  ipcMain.handle("wuu:thread-marks", (event, threadId: string) =>
    appServerRequest<ThreadMarksResult>(event, "thread/marks", {
      thread_id: threadId,
    }),
  );
  ipcMain.handle(
    "wuu:message-react",
    (event, threadId: string, seq: number, reaction: string) =>
      appServerRequest<MessageReactResult>(event, "message/react", {
        thread_id: threadId,
        seq,
        reaction,
      }),
  );
  ipcMain.handle(
    "wuu:message-post-subthread",
    (
      event,
      threadId: string,
      subthreadId: string,
      text: string,
      images?: InputImage[],
      files?: InputFile[],
    ) =>
      appServerRequest<MessagePostSubthreadResult>(
        event,
        "message/postSubthread",
        {
          thread_id: threadId,
          subthread_id: subthreadId,
          text,
          images: images ?? [],
          files: files ?? [],
        },
      ),
  );
  ipcMain.handle(
    "wuu:thread-rename",
    (event, threadId: string, title: string) =>
      appServerRequest<{ thread: Thread }>(event, "thread/rename", {
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
    "wuu:file-show-in-folder",
    (_event, path: string) => {
      // The path comes from `listWorkspaceDirectory`, so it's already a
      // workspace item the renderer is allowed to know about. We hand
      // it straight to the OS file browser — Finder highlights the row,
      // Explorer opens its parent, file managers open the folder — so
      // the user can do anything with the item beyond the in-app tree.
      // `showItemInFolder` returns void; we run it for its side effect.
      shell.showItemInFolder(path);
    },
  );
  ipcMain.handle(
    "wuu:turn-start",
    (
      event,
      threadId: string,
      prompt: string,
      images?: InputImage[],
      files?: InputFile[],
      permissionMode?: string,
      mentions?: string[],
      focusWorkspace?: string,
    ) =>
      appServerRequest<{ turn: Turn }>(event, "turn/start", {
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
      event,
      threadId: string,
      prompt: string,
      images?: InputImage[],
      clientId?: string,
      files?: InputFile[],
      permissionMode?: string,
    ) =>
      appServerRequest(event, "turn/queue", {
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
      event,
      threadId: string,
      queueId: string,
      prompt: string,
      images?: InputImage[],
      files?: InputFile[],
    ) =>
      appServerRequest(event, "turn/update-queued", {
        thread_id: threadId,
        queue_id: queueId,
        prompt,
        images: images ?? [],
        files: files ?? [],
      }),
  );
  ipcMain.handle(
    "wuu:turn-dequeue",
    (event, threadId: string, queueId: string) =>
      appServerRequest<{ ok: boolean }>(event, "turn/dequeue", {
        thread_id: threadId,
        queue_id: queueId,
      }),
  );
  ipcMain.handle(
    "wuu:turn-steer",
    (
      event,
      threadId: string,
      expectedTurnId: string,
      prompt: string,
      images?: InputImage[],
      clientId?: string,
      files?: InputFile[],
    ) =>
      appServerRequest(event, "turn/steer", {
        thread_id: threadId,
        expected_turn_id: expectedTurnId,
        prompt,
        images: images ?? [],
        files: files ?? [],
        client_id: clientId,
      }),
  );
  ipcMain.handle("wuu:turn-unsteer", (event, threadId: string, steerId: string) =>
    appServerRequest<{ ok: boolean }>(event, "turn/unsteer", {
      thread_id: threadId,
      steer_id: steerId,
    }),
  );
  ipcMain.handle("wuu:turn-interrupt", (event, threadId: string) =>
    appServerRequest<{ ok: boolean }>(event, "turn/interrupt", {
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
  ipcMain.handle("wuu:goal-active-summary", async (event, threadID?: string) => {
    const result = await appServerRequest<{
      summary?: ComposerGoalSummary | null;
    }>(event, "goal/active-summary", { thread_id: threadID });
    return result.summary ?? null;
  });
  ipcMain.handle("wuu:goal-pause", (event, goalID: string, threadID?: string) =>
    appServerRequest<{ ok: boolean }>(event, "goal/pause", {
      goal_id: goalID,
      thread_id: threadID,
      confirm_user_approved: true,
    }),
  );
  ipcMain.handle("wuu:goal-resume", (event, goalID: string, threadID?: string) =>
    appServerRequest<{ ok: boolean }>(event, "goal/resume", {
      goal_id: goalID,
      thread_id: threadID,
      confirm_user_approved: true,
    }),
  );
  ipcMain.handle("wuu:goal-clear", (event, goalID: string, threadID?: string) =>
    appServerRequest<{ ok: boolean }>(event, "goal/clear", {
      goal_id: goalID,
      thread_id: threadID,
      confirm_user_approved: true,
    }),
  );
  ipcMain.handle(
    "wuu:goal-update-text",
    (event, goalID: string, text: string, threadID?: string) =>
      appServerRequest<{ ok: boolean }>(event, "goal/update-text", {
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
  // SIGTERM goes out synchronously; the daemon's own signal handling shuts
  // the relay connection down cleanly.
  void remoteHostManager.stopHost();
});

app.on("window-all-closed", () => {
  if (process.platform !== "darwin") {
    app.quit();
  }
});
