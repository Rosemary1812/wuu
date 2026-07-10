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
  status?: "ready" | "needs_setup";
  issues?: RuntimeIssue[];
  protocol_version: string;
  core?: CoreBuildInfo;
  provider: string;
  model: string;
  effort?: string;
  variant?: string;
  workspace_root: string;
  permissions?: PermissionSummary;
  // model_profile + tool_surface summarise the per-model tool
  // surface the runtime compiled for the active session. The
  // renderer uses these to drive capability-first activity and the
  // debug surface view; the runtime side owns the values so the UI never
  // has to re-derive the
  // bash-first / patch-first split.
  model_profile?: ModelProfileSummary;
  tool_surface?: ToolSurfaceSummary;
  extension_trust?: ExtensionTrustSummary;
  model_roles?: ModelRoleSummary[];
  providers?: ProviderSummary[];
  advanced_settings?: AdvancedSettingsSummary;
  general_settings?: GeneralSettingsSummary;
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
  memory_disabled: boolean;
  mcp_server_enabled: Record<string, boolean>;
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

export type ConfigModelUpdateResult = {
  provider: string;
  model: string;
  effort?: string;
  variant?: string;
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
};

export type ConfigAdvancedUpdateResult = {
  advanced_settings: AdvancedSettingsSummary;
  providers?: ProviderSummary[];
};

