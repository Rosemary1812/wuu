const { contextBridge, ipcRenderer } = require("electron");

const cwd = process.env.WUU_STREAM_E2E_CWD || process.cwd();
const runtimeContext = { kind: "no_project", cwd };
let startedThreadCount = 0;

function projectList() {
  return {
    projects: [],
    active_context: runtimeContext
  };
}

function mockThread(id) {
  const now = new Date().toISOString();
  return {
    id,
    preview: "",
    model_provider: "e2e",
    model: "mock-stream",
    cwd,
    status: "idle",
    created_at: now,
    updated_at: now,
    turns: []
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
  startThread: async () => {
    startedThreadCount += 1;
    const id =
      startedThreadCount === 1
        ? "thread-immediate-title-e2e"
        : startedThreadCount === 2
          ? "thread-streaming-e2e"
          : `thread-started-e2e-${startedThreadCount}`;
    return { thread: mockThread(id) };
  },
  resumeThread: async () => ({ thread: null }),
  forkThread: async () => ({ thread: null }),
  listThreads: async () => ({ threads: [] }),
  startTurn: async (threadId, text, images = []) => {
    const now = new Date().toISOString();
    return {
      turn: {
        id: `turn-${threadId}`,
        items: [
          {
            id: `user-${threadId}`,
            type: "user_message",
            status: "completed",
            text,
            images
          }
        ],
        items_view: "full",
        status: "in_progress",
        started_at: now
      }
    };
  },
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
