import { contextBridge, ipcRenderer } from "electron";
import type { ServerEvent, WuuDesktopApi } from "../shared/protocol";

const api: WuuDesktopApi = {
  initialize: () => ipcRenderer.invoke("wuu:initialize"),
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