export type RuntimeGeneralSettingsUpdate = {
  append_system_prompt?: string;
  memory_disable?: boolean;
  mcp_enabled_toggles?: Record<string, boolean>;
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
  // Derived (not persisted): true when the project's directory no longer
  // exists on disk — it was moved away or deleted. The main process computes
  // it fresh on every list(); the sidebar greys such a workspace out and
  // disables its "新建会话" affordance so no new session can be created in a
  // cwd that is gone.
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
  mtime_ms: number;
  sha256: string;
  binary: boolean;
  truncated: boolean;
  text?: string;
  renderable_url?: string;
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
  | "participant_message"
  | "task_card"
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

// ParticipantSummary is the wire shape of a conversation participant
// (human, primary agent, named agent, or ephemeral task worker) embedded
// in agents and thread items for display attribution.
export type ParticipantSummary = {
  id: string;
  name: string;
  kind: string;
  role?: string;
  // avatar_image is an uploaded image data URL (see
  // ParticipantProfile.avatar_image). The backend fills it for every
  // summary resolved from the participant store — chat bubbles and group
  // thread members — but caps the embedded payload at
  // 64KB raw bytes (appserver participantSummaryAvatarMaxBytes):
  // summaries are duplicated into each thread item they attribute, so
  // larger uploads degrade to the initial-letter avatar here while
  // still rendering in full-profile surfaces (profile panel,
  // participant/list). Optional because synthesized fallback summaries
  // (legacy rows, ephemeral snapshots) never carry it.
  avatar_image?: string;
  // busy reports that this named agent is currently executing a
  // task/workflow run and cannot take a second concurrent pull
  // (decision-five concurrency lock). The backend overlays it on group
  // member summaries (threadWithGroupMembers); the UI shows a busy badge
  // and can steer toward forking a copy. Optional/false when idle or not a
  // named agent.
  busy?: boolean;
  // forked_from_id marks a temporary分身 (decision six): the母体's participant
  // id this named agent was forked from, so the UI can badge it as "X 的分身".
  // Distinct from the session/conversation fork (ThreadSummary.forked_from_id)
  // — this is a named-agent identity copy with a memory snapshot. Absent for
  // ordinary named agents.
  forked_from_id?: string;
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
  // Pinned and Archived mirror the underlying session metadata for the
  // sub-agent's own session so the info panel can offer pin/archive
  // actions without an extra round-trip.
  pinned?: boolean;
  archived?: boolean;
  participant?: ParticipantSummary;
};

export type TaskCard = {
  id: string;
  name?: string;
  role?: string;
  description?: string;
  status: string;
  agent_id?: string;
  subthread_id?: string;
  reply_count?: number;
  participant?: ParticipantSummary;
  started_at?: string;
  completed_at?: string | null;
  input_tokens?: number;
  output_tokens?: number;
  error?: string;
};

export type ParticipantStartParams = {
  thread_id: string;
  participant_id?: string;
  task_name?: string;
  description?: string;
  prompt: string;
  subagent_type?: string;
  agent_profile?: string;
  isolation?: string;
  record_user_message?: boolean;
};

export type ParticipantStartResult = {
  agent: Agent;
};

export type ParticipantRunEntry = {
  task_id?: string;
  summary?: string;
  outcome?: string;
  created_at?: string;
};

export type ParticipantProfile = {
  id: string;
  kind: string;
  name: string;
  role?: string;
  // avatar_image is an uploaded image data URL (e.g. "data:image/png;base64,...").
  // Populated by the profile read path when the workspace stores an avatar
  // image; absent on the lightweight wire Summary used in agent and task
  // card attribution.
  avatar_image?: string;
  tagline?: string;
  workspace?: string;
  model?: string;
  memory?: string;
  // forked_from_id is the母体's participant id when this profile is a
  // temporary分身 (decision six); the UI badges the copy as "X 的分身".
  // Distinct from the session/conversation fork (ThreadSummary.forked_from_id).
  forked_from_id?: string;
  track_record?: ParticipantRunEntry[];
  created_at?: string;
  updated_at?: string;
};

export type ParticipantListResult = {
  participants: ParticipantProfile[];
};

export type ParticipantSaveParams = {
  id?: string;
  name: string;
  role?: string;
  // avatar_image accepts an image data URL to upload a custom avatar.
  avatar_image?: string;
  // clear_avatar_image removes any previously uploaded avatar image.
  // Takes precedence over avatar_image when set.
  clear_avatar_image?: boolean;
  tagline?: string;
  model?: string;
  memory?: string;
};

export type ParticipantSaveResult = {
  participant: ParticipantProfile;
};

export type ParticipantFeedbackResult = {
  participant: ParticipantProfile;
};

export type ParticipantResetResult = {
  participant: ParticipantProfile;
};

export type ParticipantRetireParams = {
  participant_id: string;
};

export type ParticipantRetireResult = {
  participant: ParticipantProfile;
};

export type ParticipantUpdatedNotification = {
  participant_id?: string;
  participant?: ParticipantProfile;
};

// ---------------------------------------------------------------------------
// Memory panel (设置 → 记忆). Wire contract fixed ahead of implementation by
// docs/plans/2026-07-04-memory-redesign.md §8.2. The three RPCs are served
// by the M2 memory-panel backend; a backend without M2 rejects them with an
// "unknown method" error, which the renderer maps to a
// "记忆面板后端尚未就绪" placeholder instead of crashing.

// MemoryScope selects which notebook an RPC targets: "user" is the user
// notebook (~/.wuu/memory), "participant" is a named agent's identity
// notebook (~/.wuu/participants/<id>/memory). participant scope requires
// participant_id.
export type MemoryScope = "user" | "participant";

export type MemoryOverviewParams = {
  scope: MemoryScope;
  participant_id?: string;
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
  model_provider: string;
  model: string;
  cwd: string;
  // workspace_kind tags the thread with the workspace it was created in.
  // "scratch" threads live in the desktop-managed scratch root
  // (~/.wuu/scratch/<date>) and have no registered project; the sidebar
  // surfaces them in the standalone "对话" section. "dm" threads are
  // direct-message conversations with a named agent and live in a per-agent
  // home directory (~/.wuu/agents/<id>/home). Threads loaded from
  // older builds may omit this field — the renderer falls back to
  // classifying them by cwd against the known project list.
  workspace_kind?: "project" | "scratch" | "dm";
  status: ThreadStatus;
  read_only?: boolean;
  pinned?: boolean;
  // dm_participant_id tags the thread as the DM conversation with a named
  // participant (when started via startThread with a dm_participant_id param).
  dm_participant_id?: string;
  // group marks this thread as a chat-style group channel with no main
  // agent (chat-style-threads-design.md §3). Set once at creation.
  group?: boolean;
  // Named participants that are members of this (group) thread. Snapshot
  // of the thread_members table (docs/plans/2026-07-03-resident-named-agents.md
  // §3.1). Absent or empty for DM threads and threads without members.
  members?: ParticipantSummary[];
  // focus_workspace is the sticky "work focus" for a chat-style (DM or
  // group) thread's composer: "" or absent means the full workspace set
  // (the default), "~" scopes the resident agent to its personal home
  // directory only, and any other value names one workspace/project.
  // The composer echoes this on thread/resume so the focus chip reflects
  // the thread's last-known state instead of always resetting to
  // default.
  focus_workspace?: string;
  archived?: boolean;
  forked_from_id?: string;
  forked_from_turn_id?: string;
  forked_from_item_id?: string;
  worktree?: WorktreeInfo;
  created_at: string;
  updated_at: string;
  turns: Turn[];
  child_agents?: Agent[];
  browser_state?: ThreadBrowserState;
};

// Params for thread/start. Mutually exclusive kinds: dm_participant_id
// starts (or reuses) a DM thread with a named participant; group starts a
// chat-style group channel with an optional title
// (chat-style-threads-design.md §3.1). Omit both for a plain work session.
export type ThreadStartParams = {
  dm_participant_id?: string;
  group?: boolean;
  title?: string;
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

// "task" is the execution phase after a Thread is promoted
// (open -> task -> resolved). The lead concludes it directly; there is no
// intermediate approval state.
// 历史库中可能残留已废弃状态的行:后端原样透传,前端把未知状态当运行中处理。
export type ConversationSubthreadStatus = "open" | "task" | "resolved";

export type ConversationSubthread = {
  id: string;
  thread_id: string;
  anchor_item_id: string;
  parent_seq?: number;
  title?: string;
  status: ConversationSubthreadStatus;
  created_by?: string;
  created_at: string;
  reply_count: number;
  // 弱隔离 participant subset for a reply subthread: only these participants are
  // pushed reply/task traffic. Empty/undefined means no explicit member subset
  // yet. Populated by the backend weak-isolation router.
  participants?: string[];
  // Discussion owner chosen exactly once when the Thread is created. For a
  // named-agent message this is the author; for a human message the desktop
  // asks which named group member should own the convergence. When promoted,
  // this same participant becomes the immutable Task lead.
  thread_owner_participant_id?: string;
  // 该 reply 被(人点击)升级为 task 后由后端下发的 task_card:执行中显示活动卡、
  // 收尾后显示 result 摘要。未升级的普通 reply 为 undefined。
  task?: TaskCard;
  // 收尾冒泡回主流的一句结论(同时写在 task_card 上);escalated_by 记录升级人。
  summary?: string;
  escalated_by?: string;
  // 升级为 Task 后持编排权的 named lead。它由 Thread owner 派生,不是升级时
  // 重新选择的第二个负责人。runtime workflow 网关按
  // (caller == lead && status == task) 放行。
  lead_participant_id?: string;
  // 执行轴(与 status 分离):planning / executing / awaiting_lead / blocked /
  // needs_human / completed / failed;空 = 尚未进入执行(普通 Thread)。
  exec_state?: string;
  // lead 声明的工作分解投到线上,供 Task 面板渲染进展层(plan §T11):一行一个节点,
  // 带 Status 派生的展示态与两个 liveness 时间戳。普通(未编排)reply/task 为空。
  plan?: TaskPieceView[];
  turns?: Turn[];
};

// TaskPieceView 是一个 plan 节点在线上的投影(plan §T11):节点身份、依赖边、原始
// status(pending/active/done/blocked/failed/retrying/cancelled)、由 status 派生的展示 state
// 标签(done -> completed 等)、重试预算/尝试计数、最近失败原因,以及两个 liveness
// 时间戳。时间戳原样下发,前端只显示中性的最近活动/进展时间；是否阻塞、失败或需要
// 人处理必须来自 runtime 的显式状态,不能由桌面用固定时限猜测。
export type TaskPieceView = {
  id: string;
  title: string;
  assignee?: string;
  depends_on?: string[];
  status: string;
  state?: string;
  attempts?: number;
  retry_budget?: number;
  current_attempt_id?: string;
  failure_reason?: string;
  last_activity_at?: string;
  last_progress_at?: string;
};

export type ThreadOpenSubResult = {
  subthread: ConversationSubthread;
};

export type ThreadListSubResult = {
  subthreads: ConversationSubthread[];
};

export type ThreadEscalateSubResult = {
  subthread: ConversationSubthread;
};

export type ThreadResolveSubResult = {
  subthread: ConversationSubthread;
};

// message/postSubthread result: the refreshed reply subthread view including the
// just-posted human message, so the split reply panel updates immediately (cth
// messages carry no item/thread notification of their own).
export type MessagePostSubthreadResult = {
  subthread: ConversationSubthread;
};

// thread/taskEvents params: read the trace timeline of an escalated subthread
// (task) for the「轨迹」panel (plan §T11). subthread_id 是 task cth id;thread_id
// 是它所属的父群线程,读之前按归属校验(同其它 subthread RPC)。只读。
export type ThreadTaskEventsParams = {
  thread_id: string;
  subthread_id: string;
};

// TaskEventView 是线上一条 trace 事件:per-task 单调 seq(时间线顺序)、所属 plan
// 节点(node_id,task 级事件为空)、事件 kind(task_created / node_started /
// node_progress / handoff_created / node_failed / ...)、actor participant id、
// 一句人读 summary、可选的结构化 payload(handoff / error JSON),以及墙钟时间。
export type TaskEventView = {
  seq: number;
  node_id?: string;
  attempt_id?: string;
  kind: string;
  actor?: string;
  summary?: string;
  payload?: string;
  at: string;
};

export type ThreadTaskEventsResult = {
  events: TaskEventView[];
};

// thread/subUpdated notification: pushed whenever a reply-subthread (cth) message
// is stored (agent reply, human post, or task_card fold). cth traffic never
// appends a main-stream turn, so this is the only live signal the split reply
// panel and the reply-count badge get. thread_id is the PARENT group thread;
// subthread_id identifies the cth; subthread carries the refreshed view (turns +
// reply_count) so the panel patches in place without a follow-up RPC. Rides the
// generic wuu:server-event channel — no dedicated IPC method.
export type SubthreadUpdatedNotification = {
  thread_id: string;
  subthread_id: string;
  subthread: ConversationSubthread;
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

// Filesystem-level status of the "install wuu command" action (symlink into
// ~/.local/bin). Produced by the pure cliInstall module in the main process.
export type CliInstallFsStatus = {
  platform_supported: boolean;
  platform: string;
  source_path: string | null;
  install_dir: string;
  install_path: string;
  installed: boolean;
  linked_to_source: boolean;
  // install_path is a symlink whose target no longer exists (the app was
  // moved or updated). Safe for the startup auto-install to rebuild.
  link_dangling: boolean;
  // Something else owns install_path: a real binary (go install /
  // install.sh) or a symlink to another target. The desktop never touches
  // it automatically; only the manual overwrite flow may replace it.
  foreign_install: boolean;
  on_path: boolean;
};

// Full status shown in settings: filesystem state plus the persisted
// auto-install preference and the outcome of this session's startup
// auto-install pass (if any).
export type CliInstallStatus = CliInstallFsStatus & {
  auto_install_enabled: boolean;
  last_auto_install: CliAutoInstallResult | null;
};

export type CliAutoInstallOutcome =
  | "installed" // fresh symlink created (nothing was there)
  | "repaired" // dangling symlink rebuilt after an app move/update
  | "already-linked" // nothing to do
  | "skipped-existing" // foreign wuu present — left untouched
  | "unsupported" // platform without symlink support (Windows)
  | "no-source" // no wuu binary could be located
  | "failed"; // filesystem error; never blocks startup

export type CliAutoInstallResult = {
  outcome: CliAutoInstallOutcome;
  install_path: string;
  source_path: string | null;
  on_path: boolean;
  message?: string;
};

export type CliInstallResult = {
  ok: boolean;
  // Set when a file already exists at install_path and overwrite was not
  // requested — the UI prompts before replacing it.
  needs_overwrite?: boolean;
  install_path: string;
  source_path?: string;
  on_path: boolean;
  message?: string;
};

export type Turn = {
  id: string;
  kind?: TurnKind;
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
  attempt?: number;
  max_attempts?: number;
  retry_count?: number;
  max_retries?: number;
  retry_in_ms?: number;
  elapsed_ms?: number;
  budget_ms?: number;
  reason?: string;
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

// Structured metadata for user messages that were injected into a
// resident agent's DM thread by the envelope router rather than typed
// by the user. Mirrors the session_messages.envelope_meta JSON column
// (docs/plans/2026-07-03-resident-named-agents.md §3.3). Presence of
// this field switches the DM view from a user bubble to a collapsed
// meta row (§7.3). All fields optional so partial backend payloads
// still render.
// One record per envelope coalesced into this user message. Mirrors the
// backend's envelopeMetaRecord array (resident-named-agents.md §3.3,
// 2026-07-03 revision). Message count == array length.
export type EnvelopeMetaRecord = {
  id?: string;
  source_thread_id?: string;
  // Snapshot of the source thread title at write time (backend fills it
  // per the revised contract; may be absent on older rows).
  source_thread_title?: string;
  addressed?: boolean;
  hop?: number;
  sender_participant_id?: string;
  created_at?: string;
};
export type EnvelopeMeta = EnvelopeMetaRecord[];

// FocusMeta describes a "workspace focus" declaration the backend injects
// into a DM/group chat-style thread's message stream whenever the
// thread's active focus changes (composer focus chip — see the desktop
// chat-style-threads-design.md follow-up on per-thread work focus).
// "all" = the thread can touch every workspace; "home" = scoped to the
// resident agent's personal home directory only; "workspace" = scoped to
// one named project/workspace, identified by `name` (display) and/or
// `root` (absolute path). Rides on ThreadItem as a sibling to
// envelope_meta; the renderer treats its mere presence as the signal to
// render a divider row instead of a normal message, regardless of the
// item's `type`.
export type FocusMeta = {
  kind: "all" | "home" | "workspace";
  name?: string;
  root?: string;
};

export type ThreadItem = {
  id: string;
  // seq is the message's stable per-thread address (session_messages.seq).
  // The chat view keys read receipts and reactions (both addressed by seq) to
  // the bubble carrying the same seq. Absent for synthetic/unpersisted items.
  seq?: number;
  source_id?: string;
  agent_id?: string;
  type: ThreadItemType;
  status?: ThreadItemStatus;
  phase?: ThreadItemPhase;
  role?: string;
  text?: string;
  post_kind?: string;
  images?: InputImage[];
  files?: InputFile[];
  name?: string;
  arguments?: string;
  display?: ToolCallDisplay;
  result?: string;
  error?: string;
  reason?: string;
  task?: TaskCard;
  participant?: ParticipantSummary;
  envelope_meta?: EnvelopeMeta;
  focus_meta?: FocusMeta;
};

// One read-receipt or reaction row for a message, addressed by (thread, seq).
// Mirrors the backend message_marks wire (2026-07-04-read-receipts-and-
// reactions.md §4/§5). kind="seen" carries status; kind="reaction" carries a key.
export type MessageMarkWire = {
  seq: number;
  participant_id: string;
  kind: "seen" | "reaction";
  status?: "in_progress" | "completed" | "failed";
  reaction?: string;
  at_ms?: number;
};

// thread/marks result: every mark in a thread, fetched on chat-view load.
export type ThreadMarksResult = {
  marks: MessageMarkWire[];
};

// message/mark notification: one incremental mark change to patch a bubble.
export type MessageMarkNotification = MessageMarkWire & {
  thread_id: string;
};

// message/react result: acknowledges a human-stamped reaction. The reaction
// itself lands back on the bubble via a "message/mark" notification.
export type MessageReactResult = {
  ok: boolean;
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
  can_clear?: boolean;
};

// Appearance preference for the desktop shell. "system" follows the OS
// light/dark setting via prefers-color-scheme.
export type ThemePreference = "system" | "light" | "dark";

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
};

export type CodexPetsSnapshot = CodexPetSettings & {
  home: string;
  pets: CodexPet[];
  errors: string[];
};

export type CodexPetSettingsUpdate = Partial<CodexPetSettings>;

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
  gitStatus: () => Promise<GitStatusResult>;
  listGitChanges: () => Promise<GitChangesResult>;
  readGitFileDiff: (path: string) => Promise<GitFileDiffResult>;
  checkoutGitBranch: (branch: string) => Promise<GitStatusResult>;
  createCheckoutGitBranch: (branch: string) => Promise<GitCreateBranchResult>;
  commitGitChanges: (params: GitCommitParams) => Promise<GitCommitResult>;
  createPullRequest: (params: GitPullRequestParams) => Promise<GitPullRequestResult>;
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
  listSkills: () => Promise<SkillListResult>;
  startThread: (params?: ThreadStartParams) => Promise<{ thread: Thread }>;
  resumeThread: (sessionId?: string) => Promise<{ thread: Thread }>;
  startParticipant: (params: ParticipantStartParams) => Promise<ParticipantStartResult>;
  forkThread: (
    threadId: string,
    turnId?: string,
    itemId?: string,
    mode?: "local" | "worktree",
  ) => Promise<ThreadForkResult>;
  editThreadMessage: (threadId: string, turnId: string, itemId: string) => Promise<ThreadEditMessageResult>;
  getThreadContextComposition: (threadId: string) => Promise<ThreadContextCompositionResult>;
  // Instruction files (AGENTS.md / CLAUDE.md, ...) loaded into the base
  // system prompt at session start. Read-only; used by the session view's
  // 指令文件 block to mirror Claude Code's /memory visibility.
  listInstructionFiles: () => Promise<InstructionsListResult>;
  // "Install wuu command" action: inspect and create a ~/.local/bin/wuu
  // symlink, mirroring VS Code's "install 'code' command". Install runs
  // automatically at startup (self-repairing, never overwriting a foreign
  // install); the toggle below persists whether that auto pass is enabled.
  getCliInstallStatus: () => Promise<CliInstallStatus>;
  installCli: (overwrite?: boolean) => Promise<CliInstallResult>;
  setCliAutoInstallEnabled: (enabled: boolean) => Promise<{ ok: boolean; enabled: boolean }>;
  // 远程控制（设置 → 远程）。管理机器级 remote host 守护进程与手机配对,
  // 走主进程 RemoteHostManager 而非 app-server 协议。
  getRemoteControlSnapshot: () => Promise<RemoteControlSnapshot>;
  setRemoteRelay: (relayUrl: string) => Promise<RemoteControlSnapshot>;
  setRemoteHostEnabled: (enabled: boolean) => Promise<RemoteControlSnapshot>;
  startRemotePairing: () => Promise<RemoteControlSnapshot>;
  removeRemoteDevice: (fingerprintOrPub: string) => Promise<RemoteControlSnapshot>;
  onRemoteControlEvent: (handler: (event: RemoteControlEvent) => void) => () => void;
  // Appearance. The preference persists in desktop-settings.json; the
  // renderer resolves "system" against prefers-color-scheme and stamps
  // data-theme on <html>. `initialThemePreference` is read synchronously
  // in the preload script so the first paint already has the right theme.
  initialThemePreference?: ThemePreference;
  getThemePreference: () => Promise<ThemePreference>;
  setThemePreference: (
    theme: ThemePreference,
  ) => Promise<{ ok: boolean; theme: ThemePreference }>;
  listCodexPets: () => Promise<CodexPetsSnapshot>;
  updateCodexPetSettings: (
    settings: CodexPetSettingsUpdate,
  ) => Promise<CodexPetsSnapshot>;
  listParticipants: () => Promise<ParticipantListResult>;
  saveParticipant: (params: ParticipantSaveParams) => Promise<ParticipantSaveResult>;
  sendParticipantFeedback: (
    participantId: string,
    text: string,
    taskId?: string,
    messageId?: string
  ) => Promise<ParticipantFeedbackResult>;
  resetParticipant: (
    participantId: string,
    scope: "restart" | "session" | "full"
  ) => Promise<ParticipantResetResult>;
  retireParticipant: (participantId: string) => Promise<ParticipantRetireResult>;
  // 记忆面板（设置 → 记忆）。契约见 memory-redesign.md §8.2；后端 M2
  // 未落地时三个方法都会以 unknown method 错误拒绝，面板渲染占位态。
  getMemoryOverview: (params: MemoryOverviewParams) => Promise<MemoryOverviewResult>;
  sendMemoryChat: (params: MemoryChatParams) => Promise<MemoryChatResult>;
  readMemoryRaw: (params: MemoryReadParams) => Promise<MemoryReadResult>;
  listConversationSubthreads: (threadId: string) => Promise<ThreadListSubResult>;
  openConversationSubthread: (
    threadId: string,
    options: {
      subthreadId?: string;
      anchorItemId?: string;
      parentSeq?: number;
      title?: string;
      threadOwnerParticipantId?: string;
    }
  ) => Promise<ThreadOpenSubResult>;
  resolveConversationSubthread: (
    threadId: string,
    subthreadId: string,
    resolved: boolean
  ) => Promise<ThreadResolveSubResult>;
  // 把一条 Thread 升级为 Task:在同一个 cth 上挂 task_card + 推进到执行态。
  // Task lead 由 Thread owner 派生,桌面不允许在升级时重新选人。
  escalateConversationSubthread: (
    threadId: string,
    subthreadId: string,
    options?: { title?: string }
  ) => Promise<ThreadEscalateSubResult>;
  // 在分屏 reply 面板里以「你」的身份往 cth 发一条消息:折进 cth(thread_id=cth,
  // 不进主流,只推 cth 参与者子集),回执带刷新后的 subthread 视图。
  postSubthreadMessage: (
    threadId: string,
    subthreadId: string,
    text: string,
    // 复用主对话完整 composer 后,分屏 reply 也能带附件/截图。可选:纯文本发送时留空。
    images?: InputImage[],
    files?: InputFile[]
  ) => Promise<MessagePostSubthreadResult>;
  // 读一个已升级 subthread(task)的 trace 时间线,供 Task 面板的「轨迹」懒加载
  // (plan §T11)。只读。
  taskEvents: (
    threadId: string,
    subthreadId: string
  ) => Promise<ThreadTaskEventsResult>;
  listThreads: (cwd?: string) => Promise<{ threads: Thread[] }>;
  searchThreads: (query: string, limit?: number) => Promise<ThreadSearchResult>;
  getThreadPreview: (
    threadId: string,
    limit?: number
  ) => Promise<ThreadPreviewResult>;
  pinThread: (threadId: string, pinned: boolean) => Promise<{ thread: Thread }>;
  addThreadMember: (
    threadId: string,
    participantId: string,
  ) => Promise<{ thread: Thread }>;
  removeThreadMember: (
    threadId: string,
    participantId: string,
  ) => Promise<{ thread: Thread }>;
  // Read receipts + reactions for a chat thread, keyed by message seq. Live
  // changes arrive via onServerEvent as "message/mark" notifications.
  getThreadMarks: (threadId: string) => Promise<ThreadMarksResult>;
  // Stamp a human reaction (an emoji from the shared reaction vocabulary) on a
  // message, addressed by its seq. Terminal like the agent react tool: it never
  // routes or wakes an agent. The chip appears via the "message/mark" event.
  reactToMessage: (
    threadId: string,
    seq: number,
    reaction: string,
  ) => Promise<MessageReactResult>;
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
    // mentions carries @-mentioned participant IDs. Only attached to the
    // turn/start request when non-empty: the server decodes params with
    // DisallowUnknownFields, so plain sends must not carry the field
    // before the backend supports it.
    mentions?: string[],
    // focusWorkspace carries the chat-style composer's "work focus" chip
    // selection ("" all workspaces, "~" personal space, otherwise a
    // workspace name). Only attached when the caller has computed that
    // the in-session selection differs from Thread.focus_workspace — see
    // focusWorkspaceSendValue in AppState.ts — so a plain send that never
    // touched the chip carries nothing extra.
    focusWorkspace?: string,
  ) => Promise<{ turn: Turn }>;
  queueTurn: (
    threadId: string,
    prompt: string,
    images?: InputImage[],
    clientId?: string,
    files?: InputFile[],
    permissionMode?: string,
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
  // Open an external URL via the OS default browser. Used by the
  // assistant turn's 来源 pill to send the user to the page the agent
  // actually consulted (web_search hit / web_fetch target) instead of
  // pretending the agent fetched it for them. Validation that the
  // URL is http(s) lives in the main-process handler so the renderer
  // can't escalate arbitrary schemes via this channel.
  openExternal: (url: string) => Promise<void>;
  getSettingsUsage: (range?: SettingsUsageRange) => Promise<SettingsUsageResponse>;
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
};

/**
 * Sync return shape for `wuu:pop-out-init` (Plan §3 commit 5
 * verification). The snapshot is intentionally small and sync-safe:
 * it carries identity, not conversation data.
 */
export type PopOutInitResult = {
  /** Popped-out window kind, or null when this is the main window. */
  kind: "thread" | "draft" | "subthread" | null;
  /** The popped-out thread id, or null when this is the main window. For a
   *  subthread window this is the PARENT group thread (the cth's home). */
  threadID: string | null;
  /** The popped-out reply subthread (cth) id, or null unless kind is
   *  "subthread". */
  subthreadID: string | null;
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
    }
  | {
      // A reply subthread (cth) lifted into its own window. threadID is the
      // PARENT group thread (needed for runtime routing + participants); the
      // window renders the cth via the SAME ConversationSubthreadPanel the
      // in-window split uses.
      kind: "subthread";
      threadID: string;
      subthreadID: string;
      context: RuntimeContext;
    };

declare global {
  interface Window {
    wuu: WuuDesktopApi;
    wuuRenderableFileURL?: (encodedPath: string) => string;
  }
}
