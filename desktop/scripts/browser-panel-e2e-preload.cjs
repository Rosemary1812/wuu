const { contextBridge } = require("electron");

const cwd = process.env.WUU_BROWSER_E2E_CWD || process.cwd();
const runtimeContext = { kind: "no_project", cwd };
const projectList = () => ({ projects: [], active_context: runtimeContext });

contextBridge.exposeInMainWorld("wuu", {
  listProjects: async () => projectList(),
  createBlankProject: async () => projectList(),
  chooseProjectFolder: async () => projectList(),
  selectProject: async () => projectList(),
  selectNoProject: async () => projectList(),
  gitStatus: async () => ({
    is_repo: true,
    branch: "browser-e2e",
    branches: ["browser-e2e"],
    dirty_count: 0
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
    branches: [branch]
  }),
  commitGitChanges: async () => ({ committed: false }),
  createPullRequest: async () => ({ url: "" }),
  listGitChanges: async () => ({ changes: [] }),
  readGitFileDiff: async () => ({ path: "", diff: "" }),
  listWorkspaceFiles: async () => ({ root: cwd, paths: [], truncated: false }),
  listWorkspaceDirectory: async (path = "") => ({
    root: cwd,
    path,
    entries: [],
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
    protocol_version: "browser-e2e",
    provider: "browser-e2e",
    model: "mock",
    workspace_root: cwd,
    providers: [{ name: "browser-e2e", type: "mock", model: "mock" }]
  }),
  loadCodexModels: async () => ({ models: [] }),
  updateRuntimeSettings: async (provider, model) => ({
    provider,
    model,
    providers: [{ name: provider, type: "mock", model }]
  }),
  listSkills: async () => ({ skills: [] }),
  startThread: async () => ({
    thread: {
      id: "thread-browser-e2e",
      preview: "",
      model_provider: "browser-e2e",
      model: "mock",
      cwd,
      status: "idle",
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
      turns: []
    }
  }),
  resumeThread: async () => ({ thread: null }),
  forkThread: async () => ({ thread: null }),
  listThreads: async () => ({ threads: [] }),
  searchThreads: async () => ({ results: [] }),
  pinThread: async () => ({ thread: null }),
  archiveThread: async () => ({ thread: null }),
  startTurn: async () => ({ turn: { id: "t", items: [], items_view: "full", status: "in_progress" } }),
  interruptTurn: async () => ({ ok: true }),
  respondToServerRequest: async () => undefined,
  rejectServerRequest: async () => undefined,
  startTerminalSession: async () => ({ id: "term-mock", cwd, shell: "/bin/zsh", started_at: new Date().toISOString() }),
  writeTerminalSession: async () => ({ ok: true }),
  resizeTerminalSession: async () => ({ ok: true }),
  stopTerminalSession: async () => ({ ok: true }),
  onServerEvent: () => () => undefined,
  onTerminalEvent: () => () => undefined,
  onWindowResizeState: () => () => undefined
});
