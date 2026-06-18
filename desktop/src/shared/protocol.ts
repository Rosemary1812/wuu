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

export type CoreBuildInfo = {
  version?: string;
  commit?: string;
  date?: string;
  dirty?: boolean;
};

export type DesktopBuildInfo = {
  version: string;
  date: string;
};

export type BuildInfoResult = {
  core: CoreBuildInfo | undefined;
  desktop: DesktopBuildInfo;
};

export type InitializeResult = {
  protocol_version: string;
  core?: CoreBuildInfo;
  provider: string;
  model: string;
  effort?: string;
  variant?: string;
  workspace_root: string;
  tool_policy?: ToolPolicySummary;
  permissions?: PermissionSummary;
  extension_trust?: ExtensionTrustSummary;
  model_roles?: ModelRoleSummary[];
  providers?: ProviderSummary[];
};

export type ToolPolicySummary = {
  profile?: string;
  default_action?: string;
  tools?: Record<string, string>;
  kinds?: Record<string, string>;
  risks?: Record<string, string>;
};

export type PermissionSummary = {
  mode?: string;
  permission_profile?: string;
  approval_policy?: string;
  approvals_reviewer?: string;
};

export type ExtensionTrustSummary = {
  main_session?: ExtensionSessionTrustSummary;
  reviewer_session?: ExtensionSessionTrustSummary;
};

export type ExtensionSessionTrustSummary = {
  mcp?: ExtensionSurfaceTrustSummary;
  hooks?: ExtensionSurfaceTrustSummary;
  plugins?: ExtensionSurfaceTrustSummary;
  skills?: ExtensionSurfaceTrustSummary;
  workflows?: ExtensionSurfaceTrustSummary;
  external_tools?: ExtensionSurfaceTrustSummary;
};

export type ExtensionSurfaceTrustSummary = {
  allowed: boolean;
  active: boolean;
  count?: number;
  known_tools?: number;
  visible_tools?: number;
};

export type ConfigModelUpdateResult = {
  provider: string;
  model: string;
  effort?: string;
  variant?: string;
  tool_policy?: ToolPolicySummary;
  permissions?: PermissionSummary;
  extension_trust?: ExtensionTrustSummary;
  model_roles?: ModelRoleSummary[];
  providers?: ProviderSummary[];
};

export type ProviderSummary = {
  name: string;
  type: string;
  model: string;
  base_url?: string;
  api_key_configured?: boolean;
  connection_locked?: boolean;
  models?: ProviderModelSummary[];
};

export type ProviderModelSummary = {
  id: string;
  display_name?: string;
  default_effort?: string;
  default_variant?: string;
  supported_efforts?: string[];
  variants?: ProviderModelVariantSummary[];
  capabilities?: ModelCapabilitySummary;
  behavior?: ModelBehaviorSummary;
  source?: string;
};

export type ModelRoleSummary = {
  role: string;
  provider: string;
  model: string;
  api_model?: string;
  effort?: string;
  variant?: string;
  inherited?: boolean;
  capabilities?: ModelCapabilitySummary;
  behavior?: ModelBehaviorSummary;
};

export type ModelCapabilitySummary = {
  chat: boolean;
  responses?: boolean;
  tools: boolean;
  tool_calling?: string;
  structured_output: boolean;
  streaming: boolean;
  streaming_tool_args?: boolean;
  freeform_tool?: boolean;
  parallel_tool_calls?: boolean;
  system_role: boolean;
  developer_role?: boolean;
  reasoning: boolean;
  context_window?: number;
  input_limit?: number;
  output_limit?: number;
  image_input?: boolean;
  file_input?: boolean;
  prompt_cache?: boolean;
  cache_granularity?: string;
  protocol_family?: string;
  retry_safe_error_categories?: string[];
};

export type ModelBehaviorSummary = {
  family?: string;
  default_write_mode?: string;
  preferred_edit_primitive?: string;
  preferred_patch_grammar?: string;
  patch_reliability?: number;
  exact_edit_reliability?: number;
  whole_file_reliability?: number;
  json_reliability?: number;
  long_horizon_score?: number;
  default_max_autonomous_steps?: number;
  default_search_budget?: number;
  needs_read_before_write?: boolean;
  allow_parallel_read_only?: boolean;
  allow_direct_shell?: boolean;
  latency?: string;
  suitable?: {
    main?: boolean;
    review?: boolean;
    compact?: boolean;
    title?: boolean;
    memory?: boolean;
    worker?: boolean;
    fallback?: boolean;
  };
};

