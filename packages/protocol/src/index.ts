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

export const APP_SERVER_PROTOCOL_VERSION = "wuu-app-server/v0.1";

export const BROWSER_REVERSE_RPC_METHODS = [
  "browser/cdp",
  "browser/screenshot",
  "browser/open_tab",
  "browser/close_tab",
  "browser/set_visibility",
  "browser/list_tabs",
] as const;

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

export type WorkspaceItemMenuResult = {
  action: "none";
};

export type BuildInfoResult = {
  core: CoreBuildInfo | undefined;
  desktop: DesktopBuildInfo;
};

export type RuntimeHostSummary = {
  kind: "local" | "cloud";
  instance_id?: string;
};

export type InitializeParams = {
  protocol_version?: string;
  client?: {
    name?: string;
    version?: string;
  };
  capabilities?: {
    reverse_rpc?: {
      methods?: string[];
    };
  };
};

export type InitializeResult = {
  status?: "ready" | "needs_setup";
  issues?: RuntimeIssue[];
  protocol_version: string;
  core?: CoreBuildInfo;
  provider: string;
  model: string;
  effort?: string;
  variant?: string;
  ultra?: boolean;
  // Effective anonymous-worker execution capacity after applying the
  // default (docs/app-server-protocol.md, Ultra Mode Configuration).
  max_parallel?: number;
  runtime_host?: RuntimeHostSummary;
  workspace_root: string;
  permissions?: PermissionSummary;
  // model_profile + tool_surface summarise the workspace-default runtime.
  // Existing sessions expose their persisted provider/model selection on
  // Thread, and an in-progress Turn exposes the admitted provider/model. A
  // shell must not present InitializeResult.provider/model as the active
  // session after those selections diverge.
  model_profile?: ModelProfileSummary;
  tool_surface?: ToolSurfaceSummary;
  extension_trust?: ExtensionTrustSummary;
  // Inventory describes discovered extension assets. It intentionally carries
  // fingerprints and permission names, never resolved environment/header values.
  extension_inventory?: ExtensionInventoryRecord[];
  model_roles?: ModelRoleSummary[];
  model_aliases?: Record<string, ModelAliasSummary>;
  providers?: ProviderSummary[];
  advanced_settings?: AdvancedSettingsSummary;
  general_settings?: GeneralSettingsSummary;
  features?: FeatureFlags;
};

export type FeatureFlags = {
  helpme: boolean;
  // Advertises that this client can host the embedded browser backend
  // (hidden WebContentsView + CDP bridge). Mirrors appserver.FeatureFlags.
  browser?: boolean;
};

// Browser* are the core→desktop server-initiated request payloads. The core
// sends these TO the desktop over the reverse-RPC channel and awaits a Response;
// they are not client→core requests. Mirrored field-for-field from
// internal/appserver/protocol.go. No payload carries an activity_id: the client
// auto-rejects server requests naming a stopped activity, which would wedge a
// CDP call the moment a tab's activity is torn down. Tabs are addressed by
// tab_id, minted core-side.
export type BrowserCDPParams = {
  workdir: string;
  tab_id: string;
  method: string;
  params?: JsonValue;
};

export type BrowserCDPResult = {
  // Present when the desktop inlines the CDP result. When the >1MB size gate
  // spills it to disk, result is omitted and path/size describe the file.
  result?: JsonValue;
  path?: string;
  size?: number;
};

export type BrowserScreenshotParams = {
  workdir: string;
  tab_id: string;
  dest_path: string;
  format?: string;
};

export type BrowserScreenshotResult = {
  width: number;
  height: number;
  path: string;
};

export type BrowserOpenTabParams = {
  workdir: string;
  tab_id: string;
  initial_url?: string;
};

export type BrowserCloseTabParams = {
  workdir: string;
  tab_id: string;
};

export type BrowserSetVisibilityParams = {
  workdir: string;
  tab_id: string;
  visible: boolean;
};

export type BrowserListTabsParams = {
  workdir: string;
};

export type BrowserListTabsResult = {
  tab_ids: string[];
};

