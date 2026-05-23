export type JsonValue =
  | null
  | boolean
  | number
  | string
  | JsonValue[]
  | { [key: string]: JsonValue };

export type AppServerRequest<T = unknown> = {
  id?: string;
  method: string;
  params?: T;
};

export type AppServerResponse<T = unknown> = {
  id?: string;
  result?: T;
  error?: AppServerError;
};

export type AppServerError = {
  code: string;
  message: string;
};

export type AppServerNotification<T = unknown> = {
  method: string;
  params?: T;
};

export type InitializeResult = {
  protocol_version: string;
  provider: string;
  model: string;
  workspace_root: string;
  providers?: ProviderSummary[];
};

export type ConfigModelUpdateResult = {
  provider: string;
  model: string;
  providers?: ProviderSummary[];
};

export type ProviderSummary = {
  name: string;
  type: string;
  model: string;
};

export type DesktopProject = {
  id: string;
  name: string;
  path: string;
  created_at: string;
  updated_at: string;
};

export type RuntimeContext =
  | {
      kind: "project";
      project_id: string;
      cwd: string;
    }
  | {
      kind: "no_project";
      cwd: string;
    };

export type ProjectListResult = {
  projects: DesktopProject[];
  active_context?: RuntimeContext;
  active_project_id?: string;
};

export type GitStatusResult = {
  is_repo: boolean;
  branch?: string;
  branches?: string[];
  dirty_count: number;
};

export type FileTreeListResult = {
  root: string;
  paths: string[];
  truncated: boolean;
};

export type WorkspaceFileReadResult = {
  root: string;
  path: string;
  absolute_path: string;
  size_bytes: number;
  binary: boolean;
  truncated: boolean;
  text?: string;
};

export type ThreadStatus = "idle" | "in_progress";
export type TurnStatus = "in_progress" | "completed" | "failed" | "interrupted";
export type TurnItemsView = "full";
export type ThreadItemType =
  | "user_message"
  | "agent_message"
  | "reasoning"
  | "tool_call"
  | "collab_agent_tool_call"
  | "context_compaction"
  | "error";
export type ThreadItemStatus = "in_progress" | "completed" | "failed";

export type Thread = {
  id: string;
  preview: string;
  model_provider: string;
  model: string;
  cwd: string;
  status: ThreadStatus;
  created_at: string;
  updated_at: string;
  turns: Turn[];
};

export type Turn = {
  id: string;
  items: ThreadItem[];
  items_view: TurnItemsView;
  status: TurnStatus;
  error?: { message: string };
  started_at: string;
  completed_at?: string;
  duration_ms?: number;
};

export type ThreadItem = {
  id: string;
  type: ThreadItemType;
  status?: ThreadItemStatus;
  role?: string;
  text?: string;
  name?: string;
  arguments?: string;
  result?: string;
  error?: string;
};

export type AskUserOption = {
  label: string;
  description: string;
  preview?: string;
};

export type AskUserQuestion = {
  question: string;
  header: string;
  options: AskUserOption[];
  multi_select?: boolean;
};

export type AskUserResponse = {
  answers: Record<string, string>;
  cancelled?: boolean;
};

export type ServerEvent =
  | { kind: "notification"; message: AppServerNotification }
  | { kind: "server-request"; message: Required<AppServerRequest> }
  | { kind: "server-error"; message: string }
  | { kind: "server-exit"; code: number | null };

export type WindowResizeState = {
  resizing: boolean;
};

export type WuuDesktopApi = {
  listProjects: () => Promise<ProjectListResult>;
  createBlankProject: () => Promise<ProjectListResult>;
  chooseProjectFolder: () => Promise<ProjectListResult>;
  selectProject: (projectId: string) => Promise<ProjectListResult>;
  selectNoProject: (fresh?: boolean) => Promise<ProjectListResult>;
  gitStatus: () => Promise<GitStatusResult>;
  checkoutGitBranch: (branch: string) => Promise<GitStatusResult>;
  listWorkspaceFiles: () => Promise<FileTreeListResult>;
  readWorkspaceFile: (path: string) => Promise<WorkspaceFileReadResult>;
  initialize: () => Promise<InitializeResult>;
  updateRuntimeSettings: (provider: string, model: string) => Promise<ConfigModelUpdateResult>;
  startThread: () => Promise<{ thread: Thread }>;
  resumeThread: (sessionId?: string) => Promise<{ thread: Thread }>;
  listThreads: () => Promise<{ threads: Thread[] }>;
  startTurn: (threadId: string, prompt: string) => Promise<{ turn: Turn }>;
  interruptTurn: (threadId: string) => Promise<{ ok: boolean }>;
  respondToServerRequest: (id: string, result: unknown) => Promise<void>;
  rejectServerRequest: (id: string, message: string) => Promise<void>;
  onServerEvent: (handler: (event: ServerEvent) => void) => () => void;
  onWindowResizeState: (handler: (state: WindowResizeState) => void) => () => void;
};