export type ProviderModelVariantSummary = {
  id: string;
  options?: Record<string, JsonValue>;
};

export type SkillSummary = {
  name: string;
  description?: string;
  when_to_use?: string;
  trigger_condition?: string;
  source: string;
  path?: string;
  argument_hint?: string;
  model?: string;
  context?: string;
  agent?: string;
  allowed_tools?: string[];
  required_context?: string[];
  examples?: string[];
  verification_checklist?: string[];
  progressive_disclosure?: string;
  user_invocable: boolean;
  disable_model_invoke: boolean;
  paths?: string[];
  effort?: string;
  version?: string;
};

export type SkillListResult = {
  skills: SkillSummary[];
};

export type GoalAttentionItem = {
  source: string;
  id?: string;
  status?: string;
  message?: string;
  path?: string;
};

export type GoalWorkflowPhaseSnapshot = {
  id: string;
  name?: string;
  status: string;
  error?: string;
  agent_run_ids?: string[];
};

export type GoalWorkflowAgentSnapshot = {
  id: string;
  phase_id?: string;
  agent_id?: string;
  agent_path?: string;
  task_name?: string;
  agent_profile?: string;
  status: string;
  report_path?: string;
  report_missing?: boolean;
  changed_files?: string[];
  artifacts?: string[];
  worktree_path?: string;
  input_tokens?: number;
  output_tokens?: number;
  duration_ms?: number;
  error?: string;
};

export type GoalWorkflowTeamMemberSnapshot = {
  id?: string;
  role?: string;
  mode?: string;
  agent_profile?: string;
  task_name?: string;
  phase_id?: string;
  created_profile?: boolean;
};

export type GoalWorkflowTeamSnapshot = {
  members?: GoalWorkflowTeamMemberSnapshot[];
  created_at?: string;
  updated_at?: string;
};

export type GoalChangedFileOverlapSnapshot = {
  file: string;
  agent_run_ids: string[];
};

export type GoalWorkflowArbitration = {
  status?: string;
  open_agent_runs?: string[];
  missing_reports?: string[];
  failed_agent_runs?: string[];
  changed_file_overlaps?: GoalChangedFileOverlapSnapshot[];
  next_actions?: string[];
};

export type GoalWorkflowSnapshot = {
  id: string;
  run_dir?: string;
  event_log_path?: string;
  definition_name?: string;
  driver?: string;
  entrypoint?: string;
  status: string;
  error?: string;
  script_path?: string;
  final_report_path?: string;
  goal_id?: string;
  goal_dir?: string;
  phases?: GoalWorkflowPhaseSnapshot[];
  agent_runs?: GoalWorkflowAgentSnapshot[];
  team?: GoalWorkflowTeamSnapshot;
  arbitration?: GoalWorkflowArbitration;
  event_count?: number;
};

export type GoalHarnessTaskSnapshot = {
  id: string;
  parent_id?: string;
  path?: string;
  name?: string;
  role?: string;
  goal_id?: string;
  goal_dir?: string;
  status: string;
  report_path?: string;
  artifact_paths?: string[];
  input_tokens?: number;
  output_tokens?: number;
  error?: string;
};

export type GoalHarnessReportSnapshot = {
  id: string;
  task_id: string;
  run_id?: string;
  agent_id?: string;
  agent_path?: string;
  outcome: string;
  summary?: string;
  changed_files?: string[];
  verification?: string[];
  artifacts?: string[];
  report_path?: string;
};

export type GoalHarnessSnapshot = {
  tasks?: GoalHarnessTaskSnapshot[];
  reports?: GoalHarnessReportSnapshot[];
};

export type GoalApprovalSnapshot = {
  id: string;
  goal_id?: string;
  goal_dir?: string;
  step?: string;
  source?: string;
  source_id?: string;
  title: string;
  reason?: string;
  requested_action?: string;
  risk?: string;
  artifact?: string;
  status: string;
  requested_by?: string;
  resolved_by?: string;
  resolution?: string;
  created_at: string;
  resolved_at?: string;
};

