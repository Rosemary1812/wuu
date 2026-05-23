const { contextBridge, ipcRenderer } = require("electron");

const cwd = process.env.WUU_STREAM_E2E_CWD || process.cwd();
const runtimeContext = { kind: "no_project", cwd };

function projectList() {
  return {
    projects: [],
    active_context: runtimeContext
  };
}

contextBridge.exposeInMainWorld("wuu", {
  listProjects: async () => projectList(),
  createBlankProject: async () => projectList(),
  chooseProjectFolder: async () => projectList(),
  selectProject: async () => projectList(),
  selectNoProject: async () => projectList(),
  gitStatus: async () => ({
    is_repo: true,
    branch: "streaming-e2e",
    branches: ["streaming-e2e"],
    dirty_count: 0
  }),
  checkoutGitBranch: async (branch) => ({
    is_repo: true,
    branch,
    branches: [branch],
    dirty_count: 0
  }),
  listWorkspaceFiles: async () => ({
    root: cwd,
    paths: [],
    truncated: false
  }),
  readWorkspaceFile: async (path) => ({
    root: cwd,
    path,
    absolute_path: path,
    size_bytes: 0,
    binary: false,
    truncated: false,
    text: ""
  }),
  initialize: async () => ({
    protocol_version: "e2e",
    provider: "e2e",
    model: "mock-stream",
    workspace_root: cwd,
    providers: [{ name: "e2e", type: "mock", model: "mock-stream" }]
  }),
  updateRuntimeSettings: async (provider, model) => ({
    provider,
    model,
    providers: [{ name: provider, type: "mock", model }]
  }),
  startThread: async () => ({ thread: null }),
  resumeThread: async () => ({ thread: null }),
  listThreads: async () => ({ threads: [] }),
  startTurn: async () => ({ turn: null }),
  interruptTurn: async () => ({ ok: true }),
  respondToServerRequest: async () => undefined,
  rejectServerRequest: async () => undefined,
  onServerEvent: (handler) => {
    const listener = (_event, payload) => handler(payload);
    ipcRenderer.on("test:server-event", listener);
    return () => ipcRenderer.removeListener("test:server-event", listener);
  },
  onWindowResizeState: () => () => undefined
});
