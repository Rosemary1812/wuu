import { contextBridge, ipcRenderer } from "electron";
import type {
  ServerEvent,
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
    toolPolicyProfile?: string,
  ) =>
    ipcRenderer.invoke(
      "wuu:config-model-update",
      provider,
      model,
      effort,
      connection,
      variant,
      toolPolicyProfile,
    ),
  listSkills: () => ipcRenderer.invoke("wuu:skill-list"),
  startThread: () => ipcRenderer.invoke("wuu:thread-start"),
  resumeThread: (sessionId?: string) =>
    ipcRenderer.invoke("wuu:thread-resume", sessionId),
  forkThread: (threadId: string, turnId?: string, itemId?: string) =>
    ipcRenderer.invoke("wuu:thread-fork", threadId, turnId, itemId),
  listThreads: () => ipcRenderer.invoke("wuu:thread-list"),
  searchThreads: (query: string, limit?: number) =>
    ipcRenderer.invoke("wuu:thread-search", query, limit),
  pinThread: (threadId: string, pinned: boolean) =>
    ipcRenderer.invoke("wuu:thread-pin", threadId, pinned),
  archiveThread: (threadId: string, archived: boolean) =>
    ipcRenderer.invoke("wuu:thread-archive", threadId, archived),
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