export type GoalStateSnapshot = {
  id: string;
  goal_dir?: string;
  goal: string;
  task?: string;
  status: string;
  current_step?: string;
  assigned_agent?: string;
  needs_human?: boolean;
  current_blocker?: string;
  final_artifact?: string;
  modified_files?: string[];
  retry_count?: number;
  pending_approvals?: GoalApprovalSnapshot[];
  updated_at?: string;
};

export type GoalSystemSnapshot = {
  generated_at: string;
  goal_root?: string;
  workflow_dir?: string;
  harness_dir?: string;
  goals?: GoalStateSnapshot[];
  workflows?: GoalWorkflowSnapshot[];
  harness?: GoalHarnessSnapshot;
  approvals?: GoalApprovalSnapshot[];
  attention?: GoalAttentionItem[];
  warnings?: string[];
};

export type GoalSnapshotResult = {
  snapshot: GoalSystemSnapshot;
};

export type GoalWorktreeStatus = {
  dirty: boolean;
  changed_files?: string[];
  porcelain?: string[];
};

export type GoalWorktreeMergePreview = {
  can_apply: boolean;
  conflict_files?: string[];
  error?: string;
};

export type GoalWorktreeReview = {
  worktree_path: string;
  target_repo?: string;
  status: GoalWorktreeStatus;
  diff?: string;
  diff_truncated?: boolean;
  merge_preview: GoalWorktreeMergePreview;
};

export type GoalWorktreeReviewResult = {
  review: GoalWorktreeReview;
};

export type GoalWorktreeCleanup = {
  worktree_path: string;
  removed: boolean;
  kept: boolean;
  status_before: GoalWorktreeStatus;
};

export type GoalWorktreeCleanupResult = {
  cleanup: GoalWorktreeCleanup;
};

export type GoalWorktreeRollback = {
  worktree_path: string;
  rolled_back: boolean;
  status_before: GoalWorktreeStatus;
  status_after: GoalWorktreeStatus;
};

export type GoalWorktreeRollbackResult = {
  rollback: GoalWorktreeRollback;
};

export type GoalWorktreeMerge = {
  worktree_path: string;
  target_repo: string;
  applied: boolean;
  changed_files?: string[];
  preview: GoalWorktreeMergePreview;
};

export type GoalWorktreeMergeResult = {
  merge: GoalWorktreeMerge;
};

export type GoalApprovalResolveResult = {
  approval: {
    id: string;
    step?: string;
    source?: string;
    source_id?: string;
    title: string;
    reason?: string;
    requested_action?: string;
    risk?: string;
    artifact?: string;
    status: string;
    requested_by?: string;
    resolved_by?: string;
    resolution?: string;
    created_at: string;
    resolved_at?: string;
  };
};

export type ManagedProcess = {
  action?: string;
  id: string;
  owner_kind: string;
  owner_id: string;
  lifecycle: string;
  status: string;
  pid: number;
  tty?: boolean;
  command: string;
  cwd: string;
  preview_urls?: string[];
  primary_preview_url?: string;
  started_at: string;
  updated_at: string;
  stopped_at?: string;
  exit_code?: number;
  last_error?: string;
};

export type ManagedProcessListResult = {
  processes: ManagedProcess[];
};

export type ManagedProcessStopResult = {
  process: ManagedProcess;
};

export type RuntimeConnectionUpdate = {
  base_url?: string;
  api_key?: string;
  create_provider?: boolean;
};

export type CodexModelSummary = {
  slug: string;
  display_name?: string;
  default_reasoning_level?: string;
  supported_reasoning?: string[];
  supported_in_api: boolean;
};

export type ConfigCodexModelsResult = {
  provider: string;
  model: string;
  effort?: string;
  variant?: string;
  models: CodexModelSummary[];
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
  detached?: boolean;
  diff?: GitDiffStats;
  staged_diff?: GitDiffStats;
  upstream?: string;
  ahead_count?: number;
  behind_count?: number;
  remote?: string;
  default_branch?: string;
  gh_available?: boolean;
  pr_url?: string;
};

export type GitDiffStats = {
  files: number;
  additions: number;
  deletions: number;
};

export type GitChangeStatus = "modified" | "added" | "deleted" | "renamed" | "copied" | "untracked" | "unknown";

export type GitChangeFile = {
  path: string;
  old_path?: string;
  status: GitChangeStatus;
  additions: number;
  deletions: number;
  binary?: boolean;
};

