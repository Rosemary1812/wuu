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
  // model_profile + tool_surface summarise the per-model tool
  // surface the runtime compiled for the active session. The
  // renderer uses these to drive capability-first activity,
  // approval UI, and the debug surface view; the runtime side
  // owns the values so the UI never has to re-derive the
  // bash-first / patch-first split.
  model_profile?: ModelProfileSummary;
  tool_surface?: ToolSurfaceSummary;
  extension_trust?: ExtensionTrustSummary;
  model_roles?: ModelRoleSummary[];
  providers?: ProviderSummary[];
  advanced_settings?: AdvancedSettingsSummary;
};

export type AdvancedSettingsSummary = {
  max_steps: number;
  max_context_tokens: number;
  temperature: number;
  compact_threshold_pct?: number;
  compact_keep_recent_tokens?: number;
  disable_auto_compact: boolean;
  provider_context_window?: number;
  context_window_tokens?: number;
  context_window_source?: string;
  input_limit_tokens?: number;
  output_reserve_tokens?: number;
  compact_threshold_tokens?: number;
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

export type ModelProfileSummary = {
  profile_name: string;
  provider: string;
  model: string;
  edit_primitive: string;
  bash_first: boolean;
};

export type ToolSurfaceSummary = {
  profile_name: string;
  provider?: string;
  model?: string;
  edit_primitive?: string;
  bash_first?: boolean;
  system_fragment?: string;
  tool_names: string[];
  hidden_tool_names: string[];
  capabilities: string[];
  hidden_capabilities: string[];
  tool_capability_map: Record<string, string>;
  hidden_capability_map: Record<string, string>;
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
  // model_profile + tool_surface mirror the initialize result. The
  // runtime recomputes the surface when the model changes, so the
  // renderer can re-key capability-aware UI off the new
  // profile without an extra initialize round-trip.
  model_profile?: ModelProfileSummary;
  tool_surface?: ToolSurfaceSummary;
  extension_trust?: ExtensionTrustSummary;
  model_roles?: ModelRoleSummary[];
  providers?: ProviderSummary[];
  advanced_settings?: AdvancedSettingsSummary;
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

export type MCPServerStatus = {
  name: string;
  state: string;
  auth_status?: string;
  connected: boolean;
  tool_count: number;
  error?: string;
};

export type MCPListResult = {
  servers: MCPServerStatus[];
};

export type MCPServerActionResult = {
  status: MCPServerStatus;
};

export type RuntimeConnectionUpdate = {
  base_url?: string;
  api_key?: string;
  create_provider?: boolean;
};

export type RuntimeAdvancedSettingsUpdate = {
  max_steps?: number;
  max_context_tokens?: number;
  temperature?: number;
  compact_threshold_pct?: number;
  compact_keep_recent_tokens?: number;
  disable_auto_compact?: boolean;
  provider_context_window?: number;
};

export type ConfigAdvancedUpdateResult = {
  advanced_settings: AdvancedSettingsSummary;
  providers?: ProviderSummary[];
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
  // Capability is the stable dotted identifier the runtime surface
  // maps this tool to (e.g. "command.bash"). Optional: legacy
  // callers that build a Display without a surface (e.g. older
  // builds, tests) leave it empty and the renderer falls back to
  // Kind.
  capability?: string;
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
  // workspace_kind tags the thread with the workspace it was created in.
  // "scratch" threads live in the desktop-managed scratch root
  // (~/.wuu/scratch/<date>) and have no registered project; the sidebar
  // surfaces them in the standalone "对话" section. Threads loaded from
  // older builds may omit this field — the renderer falls back to
  // classifying them by cwd against the known project list.
  workspace_kind?: "project" | "scratch";
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

export type ThreadEditDraft = {
  prompt: string;
  images?: InputImage[];
  files?: InputFile[];
};

export type ThreadEditMessageResult = {
  thread: Thread;
  draft: ThreadEditDraft;
};

export type ThreadContextCompositionResult = {
  thread_id: string;
  available: boolean;
  reason?: string;
  mode?: string;
  trace_path?: string;
  turn_id?: string;
  step_index?: number;
  provider?: string;
  model?: string;
  context_window_tokens?: number;
  prompt_tokens?: number;
  total_context_tokens?: number;
  retained_tokens?: number;
  input_tokens?: number;
  output_tokens?: number;
  cache_creation_tokens?: number;
  cache_read_tokens?: number;
  token_estimate_source?: string;
  message_count?: number;
  system_messages?: number;
  hidden_messages?: number;
  tool_count?: number;
  stable_prefix?: number;
  turn_prefix?: number;
  dynamic_context_bytes?: number;
  system_hash?: string;
  stable_prefix_hash?: string;
  turn_prefix_hash?: string;
  tool_surface_hash?: string;
  prompt_cache_key?: string;
  categories?: ContextCompositionCategory[];
  system_sections?: ContextCompositionSection[];
  block_kind_bytes?: Record<string, number>;
  segment_counts?: ContextSegmentCountSummary;
};

export type ContextCompositionCategory = {
  id: string;
  label: string;
  description?: string;
  tone?: string;
  bytes?: number;
  tokens?: number;
  contributes: boolean;
  durable?: boolean;
  cache_scope?: string;
  request_only?: boolean;
  deferred?: boolean;
};

export type ContextCompositionSection = {
  key: string;
  static: boolean;
  bytes: number;
  tokens?: number;
  hash?: string;
};

export type ContextSegmentCountSummary = {
  lifecycle?: Record<string, number>;
  placement?: Record<string, number>;
  cache_policy?: Record<string, number>;
};

export type Turn = {
  id: string;
  items: ThreadItem[];
  items_view: TurnItemsView;
  status: TurnStatus;
  // Structured turn-end error populated by the Go core's BuildTurnError.
  // Older clients that only read `message` still work because every new
  // field is optional; the front-end prefers `code` and `category` for
  // the visible chip and uses `action` to drive the recommended
  // next-step button. Mirrors internal/appserver/protocol.go::TurnError.
  error?: TurnError;
  started_at?: string | null;
  completed_at?: string | null;
  duration_ms?: number;
  input_tokens?: number;
  output_tokens?: number;
  context_tokens?: number;
  cache_creation_tokens?: number;
  cache_read_tokens?: number;
  usage_model?: string;
};

// Structured end-of-turn error from the Go core. The `message` is
// always present; every other field is optional and falls back to the
// front-end's UserFacingErrors classifier when missing (so a new
// category added server-side does not break an old front-end).
export type TurnError = {
  message: string;
  code?: string;
  category?: TurnErrorCategory;
  provider?: string;
  status_code?: number;
  action?: TurnErrorAction;
};

// Canonical error category taxonomy shared with the Go core. The values
// are the same strings BuildTurnError emits from
// internal/appserver/turn_error.go::TurnErrorCategory. The Go side has
// 7 categories that match the front-end's existing UserFacingErrorCategory
// 1:1; the front-end keeps its own internal vocabulary for legacy
// reasons and translates from these wire values when the action
// surfaces in the chip.
export type TurnErrorCategory =
  | "cancelled"
  | "network"
  | "auth"
  | "provider"
  | "tool"
  | "local"
  | "internal";

// Structured next-step the Go core wants the user to see. Mirrors
// opencode's Retryable.action shape. The `reason` is a stable enum
// suitable for telemetry; `title` / `message` / `label` are the
// user-facing strings; `link` is an optional URL or in-app focus hint
// the front-end can route to.
export type TurnErrorAction = {
  reason: string;
  title: string;
  message: string;
  label: string;
  link?: string;
};

export type TurnErrorNotification = {
  thread_id: string;
  turn_id: string;
  // Legacy string payload. The `error` field on `turn` (above) is
  // the structured version; the top-level `error` here stays for
  // backward compatibility with clients that did not yet read `turn.error`.
  error: string;
  // Flattened copy of the structured fields so listeners that only
  // watch the notification (and not the embedded `turn`) still get the
  // chip-ready values. Matches TurnErrorNotification in Go.
  code?: string;
  category?: TurnErrorCategory;
  provider?: string;
  status_code?: number;
  action?: TurnErrorAction;
  turn: Turn;
};

export type TurnCompletedNotification = {
  thread_id: string;
  turn: Turn;
  content: string;
  input_tokens?: number;
  output_tokens?: number;
  context_tokens?: number;
  cache_creation_tokens?: number;
  cache_read_tokens?: number;
  trace_path?: string;
};

export type TurnUsageNotification = {
  thread_id: string;
  turn_id: string;
  model?: string;
  input_tokens?: number;
  output_tokens?: number;
  context_tokens?: number;
  cache_creation_tokens?: number;
  cache_read_tokens?: number;
  // Resolved runtime context-window size for the active model at the
  // time this snapshot was emitted. Zero / undefined means the meter
  // should hide rather than render a divide-by-zero ratio.
  context_window_tokens?: number;
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
  reason?: string;
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
  permission?: string;
  permission_patterns?: string[];
  permission_always?: string[];
  permission_rule?: string;
  model_next_action?: string;
  // capability + capability_object + capability_action are the
  // capability-first view of the same approval. The renderer
  // shows these so the user reads "approval for command.bash
  // / git push origin main" instead of "approval for run_shell".
  capability?: string;
  capability_object?: string;
  capability_action?: string;
  capability_rule?: string;
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

// SettingsUsageMetrics is the headline number block shown at the top of
// the desktop usage page. Every number is summed across token_usage rows
// whose At timestamp falls inside the requested range. Prompt tokens
// count input + cache_read (the prompt side); context tokens also add
// output so the user sees the full token footprint per session.
export type SettingsUsageMetrics = {
  prompt_tokens: number;
  context_tokens: number;
  input_tokens: number;
  output_tokens: number;
  cache_read_tokens: number;
  cache_creation_tokens: number;
  cache_hit_rate: number;
  turns: number;
  agents: number;
  date_range: [string, string];
  active_days: number;
};

// SettingsUsageDay is one calendar day of token activity, bucketed by
// the token_usage row's At timestamp. Days are emitted in ascending
// date order; the desktop fills in the heatmap gaps locally so the
// backend only ships days that actually saw activity.
export type SettingsUsageDay = {
  date: string;
  input_tokens: number;
  output_tokens: number;
  cache_creation_tokens: number;
  cache_read_tokens: number;
  cache_hit_rate: number;
  turns: number;
  agents: number;
};

// SettingsUsageEntry is one recent token-spending record surfaced in
// the "最近记录" list. Source distinguishes primary-session turns from
// subagent runs so the UI can label them differently.
export type SettingsUsageEntry = {
  id: string;
  source: "turn" | "agent";
  title: string;
  provider: string;
  model: string;
  at: string;
  input_tokens: number;
  output_tokens: number;
  cache_creation_tokens: number;
  cache_read_tokens: number;
};

// SettingsUsageResponse is the single source of truth for the desktop
// usage page. ModelBreakdowns is sorted by total context tokens
// descending; empty Provider+Model entries are bucketed as "(unknown)"
// in the UI. Days carries the calendar-day series for the heatmap and
// Entries carries the most recent N rows for the "最近记录" list —
// both are derived from the same per-row token_usage trail so the
// three views always sum to the same totals.
export type SettingsUsageResponse = {
  range: SettingsUsageRange;
  total_sessions: number;
  generated_at: string;
  metrics: SettingsUsageMetrics;
  model_breakdowns: ModelUsage[];
  days: SettingsUsageDay[];
  entries: SettingsUsageEntry[];
};

// ComposerGoalSummary is the composer-banner view of the current thread goal.
// The backend owns runtime status, control availability, and progress text so
// the renderer can stay a thin control surface instead of rebuilding goal
// orchestration state.
export type ComposerGoalSummary = {
  id: string;
  thread_id?: string;
  text: string;
  status: string;
  step?: string;
  started_at?: string;
  updated_at?: string;
  stop_reason?: string;
  recent_progress?: string;
  tokens_used?: number;
  time_used_seconds?: number;
  goal_turns?: number;
  blocker?: string;
  blocker_consecutive_turns?: number;
  can_pause?: boolean;
  can_resume?: boolean;
  can_cancel?: boolean;
  can_clear?: boolean;
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
  updateAdvancedSettings: (
    settings: RuntimeAdvancedSettingsUpdate
  ) => Promise<ConfigAdvancedUpdateResult>;
  listManagedProcesses: () => Promise<ManagedProcessListResult>;
  stopManagedProcess: (processId: string) => Promise<ManagedProcessStopResult>;
  listMCPServers: () => Promise<MCPListResult>;
  connectMCPServer: (name: string) => Promise<MCPServerActionResult>;
  disconnectMCPServer: (name: string) => Promise<MCPServerActionResult>;
  refreshMCPServer: (name: string) => Promise<MCPServerActionResult>;
  listSkills: () => Promise<SkillListResult>;
  startThread: () => Promise<{ thread: Thread }>;
  resumeThread: (sessionId?: string) => Promise<{ thread: Thread }>;
  forkThread: (threadId: string, turnId?: string, itemId?: string) => Promise<{ thread: Thread }>;
  editThreadMessage: (threadId: string, turnId: string, itemId: string) => Promise<ThreadEditMessageResult>;
  getThreadContextComposition: (threadId: string) => Promise<ThreadContextCompositionResult>;
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
  // Composer goal banner surface. The renderer only needs a lightweight
  // summary plus explicit runtime controls; the full GoalSnapshot and
  // workflow/agent run detail stay on the agent tool loop.
  getActiveGoalSummary: (threadId?: string) => Promise<ComposerGoalSummary | null>;
  pauseGoal: (goalId: string, threadId?: string) => Promise<{ ok: boolean }>;
  resumeGoal: (goalId: string, threadId?: string) => Promise<{ ok: boolean }>;
  cancelGoal: (goalId: string, threadId?: string) => Promise<{ ok: boolean }>;
  clearGoal: (goalId: string, threadId?: string) => Promise<{ ok: boolean }>;
  updateGoalText: (
    goalId: string,
    text: string,
    threadId?: string
  ) => Promise<{ ok: boolean }>;
};

declare global {
  interface Window {
    wuu: WuuDesktopApi;
    wuuRenderableFileURL?: (encodedPath: string) => string;
  }
}
