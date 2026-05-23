import { contextBridge, ipcRenderer } from "electron";
import type { ServerEvent, WuuDesktopApi } from "../shared/protocol";

const api: WuuDesktopApi = {
  listProjects: () => ipcRenderer.invoke("wuu:project-list"),
  createBlankProject: () => ipcRenderer.invoke("wuu:project-create-blank"),
  chooseProjectFolder: () => ipcRenderer.invoke("wuu:project-choose-folder"),
  selectProject: (projectId: string) => ipcRenderer.invoke("wuu:project-select", projectId),
  selectNoProject: (fresh?: boolean) => ipcRenderer.invoke("wuu:project-select-none", fresh),
  gitStatus: () => ipcRenderer.invoke("wuu:git-status"),
  checkoutGitBranch: (branch: string) => ipcRenderer.invoke("wuu:git-checkout-branch", branch),
  initialize: () => ipcRenderer.invoke("wuu:initialize"),
  updateRuntimeSettings: (provider: string, model: string) =>
    ipcRenderer.invoke("wuu:config-model-update", provider, model),
  startThread: () => ipcRenderer.invoke("wuu:thread-start"),
  resumeThread: (sessionId?: string) => ipcRenderer.invoke("wuu:thread-resume", sessionId),
  listThreads: () => ipcRenderer.invoke("wuu:thread-list"),
  startTurn: (threadId: string, prompt: string) => ipcRenderer.invoke("wuu:turn-start", threadId, prompt),
  interruptTurn: (threadId: string) => ipcRenderer.invoke("wuu:turn-interrupt", threadId),
  respondToServerRequest: (id: string, result: unknown) =>
    ipcRenderer.invoke("wuu:respond-server-request", id, result),
  rejectServerRequest: (id: string, message: string) =>
    ipcRenderer.invoke("wuu:reject-server-request", id, message),
  onServerEvent: (handler: (event: ServerEvent) => void) => {
    const listener = (_event: Electron.IpcRendererEvent, payload: ServerEvent) => handler(payload);
    ipcRenderer.on("wuu:server-event", listener);
    return () => ipcRenderer.removeListener("wuu:server-event", listener);
  }
};

contextBridge.exposeInMainWorld("wuu", api);