export type GitChangesResult = {
  is_repo: boolean;
  root?: string;
  files: GitChangeFile[];
};

export type GitFileDiffResult = {
  is_repo: boolean;
  path: string;
  old_path?: string;
  status: GitChangeStatus;
  additions: number;
  deletions: number;
  binary?: boolean;
  patch: string;
  truncated: boolean;
};

export type GitCommitParams = {
  message?: string;
  include_unstaged?: boolean;
};

export type GitCommitResult = {
  status: GitStatusResult;
  commit: string;
  message: string;
};

export type GitCreateBranchResult = {
  status: GitStatusResult;
};

export type GitPullRequestParams = {
  title?: string;
  body?: string;
  draft?: boolean;
};

export type GitPullRequestResult = {
  status: GitStatusResult;
  url: string;
  already_exists: boolean;
};

export type FileTreeListResult = {
  root: string;
  paths: string[];
  truncated: boolean;
};

export type WorkspaceFileTreeEntry = {
  name: string;
  path: string;
  kind: "directory" | "file";
};

export type WorkspaceDirectoryListResult = {
  root: string;
  path: string;
  entries: WorkspaceFileTreeEntry[];
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

export type TerminalSessionStartParams = {
  cols?: number;
  rows?: number;
};

export type TerminalSessionStartResult = {
  id: string;
  cwd: string;
  shell: string;
  started_at: string;
};

export type TerminalSessionActionResult = {
  ok: boolean;
};

export type TerminalSessionEvent =
  | {
      type: "data";
      id: string;
      text: string;
    }
  | {
      type: "exit";
      id: string;
      exit_code: number | null;
      signal: string | number | null;
      duration_ms: number;
      finished_at: string;
    }
  | {
      type: "error";
      id: string;
      message: string;
      finished_at: string;
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
export type ThreadItemPhase = "commentary" | "final_answer";

export type ToolCallDisplay = {
  kind?: string;
  text?: string;
};

export type Agent = {
  id: string;
  type?: string;
  task_name?: string;
  agent_profile?: string;
  agent_path?: string;
  parent_id?: string;
  description?: string;
  status: string;
  result?: string;
  error?: string;
  input_tokens?: number;
  output_tokens?: number;
  cache_creation_tokens?: number;
  cache_read_tokens?: number;
  nested_count?: number;
  nested_running_count?: number;
  started_at?: string;
  completed_at?: string | null;
};

export type Thread = {
  id: string;
  parent_id?: string;
  agent_path?: string;
  preview: string;
  title?: string;
  model_provider: string;
  model: string;
  cwd: string;
  status: ThreadStatus;
  read_only?: boolean;
  pinned?: boolean;
  archived?: boolean;
  forked_from_id?: string;
  forked_from_turn_id?: string;
  forked_from_item_id?: string;
  created_at: string;
  updated_at: string;
  turns: Turn[];
  child_agents?: Agent[];
  // Latest deduped, sorted list of localhost ports the agent surfaced
  // via the report_listening_ports tool. The first entry drives the
  // in-app browser auto-open behaviour; the full list is rendered as
  // clickable chips in the workspace sidebar.
  listening_ports?: number[];
  browser_state?: ThreadBrowserState;
};

export type ThreadBrowserState = {
  current_url?: string;
  primary_preview_url?: string;
  linked_process_id?: string;
};

export type ThreadSearchResultItem = {
  thread: Thread;
  snippet?: string;
};

export type ThreadSearchResult = {
  results: ThreadSearchResultItem[];
};

export type Turn = {
  id: string;
  items: ThreadItem[];
  items_view: TurnItemsView;
  status: TurnStatus;
  error?: { message: string };
  started_at?: string | null;
  completed_at?: string | null;
  duration_ms?: number;
};

export type TurnCompletedNotification = {
  thread_id: string;
  turn: Turn;
  content: string;
  input_tokens?: number;
  output_tokens?: number;
  cache_creation_tokens?: number;
  cache_read_tokens?: number;
  trace_path?: string;
};

export type TurnUsageNotification = {
  thread_id: string;
  turn_id: string;
  input_tokens?: number;
  output_tokens?: number;
  cache_creation_tokens?: number;
  cache_read_tokens?: number;
};

export type ThreadItem = {
  id: string;
  source_id?: string;
  type: ThreadItemType;
  status?: ThreadItemStatus;
  phase?: ThreadItemPhase;
  role?: string;
  text?: string;
  images?: InputImage[];
  files?: InputFile[];
  name?: string;
  arguments?: string;
  display?: ToolCallDisplay;
  result?: string;
  error?: string;
};

export type PlanStepStatus = "pending" | "in_progress" | "completed";

export type PlanStep = {
  step: string;
  status: PlanStepStatus;
};

export type PlanUpdate = {
  explanation?: string;
  plan: PlanStep[];
};

export type InputImage = {
  media_type: string;
  data: string;
};

export type InputFile = {
  media_type: string;
  data: string;
  filename?: string;
};

export type QueuedTurn = {
  id: string;
  thread_id: string;
  preview?: string;
  image_count?: number;
  file_count?: number;
};

export type ServerEvent = {
  workdir: string;
} & (
  | { kind: "notification"; message: AppServerNotification }
  | { kind: "server-request"; message: Required<AppServerRequest> }
  | { kind: "server-error"; message: string }
  | { kind: "server-exit"; code: number | null }
);

export type ToolApprovalRequest = {
  id: string;
  tool_name: string;
  call_id?: string;
  kind?: string;
  risk?: string;
  policy_action?: string;
  policy_reason?: string;
  classification_reason?: string;
  read_only?: boolean;
  destructive?: boolean;
  revision?: string;
  arguments_sha256?: string;
  arguments_preview?: string;
  approval_ref?: string;
  model_next_action?: string;
};

export type PendingToolApproval = ToolApprovalRequest & {
  server_request_id: string;
};

export type WindowResizeState = {
  resizing: boolean;
};

// ModelUsage aggregates token consumption and session count for one
// provider/model pair. Empty Provider+Model represents legacy token_usage
// rows persisted before provider/model were tracked; UI code renders
// these as "(unknown)".
export type ModelUsage = {
  provider: string;
  model: string;
  input_tokens: number;
  output_tokens: number;
  cache_creation_tokens: number;
  cache_read_tokens: number;
  sessions: number;
};

// SettingsUsageRange selects which time window the settings/usage RPC
// covers. Empty string is treated as "all" by the appserver.
export type SettingsUsageRange = "all" | "7d" | "30d" | "90d";

// SettingsUsageQuery is the input for the settings/usage RPC. Range
// selects the time window; empty defaults to "all".
export type SettingsUsageQuery = {
  range?: SettingsUsageRange;
};

// SettingsUsageResponse carries the aggregated usage snapshot returned
// to the desktop. ModelBreakdowns is sorted by total context tokens
// descending; empty Provider+Model entries are bucketed as "(unknown)"
// in the UI. CacheHitRate is the prompt-cache hit rate weighted by
// token count.
export type SettingsUsageResponse = {
  range: SettingsUsageRange;
  total_sessions: number;
  date_range: [string, string];
  model_breakdowns: ModelUsage[];
  cache_hit_rate: number;
  generated_at: string;
};

export type WuuDesktopApi = {
  listProjects: () => Promise<ProjectListResult>;
  createBlankProject: () => Promise<ProjectListResult>;
  chooseProjectFolder: () => Promise<ProjectListResult>;
  selectProject: (projectId: string) => Promise<ProjectListResult>;
  selectNoProject: (fresh?: boolean, cwd?: string) => Promise<ProjectListResult>;
  gitStatus: () => Promise<GitStatusResult>;
  listGitChanges: () => Promise<GitChangesResult>;
  readGitFileDiff: (path: string) => Promise<GitFileDiffResult>;
  checkoutGitBranch: (branch: string) => Promise<GitStatusResult>;
  createCheckoutGitBranch: (branch: string) => Promise<GitCreateBranchResult>;
  commitGitChanges: (params: GitCommitParams) => Promise<GitCommitResult>;
  createPullRequest: (params: GitPullRequestParams) => Promise<GitPullRequestResult>;
  listWorkspaceFiles: () => Promise<FileTreeListResult>;
  listWorkspaceDirectory: (path?: string) => Promise<WorkspaceDirectoryListResult>;
  readWorkspaceFile: (path: string) => Promise<WorkspaceFileReadResult>;
  startTerminalSession: (params?: TerminalSessionStartParams) => Promise<TerminalSessionStartResult>;
  writeTerminalSession: (id: string, data: string) => Promise<TerminalSessionActionResult>;
  resizeTerminalSession: (id: string, cols: number, rows: number) => Promise<TerminalSessionActionResult>;
  stopTerminalSession: (id: string) => Promise<TerminalSessionActionResult>;
  initialize: () => Promise<InitializeResult>;
  getBuildInfo: () => Promise<BuildInfoResult>;
  loadCodexModels: (provider?: string) => Promise<ConfigCodexModelsResult>;
  updateRuntimeSettings: (
    provider: string,
    model: string,
    effort?: string,
    connection?: RuntimeConnectionUpdate,
    variant?: string,
    permissionMode?: string
  ) => Promise<ConfigModelUpdateResult>;
  listManagedProcesses: () => Promise<ManagedProcessListResult>;
  stopManagedProcess: (processId: string) => Promise<ManagedProcessStopResult>;
  listSkills: () => Promise<SkillListResult>;
  getGoalSnapshot: (threadId?: string) => Promise<GoalSnapshotResult>;
  getGoalWorktreeReview: (worktreePath: string) => Promise<GoalWorktreeReviewResult>;
  cleanupGoalWorktree: (
    worktreePath: string,
    confirmUserApproved: boolean,
    confirmRemoveCleanWorktree: boolean
  ) => Promise<GoalWorktreeCleanupResult>;
  rollbackGoalWorktree: (
    worktreePath: string,
    confirmUserApproved: boolean,
    confirmDiscardWorktreeChanges: boolean
  ) => Promise<GoalWorktreeRollbackResult>;
  mergeGoalWorktree: (
    worktreePath: string,
    confirmUserApproved: boolean,
    confirmApplyWorktreeDiff: boolean,
    confirmTargetRepoMutation: boolean
  ) => Promise<GoalWorktreeMergeResult>;
  resolveGoalApproval: (
    goalId: string,
    approvalId: string,
    approved: boolean,
    rejected: boolean,
    resolvedBy: string,
    resolution: string,
    confirmUserApproved: boolean
  ) => Promise<GoalApprovalResolveResult>;
  startThread: () => Promise<{ thread: Thread }>;
  resumeThread: (sessionId?: string) => Promise<{ thread: Thread }>;
  forkThread: (threadId: string, turnId?: string, itemId?: string) => Promise<{ thread: Thread }>;
  listThreads: () => Promise<{ threads: Thread[] }>;
  searchThreads: (query: string, limit?: number) => Promise<ThreadSearchResult>;
  pinThread: (threadId: string, pinned: boolean) => Promise<{ thread: Thread }>;
  archiveThread: (threadId: string, archived: boolean) => Promise<{ thread: Thread }>;
  startTurn: (threadId: string, prompt: string, images?: InputImage[], files?: InputFile[]) => Promise<{ turn: Turn }>;
  queueTurn: (
    threadId: string,
    prompt: string,
    images?: InputImage[],
    clientId?: string,
    files?: InputFile[],
  ) => Promise<{ queued: QueuedTurn }>;
  dequeueTurn: (threadId: string, queueId: string) => Promise<{ ok: boolean }>;
  steerTurn: (
    threadId: string,
    expectedTurnId: string,
    prompt: string,
    images?: InputImage[],
    clientId?: string,
    files?: InputFile[],
  ) => Promise<{ turn_id: string }>;
  unsteerTurn: (threadId: string, steerId: string) => Promise<{ ok: boolean }>;
  interruptTurn: (threadId: string) => Promise<{ ok: boolean }>;
  respondToServerRequest: (id: string, result: unknown) => Promise<void>;
  rejectServerRequest: (id: string, message: string) => Promise<void>;
  onServerEvent: (handler: (event: ServerEvent) => void) => () => void;
  onTerminalEvent: (handler: (event: TerminalSessionEvent) => void) => () => void;
  onWindowResizeState: (handler: (state: WindowResizeState) => void) => () => void;
  renameThread: (threadId: string, title: string) => Promise<{ thread: Thread }>;
  revealSession: (threadId: string) => Promise<void>;
  getSettingsUsage: (range?: SettingsUsageRange) => Promise<SettingsUsageResponse>;
};

declare global {
  interface Window {
    wuu: WuuDesktopApi;
    wuuRenderableFileURL?: (encodedPath: string) => string;
  }
}