export type RuntimeIssue = {
  code: string;
  provider?: string;
  message: string;
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

export type GeneralSettingsSummary = {
  append_system_prompt: string;
  git_attribution_enabled?: boolean;
  memory_disabled: boolean;
  mcp_server_enabled: Record<string, boolean>;
  dream_enabled: boolean;
  dream_interval_days: number;
  dream_provider?: string;
  dream_model?: string;
};

export type PermissionSummary = {
  mode?: string;
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

export type ExtensionKind = "skill" | "command" | "mcp" | "hook" | "plugin";

export type ExtensionState = "active" | "read_only" | "pending" | "granted" | "rejected" | "changed";

export type ExtensionProvenance = {
  kind: ExtensionKind;
  source: string;
  scope: string;
  path?: string;
  plugin_id?: string;
  official?: boolean;
};

export type ExtensionInventoryRecord = {
  id: string;
  name: string;
  description?: string;
  kind: ExtensionKind;
  provenance: ExtensionProvenance;
  state: ExtensionState;
  executable?: boolean;
  fingerprint?: string;
  grant_scope?: "action" | "session" | "project" | "user";
  requested_permissions?: string[];
  unsupported_fields?: string[];
};

export type ConfigModelUpdateResult = {
  provider: string;
  model: string;
  effort?: string;
  variant?: string;
  ultra: boolean;
  // Readback of the effective anonymous-worker capacity, mirroring
  // InitializeResult.max_parallel.
  max_parallel: number;
  permissions?: PermissionSummary;
  // model_profile + tool_surface mirror the initialize result. The
  // runtime recomputes the surface when the model changes, so the
  // renderer can re-key capability-aware UI off the new
  // profile without an extra initialize round-trip.
  model_profile?: ModelProfileSummary;
  tool_surface?: ToolSurfaceSummary;
  extension_trust?: ExtensionTrustSummary;
  model_roles?: ModelRoleSummary[];
  model_aliases?: Record<string, ModelAliasSummary>;
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

export type ModelAliasSummary = {
  provider: string;
  model: string;
  effort?: string;
  variant?: string;
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

export type SkillContentParams = {
  name: string;
  source: string;
};

export type SkillContentResult = {
  content: string;
};

export type NamedAgent = {
  id: string;
  name: string;
  memory_dir: string;
  avatar_key: string;
  avatar_image?: string;
  provider_override?: string;
  model_override?: string;
  autostart: boolean;
  created_at: string;
  activity_status?: "idle" | "thinking";
};

export type ChannelRoomMember = {
  room_id: string;
  member_type: "human" | "agent";
  member_id: string;
  joined_at: string;
};

export type ChannelRoom = {
  id: string;
  kind: "channel" | "dm";
  name: string;
  created_by: string;
  created_at: string;
  members: ChannelRoomMember[];
};

export type ChannelMessage = {
  id: string;
  room_id: string;
  seq: number;
  thread_id?: string;
  author_type: "human" | "agent";
  author_id: string;
  kind: "text" | "task" | "system";
  body: string;
  mentions?: string[];
  reply_to?: string;
  task_state?: string;
  task_owner?: string;
  created_at: string;
};

export type ChannelAgentListResult = { agents: NamedAgent[] };
export type ChannelBootstrapResult = { agents: NamedAgent[]; rooms: ChannelRoom[] };
export type ChannelAgentCreateParams = {
  name: string;
  avatar_key?: string;
  avatar_image?: string;
  provider_override?: string;
  model_override?: string;
};
export type ChannelAgentCreateResult = { agent: NamedAgent };
export type ChannelAgentUpdateParams = ChannelAgentCreateParams & { agent_id: string };
export type ChannelAgentUpdateResult = { agent: NamedAgent };
export type ChannelAgentDeleteParams = { agent_id: string };
export type ChannelAgentDeleteResult = { deleted: boolean };
export type ChannelAgentStartParams = { agent_id: string };
export type ChannelAgentStartResult = { agent: NamedAgent };
export type ChannelRoomListResult = { rooms: ChannelRoom[] };
export type ChannelRoomCreateParams = {
  name: string;
  agent_ids?: string[];
};
export type ChannelRoomCreateResult = { room: ChannelRoom };
export type ChannelRoomDeleteParams = { room_id: string };
export type ChannelRoomDeleteResult = { deleted: boolean };
export type ChannelMessageListParams = {
  room_id: string;
  after_seq?: number;
  limit?: number;
};
export type ChannelMessageListResult = { messages: ChannelMessage[] };
export type ChannelMessageSendParams = {
  room_id: string;
  thread_id?: string;
  reply_to?: string;
  body: string;
};
export type ChannelMessageSendResult = { message: ChannelMessage };
export type ChannelTaskCreateParams = {
  room_id: string;
  title: string;
  owner_id: string;
};
export type ChannelTaskCreateResult = { task: ChannelMessage };
export type ChannelTaskUpdateParams = {
  task_id: string;
  state?: "open" | "doing" | "done";
  owner_id?: string;
};
export type ChannelTaskUpdateResult = { task: ChannelMessage };
export type ChannelHumanMentionStatusResult = { count: number };
export type ChannelHumanMentionAckResult = { acknowledged: number };

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

export type MCPAuthStartResult = {
  authorization_url: string;
  state: string;
  scopes?: string[];
};

export type MCPAuthStatusResult = {
  name: string;
  authenticated: boolean;
  expires_at?: string;
  scopes?: string[];
};

export type MCPAuthFinishResult = {
  auth: MCPAuthStatusResult;
  server: MCPServerStatus;
};

export type MCPAuthRemoveResult = {
  auth: MCPAuthStatusResult;
  server: MCPServerStatus;
};

export type ActivitySession = {
  id: string;
  kind: "browser" | "cua";
  thread_id: string;
  workdir: string;
  plugin_id?: string;
  target?: string;
  process_id?: number;
  window_id?: number;
  state: "starting" | "active" | "background_controlled" | "foreground_controlled" | "user_controlled" | "waiting_confirmation" | "stopped" | "error";
  controller: "agent" | "user" | "none";
  preview?: string;
  error?: string;
  interaction?: {
    kind: "click" | "drag" | "scroll" | "type";
    x: number;
    y: number;
    to_x?: number;
    to_y?: number;
    direction?: string;
    revision: number;
  };
  created_at: string;
  updated_at: string;
};

export type ActivityListResult = {
  activities: ActivitySession[];
};

export type ActivityActionResult = {
  activity: ActivitySession;
};

export type ActivityReleaseResult = {
  activity: ActivitySession;
  lease_token: string;
};

// Embedded-browser visibility takeover (M3). When an agent-owned browser
// activity is promoted to a foreground (visible) state the renderer streams
// the on-screen position + size of the browser panel so the main process can
// keep its (main-owned) WebContentsView aligned over it. Values are CSS
// pixels relative to the window's top-left corner.
export type BrowserBoundsRect = {
  x: number;
  y: number;
  width: number;
  height: number;
};

export type RuntimeConnectionUpdate = {
  base_url?: string;
  api_key?: string;
  auth_token?: string;
  // Optional provider protocol type used when creating a new provider.
  // Supported values: "openai", "openai-compatible", "anthropic", "claude",
  // "anthropic-official". Omitted or empty keeps the default of
  // "openai-compatible". OAuth-managed Codex types are intentionally not
  // listed because they require a separate connection flow.
  type?: string;
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
  model_aliases?: Record<string, ModelAliasSummary>;
};

export type ConfigAdvancedUpdateResult = {
  advanced_settings: AdvancedSettingsSummary;
  model_aliases?: Record<string, ModelAliasSummary>;
  providers?: ProviderSummary[];
};

export type RuntimeGeneralSettingsUpdate = {
  append_system_prompt?: string;
  git_attribution_enabled?: boolean;
  memory_disable?: boolean;
  mcp_enabled_toggles?: Record<string, boolean>;
  dream_enabled?: boolean;
  dream_interval_days?: number;
  dream_provider?: string;
  dream_model?: string;
};

export type ConfigGeneralUpdateResult = {
  general_settings: GeneralSettingsSummary;
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
  // Derived (not persisted): true when the project's path cannot currently be
  // accessed as a directory — for example because it was moved, deleted, or
  // its volume is temporarily unavailable. The main process computes it fresh
  // on every list(); the sidebar greys such a workspace out and disables its
  // "新建会话" affordance.
  missing?: boolean;
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

export type ProjectRuntimeIssue = {
  code: "active_project_unavailable";
  message: string;
  project_id: string;
  cwd: string;
};

export type ProjectListResult = {
  projects: DesktopProject[];
  active_context?: RuntimeContext;
  active_project_id?: string;
  // A selected project stays selected while its directory is temporarily
  // unavailable. Runtime consumers must surface this issue instead of silently
  // routing work to the no-project scratch workspace.
  runtime_issue?: ProjectRuntimeIssue;
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

export type GitChangeStatus = "modified" | "added" | "deleted" | "renamed" | "copied" | "untracked" | "ignored" | "unknown";

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
  mtime_ms: number;
  sha256: string;
  binary: boolean;
  truncated: boolean;
  text?: string;
  renderable_url?: string;
  renderable_kind?: "image" | "pdf";
};

export type WorkspaceFileSaveParams = {
  path: string;
  text: string;
  base_mtime_ms: number;
  base_sha256: string;
};

export type WorkspaceFileSaveResult = {
  status: "saved" | "conflict";
  file: WorkspaceFileReadResult;
};

export type WorkspaceFileReferenceResolveResult = {
  root: string;
  reference: string;
  status: "resolved" | "missing" | "ambiguous" | "invalid";
  path?: string;
  absolute_path?: string;
  matches?: string[];
};

export type TerminalSessionStartParams = {
  cols?: number;
  rows?: number;
  // Optional absolute directory override to spawn the pty in, instead of
  // the active runtime context's cwd. Used to root the terminal at the
  // active thread's own cwd (e.g. a worktree fork) — see
  // workspacePanelContext in AppState.ts.
  cwd?: string;
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

export type ManagedProcessStatus =
  | "starting"
  | "running"
  | "stopping"
  | "stopped"
  | "failed";

export type ManagedProcessSummary = {
  id: string;
  owner_kind: string;
  owner_id: string;
  lifecycle: string;
  status: ManagedProcessStatus;
  pid: number;
  tty?: boolean;
  command: string;
  cwd: string;
  started_at: string;
  updated_at: string;
  stopped_at?: string;
  exit_code?: number;
  last_error?: string;
  input_available?: boolean;
};

export type ManagedProcessListResult = {
  processes: ManagedProcessSummary[];
};

export type ManagedProcessReadParams = {
  thread_id: string;
  process_id: string;
  offset_bytes?: number;
  max_bytes?: number;
  wait_ms?: number;
};

export type ManagedProcessReadResult = {
  process: ManagedProcessSummary;
  output: string;
  truncated: boolean;
  start_offset: number;
  end_offset: number;
  total_bytes: number;
  timed_out: boolean;
};

export type ManagedProcessActionResult = {
  process: ManagedProcessSummary;
};

export type ManagedProcessWriteResult = ManagedProcessActionResult & {
  bytes_written: number;
};

export type ThreadStatus = "idle" | "in_progress";
export type TurnStatus = "in_progress" | "completed" | "failed" | "interrupted";
export type TurnKind = "user" | "internal" | "compact";
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

export type ToolResultContentPart = {
  type: string;
  text?: string;
  data?: string;
  mime_type?: string;
  uri?: string;
  name?: string;
  resource?: JsonValue;
};

export type ToolResultActivityRef = {
  id: string;
  kind: string;
  state?: string;
  thread_id?: string;
  preview_uri?: string;
};

export type ToolResult = {
  content?: ToolResultContentPart[];
  structured_content?: JsonValue;
  meta?: JsonValue;
  is_error?: boolean;
  activity?: ToolResultActivityRef;
};

// ParticipantSummary is the wire identity attached to named subagent activity
// for display attribution.
export type ParticipantSummary = {
  id: string;
  name: string;
  kind: string;
  role?: string;
  // avatar_image is an uploaded image data URL. The backend fills it for every
  // summary resolved from the participant store, but caps the embedded payload at
  // 64KB raw bytes (appserver participantSummaryAvatarMaxBytes):
  // summaries are duplicated into each thread item they attribute, so
  // larger uploads degrade to the initial-letter avatar here while
  // still rendering in full-profile surfaces (profile panel,
  // participant/list). Optional because synthesized fallback summaries
  // (ephemeral snapshots) never carry it.
  avatar_image?: string;
  // busy reports that this named agent is currently executing a
  // task/workflow run and cannot take a second concurrent pull
  // (decision-five concurrency lock). Optional/false when idle or not a named
  // agent.
  busy?: boolean;
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
  model_alias?: string;
  model_alias_fallback?: boolean;
  provider?: string;
  model?: string;
  api_model?: string;
  effort?: string;
  variant?: string;
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
  // Pinned and Archived mirror the underlying session metadata for the
  // sub-agent's own session so the info panel can offer pin/archive
  // actions without an extra round-trip.
  pinned?: boolean;
  archived?: boolean;
  participant?: ParticipantSummary;
};

// ---------------------------------------------------------------------------
// Memory panel (设置 → 记忆). Wire contract fixed ahead of implementation by
// docs/plans/2026-07-04-memory-redesign.md §8.2. The three RPCs are served
// by the M2 memory-panel backend; a backend without M2 rejects them with an
// "unknown method" error, which the renderer maps to a
// "记忆面板后端尚未就绪" placeholder instead of crashing.

// MemoryScope selects which notebook an RPC targets: "user" is the user
// notebook (~/.wuu/memory). The participant scope is no longer exposed by the
// product; the type remains for backwards compatibility with persisted data.
export type MemoryScope = "user" | "participant";

export type MemoryOverviewParams = {
  scope: MemoryScope;
  participant_id?: string;
  force_refresh?: boolean;
};

// memory/overview result: the structured essay the overview agent generated
// from the real notebook (one LLM pass). cached indicates the backend served
// its (notebook, index mtime) cache instead of regenerating.
export type MemoryOverviewResult = {
  essay_md: string;
  generated_at: string;
  source_mtime: string;
  cached: boolean;
};

export type MemoryChatParams = {
  scope: MemoryScope;
  participant_id?: string;
  message: string;
};

export type MemoryChangedFileAction = "created" | "modified" | "deleted";

export type MemoryChangedFile = {
  path: string;
  action: MemoryChangedFileAction;
};

// memory/chat result: the manager agent's natural-language reply plus the
// real files it touched (topic files and/or the MEMORY.md index).
export type MemoryChatResult = {
  reply_md: string;
  changed_files: MemoryChangedFile[];
};

export type MemoryReadParams = {
  scope: MemoryScope;
  participant_id?: string;
};

// One real memory file in the notebook. `type` mirrors the file's
// frontmatter; canonical values are user | feedback | reference | lesson but
// it stays a string so entries written by newer backends still render.
export type MemoryFileInfo = {
  name: string;
  description: string;
  type: string;
  mtime: string;
};

// memory/read result: the raw MEMORY.md index plus the file inventory —
// no LLM involved; the panel's "查看原文" audit/fallback view.
export type MemoryReadResult = {
  index_md: string;
  files: MemoryFileInfo[];
};

export type WorktreeInfo = {
  path: string;
  base_head?: string;
  base_repo?: string;
  dirty?: boolean;
  changed_files?: string[];
};

export type Thread = {
  id: string;
  parent_id?: string;
  agent_path?: string;
  preview: string;
  title?: string;
  source?: string;
  model_provider: string;
  model: string;
  model_variant?: string;
  model_effort?: string;
  permission_mode?: string;
  cwd: string;
  // workspace_kind tags the thread with the workspace it was created in.
  workspace_kind?: "project" | "scratch";
  status: ThreadStatus;
  read_only?: boolean;
  pinned?: boolean;
  archived?: boolean;
  forked_from_id?: string;
  forked_from_turn_id?: string;
  forked_from_item_id?: string;
  worktree?: WorktreeInfo;
  created_at: string;
  updated_at: string;
  turns: Turn[];
  child_agents?: Agent[];
};

export type ThreadStartParams = {
  ephemeral?: boolean;
};

export type ThreadSearchResultItem = {
  thread: Thread;
  snippet?: string;
};

export type ThreadSearchResult = {
  results: ThreadSearchResultItem[];
};

export type ThreadPreviewResult = {
  turns: Turn[];
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

// ThreadForkResult mirrors the JSON-RPC `thread/fork` response. `mode` on
// `forkThread` decides whether the backend creates a git worktree; the
// resulting thread carries whatever the snapshot produced, including the
// `worktree` block when forking into one.
export type ThreadForkResult = {
  thread: Thread;
  worktree?: WorktreeInfo;
};

// ============================================================================
// Side Thread
//
// A side thread is bound to a main thread and shares its context but does
// not pollute the main conversation. It exists only inside the session tab
// that owns its main thread; it never appears in the global session list
// and cannot be detached into a new tab. Each main thread owns at most one.
// ============================================================================

// A side thread begins running on its first persisted message. Deleting the
// main thread deletes the side thread instead of creating another status.
export type SideThreadStatus =
  | "running"
  | "completed"
  | "failed"
  | "interrupted";

// One side thread attached to a main thread. Deleting the main thread
// removes its side thread.
export type SideThreadSummary = {
  side_thread_id: string;
  main_thread_id: string;
  status: SideThreadStatus;
  // Monotonic durable version used to reject stale responses and events.
  revision: number;
  // Lightweight state for the main-task status shown in the panel header.
  main_task_summary?: {
    running: boolean;
    last_user_message?: string;
  };
  created_at: string;
  updated_at: string;
};

// Side-thread history is independent and never appends to the main thread.
export type SideThreadMessage = {
  id: string;
  side_thread_id: string;
  role: "user" | "assistant";
  text: string;
  // Canonical assistant/process items rendered by the shared TurnView.
  // Absent on legacy text-only side-thread records.
  items?: ThreadItem[];
  // Assistant messages expose only the fields required by the side panel.
  status?: "streaming" | "completed" | "failed" | "interrupted";
  error_message?: string;
  created_at: string;
};

// Opening a side thread is lazy: if no side thread exists for this main
// thread yet, `openSideThread` returns `summary: null` and the side panel
// renders an empty state. The actual agent thread is only created the
// first time the user sends a message in the side panel.
export type SideThreadOpenResult = {
  summary: SideThreadSummary | null;
};

export type SideThreadHistoryResult = {
  summary: SideThreadSummary;
  messages: SideThreadMessage[];
};

export type SideThreadSendParams = {
  main_thread_id: string;
  // The user's prompt. Empty prompts are rejected by the IPC layer.
  prompt: string;
};

export type SideThreadSendResult = {
  // Echoes the persisted id assigned to the user's prompt.
  user_message_id: string;
  summary: SideThreadSummary;
};

// The app-server sends these as `sideThread/event` notifications. Electron
// forwards their params through the dedicated `wuu:side-thread-event` channel.
export type SideThreadEvent =
  | {
      type: "status";
      side_thread_id: string;
      main_thread_id: string;
      summary: SideThreadSummary;
    }
  | {
      type: "delta";
      side_thread_id: string;
      main_thread_id: string;
      revision: number;
      // id of the assistant message being streamed; matches the
      // SideThreadMessage.id once the message is finalized.
      message_id: string;
      text_delta: string;
    }
  | {
      type: "items";
      side_thread_id: string;
      main_thread_id: string;
      revision: number;
      message_id: string;
      items: ThreadItem[];
    }
  | {
      type: "message";
      side_thread_id: string;
      main_thread_id: string;
      revision: number;
      message: SideThreadMessage;
    }
  | {
      type: "error";
      side_thread_id: string;
      main_thread_id: string;
      revision: number;
      message_id: string;
      error_message: string;
    }
  | {
      // The side thread was reset: its record is gone and the panel starts
      // over. The next send rebases onto the latest main history.
      type: "reset";
      side_thread_id: string;
      main_thread_id: string;
    };

// Electron broadcasts app-server notifications to every window. Keep the
// source runtime attached so renderers can reject events from other workspaces.
export type SideThreadEventEnvelope = {
  workdir: string;
  event: SideThreadEvent;
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
  input_limit_tokens?: number;
  usable_input_tokens?: number;
  compact_threshold_tokens?: number;
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

// Two-level origin the desktop shows for an instruction file: "global"
// (user-level, applies everywhere) or "project" (discovered in the project
// hierarchy). Mirrors appserver.instructionFileScope.
export type InstructionFileScope = "global" | "project";

// InstructionFile mirrors appserver.InstructionFile: one AGENTS.md / CLAUDE.md
// style file that memory.Discover loaded into the base system prompt.
export type InstructionFile = {
  path: string;
  name: string;
  source: string;
  scope: InstructionFileScope;
  bytes: number;
  content: string;
};

export type InstructionsListResult = {
  files: InstructionFile[];
};

export type Turn = {
  id: string;
  kind?: TurnKind;
  // The runtime selection captured when this turn began. It remains stable
  // while a later default-model update prepares subsequent turns.
  model_provider?: string;
  model?: string;
  items: ThreadItem[];
  items_view: TurnItemsView;
  status: TurnStatus;
  // Structured turn-end error populated by the Go core's BuildTurnError.
  // Older clients that only read `message` still work because every new
  // field is optional; shells use these diagnostic facts to derive a
  // concise read-only system event. Mirrors internal/appserver/protocol.go::TurnError.
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
};

// Canonical error category taxonomy shared with the Go core. The values
// are the same strings BuildTurnError emits from
// internal/appserver/turn_error.go::TurnErrorCategory; keep the two lists
// in sync. Shells translate these diagnostic values into their own
// user-facing system-event vocabulary and must degrade an unrecognized
// wire value to their internal-error rendering (a newer core may emit
// categories an older shell does not know yet).
export type TurnErrorCategory =
  | "cancelled"
  | "network"
  | "auth"
  | "provider"
  | "invalid_request"
  | "tool"
  | "local"
  | "internal";

export type TurnErrorNotification = {
  thread_id: string;
  turn_id: string;
  // Legacy string payload. The `error` field on `turn` (above) is
  // the structured version; the top-level `error` here stays for
  // backward compatibility with clients that did not yet read `turn.error`.
  error: string;
  // Flattened copy of the structured fields so listeners that only
  // watch the notification (and not the embedded `turn`) still get the
  // diagnostic values. Matches TurnErrorNotification in Go.
  code?: string;
  category?: TurnErrorCategory;
  provider?: string;
  status_code?: number;
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
  // Resolved runtime context ceiling for the active provider/model at
  // the time this snapshot was emitted. This may be the model window or
  // a lower provider input cap. Zero / undefined means the meter should
  // hide rather than render a divide-by-zero ratio.
  context_window_tokens?: number;
};

export type StreamLifecyclePhase =
  | "connecting"
  | "connected"
  | "reconnecting"
  | "failed";

export type StreamLifecyclePayload = {
  phase: StreamLifecyclePhase | string;
  operation_id?: string;
  operation_kind?: string;
  workload_profile?: string;
  payload_version?: number;
  attempt_id?: string;
  attempt?: number;
  max_attempts?: number;
  submission_id?: string;
  submission_count?: number;
  retry_count?: number;
  max_retries?: number;
  retry_in_ms?: number;
  elapsed_ms?: number;
  budget_ms?: number;
  reason?: string;
  reset_partial?: boolean;
};

export type ProviderStatePayload = {
  step_index?: number;
  provider?: string;
  protocol?: string;
  transport?: string;
  configured_transport?: string;
  replay_mode?: string;
  previous_response_id_used?: boolean;
  connection_reused?: boolean;
  diagnostic?: string;
  transport_failure_phase?: string;
  failed_transport?: string;
  fallback_transport?: string;
  events_emitted?: boolean;
  fallback_active?: boolean;
  fallback_reason?: string;
  input_items?: number;
  full_input_items?: number;
  delta_input_items?: number;
};

export type StreamEventPayload = {
  type: string;
  lifecycle?: StreamLifecyclePayload;
  provider_state?: ProviderStatePayload;
};

export type TurnEventNotification = {
  thread_id: string;
  turn_id: string;
  event: StreamEventPayload;
};

export type ThreadItem = {
  id: string;
  // seq is the message's stable per-thread address (session_messages.seq).
  // Absent for synthetic or unpersisted items.
  seq?: number;
  source_id?: string;
  agent_id?: string;
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
  result_detail?: ToolResult;
  error?: string;
  reason?: string;
};

export type ThreadForkTarget = Pick<ThreadItem, "type" | "seq" | "source_id">;

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

export type HeldUserMessage = {
  id: string;
  thread_id: string;
  origin: "queue" | "steer";
  prompt?: string;
  images?: InputImage[];
  files?: InputFile[];
};

export type ThreadResumeResult = {
  thread: Thread;
  held_user_messages?: HeldUserMessage[];
};

export type ServerEvent = {
  workdir: string;
} & (
  | { kind: "notification"; message: AppServerNotification }
  | { kind: "server-request"; message: Required<AppServerRequest> }
  | { kind: "server-error"; message: string }
  | { kind: "server-exit"; code: number | null; message: string }
);

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

// SettingsUsageQuery is the input for the settings/usage RPC. It takes
// no parameters: the snapshot always covers the full recorded history.
export type SettingsUsageQuery = Record<string, never>;

// SettingsUsageMetrics is the headline number block shown at the top of
// the desktop usage page. Every number is summed across the full
// token_usage trail. Prompt tokens count input + cache_read (the prompt
// side); context tokens also add output so the user sees the full token
// footprint per session.
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

// SettingsUsageResponse is the single source of truth for the desktop
// usage page. ModelBreakdowns is sorted by total context tokens
// descending; empty Provider+Model entries are bucketed as "(unknown)"
// in the UI. Days carries the calendar-day series for the heatmap —
// both are derived from the same per-row token_usage trail so the
// views always sum to the same totals.
export type SettingsUsageResponse = {
  total_sessions: number;
  generated_at: string;
  metrics: SettingsUsageMetrics;
  model_breakdowns: ModelUsage[];
  days: SettingsUsageDay[];
};

// ComposerGoalSummary is the composer-banner view of the current thread goal.
// The backend owns runtime status, control availability, completed usage, and
// the current in-flight start time. The renderer only advances that live slice.
export type ComposerGoalSummary = {
  id: string;
  thread_id?: string;
  text: string;
  status: string;
  step?: string;
  started_at?: string;
  updated_at?: string;
  running_since?: string;
  stop_reason?: string;
  recent_progress?: string;
  tokens_used?: number;
  time_used_seconds?: number;
  goal_turns?: number;
  blocker?: string;
  blocker_consecutive_turns?: number;
  can_pause?: boolean;
  can_resume?: boolean;
  can_clear?: boolean;
};

// Appearance preference for the desktop shell. "system" follows the OS
// light/dark setting via prefers-color-scheme.
// Continuous px value the user picks on the message-stream font-size
// slider. Range mirrors the developer-only design-tokens mixer
// (ConversationDesignTokens.ts → msg-font-size: 13–20 step 0.5 default
// 14) so the user setting and the mixer operate in the same coordinate
// space. The renderer clamps incoming values to this range before
// applying them; the main process keeps the same range check at the
// IPC boundary.
export type MessageFlowFontSize = number;

export const MESSAGE_FLOW_FONT_SIZE_RANGE = {
  min: 13,
  max: 20,
  step: 0.5,
  default: 14,
} as const;

export type ThemePreference = "system" | "light" | "dark";
export type LanguagePreference = "system" | "zh-CN" | "en-US";
export type AppLocale = Exclude<LanguagePreference, "system">;

// The three OS families the desktop shell distinguishes. Anything more
// exotic collapses to "linux" (native-frame fallback chrome).
export type DesktopPlatform = "darwin" | "win32" | "linux";

export type CodexPetStateID =
  | "idle"
  | "running-right"
  | "running-left"
  | "waving"
  | "jumping"
  | "failed"
  | "waiting"
  | "running"
  | "review";

export type CodexPetState = {
  id: CodexPetStateID;
  label: string;
  row: number;
  frames: number;
};

// 桌宠在桌面上的渲染尺寸档位。multiplier 是相对于"默认"尺寸的缩放比例：
// 默认档的 sprite 渲染成 192×208 单元格的 50%（96×104），"小"是 75%，"大"
// 是 150%，"超大"是 200%。窗口尺寸、sprite 的 CSS transform 都按这个比例
// 联动；气泡宽高保持不变以维持可读性。size 是可选字段——旧版
// desktop-settings.json 没有它时由读取层兜底成 CODEX_PET_SIZE_DEFAULT，写
// 回时允许省略。
export type CodexPetSize = "small" | "default" | "large" | "extra-large";

export const CODEX_PET_SIZE_DEFAULT: CodexPetSize = "default";

export const CODEX_PET_SIZE_OPTIONS = [
  { id: "small", label: "小 (75%)", multiplier: 0.75 },
  { id: "default", label: "默认 (100%)", multiplier: 1.0 },
  { id: "large", label: "大 (150%)", multiplier: 1.5 },
  { id: "extra-large", label: "超大 (200%)", multiplier: 2.0 },
] as const satisfies ReadonlyArray<{
  id: CodexPetSize;
  label: string;
  multiplier: number;
}>;

export type CodexPet = {
  id: string;
  display_name: string;
  description: string;
  manifest_path: string;
  spritesheet_path: string;
  spritesheet_url: string;
};

export type CodexPetSettings = {
  enabled: boolean;
  selected_id: string;
  // 桌宠渲染尺寸档位。可选：旧版持久化文件没有这个字段时由 desktop-settings
  // 读取层兜底成 CODEX_PET_SIZE_DEFAULT，CodexPetsSnapshot 把它透出来给
  // renderer（如果关心的话）。
  size?: CodexPetSize;
  // 连续缩放值（sprite 的 CSS transform scale，默认档 = 0.5）。用户在桌宠
  // 窗口上拖动边缘热区实时缩放，松手时提交这个原始值——不吸附回 size 档
  // 位，所以尺寸可以停在任意中间值。有 scale 时它覆盖 size 档位；显式选
  // 档位（若未来恢复档位入口）时应清掉 scale。
  scale?: number;
};

export type CodexPetsSnapshot = CodexPetSettings & {
  home: string;
  pets: CodexPet[];
  errors: string[];
};

export type CodexPetSettingsUpdate = Partial<CodexPetSettings>;

// 桌宠动画输入：渲染进程把会话运行态推给主进程，主进程据此切换独立
// 桌宠窗口（无边框置顶小窗，主窗口隐藏/最小化后仍在桌面上）的精灵状态。
export type CodexPetRuntime = {
  running: boolean;
  status: string;
};

// 桌宠气泡：渲染进程按优先级（attention > running > idle）从当前所有
// session 中派生最相关的最多 CODEX_PET_HINTS_MAX 条，以轻量 hint 列表
// 推到主进程，桌宠窗口据此在 sprite 上方或右侧展示一个小气泡，每条
// hint 占一行。仅作为 hint，不承担观测面板或导航职责；点击某一行触发
// wuu-pet://action/jump 跳回主窗口定位该 thread。
export const CODEX_PET_HINTS_MAX = 3;

export type CodexPetHintStatus =
  | "running"
  | "done"
  | "failed"
  | "needs_review"
  | "idle";

export type CodexPetHint = {
  thread_id: string;
  title: string;
  status: CodexPetHintStatus;
  preview: string;
  attention: boolean;
  updated_at: number;
};

// Remote control (设置 → 远程): desktop-local surface managing the
// machine-global `wuu remote host` daemon and phone pairing. Status comes
// from `wuu remote status --json`; none of this goes through the app-server
// protocol.
export type RemoteControlDevice = {
  pub: string;
  fingerprint: string;
  name?: string;
  added_at: string;
};

export type RemoteControlStatus = {
  fingerprint: string;
  host_name?: string;
  relay_url?: string;
  store: string;
  devices: RemoteControlDevice[];
};

// One coherent view of the remote-control state; every mutating call
// returns a fresh snapshot so the renderer has a single refresh path.
export type RemoteControlSnapshot = {
  status: RemoteControlStatus | null;
  status_error?: string;
  host_running: boolean;
  pair_uri: string | null;
};

// Main-process RemoteHostManager events, broadcast to all windows.
export type RemoteControlEvent =
  | { kind: "pair-uri"; uri: string }
  | { kind: "paired"; detail: string }
  | { kind: "host-log"; message: string }
  | { kind: "host-exit"; code: number | null };

export type ActiveDocumentContext = {
  path: string;
};

export type WuuDesktopApi = {
  listProjects: () => Promise<ProjectListResult>;
  createBlankProject: () => Promise<ProjectListResult>;
  chooseProjectFolder: () => Promise<ProjectListResult>;
  selectProject: (projectId: string) => Promise<ProjectListResult>;
  removeProject: (projectId: string) => Promise<ProjectListResult>;
  // Opt-in second step after removeProject: reclaim the removed workspace's
  // local state directory. Non-memory state (session artifacts, goals,
  // worktrees, runtime files) is deleted; memory directories are archived
  // into `.archived/` inside the state dir, never hard-deleted. Mirrors the
  // `workspace/state/cleanup` RPC.
  cleanupProjectState: (
    projectId: string,
    projectPath: string,
  ) => Promise<{ state_dir: string; removed: boolean; memory_archived: boolean }>;
  relocateProject: (projectId: string) => Promise<ProjectListResult>;
  selectNoProject: (fresh?: boolean, cwd?: string) => Promise<ProjectListResult>;
  gitStatus: (root?: string) => Promise<GitStatusResult>;
  listGitChanges: (root?: string) => Promise<GitChangesResult>;
  readGitFileDiff: (path: string, root?: string) => Promise<GitFileDiffResult>;
  gitActionBusy?: (root?: string) => Promise<boolean>;
  checkoutGitBranch: (branch: string, root?: string) => Promise<GitStatusResult>;
  createCheckoutGitBranch: (branch: string, root?: string) => Promise<GitCreateBranchResult>;
  commitGitChanges: (params: GitCommitParams, root?: string) => Promise<GitCommitResult>;
  createPullRequest: (params: GitPullRequestParams, root?: string) => Promise<GitPullRequestResult>;
  // root is an optional absolute directory override, used to root the
  // workspace file tree / preview at the active thread's own cwd (e.g. a
  // worktree fork) instead of the active project's cwd. See
  // workspacePanelContext in AppState.ts.
  listWorkspaceFiles: (root?: string) => Promise<FileTreeListResult>;
  listWorkspaceDirectory: (path?: string, root?: string) => Promise<WorkspaceDirectoryListResult>;
  readWorkspaceFile: (path: string, root?: string) => Promise<WorkspaceFileReadResult>;
  writeWorkspaceFile: (params: WorkspaceFileSaveParams, root?: string) => Promise<WorkspaceFileSaveResult>;
  resolveWorkspaceFileReference: (reference: string, root?: string) => Promise<WorkspaceFileReferenceResolveResult>;
  startTerminalSession: (params?: TerminalSessionStartParams) => Promise<TerminalSessionStartResult>;
  writeTerminalSession: (id: string, data: string) => Promise<TerminalSessionActionResult>;
  resizeTerminalSession: (id: string, cols: number, rows: number) => Promise<TerminalSessionActionResult>;
  stopTerminalSession: (id: string) => Promise<TerminalSessionActionResult>;
  listManagedProcesses: (threadId: string) => Promise<ManagedProcessListResult>;
  readManagedProcess: (params: ManagedProcessReadParams) => Promise<ManagedProcessReadResult>;
  writeManagedProcess: (
    threadId: string,
    processId: string,
    input: string,
  ) => Promise<ManagedProcessWriteResult>;
  resizeManagedProcess: (
    threadId: string,
    processId: string,
    cols: number,
    rows: number,
  ) => Promise<ManagedProcessActionResult>;
  stopManagedProcess: (
    threadId: string,
    processId: string,
  ) => Promise<ManagedProcessActionResult>;
  initialize: () => Promise<InitializeResult>;
  getBuildInfo: () => Promise<BuildInfoResult>;
  loadCodexModels: (provider?: string) => Promise<ConfigCodexModelsResult>;
  // provider/model may be omitted when threadId is set: the server inherits
  // omitted selection fields from the target thread and leaves the workspace
  // defaults for them untouched.
  updateRuntimeSettings: (
    provider?: string,
    model?: string,
    effort?: string,
    connection?: RuntimeConnectionUpdate,
    variant?: string,
    permissionMode?: string,
    threadId?: string
  ) => Promise<ConfigModelUpdateResult>;
  updateUltraMode: (enabled: boolean) => Promise<ConfigModelUpdateResult>;
  removeProvider: (
    provider: string,
    options?: { fallbackProvider?: string; fallbackModel?: string }
  ) => Promise<ConfigModelUpdateResult>;
  updateAdvancedSettings: (
    settings: RuntimeAdvancedSettingsUpdate
  ) => Promise<ConfigAdvancedUpdateResult>;
  updateGeneralSettings: (
    settings: RuntimeGeneralSettingsUpdate
  ) => Promise<ConfigGeneralUpdateResult>;
  listMCPServers: () => Promise<MCPListResult>;
  connectMCPServer: (name: string) => Promise<MCPServerActionResult>;
  disconnectMCPServer: (name: string) => Promise<MCPServerActionResult>;
  refreshMCPServer: (name: string) => Promise<MCPServerActionResult>;
  startMCPAuth: (name: string) => Promise<MCPAuthStartResult>;
  getMCPAuthStatus: (name: string) => Promise<MCPAuthStatusResult>;
  finishMCPAuth: (name: string, state: string, code: string) => Promise<MCPAuthFinishResult>;
  removeMCPAuth: (name: string) => Promise<MCPAuthRemoveResult>;
  listActivities: (threadId: string) => Promise<ActivityListResult>;
  takeoverActivity: (threadId: string, activityId: string) => Promise<ActivityActionResult>;
  releaseActivity: (threadId: string, activityId: string) => Promise<ActivityReleaseResult>;
  stopActivity: (threadId: string, activityId: string) => Promise<ActivityActionResult>;
  listSkills: () => Promise<SkillListResult>;
  readSkillContent: (params: SkillContentParams) => Promise<SkillContentResult>;
  listNamedAgents: () => Promise<ChannelAgentListResult>;
  bootstrapChannels: () => Promise<ChannelBootstrapResult>;
  createNamedAgent: (params: ChannelAgentCreateParams) => Promise<ChannelAgentCreateResult>;
  updateNamedAgent: (params: ChannelAgentUpdateParams) => Promise<ChannelAgentUpdateResult>;
  deleteNamedAgent: (params: ChannelAgentDeleteParams) => Promise<ChannelAgentDeleteResult>;
  startNamedAgent: (params: ChannelAgentStartParams) => Promise<ChannelAgentStartResult>;
  listChannelRooms: () => Promise<ChannelRoomListResult>;
  createChannelRoom: (params: ChannelRoomCreateParams) => Promise<ChannelRoomCreateResult>;
  deleteChannelRoom: (params: ChannelRoomDeleteParams) => Promise<ChannelRoomDeleteResult>;
  listChannelMessages: (params: ChannelMessageListParams) => Promise<ChannelMessageListResult>;
  sendChannelMessage: (params: ChannelMessageSendParams) => Promise<ChannelMessageSendResult>;
  createChannelTask: (params: ChannelTaskCreateParams) => Promise<ChannelTaskCreateResult>;
  updateChannelTask: (params: ChannelTaskUpdateParams) => Promise<ChannelTaskUpdateResult>;
  getChannelHumanMentionStatus: () => Promise<ChannelHumanMentionStatusResult>;
  ackChannelHumanMentions: () => Promise<ChannelHumanMentionAckResult>;
  startThread: (params?: ThreadStartParams) => Promise<{ thread: Thread }>;
  resumeThread: (sessionId?: string) => Promise<ThreadResumeResult>;
  forkThread: (
    threadId: string,
    turnId?: string,
    itemId?: string,
    mode?: "local" | "worktree",
    target?: ThreadForkTarget,
  ) => Promise<ThreadForkResult>;
  editThreadMessage: (threadId: string, turnId: string, itemId: string) => Promise<ThreadEditMessageResult>;
  getThreadContextComposition: (threadId: string) => Promise<ThreadContextCompositionResult>;
  // Instruction files (AGENTS.md / CLAUDE.md, ...) loaded into the base
  // system prompt at session start. Read-only; used by the session view's
  // 指令文件 block to mirror Claude Code's /memory visibility.
  listInstructionFiles: () => Promise<InstructionsListResult>;
  // 远程控制（设置 → 远程）。管理机器级 remote host 守护进程与手机配对,
  // 走主进程 RemoteHostManager 而非 app-server 协议。
  getRemoteControlSnapshot: () => Promise<RemoteControlSnapshot>;
  setRemoteRelay: (relayUrl: string) => Promise<RemoteControlSnapshot>;
  setRemoteHostEnabled: (enabled: boolean) => Promise<RemoteControlSnapshot>;
  startRemotePairing: () => Promise<RemoteControlSnapshot>;
  removeRemoteDevice: (fingerprintOrPub: string) => Promise<RemoteControlSnapshot>;
  onRemoteControlEvent: (handler: (event: RemoteControlEvent) => void) => () => void;
  // Which OS the desktop shell runs on. The preload stamps data-platform
  // on <html> before first paint (window-chrome CSS keys off it) and
  // mirrors the value here for renderer logic (shortcut hints, OS labels).
  platform?: DesktopPlatform;
  // Appearance. The preference persists in desktop-settings.json; the
  // renderer resolves "system" against prefers-color-scheme and stamps
  // data-theme on <html>. `initialThemePreference` is read synchronously
  // in the preload script so the first paint already has the right theme.
  initialThemePreference?: ThemePreference;
  getThemePreference: () => Promise<ThemePreference>;
  setThemePreference: (
    theme: ThemePreference,
  ) => Promise<{ ok: boolean; theme: ThemePreference }>;
  initialLanguagePreference?: LanguagePreference;
  initialSystemLocale?: string;
  getLanguagePreference: () => Promise<LanguagePreference>;
  setLanguagePreference: (
    language: LanguagePreference,
  ) => Promise<{ ok: boolean; language: LanguagePreference }>;
  // Language is app-global. Main broadcasts changes so already-open pop-outs
  // update alongside the window where the preference was changed.
  onLanguagePreferenceChange: (
    handler: (language: LanguagePreference) => void,
  ) => () => void;
  // The preference is app-global: the main process broadcasts every change
  // (explicit choice, or an OS dark-mode flip while on "system") to all
  // windows, and each renderer re-applies data-theme. Returns a disposer.
  onThemePreferenceChange: (
    handler: (theme: ThemePreference) => void,
  ) => () => void;
  // Message-stream reading size. Persists to desktop-settings.json as a
  // fixed three-step ladder. `initialMessageFlowFontSize` is read
  // synchronously in the preload so the first paint already has the
  // right --conversation-message-font-size on <html>, mirroring the
  // theme pre-paint to avoid a flash.
  initialMessageFlowFontSize?: MessageFlowFontSize;
  getMessageFlowFontSize: () => Promise<MessageFlowFontSize>;
  setMessageFlowFontSize: (
    fontSize: MessageFlowFontSize,
  ) => Promise<{ ok: boolean; fontSize: MessageFlowFontSize }>;
  listCodexPets: () => Promise<CodexPetsSnapshot>;
  updateCodexPetSettings: (
    settings: CodexPetSettingsUpdate,
  ) => Promise<CodexPetsSnapshot>;
  updateCodexPetRuntime: (runtime: CodexPetRuntime) => Promise<void>;
  updateCodexPetHints: (hints: CodexPetHint[]) => Promise<void>;
  // 桌宠点击气泡某一行触发；主窗口前置并切到该 thread。监听器返回 dispose。
  onCodexPetJumpRequest: (
    handler: (event: { thread_id: string }) => void,
  ) => () => void;
  // 记忆面板（设置 → 记忆）。契约见 memory-redesign.md §8.2；后端 M2
  // 未落地时三个方法都会以 unknown method 错误拒绝，面板渲染占位态。
  getMemoryOverview: (params: MemoryOverviewParams) => Promise<MemoryOverviewResult>;
  sendMemoryChat: (params: MemoryChatParams) => Promise<MemoryChatResult>;
  readMemoryRaw: (params: MemoryReadParams) => Promise<MemoryReadResult>;
  listThreads: (cwd?: string) => Promise<{ threads: Thread[] }>;
  // Settings → Archive panel uses this to surface archived sessions across
  // every cwd after a restart; the active list above stays non-archived.
  listArchivedThreads: () => Promise<{ threads: Thread[] }>;
  searchThreads: (query: string, limit?: number) => Promise<ThreadSearchResult>;
  getThreadPreview: (
    threadId: string,
    limit?: number
  ) => Promise<ThreadPreviewResult>;
  pinThread: (threadId: string, pinned: boolean) => Promise<{ thread: Thread }>;
  archiveThread: (threadId: string, archived: boolean) => Promise<{ thread: Thread }>;
  // Permanently deletes a conversation (history, artifacts, and any fork
  // worktree). Mirrors the `thread/delete` RPC; running threads are rejected
  // server-side.
  deleteThread: (threadId: string) => Promise<{ thread_id: string }>;
  compactThread: (threadId: string) => Promise<{ turn: Turn }>;
  startTurn: (
    threadId: string,
    prompt: string,
    images?: InputImage[],
    files?: InputFile[],
    permissionMode?: string,
    activeDocument?: ActiveDocumentContext,
  ) => Promise<{ turn: Turn }>;
  queueTurn: (
    threadId: string,
    prompt: string,
    images?: InputImage[],
    clientId?: string,
    files?: InputFile[],
    permissionMode?: string,
    activeDocument?: ActiveDocumentContext,
  ) => Promise<{ queued: QueuedTurn }>;
  updateQueuedTurn: (
    threadId: string,
    queueId: string,
    prompt: string,
    images?: InputImage[],
    files?: InputFile[],
  ) => Promise<{ ok: boolean; queued: QueuedTurn }>;
  dequeueTurn: (threadId: string, queueId: string) => Promise<{ ok: boolean }>;
  steerTurn: (
    threadId: string,
    expectedTurnId: string,
    prompt: string,
    images?: InputImage[],
    clientId?: string,
    files?: InputFile[],
    activeDocument?: ActiveDocumentContext,
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
  // Reveal a workspace file or folder in the OS file browser (Finder /
  // Explorer / file manager) with the item highlighted. Mirrors
  // `wuu:reveal-session` semantics for in-workspace items: the path
  // comes from `listWorkspaceDirectory`, so it's already known to the
  // renderer via the same channel and we trust it without an extra
  // project-root check (the dir listing itself is what gates that).
  revealWorkspaceItem: (path: string) => Promise<void>;
  // macOS uses Launch Services to build a native file menu with the current
  // default app and the installed apps that can open this specific item.
  // Other shells keep their existing renderer-owned context menu.
  showWorkspaceItemMenu: (path: string) => Promise<WorkspaceItemMenuResult>;
  // Open an external URL via the OS default browser. Used by the
  // assistant turn's 来源 pill to send the user to the page the agent
  // actually consulted (web_search hit / web_fetch target) instead of
  // pretending the agent fetched it for them. Validation that the
  // URL is http(s) lives in the main-process handler so the renderer
  // can't escalate arbitrary schemes via this channel.
  openExternal: (url: string) => Promise<void>;
  getSettingsUsage: () => Promise<SettingsUsageResponse>;
  // Composer goal banner surface. The renderer only needs a lightweight
  // summary plus explicit runtime controls; the full GoalSnapshot and
  // workflow/agent run detail stay on the agent tool loop.
  getActiveGoalSummary: (threadId?: string) => Promise<ComposerGoalSummary | null>;
  pauseGoal: (goalId: string, threadId?: string) => Promise<{ ok: boolean }>;
  resumeGoal: (goalId: string, threadId?: string) => Promise<{ ok: boolean }>;
  clearGoal: (goalId: string, threadId?: string) => Promise<{ ok: boolean }>;
  updateGoalText: (
    goalId: string,
    text: string,
    threadId?: string
  ) => Promise<{ ok: boolean }>;
  /**
   * Pop-out session IPC (Plan §2.2 `wuu:pop-out-session`). Renderer
   * sends either a thread tab or a draft tab plus its runtime context.
   * Thread tabs focus the single BrowserWindow registered for that
   * thread; draft tabs open a clean popped-out draft window.
   */
  popOutSession: (params: PopOutSessionParams) => Promise<{ windowID: number }>;
  /**
   * Optional belt-and-suspenders IPC for the popped-out window to tell
   * main it's closing (Plan §2.2 `wuu:pop-out-closed`). Main only clears
   * the thread-window mapping and closes the window if still alive; the
   * backing thread and any running turn are left untouched.
   */
  popOutClosed: (params: { threadID: string }) => Promise<{ ok: boolean }>;
  /**
   * Sync bootstrap IPC for the popped-out window's renderer (Plan §3
   * commit 5 verification, M1 sync IPC parity test §7 risk #7). The
   * popped-out window calls `window.wuu.popOutInit()` during boot. Main
   * returns only the window-owned thread id and runtime context; the
   * renderer then hydrates through the normal async app-server calls,
   * routed by that window identity.
   */
  popOutInit: () => PopOutInitResult;
  // Side-thread IPC is keyed by the owning main thread.
  // Non-Electron hosts may omit these optional methods.
  openSideThread?: (
    mainThreadId: string,
  ) => Promise<SideThreadOpenResult>;
  getSideThreadHistory?: (
    mainThreadId: string,
  ) => Promise<SideThreadHistoryResult | null>;
  sendSideThreadMessage?: (
    params: SideThreadSendParams,
  ) => Promise<SideThreadSendResult>;
  interruptSideThread?: (
    mainThreadId: string,
  ) => Promise<{ ok: boolean }>;
  // Clears the side-thread history so the next message starts from the
  // latest main-conversation state.
  resetSideThread?: (
    mainThreadId: string,
  ) => Promise<{ ok: boolean }>;
  // Streamed side-thread output forwarded by the Electron main process.
  onSideThreadEvent?: (
    handler: (envelope: SideThreadEventEnvelope) => void,
  ) => () => void;
  // Embedded-browser visibility takeover (M3). These are wired only by the
  // Electron desktop preload; non-Electron hosts omit them, so the renderer
  // guards each call with `typeof window.wuu.x === "function"`.
  //
  // Renderer→main: report where the browser panel sits on screen so the main
  // process can overlay the agent's WebContentsView on it (polled via rAF).
  reportBrowserBounds?: (
    workdir: string,
    tabID: string,
    rect: BrowserBoundsRect,
  ) => void;
  // Renderer→main: hide the agent view while a full-window overlay (settings,
  // dialogs, search) is open so it can't occlude the modal; false restores it.
  suppressBrowserOverlay?: (
    workdir: string,
    tabID: string,
    suppressed: boolean,
  ) => void;
  // Main→renderer: the core owning `workdir` was torn down / evicted, so any
  // browser activity for it is dead. Lets the renderer drop ghost activity UI
  // even when the Close-time "stopped" events were lost. Returns unsubscribe.
  onBrowserInvalidate?: (
    handler: (payload: { workdir: string }) => void,
  ) => () => void;
};

/**
 * Sync return shape for `wuu:pop-out-init` (Plan §3 commit 5
 * verification). The snapshot is intentionally small and sync-safe:
 * it carries identity, not conversation data.
 */
export type PopOutInitResult = {
  /** Popped-out window kind, or null when this is the main window. */
  kind: "thread" | "draft" | null;
  /** The popped-out thread id, or null when this is the main window. */
  threadID: string | null;
  /** Runtime context pinned to the popped-out window. */
  context: RuntimeContext | null;
};

export type PopOutSessionParams =
  | {
      kind?: "thread";
      threadID: string;
      context: RuntimeContext;
    }
  | {
      kind: "draft";
      context: RuntimeContext;
    };

declare global {
  interface Window {
    wuu: WuuDesktopApi;
    wuuRenderableFileURL?: (encodedPath: string) => string;
  }
}
