const { contextBridge } = require("electron");

const cwd = process.env.WUU_RESIZE_E2E_CWD || process.cwd();
const runtimeContext = { kind: "no_project", cwd };
const now = new Date().toISOString();
const workspacePaths = Array.from({ length: 220 }, (_value, index) => {
  const file = String(index).padStart(3, "0");
  return `resize-fixture-${file}.ts`;
});
const resizeThread = {
  id: "resize-thread",
  preview: "Resize fixture",
  model_provider: "e2e",
  model: "mock-resize",
  cwd,
  status: "idle",
  created_at: now,
  updated_at: now,
  turns: [
    {
      id: "resize-turn",
      status: "completed",
      items_view: "full",
      started_at: now,
      completed_at: now,
      duration_ms: 42,
      items: [
        {
          id: "resize-user",
          type: "user_message",
          text: "Resize the window while this conversation is visible.",
          status: "completed"
        },
        {
          id: "resize-agent",
          type: "agent_message",
          text:
            "This fixture keeps the conversation non-empty so the inline environment panel and docked composer resize path are exercised.",
          status: "completed"
        }
      ]
    }
  ]
};

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
    branch: "resize-e2e",
    branches: ["resize-e2e"],
    dirty_count: 0,
    diff: { files: 0, additions: 0, deletions: 0 },
    staged_diff: { files: 0, additions: 0, deletions: 0 }
  }),
  listGitChanges: async () => ({
    is_repo: true,
    root: cwd,
    files: []
  }),
  readGitFileDiff: async (path) => ({
    is_repo: true,
    path,
    status: "unknown",
    additions: 0,
    deletions: 0,
    patch: "",
    truncated: false
  }),
  checkoutGitBranch: async (branch) => ({
    is_repo: true,
    branch,
    branches: [branch],
    dirty_count: 0
  }),
  createCheckoutGitBranch: async (branch) => ({
    is_repo: true,
    branch,
    branches: [branch],
    dirty_count: 0,
    created: true
  }),
  commitGitChanges: async () => ({ ok: true, committed: false }),
  createPullRequest: async () => ({ url: "", already_exists: false }),
  listWorkspaceFiles: async () => ({
    root: cwd,
    paths: workspacePaths,
    truncated: false
  }),
  readWorkspaceFile: async (path) => ({
    root: cwd,
    path,
    absolute_path: path,
    size_bytes: 32,
    binary: false,
    truncated: false,
    text: `export const path = ${JSON.stringify(path)};\n`
  }),
  initialize: async () => ({
    protocol_version: "e2e",
    provider: "e2e",
    model: "mock-resize",
    workspace_root: cwd,
    providers: [{ name: "e2e", type: "mock", model: "mock-resize" }]
  }),
  updateRuntimeSettings: async (provider, model) => ({
    provider,
    model,
    providers: [{ name: provider, type: "mock", model }]
  }),
  loadCodexModels: async (provider) => ({
    provider,
    models: [{ id: "mock-resize", name: "mock-resize" }]
  }),
  startThread: async () => ({ thread: resizeThread }),
  resumeThread: async () => ({ thread: resizeThread }),
  listThreads: async () => ({ threads: [resizeThread] }),
  pinThread: async (_id, pinned) => ({ pinned }),
  archiveThread: async () => ({ ok: true }),
  startTurn: async () => ({ turn: null }),
  interruptTurn: async () => ({ ok: true }),
  respondToServerRequest: async () => undefined,
  rejectServerRequest: async () => undefined,
  onServerEvent: () => () => undefined,
  onWindowResizeState: () => () => undefined
});
