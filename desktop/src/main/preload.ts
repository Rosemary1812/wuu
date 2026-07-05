import { contextBridge, ipcRenderer } from "electron";
import type {
  ServerEvent,
  SettingsUsageRange,
  ThemePreference,
  ThreadStartParams,
  WindowResizeState,
  WuuDesktopApi,
} from "../shared/protocol";

// Read the persisted theme preference synchronously so the very first
// paint carries the right data-theme — an async round-trip would flash
// the light theme for dark-mode users on every launch.
const initialThemePreference = ((): ThemePreference => {
  try {
    const value = ipcRenderer.sendSync("wuu:theme-preference-get-sync") as unknown;
    return value === "light" || value === "dark" || value === "system"
      ? value
      : "system";
  } catch {
    return "system";
  }
})();

function resolveTheme(preference: ThemePreference): "light" | "dark" {
  if (preference === "system") {
    return window.matchMedia?.("(prefers-color-scheme: dark)").matches
      ? "dark"
      : "light";
  }
  return preference;
}

try {
  document.documentElement.dataset.theme = resolveTheme(initialThemePreference);
} catch {
  // The renderer's theme controller re-applies on boot; losing the
  // preload stamp only costs a one-frame flash.
}

const api: WuuDesktopApi = {
  listProjects: () => ipcRenderer.invoke("wuu:project-list"),
  createBlankProject: () => ipcRenderer.invoke("wuu:project-create-blank"),
  chooseProjectFolder: () => ipcRenderer.invoke("wuu:project-choose-folder"),
  selectProject: (projectId: string) =>
    ipcRenderer.invoke("wuu:project-select", projectId),
  removeProject: (projectId: string) =>
    ipcRenderer.invoke("wuu:project-remove", projectId),
  cleanupProjectState: (projectId: string, projectPath: string) =>
    ipcRenderer.invoke("wuu:project-cleanup-state", projectId, projectPath),
  relocateProject: (projectId: string) =>
    ipcRenderer.invoke("wuu:project-relocate", projectId),
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
  listWorkspaceFiles: (root?: string) =>
    ipcRenderer.invoke("wuu:file-tree-list", root),
  listWorkspaceDirectory: (path?: string, root?: string) =>
    ipcRenderer.invoke("wuu:file-directory-list", path, root),
  readWorkspaceFile: (path: string, root?: string) =>
    ipcRenderer.invoke("wuu:file-read", path, root),
  resolveWorkspaceFileReference: (reference: string, root?: string) =>
    ipcRenderer.invoke("wuu:file-reference-resolve", reference, root),
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
  updateGeneralSettings: (settings) =>
    ipcRenderer.invoke("wuu:config-general-update", settings),
  listSkills: () => ipcRenderer.invoke("wuu:skill-list"),
  getSettingsUsage: (range?: SettingsUsageRange) =>
    ipcRenderer.invoke("wuu:settings-usage", range),
  listMCPServers: () => ipcRenderer.invoke("wuu:mcp-list"),
  connectMCPServer: (name: string) => ipcRenderer.invoke("wuu:mcp-connect", name),
  disconnectMCPServer: (name: string) =>
    ipcRenderer.invoke("wuu:mcp-disconnect", name),
  refreshMCPServer: (name: string) => ipcRenderer.invoke("wuu:mcp-refresh", name),
  startThread: (params?: ThreadStartParams) =>
    ipcRenderer.invoke("wuu:thread-start", params),
  resumeThread: (sessionId?: string) =>
    ipcRenderer.invoke("wuu:thread-resume", sessionId),
  startParticipant: (params) => ipcRenderer.invoke("wuu:participant-start", params),
  forkThread: (
    threadId: string,
    turnId?: string,
    itemId?: string,
    mode?: "local" | "worktree",
  ) => ipcRenderer.invoke("wuu:thread-fork", threadId, turnId, itemId, mode),
  editThreadMessage: (threadId: string, turnId: string, itemId: string) =>
    ipcRenderer.invoke("wuu:thread-edit-message", threadId, turnId, itemId),
  getThreadContextComposition: (threadId: string) =>
    ipcRenderer.invoke("wuu:thread-context-composition", threadId),
  listInstructionFiles: () => ipcRenderer.invoke("wuu:instructions-list"),
  getCliInstallStatus: () => ipcRenderer.invoke("wuu:cli-install-status"),
  installCli: (overwrite?: boolean) =>
    ipcRenderer.invoke("wuu:cli-install", overwrite),
  setCliAutoInstallEnabled: (enabled: boolean) =>
    ipcRenderer.invoke("wuu:cli-auto-install-set", enabled),
  initialThemePreference,
  getThemePreference: () => ipcRenderer.invoke("wuu:theme-preference-get"),
  setThemePreference: (theme: ThemePreference) =>
    ipcRenderer.invoke("wuu:theme-preference-set", theme),
  listParticipants: () => ipcRenderer.invoke("wuu:participant-list"),
  saveParticipant: (params) => ipcRenderer.invoke("wuu:participant-save", params),
  sendParticipantFeedback: (participantId, text, taskId, messageId) =>
    ipcRenderer.invoke(
      "wuu:participant-feedback",
      participantId,
      text,
      taskId,
      messageId,
    ),
  resetParticipant: (participantId, scope) =>
    ipcRenderer.invoke("wuu:participant-reset", participantId, scope),
  retireParticipant: (participantId) =>
    ipcRenderer.invoke("wuu:participant-retire", participantId),
  getMemoryOverview: (params) =>
    ipcRenderer.invoke("wuu:memory-overview", params),
  sendMemoryChat: (params) => ipcRenderer.invoke("wuu:memory-chat", params),
  readMemoryRaw: (params) => ipcRenderer.invoke("wuu:memory-read", params),
  listConversationSubthreads: (threadId: string) =>
    ipcRenderer.invoke("wuu:thread-list-sub", threadId),
  openConversationSubthread: (threadId: string, options) =>
    ipcRenderer.invoke("wuu:thread-open-sub", threadId, options),
  resolveConversationSubthread: (threadId: string, subthreadId: string, resolved: boolean) =>
    ipcRenderer.invoke("wuu:thread-resolve-sub", threadId, subthreadId, resolved),
  listThreads: (cwd?: string) => ipcRenderer.invoke("wuu:thread-list", cwd),
  searchThreads: (query: string, limit?: number) =>
    ipcRenderer.invoke("wuu:thread-search", query, limit),
  pinThread: (threadId: string, pinned: boolean) =>
    ipcRenderer.invoke("wuu:thread-pin", threadId, pinned),
  addThreadMember: (threadId: string, participantId: string) =>
    ipcRenderer.invoke("wuu:thread-members-add", threadId, participantId),
  removeThreadMember: (threadId: string, participantId: string) =>
    ipcRenderer.invoke("wuu:thread-members-remove", threadId, participantId),
  getThreadMarks: (
    threadId: string,
  ): Promise<import("../shared/protocol").ThreadMarksResult> =>
    ipcRenderer.invoke("wuu:thread-marks", threadId),
  archiveThread: (threadId: string, archived: boolean) =>
    ipcRenderer.invoke("wuu:thread-archive", threadId, archived),
  deleteThread: (threadId: string) =>
    ipcRenderer.invoke("wuu:thread-delete", threadId),
  compactThread: (threadId: string) =>
    ipcRenderer.invoke("wuu:thread-compact-start", threadId),
  renameThread: (threadId: string, title: string) =>
    ipcRenderer.invoke("wuu:thread-rename", threadId, title),
  revealSession: (threadId: string) =>
    ipcRenderer.invoke("wuu:reveal-session", threadId),
  openExternal: (url: string) =>
    ipcRenderer.invoke("wuu:open-external", url),
  startTurn: (threadId: string, prompt: string, images, files, permissionMode, mentions, focusWorkspace) =>
    ipcRenderer.invoke("wuu:turn-start", threadId, prompt, images, files, permissionMode, mentions, focusWorkspace),
  queueTurn: (threadId: string, prompt: string, images, clientId, files, permissionMode) =>
    ipcRenderer.invoke("wuu:turn-queue", threadId, prompt, images, clientId, files, permissionMode),
  updateQueuedTurn: (threadId: string, queueId: string, prompt: string, images, files) =>
    ipcRenderer.invoke("wuu:turn-update-queued", threadId, queueId, prompt, images, files),
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
  popOutSession: (params) =>
    ipcRenderer.invoke("wuu:pop-out-session", params),
  popOutClosed: (params) =>
    ipcRenderer.invoke("wuu:pop-out-closed", params),
};

contextBridge.exposeInMainWorld("wuu", api);
