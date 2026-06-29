import { contextBridge, ipcRenderer } from "electron";
import type {
  ServerEvent,
  SettingsUsageRange,
  WindowResizeState,
  WuuDesktopApi,
} from "../shared/protocol";

const api: WuuDesktopApi = {
  listProjects: () => ipcRenderer.invoke("wuu:project-list"),
  createBlankProject: () => ipcRenderer.invoke("wuu:project-create-blank"),
  chooseProjectFolder: () => ipcRenderer.invoke("wuu:project-choose-folder"),
  selectProject: (projectId: string) =>
    ipcRenderer.invoke("wuu:project-select", projectId),
  selectNoProject: (fresh?: boolean, cwd?: string) =>
    ipcRenderer.invoke("wuu:project-select-none", fresh, cwd),
  gitStatus: () => ipcRenderer.invoke("wuu:git-status"),
  listGitChanges: () => ipcRenderer.invoke("wuu:git-changes"),
  readGitFileDiff: (path: string) =>
    ipcRenderer.invoke("wuu:git-file-diff", path),
  checkoutGitBranch: (branch: string) =>
    ipcRenderer.invoke("wuu:git-checkout-branch", branch),
  createCheckoutGitBranch: (branch: string) =>
    ipcRenderer.invoke("wuu:git-create-checkout-branch", branch),
  commitGitChanges: (params) => ipcRenderer.invoke("wuu:git-commit", params),
  createPullRequest: (params) =>
    ipcRenderer.invoke("wuu:git-create-pr", params),
  listWorkspaceFiles: () => ipcRenderer.invoke("wuu:file-tree-list"),
  listWorkspaceDirectory: (path?: string) =>
    ipcRenderer.invoke("wuu:file-directory-list", path),
  readWorkspaceFile: (path: string) =>
    ipcRenderer.invoke("wuu:file-read", path),
  startTerminalSession: (params) =>
    ipcRenderer.invoke("wuu:terminal-start", params),
  writeTerminalSession: (id: string, data: string) =>
    ipcRenderer.invoke("wuu:terminal-write", id, data),
  resizeTerminalSession: (id: string, cols: number, rows: number) =>
    ipcRenderer.invoke("wuu:terminal-resize", id, cols, rows),
  stopTerminalSession: (id: string) =>
    ipcRenderer.invoke("wuu:terminal-stop", id),
  initialize: () => ipcRenderer.invoke("wuu:initialize"),
  getBuildInfo: () => ipcRenderer.invoke("wuu:build-info"),
  loadCodexModels: (provider?: string) =>
    ipcRenderer.invoke("wuu:config-codex-models", provider),
  updateRuntimeSettings: (
    provider: string,
    model: string,
    effort?: string,
    connection?: Parameters<WuuDesktopApi["updateRuntimeSettings"]>[3],
    variant?: string,
    permissionMode?: string,
  ) =>
    ipcRenderer.invoke(
      "wuu:config-model-update",
      provider,
      model,
      effort,
      connection,
      variant,
      permissionMode,
    ),
  removeProvider: (
    provider: string,
    options?: { fallbackProvider?: string; fallbackModel?: string },
  ) => ipcRenderer.invoke("wuu:config-provider-remove", provider, options),
  updateAdvancedSettings: (settings) =>
    ipcRenderer.invoke("wuu:config-advanced-update", settings),
  listSkills: () => ipcRenderer.invoke("wuu:skill-list"),
  getSettingsUsage: (range?: SettingsUsageRange) =>
    ipcRenderer.invoke("wuu:settings-usage", range),
  listManagedProcesses: () => ipcRenderer.invoke("wuu:process-list"),
  stopManagedProcess: (processId: string) =>
    ipcRenderer.invoke("wuu:process-stop", processId),
  listMCPServers: () => ipcRenderer.invoke("wuu:mcp-list"),
  connectMCPServer: (name: string) => ipcRenderer.invoke("wuu:mcp-connect", name),
  disconnectMCPServer: (name: string) =>
    ipcRenderer.invoke("wuu:mcp-disconnect", name),
  refreshMCPServer: (name: string) => ipcRenderer.invoke("wuu:mcp-refresh", name),
  startThread: () => ipcRenderer.invoke("wuu:thread-start"),
  resumeThread: (sessionId?: string) =>
    ipcRenderer.invoke("wuu:thread-resume", sessionId),
  forkThread: (threadId: string, turnId?: string, itemId?: string) =>
    ipcRenderer.invoke("wuu:thread-fork", threadId, turnId, itemId),
  editThreadMessage: (threadId: string, turnId: string, itemId: string) =>
    ipcRenderer.invoke("wuu:thread-edit-message", threadId, turnId, itemId),
  getThreadContextComposition: (threadId: string) =>
    ipcRenderer.invoke("wuu:thread-context-composition", threadId),
  listThreads: (cwd?: string) => ipcRenderer.invoke("wuu:thread-list", cwd),
  searchThreads: (query: string, limit?: number) =>
    ipcRenderer.invoke("wuu:thread-search", query, limit),
  pinThread: (threadId: string, pinned: boolean) =>
    ipcRenderer.invoke("wuu:thread-pin", threadId, pinned),
  archiveThread: (threadId: string, archived: boolean) =>
    ipcRenderer.invoke("wuu:thread-archive", threadId, archived),
  renameThread: (threadId: string, title: string) =>
    ipcRenderer.invoke("wuu:thread-rename", threadId, title),
  revealSession: (threadId: string) =>
    ipcRenderer.invoke("wuu:reveal-session", threadId),
  startTurn: (threadId: string, prompt: string, images, files) =>
    ipcRenderer.invoke("wuu:turn-start", threadId, prompt, images, files),
  queueTurn: (threadId: string, prompt: string, images, clientId, files) =>
    ipcRenderer.invoke("wuu:turn-queue", threadId, prompt, images, clientId, files),
  dequeueTurn: (threadId: string, queueId: string) =>
    ipcRenderer.invoke("wuu:turn-dequeue", threadId, queueId),
  steerTurn: (threadId: string, expectedTurnId: string, prompt: string, images, clientId, files) =>
    ipcRenderer.invoke(
      "wuu:turn-steer",
      threadId,
      expectedTurnId,
      prompt,
      images,
      clientId,
      files,
    ),
  unsteerTurn: (threadId: string, steerId: string) =>
    ipcRenderer.invoke("wuu:turn-unsteer", threadId, steerId),
  interruptTurn: (threadId: string) =>
    ipcRenderer.invoke("wuu:turn-interrupt", threadId),
  respondToServerRequest: (id: string, result: unknown) =>
    ipcRenderer.invoke("wuu:respond-server-request", id, result),
  rejectServerRequest: (id: string, message: string) =>
    ipcRenderer.invoke("wuu:reject-server-request", id, message),
  getActiveGoalSummary: (threadId?: string) =>
    ipcRenderer.invoke("wuu:goal-active-summary", threadId),
  pauseGoal: (goalId: string, threadId?: string) =>
    ipcRenderer.invoke("wuu:goal-pause", goalId, threadId),
  resumeGoal: (goalId: string, threadId?: string) =>
    ipcRenderer.invoke("wuu:goal-resume", goalId, threadId),
  cancelGoal: (goalId: string, threadId?: string) =>
    ipcRenderer.invoke("wuu:goal-cancel", goalId, threadId),
  clearGoal: (goalId: string, threadId?: string) =>
    ipcRenderer.invoke("wuu:goal-clear", goalId, threadId),
  updateGoalText: (goalId: string, text: string, threadId?: string) =>
    ipcRenderer.invoke("wuu:goal-update-text", goalId, text, threadId),
  onServerEvent: (handler: (event: ServerEvent) => void) => {
    const listener = (
      _event: Electron.IpcRendererEvent,
      payload: ServerEvent,
    ) => handler(payload);
    ipcRenderer.on("wuu:server-event", listener);
    return () => ipcRenderer.removeListener("wuu:server-event", listener);
  },
  onTerminalEvent: (handler) => {
    const listener = (
      _event: Electron.IpcRendererEvent,
      payload: Parameters<typeof handler>[0],
    ) => handler(payload);
    ipcRenderer.on("wuu:terminal-event", listener);
    return () => ipcRenderer.removeListener("wuu:terminal-event", listener);
  },
  onWindowResizeState: (handler: (state: WindowResizeState) => void) => {
    const listener = (
      _event: Electron.IpcRendererEvent,
      payload: WindowResizeState,
    ) => handler(payload);
    ipcRenderer.on("wuu:window-resize-state", listener);
    return () =>
      ipcRenderer.removeListener("wuu:window-resize-state", listener);
  },
};

contextBridge.exposeInMainWorld("wuu", api);
