package appserver

import (
	"encoding/json"
	"time"

	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/capability"
	"github.com/blueberrycongee/wuu/internal/goal"
	"github.com/blueberrycongee/wuu/internal/insight"
	"github.com/blueberrycongee/wuu/internal/modelroles"
	"github.com/blueberrycongee/wuu/internal/providers"
)

const (
	ProtocolVersion = "wuu-app-server/v0.1"

	MethodInitialize           = "initialize"
	MethodConfigRead           = "config/read"
	MethodConfigModelUpdate    = "config/model/update"
	MethodConfigAdvancedUpdate = "config/advanced/update"
	MethodConfigCodexModels    = "config/codex/models"
	MethodConfigProviderRemove = "config/provider/remove"
	MethodSkillList            = "skill/list"
	MethodGoalSnapshot         = "goal/snapshot"
	MethodGoalWorktreeReview   = "goal/worktree/review"
	MethodGoalWorktreeCleanup  = "goal/worktree/cleanup"
	MethodGoalWorktreeRollback = "goal/worktree/rollback"
	MethodGoalWorktreeMerge    = "goal/worktree/merge"
	MethodGoalApprovalResolve  = "goal/approval/resolve"
	// MethodGoalActiveSummary returns the lightweight composer-banner view
	// of the most-recently-updated non-terminal goal in the requested thread
	// scope. The mutation methods are user-owned controls for the active
	// runtime Goal; full workflow/agent detail stays on the agent tool loop.
	MethodGoalActiveSummary        = "goal/active-summary"
	MethodGoalPause                = "goal/pause"
	MethodGoalResume               = "goal/resume"
	MethodGoalClear                = "goal/clear"
	MethodGoalCancel               = "goal/cancel"
	MethodGoalUpdateText           = "goal/update-text"
	MethodThreadStart              = "thread/start"
	MethodThreadResume             = "thread/resume"
	MethodThreadFork               = "thread/fork"
	MethodThreadEditMessage        = "thread/edit-message"
	MethodThreadContextComposition = "thread/context-composition"
	MethodThreadList               = "thread/list"
	MethodThreadSearch             = "thread/search"
	MethodThreadPin                = "thread/pin"
	MethodThreadArchive            = "thread/archive"
	MethodThreadRegenerateTitle    = "thread/regenerate-title"
	MethodThreadRename             = "thread/rename"
	MethodTurnStart                = "turn/start"
	MethodTurnQueue                = "turn/queue"
	MethodTurnUpdateQueued         = "turn/update-queued"
	MethodTurnDequeue              = "turn/dequeue"
	MethodTurnSteer                = "turn/steer"
	MethodTurnUnsteer              = "turn/unsteer"
	MethodTurnInterrupt            = "turn/interrupt"
	MethodProcessList              = "process/list"
	MethodProcessStop              = "process/stop"
	MethodMCPList                  = "mcp/list"
	MethodMCPConnect               = "mcp/connect"
	MethodMCPDisconnect            = "mcp/disconnect"
	MethodMCPRefresh               = "mcp/refresh"
	MethodShutdown                 = "shutdown"
	// MethodSettingsUsage returns the aggregated per-provider/model token
	// usage snapshot for the desktop settings page. Range filter selects
	// the time window ("all", "7d", "30d", "90d"); empty defaults to "all".
	MethodSettingsUsage = "settings/usage"

	NotificationThreadStarted = "thread/started"
	NotificationThreadResumed = "thread/resumed"
	NotificationThreadUpdated = "thread/updated"
	NotificationTurnStarted   = "turn/started"
	NotificationTurnQueued    = "turn/queued"
	NotificationTurnDequeued  = "turn/dequeued"
	NotificationTurnEvent     = "turn/event"
	NotificationTurnError     = "turn/error"
	NotificationTurnCompleted = "turn/completed"
	// NotificationTurnUsage carries cumulative input/output token counts
	// for an in-flight turn so live UIs can render a real-time generation
	// speed gauge. Appserver-side throttles to a small number of pushes
	// per second; the renderer is expected to derive t/s from the deltas.
	NotificationTurnUsage = "turn/usage"

	NotificationItemStarted         = "item/started"
	NotificationItemCompleted       = "item/completed"
	NotificationAgentMessageDelta   = "item/agentMessage/delta"
	NotificationAgentMessageReplace = "item/agentMessage/replace"
	NotificationReasoningDelta      = "item/reasoning/delta"
	NotificationReasoningReplace    = "item/reasoning/replace"
	NotificationToolCallDelta       = "item/toolCall/delta"
	NotificationToolCallOutput      = "item/toolCall/outputDelta"
	NotificationAgentUpdated        = "agent/updated"
	NotificationAgentMailbox        = "agent/mailbox"
	NotificationMCPStatusUpdated    = "mcp/status/updated"
)

type Request struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type ServerRequest struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params any             `json:"params,omitempty"`
}

type ClientResponse struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *ResponseError  `json:"error,omitempty"`
}

type Response struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Result any             `json:"result,omitempty"`
	Error  *ResponseError  `json:"error,omitempty"`
}

type ResponseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Notification struct {
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
}

// CoreBuildInfo mirrors the fields of version.BuildInfo that the desktop
// needs to render the build identity of the wuu app-server. Kept as a
// separate struct (rather than embedding version.BuildInfo) so the wire
// schema stays stable even if the version package evolves.
type CoreBuildInfo struct {
	Version string `json:"version,omitempty"`
	Commit  string `json:"commit,omitempty"`
	Date    string `json:"date,omitempty"`
	Dirty   bool   `json:"dirty,omitempty"`
}

type InitializeResult struct {
	ProtocolVersion  string                  `json:"protocol_version"`
	Core             CoreBuildInfo           `json:"core"`
	Provider         string                  `json:"provider"`
	Model            string                  `json:"model"`
	Effort           string                  `json:"effort,omitempty"`
	Variant          string                  `json:"variant,omitempty"`
	WorkspaceRoot    string                  `json:"workspace_root"`
	ToolPolicy       ToolPolicySummary       `json:"tool_policy"`
	Permissions      PermissionSummary       `json:"permissions"`
	ExtensionTrust   ExtensionTrustSummary   `json:"extension_trust"`
	ModelProfile     *ModelProfileSummary    `json:"model_profile,omitempty"`
	ToolSurface      *ToolSurfaceSummary     `json:"tool_surface,omitempty"`
	ModelRoles       []ModelRoleSummary      `json:"model_roles,omitempty"`
	Providers        []ProviderSummary       `json:"providers,omitempty"`
	AdvancedSettings AdvancedSettingsSummary `json:"advanced_settings"`
}

type ModelProfileSummary struct {
	ProfileName   string `json:"profile_name"`
	Provider      string `json:"provider"`
	Model         string `json:"model"`
	EditPrimitive string `json:"edit_primitive"`
	BashFirst     bool   `json:"bash_first"`
}

type ToolSurfaceSummary = capability.Summary

type ConfigReadResult struct {
	Provider         string                  `json:"provider"`
	Model            string                  `json:"model"`
	Effort           string                  `json:"effort,omitempty"`
	Variant          string                  `json:"variant,omitempty"`
	ConfigPath       string                  `json:"config_path"`
	WorkspaceRoot    string                  `json:"workspace_root"`
	SessionDir       string                  `json:"session_dir"`
	ToolPolicy       ToolPolicySummary       `json:"tool_policy"`
	Permissions      PermissionSummary       `json:"permissions"`
	ExtensionTrust   ExtensionTrustSummary   `json:"extension_trust"`
	ModelProfile     *ModelProfileSummary    `json:"model_profile,omitempty"`
	ToolSurface      *ToolSurfaceSummary     `json:"tool_surface,omitempty"`
	ModelRoles       []ModelRoleSummary      `json:"model_roles,omitempty"`
	Providers        []ProviderSummary       `json:"providers,omitempty"`
	AdvancedSettings AdvancedSettingsSummary `json:"advanced_settings"`
}

type ToolPolicySummary struct {
	Profile       string            `json:"profile,omitempty"`
	DefaultAction string            `json:"default_action,omitempty"`
	Tools         map[string]string `json:"tools,omitempty"`
	Kinds         map[string]string `json:"kinds,omitempty"`
	Risks         map[string]string `json:"risks,omitempty"`
}

type PermissionSummary struct {
	Mode              string `json:"mode,omitempty"`
	PermissionProfile string `json:"permission_profile,omitempty"`
	ApprovalPolicy    string `json:"approval_policy,omitempty"`
	ApprovalsReviewer string `json:"approvals_reviewer,omitempty"`
}

type MCPServerStatus struct {
	Name       string `json:"name"`
	State      string `json:"state"`
	AuthStatus string `json:"auth_status,omitempty"`
	Connected  bool   `json:"connected"`
	ToolCount  int    `json:"tool_count"`
	Error      string `json:"error,omitempty"`
}

type MCPListResult struct {
	Servers []MCPServerStatus `json:"servers"`
}

type MCPServerActionParams struct {
	Name string `json:"name,omitempty"`
}

type MCPServerActionResult struct {
	Status MCPServerStatus `json:"status"`
}

type ExtensionTrustSummary struct {
	MainSession     ExtensionSessionTrustSummary `json:"main_session"`
	ReviewerSession ExtensionSessionTrustSummary `json:"reviewer_session"`
}

type ExtensionSessionTrustSummary struct {
	MCP           ExtensionSurfaceTrustSummary `json:"mcp"`
	Hooks         ExtensionSurfaceTrustSummary `json:"hooks"`
	Plugins       ExtensionSurfaceTrustSummary `json:"plugins"`
	Skills        ExtensionSurfaceTrustSummary `json:"skills"`
	Workflows     ExtensionSurfaceTrustSummary `json:"workflows"`
	ExternalTools ExtensionSurfaceTrustSummary `json:"external_tools"`
}

type ExtensionSurfaceTrustSummary struct {
	Allowed      bool `json:"allowed"`
	Active       bool `json:"active"`
	Count        int  `json:"count,omitempty"`
	KnownTools   int  `json:"known_tools,omitempty"`
	VisibleTools int  `json:"visible_tools,omitempty"`
}

type ConfigModelUpdateParams struct {
	Provider       string  `json:"provider,omitempty"`
	Model          string  `json:"model"`
	Effort         *string `json:"effort,omitempty"`
	Variant        *string `json:"variant,omitempty"`
	PermissionMode *string `json:"permission_mode,omitempty"`
	BaseURL        *string `json:"base_url,omitempty"`
	APIKey         *string `json:"api_key,omitempty"`
	// Type is the provider protocol type used when CreateProvider is true.
	// Accepted values: "openai", "openai-compatible", "anthropic", "claude",
	// "anthropic-official". Codex OAuth types are intentionally excluded here
	// because they require a separate OAuth-managed connection flow.
	Type           *string `json:"type,omitempty"`
	CreateProvider bool    `json:"create_provider,omitempty"`
}

type ConfigModelUpdateResult struct {
	Provider         string                  `json:"provider"`
	Model            string                  `json:"model"`
	Effort           string                  `json:"effort,omitempty"`
	Variant          string                  `json:"variant,omitempty"`
	ToolPolicy       ToolPolicySummary       `json:"tool_policy"`
	Permissions      PermissionSummary       `json:"permissions"`
	ExtensionTrust   ExtensionTrustSummary   `json:"extension_trust"`
	ModelProfile     *ModelProfileSummary    `json:"model_profile,omitempty"`
	ToolSurface      *ToolSurfaceSummary     `json:"tool_surface,omitempty"`
	ModelRoles       []ModelRoleSummary      `json:"model_roles,omitempty"`
	Providers        []ProviderSummary       `json:"providers,omitempty"`
	AdvancedSettings AdvancedSettingsSummary `json:"advanced_settings"`
}

// ConfigProviderRemoveParams requests the deletion of a configured
// provider. The handler enforces the same safety guards as the
// existing config handlers (no removal of the last provider, no
// removal of OAuth/connection-locked providers, and no removal of a
// provider currently used by a running turn) and atomically swaps the
// default provider to FallbackProvider when the removed provider was
// active.
//
// FallbackModel is applied to the new default after the swap so
// the runtime has a model to use. When the caller does not
// specify a fallback, the server picks another existing provider
// (or, in the single-provider removal path, returns an error).
type ConfigProviderRemoveParams struct {
	// Provider is the configured provider name to delete. Required.
	Provider string `json:"provider"`
	// FallbackProvider becomes the new default_provider if the removed
	// provider was the active one. Optional; server picks another
	// existing provider when empty. Required when removing the last
	// remaining provider so the runtime still has a model to use.
	FallbackProvider string `json:"fallback_provider,omitempty"`
	// FallbackModel is applied to the new default provider after the
	// swap so the runtime has a model to use. Optional; server reuses
	// the removed provider's model when empty.
	FallbackModel string `json:"fallback_model,omitempty"`
}

// ConfigProviderRemoveResult mirrors ConfigModelUpdateResult. The
// renderer reuses its existing updateRuntimeSettings reducer to
// merge provider/model/toolpolicy/permissions into the initialized
// state, so the shape intentionally matches that result.
type ConfigProviderRemoveResult struct {
	Provider         string                  `json:"provider"`
	Model            string                  `json:"model"`
	Variant          string                  `json:"variant,omitempty"`
	ToolPolicy       ToolPolicySummary       `json:"tool_policy"`
	Permissions      PermissionSummary       `json:"permissions"`
	ExtensionTrust   ExtensionTrustSummary   `json:"extension_trust"`
	ModelProfile     *ModelProfileSummary    `json:"model_profile,omitempty"`
	ToolSurface      *ToolSurfaceSummary     `json:"tool_surface,omitempty"`
	ModelRoles       []ModelRoleSummary      `json:"model_roles,omitempty"`
	Providers        []ProviderSummary       `json:"providers,omitempty"`
	AdvancedSettings AdvancedSettingsSummary `json:"advanced_settings"`
}

type ConfigAdvancedUpdateParams struct {
	MaxSteps                *int     `json:"max_steps,omitempty"`
	MaxContextTokens        *int     `json:"max_context_tokens,omitempty"`
	Temperature             *float64 `json:"temperature,omitempty"`
	CompactThresholdPct     *float64 `json:"compact_threshold_pct,omitempty"`
	CompactKeepRecentTokens *int     `json:"compact_keep_recent_tokens,omitempty"`
	DisableAutoCompact      *bool    `json:"disable_auto_compact,omitempty"`
	ProviderContextWindow   *int     `json:"provider_context_window,omitempty"`
}

type ConfigAdvancedUpdateResult struct {
	AdvancedSettings AdvancedSettingsSummary `json:"advanced_settings"`
	Providers        []ProviderSummary       `json:"providers,omitempty"`
}

type AdvancedSettingsSummary struct {
	MaxSteps                int     `json:"max_steps"`
	MaxContextTokens        int     `json:"max_context_tokens"`
	Temperature             float64 `json:"temperature"`
	CompactThresholdPct     float64 `json:"compact_threshold_pct,omitempty"`
	CompactKeepRecentTokens int     `json:"compact_keep_recent_tokens,omitempty"`
	DisableAutoCompact      bool    `json:"disable_auto_compact"`
	ProviderContextWindow   int     `json:"provider_context_window,omitempty"`
	ContextWindowTokens     int     `json:"context_window_tokens,omitempty"`
	ContextWindowSource     string  `json:"context_window_source,omitempty"`
	InputLimitTokens        int     `json:"input_limit_tokens,omitempty"`
	OutputReserveTokens     int     `json:"output_reserve_tokens,omitempty"`
	CompactThresholdTokens  int     `json:"compact_threshold_tokens,omitempty"`
}

type ConfigCodexModelsParams struct {
	Provider string `json:"provider,omitempty"`
}

type ConfigCodexModelsResult struct {
	Provider string              `json:"provider"`
	Model    string              `json:"model"`
	Effort   string              `json:"effort,omitempty"`
	Variant  string              `json:"variant,omitempty"`
	Models   []CodexModelSummary `json:"models"`
}

type SkillSummary struct {
	Name                  string   `json:"name"`
	Description           string   `json:"description,omitempty"`
	WhenToUse             string   `json:"when_to_use,omitempty"`
	TriggerCondition      string   `json:"trigger_condition,omitempty"`
	Source                string   `json:"source"`
	Path                  string   `json:"path,omitempty"`
	ArgumentHint          string   `json:"argument_hint,omitempty"`
	Model                 string   `json:"model,omitempty"`
	Context               string   `json:"context,omitempty"`
	Agent                 string   `json:"agent,omitempty"`
	AllowedTools          []string `json:"allowed_tools,omitempty"`
	RequiredContext       []string `json:"required_context,omitempty"`
	Examples              []string `json:"examples,omitempty"`
	VerificationChecklist []string `json:"verification_checklist,omitempty"`
	ProgressiveDisclosure string   `json:"progressive_disclosure,omitempty"`
	UserInvocable         bool     `json:"user_invocable"`
	DisableModelInvoke    bool     `json:"disable_model_invoke"`
	Paths                 []string `json:"paths,omitempty"`
	Effort                string   `json:"effort,omitempty"`
	Version               string   `json:"version,omitempty"`
}

type SkillListResult struct {
	Skills []SkillSummary `json:"skills"`
}

type GoalSnapshotParams struct {
	ThreadID string `json:"thread_id,omitempty"`
}

type GoalSnapshotResult struct {
	Snapshot goal.SystemSnapshot `json:"snapshot"`
}

// GoalActiveSummary is the composer-banner view of the most recently
// updated non-terminal goal in one thread/session orchestration scope.
// The handler filters terminal statuses (completed, failed, cancelled)
// so the renderer can treat a nil summary as "no active goal" without
// re-checking status.
// Text is the first line of goal.Goal. The renderer owns visual ellipsis
// so editing a long first line never persists a server-side truncation.
// StartedAt is the canonical goal creation timestamp; the renderer uses
// it as the baseline for an elapsed-time counter so the timer survives a
// desktop reload instead of restarting from "first time the renderer saw
// the goal". It intentionally omits task / step / approvals to keep the
// composer surface quiet.
type GoalActiveSummary struct {
	ID                      string `json:"id"`
	ThreadID                string `json:"thread_id,omitempty"`
	Text                    string `json:"text"`
	Status                  string `json:"status"`
	Step                    string `json:"step,omitempty"`
	StartedAt               string `json:"started_at,omitempty"`
	UpdatedAt               string `json:"updated_at,omitempty"`
	StopReason              string `json:"stop_reason,omitempty"`
	RecentProgress          string `json:"recent_progress,omitempty"`
	TokensUsed              int    `json:"tokens_used,omitempty"`
	TimeUsedSeconds         int64  `json:"time_used_seconds,omitempty"`
	GoalTurns               int    `json:"goal_turns,omitempty"`
	Blocker                 string `json:"blocker,omitempty"`
	BlockerConsecutiveTurns int    `json:"blocker_consecutive_turns,omitempty"`
	CanPause                bool   `json:"can_pause,omitempty"`
	CanResume               bool   `json:"can_resume,omitempty"`
	CanCancel               bool   `json:"can_cancel,omitempty"`
	CanClear                bool   `json:"can_clear,omitempty"`
}

type GoalActiveSummaryParams struct {
	ThreadID string `json:"thread_id,omitempty"`
}

type GoalActiveSummaryResult struct {
	Summary *GoalActiveSummary `json:"summary,omitempty"`
}

type GoalCancelParams struct {
	GoalID              string `json:"goal_id"`
	ThreadID            string `json:"thread_id,omitempty"`
	ConfirmUserApproved bool   `json:"confirm_user_approved,omitempty"`
}

type GoalPauseParams struct {
	GoalID              string `json:"goal_id"`
	ThreadID            string `json:"thread_id,omitempty"`
	ConfirmUserApproved bool   `json:"confirm_user_approved,omitempty"`
}

type GoalResumeParams struct {
	GoalID              string `json:"goal_id"`
	ThreadID            string `json:"thread_id,omitempty"`
	ConfirmUserApproved bool   `json:"confirm_user_approved,omitempty"`
}

type GoalClearParams struct {
	GoalID              string `json:"goal_id"`
	ThreadID            string `json:"thread_id,omitempty"`
	ConfirmUserApproved bool   `json:"confirm_user_approved,omitempty"`
}

type GoalUpdateTextParams struct {
	GoalID              string `json:"goal_id"`
	ThreadID            string `json:"thread_id,omitempty"`
	Text                string `json:"text"`
	ConfirmUserApproved bool   `json:"confirm_user_approved,omitempty"`
}

type GoalCancelResult struct {
	OK bool `json:"ok"`
}

type GoalPauseResult struct {
	OK bool `json:"ok"`
}

type GoalResumeResult struct {
	OK bool `json:"ok"`
}

type GoalClearResult struct {
	OK bool `json:"ok"`
}

type GoalUpdateTextResult struct {
	OK bool `json:"ok"`
}

type GoalWorktreeReviewParams struct {
	WorktreePath string `json:"worktree_path"`
	MaxDiffBytes int    `json:"max_diff_bytes,omitempty"`
}

type GoalWorktreeReviewResult struct {
	Review goal.WorktreeReview `json:"review"`
}

type GoalWorktreeCleanupParams struct {
	WorktreePath               string `json:"worktree_path"`
	ConfirmUserApproved        bool   `json:"confirm_user_approved,omitempty"`
	ConfirmRemoveCleanWorktree bool   `json:"confirm_remove_clean_worktree,omitempty"`
}

type GoalWorktreeCleanupResult struct {
	Cleanup goal.WorktreeCleanupResult `json:"cleanup"`
}

type GoalWorktreeRollbackParams struct {
	WorktreePath                  string `json:"worktree_path"`
	ConfirmUserApproved           bool   `json:"confirm_user_approved,omitempty"`
	ConfirmDiscardWorktreeChanges bool   `json:"confirm_discard_worktree_changes,omitempty"`
}

type GoalWorktreeRollbackResult struct {
	Rollback goal.WorktreeRollbackResult `json:"rollback"`
}

type GoalWorktreeMergeParams struct {
	WorktreePath              string `json:"worktree_path"`
	ConfirmUserApproved       bool   `json:"confirm_user_approved,omitempty"`
	ConfirmApplyWorktreeDiff  bool   `json:"confirm_apply_worktree_diff,omitempty"`
	ConfirmTargetRepoMutation bool   `json:"confirm_target_repo_mutation,omitempty"`
}

type GoalWorktreeMergeResult struct {
	Merge goal.WorktreeMergeResult `json:"merge"`
}

type GoalApprovalResolveParams struct {
	GoalID              string `json:"goal_id"`
	ThreadID            string `json:"thread_id,omitempty"`
	ApprovalID          string `json:"approval_id"`
	Approved            bool   `json:"approved,omitempty"`
	Rejected            bool   `json:"rejected,omitempty"`
	ResolvedBy          string `json:"resolved_by,omitempty"`
	Resolution          string `json:"resolution,omitempty"`
	ConfirmUserApproved bool   `json:"confirm_user_approved,omitempty"`
}

type GoalApprovalResolveResult struct {
	Approval goal.ApprovalRequest `json:"approval"`
}

type ManagedProcessSummary struct {
	ID                string    `json:"id"`
	OwnerKind         string    `json:"owner_kind"`
	OwnerID           string    `json:"owner_id"`
	Lifecycle         string    `json:"lifecycle"`
	Status            string    `json:"status"`
	PID               int       `json:"pid"`
	TTY               bool      `json:"tty,omitempty"`
	Command           string    `json:"command"`
	CWD               string    `json:"cwd"`
	PreviewURLs       []string  `json:"preview_urls,omitempty"`
	PrimaryPreviewURL string    `json:"primary_preview_url,omitempty"`
	StartedAt         time.Time `json:"started_at"`
	UpdatedAt         time.Time `json:"updated_at"`
	StoppedAt         time.Time `json:"stopped_at,omitempty"`
	ExitCode          int       `json:"exit_code,omitempty"`
	LastError         string    `json:"last_error,omitempty"`
}

type ProcessListResult struct {
	Processes []ManagedProcessSummary `json:"processes"`
}

type ProcessStopParams struct {
	ProcessID string `json:"process_id"`
}

type ProcessStopResult struct {
	Process ManagedProcessSummary `json:"process"`
}

type CodexModelSummary struct {
	Slug                  string   `json:"slug"`
	DisplayName           string   `json:"display_name,omitempty"`
	DefaultReasoningLevel string   `json:"default_reasoning_level,omitempty"`
	SupportedReasoning    []string `json:"supported_reasoning,omitempty"`
	SupportedInAPI        bool     `json:"supported_in_api"`
}

type ProviderSummary struct {
	Name             string                 `json:"name"`
	Type             string                 `json:"type"`
	Model            string                 `json:"model"`
	BaseURL          string                 `json:"base_url,omitempty"`
	APIKeyConfigured bool                   `json:"api_key_configured,omitempty"`
	ConnectionLocked bool                   `json:"connection_locked,omitempty"`
	Models           []ProviderModelSummary `json:"models,omitempty"`
}

type ProviderModelSummary struct {
	ID               string                        `json:"id"`
	DisplayName      string                        `json:"display_name,omitempty"`
	DefaultEffort    string                        `json:"default_effort,omitempty"`
	DefaultVariant   string                        `json:"default_variant,omitempty"`
	SupportedEfforts []string                      `json:"supported_efforts,omitempty"`
	Variants         []ProviderModelVariantSummary `json:"variants,omitempty"`
	Capabilities     ModelCapabilitySummary        `json:"capabilities,omitempty"`
	Behavior         ModelBehaviorSummary          `json:"behavior,omitempty"`
	Source           string                        `json:"source,omitempty"`
}

type ModelRoleSummary struct {
	Role         string                 `json:"role"`
	Provider     string                 `json:"provider"`
	Model        string                 `json:"model"`
	APIModel     string                 `json:"api_model,omitempty"`
	Effort       string                 `json:"effort,omitempty"`
	Variant      string                 `json:"variant,omitempty"`
	Inherited    bool                   `json:"inherited,omitempty"`
	Capabilities ModelCapabilitySummary `json:"capabilities,omitempty"`
	Behavior     ModelBehaviorSummary   `json:"behavior,omitempty"`
}

type ModelCapabilitySummary = modelroles.Capabilities

type ModelBehaviorSummary = modelroles.Behavior

type ProviderModelVariantSummary struct {
	ID      string         `json:"id"`
	Options map[string]any `json:"options,omitempty"`
}

type ThreadStartParams struct {
	Ephemeral bool `json:"ephemeral,omitempty"`
}

type ThreadStartResult struct {
	Thread Thread `json:"thread"`
}

type ThreadResumeParams struct {
	SessionID string `json:"session_id,omitempty"`
}

type ThreadResumeResult struct {
	Thread Thread `json:"thread"`
}

type ThreadForkParams struct {
	ThreadID string `json:"thread_id"`
	TurnID   string `json:"turn_id,omitempty"`
	ItemID   string `json:"item_id,omitempty"`
	Mode     string `json:"mode,omitempty"`
}

type ThreadForkResult struct {
	Thread   Thread        `json:"thread"`
	Worktree *WorktreeInfo `json:"worktree,omitempty"`
}

type ThreadEditMessageParams struct {
	ThreadID string `json:"thread_id"`
	TurnID   string `json:"turn_id"`
	ItemID   string `json:"item_id"`
}

type ThreadEditDraft struct {
	Prompt string           `json:"prompt"`
	Images []TurnStartImage `json:"images,omitempty"`
	Files  []TurnStartFile  `json:"files,omitempty"`
}

type ThreadEditMessageResult struct {
	Thread Thread          `json:"thread"`
	Draft  ThreadEditDraft `json:"draft"`
}

type ThreadContextCompositionParams struct {
	ThreadID string `json:"thread_id"`
}

type ThreadContextCompositionResult struct {
	ThreadID               string                       `json:"thread_id"`
	Available              bool                         `json:"available"`
	Reason                 string                       `json:"reason,omitempty"`
	Mode                   string                       `json:"mode,omitempty"`
	TracePath              string                       `json:"trace_path,omitempty"`
	TurnID                 string                       `json:"turn_id,omitempty"`
	StepIndex              int                          `json:"step_index,omitempty"`
	Provider               string                       `json:"provider,omitempty"`
	Model                  string                       `json:"model,omitempty"`
	ContextWindowTokens    int                          `json:"context_window_tokens,omitempty"`
	InputLimitTokens       int                          `json:"input_limit_tokens,omitempty"`
	UsableInputTokens      int                          `json:"usable_input_tokens,omitempty"`
	CompactThresholdTokens int                          `json:"compact_threshold_tokens,omitempty"`
	PromptTokens           int                          `json:"prompt_tokens,omitempty"`
	TotalContextTokens     int                          `json:"total_context_tokens,omitempty"`
	RetainedTokens         int                          `json:"retained_tokens,omitempty"`
	InputTokens            int                          `json:"input_tokens,omitempty"`
	OutputTokens           int                          `json:"output_tokens,omitempty"`
	CacheCreationTokens    int                          `json:"cache_creation_tokens,omitempty"`
	CacheReadTokens        int                          `json:"cache_read_tokens,omitempty"`
	TokenEstimateSource    string                       `json:"token_estimate_source,omitempty"`
	MessageCount           int                          `json:"message_count,omitempty"`
	SystemMessages         int                          `json:"system_messages,omitempty"`
	HiddenMessages         int                          `json:"hidden_messages,omitempty"`
	ToolCount              int                          `json:"tool_count,omitempty"`
	StablePrefix           int                          `json:"stable_prefix,omitempty"`
	TurnPrefix             int                          `json:"turn_prefix,omitempty"`
	DynamicBytes           int                          `json:"dynamic_context_bytes,omitempty"`
	SystemHash             string                       `json:"system_hash,omitempty"`
	StablePrefixHash       string                       `json:"stable_prefix_hash,omitempty"`
	TurnPrefixHash         string                       `json:"turn_prefix_hash,omitempty"`
	ToolSurfaceHash        string                       `json:"tool_surface_hash,omitempty"`
	PromptCacheKey         string                       `json:"prompt_cache_key,omitempty"`
	Categories             []ContextCompositionCategory `json:"categories,omitempty"`
	SystemSections         []ContextCompositionSection  `json:"system_sections,omitempty"`
	BlockKindBytes         map[string]int               `json:"block_kind_bytes,omitempty"`
	SegmentCounts          ContextSegmentCountSummary   `json:"segment_counts,omitempty"`
}

type ContextCompositionCategory struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Tone        string `json:"tone,omitempty"`
	Bytes       int    `json:"bytes,omitempty"`
	Tokens      int    `json:"tokens,omitempty"`
	Contributes bool   `json:"contributes"`
	Durable     bool   `json:"durable,omitempty"`
	CacheScope  string `json:"cache_scope,omitempty"`
	RequestOnly bool   `json:"request_only,omitempty"`
	Deferred    bool   `json:"deferred,omitempty"`
}

type ContextCompositionSection struct {
	Key    string `json:"key"`
	Static bool   `json:"static"`
	Bytes  int    `json:"bytes"`
	Tokens int    `json:"tokens,omitempty"`
	Hash   string `json:"hash,omitempty"`
}

type ContextSegmentCountSummary struct {
	Lifecycle   map[string]int `json:"lifecycle,omitempty"`
	Placement   map[string]int `json:"placement,omitempty"`
	CachePolicy map[string]int `json:"cache_policy,omitempty"`
}

type ThreadListResult struct {
	Threads []Thread `json:"threads"`
}

type ThreadListParams struct {
	CWD string `json:"cwd,omitempty"`
}

type ThreadSearchParams struct {
	Query string `json:"query,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type ThreadSearchResult struct {
	Results []ThreadSearchResultItem `json:"results"`
}

type ThreadSearchResultItem struct {
	Thread  Thread `json:"thread"`
	Snippet string `json:"snippet,omitempty"`
}

type ThreadPinParams struct {
	ThreadID string `json:"thread_id"`
	Pinned   bool   `json:"pinned"`
}

type ThreadPinResult struct {
	Thread Thread `json:"thread"`
}

type ThreadArchiveParams struct {
	ThreadID string `json:"thread_id"`
	Archived bool   `json:"archived"`
}

type ThreadArchiveResult struct {
	Thread Thread `json:"thread"`
}

type ThreadRenameParams struct {
	ThreadID string `json:"thread_id"`
	Title    string `json:"title"`
}

type ThreadRenameResult struct {
	Thread Thread `json:"thread"`
}

type TurnStartParams struct {
	ThreadID       string           `json:"thread_id"`
	Prompt         string           `json:"prompt"`
	Images         []TurnStartImage `json:"images,omitempty"`
	Files          []TurnStartFile  `json:"files,omitempty"`
	PermissionMode *string          `json:"permission_mode,omitempty"`
}

type TurnStartImage struct {
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
	// Original asks the core to forward the image at original resolution
	// without resizing. Maps to Codex's ImageDetail::Original opt-out; only
	// honored when the target model supports it.
	Original bool `json:"original,omitempty"`
}

type TurnStartFile struct {
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
	Filename  string `json:"filename,omitempty"`
}

type TurnStartResult struct {
	Turn Turn `json:"turn"`
}

type TurnQueueParams struct {
	ThreadID       string           `json:"thread_id"`
	Prompt         string           `json:"prompt"`
	Images         []TurnStartImage `json:"images,omitempty"`
	Files          []TurnStartFile  `json:"files,omitempty"`
	ClientID       string           `json:"client_id,omitempty"`
	PermissionMode *string          `json:"permission_mode,omitempty"`
}

type QueuedTurn struct {
	ID         string `json:"id"`
	ThreadID   string `json:"thread_id"`
	Preview    string `json:"preview,omitempty"`
	ImageCount int    `json:"image_count,omitempty"`
	FileCount  int    `json:"file_count,omitempty"`
}

type TurnQueueResult struct {
	Queued QueuedTurn `json:"queued"`
}

type TurnUpdateQueuedParams struct {
	ThreadID string           `json:"thread_id"`
	QueueID  string           `json:"queue_id"`
	Prompt   string           `json:"prompt"`
	Images   []TurnStartImage `json:"images,omitempty"`
	Files    []TurnStartFile  `json:"files,omitempty"`
}

type TurnUpdateQueuedResult struct {
	OK     bool       `json:"ok"`
	Queued QueuedTurn `json:"queued,omitempty"`
}

type TurnDequeueParams struct {
	ThreadID string `json:"thread_id"`
	QueueID  string `json:"queue_id"`
}

type TurnSteerParams struct {
	ThreadID       string           `json:"thread_id"`
	Prompt         string           `json:"prompt"`
	Images         []TurnStartImage `json:"images,omitempty"`
	Files          []TurnStartFile  `json:"files,omitempty"`
	ExpectedTurnID string           `json:"expected_turn_id"`
	ClientID       string           `json:"client_id,omitempty"`
}

type TurnSteerResult struct {
	TurnID string `json:"turn_id"`
}

type TurnUnsteerParams struct {
	ThreadID string `json:"thread_id"`
	SteerID  string `json:"steer_id"`
}

type TurnInterruptParams struct {
	ThreadID string `json:"thread_id"`
}

type OKResult struct {
	OK bool `json:"ok"`
}

type ThreadStartedNotification struct {
	Thread Thread `json:"thread"`
}

type ThreadResumedNotification struct {
	Thread Thread `json:"thread"`
}

type ThreadUpdatedNotification struct {
	Thread Thread `json:"thread"`
}

// ThreadRegenerateTitleParams is the input for the `thread/regenerate-title`
// JSON-RPC method. The desktop uses this to manually re-run the title
// pipeline for an existing thread (e.g. after the user changes provider)
// and to inspect what the pipeline would produce.
type ThreadRegenerateTitleParams struct {
	ThreadID      string `json:"thread_id"`
	DryRun        bool   `json:"dry_run,omitempty"`
	ModelOverride string `json:"model_override,omitempty"`
	ProviderName  string `json:"provider,omitempty"`
}

// ThreadRegenerateTitleResult mirrors TitleGenerationResult and is what
// the desktop receives when it calls thread/regenerate-title. Persisted
// is the only field the desktop typically renders, but everything else
// is useful for surfacing in a dev panel.
type ThreadRegenerateTitleResult struct {
	TitleGenerationResult
}

type TurnStartedNotification struct {
	ThreadID string `json:"thread_id"`
	Turn     Turn   `json:"turn"`
	QueueID  string `json:"queue_id,omitempty"`
}

type TurnQueuedNotification struct {
	Queued QueuedTurn `json:"queued"`
}

type TurnDequeuedNotification struct {
	ThreadID string `json:"thread_id"`
	QueueID  string `json:"queue_id"`
}

type TurnEventNotification struct {
	ThreadID string             `json:"thread_id"`
	TurnID   string             `json:"turn_id"`
	Event    StreamEventPayload `json:"event"`
}

type TurnErrorNotification struct {
	ThreadID string `json:"thread_id"`
	TurnID   string `json:"turn_id"`
	Error    string `json:"error"`
	// Structured error fields surface the Go core's typed classification
	// (StreamError, HTTPError, ClassifyError) directly to the front-end so
	// the chip can show provider-specific codes and the renderer can drive
	// a structured next-step action. Empty fields fall back to the legacy
	// `error` string for older clients.
	Code       string           `json:"code,omitempty"`
	Category   string           `json:"category,omitempty"`
	Provider   string           `json:"provider,omitempty"`
	StatusCode int              `json:"status_code,omitempty"`
	Action     *TurnErrorAction `json:"action,omitempty"`
	Turn       Turn             `json:"turn"`
}

// TurnErrorAction is the structured next-step the front-end can render as
// a button beneath a turn-end notice. It mirrors opencode's Retryable.action
// shape (reason, provider, title, message, label, link) and is the
// authoritative source for the recommended action — the front-end does
// not re-derive it from the error message.
type TurnErrorAction struct {
	Reason  string `json:"reason"`
	Title   string `json:"title"`
	Message string `json:"message"`
	Label   string `json:"label"`
	Link    string `json:"link,omitempty"`
}

type TurnUsageNotification struct {
	ThreadID            string `json:"thread_id"`
	TurnID              string `json:"turn_id"`
	Model               string `json:"model,omitempty"`
	InputTokens         int    `json:"input_tokens"`
	OutputTokens        int    `json:"output_tokens"`
	ContextTokens       int    `json:"context_tokens,omitempty"`
	CacheCreationTokens int    `json:"cache_creation_tokens,omitempty"`
	CacheReadTokens     int    `json:"cache_read_tokens,omitempty"`
	// ContextWindowTokens is the resolved runtime context ceiling for the
	// active provider/model at the time of this usage snapshot. It may be the
	// model window or a lower provider input cap. The renderer uses it to show
	// "已用 / 总数" meters next to the token-speed gauge. Zero means no trusted
	// ceiling is known — the UI should hide the meter instead of computing a
	// misleading ratio against 0.
	ContextWindowTokens int `json:"context_window_tokens,omitempty"`
}

type TurnCompletedNotification struct {
	ThreadID            string `json:"thread_id"`
	Turn                Turn   `json:"turn"`
	Content             string `json:"content"`
	InputTokens         int    `json:"input_tokens"`
	OutputTokens        int    `json:"output_tokens"`
	ContextTokens       int    `json:"context_tokens,omitempty"`
	CacheCreationTokens int    `json:"cache_creation_tokens,omitempty"`
	CacheReadTokens     int    `json:"cache_read_tokens,omitempty"`
	FinishReason        string `json:"finish_reason,omitempty"`
	StopReason          string `json:"stop_reason,omitempty"`
	Truncated           bool   `json:"truncated,omitempty"`
	TracePath           string `json:"trace_path,omitempty"`
}

type Agent struct {
	ID                  string `json:"id"`
	Type                string `json:"type"`
	TaskName            string `json:"task_name,omitempty"`
	AgentProfile        string `json:"agent_profile,omitempty"`
	AgentPath           string `json:"agent_path,omitempty"`
	ParentID            string `json:"parent_id,omitempty"`
	Description         string `json:"description,omitempty"`
	Status              string `json:"status"`
	Result              string `json:"result,omitempty"`
	ResultPath          string `json:"result_path,omitempty"`
	ResultBytes         int    `json:"result_bytes,omitempty"`
	ResultTruncated     bool   `json:"result_truncated,omitempty"`
	Error               string `json:"error,omitempty"`
	InputTokens         int    `json:"input_tokens,omitempty"`
	OutputTokens        int    `json:"output_tokens,omitempty"`
	CacheCreationTokens int    `json:"cache_creation_tokens,omitempty"`
	CacheReadTokens     int    `json:"cache_read_tokens,omitempty"`
	NestedCount         int    `json:"nested_count,omitempty"`
	NestedRunningCount  int    `json:"nested_running_count,omitempty"`
	// Pinned and Archived mirror the underlying session metadata for the
	// sub-agent's own session so the renderer can offer pin/archive actions
	// in the session info panel without an extra round-trip. The child
	// session lives in the same store keyed by the agent ID, so this is
	// sourced from session.Find at list time.
	Pinned      bool      `json:"pinned,omitempty"`
	Archived    bool      `json:"archived,omitempty"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
}

type AgentUpdatedNotification struct {
	ThreadID string `json:"thread_id,omitempty"`
	Agent    Agent  `json:"agent"`
}

type AgentMailboxNotification struct {
	ThreadID string                           `json:"thread_id,omitempty"`
	Message  agentcontrol.AgentMailboxMessage `json:"message"`
}

type ThreadStatus string

const (
	ThreadStatusIdle       ThreadStatus = "idle"
	ThreadStatusInProgress ThreadStatus = "in_progress"
)

type TurnStatus string

const (
	TurnStatusInProgress  TurnStatus = "in_progress"
	TurnStatusCompleted   TurnStatus = "completed"
	TurnStatusFailed      TurnStatus = "failed"
	TurnStatusInterrupted TurnStatus = "interrupted"
)

type TurnItemsView string

const (
	TurnItemsViewFull TurnItemsView = "full"
)

type WorkspaceKind string

const (
	// WorkspaceKindProject threads belong to a registered project workspace
	// (i.e. cwd matches a DesktopProject path).
	WorkspaceKindProject WorkspaceKind = "project"
	// WorkspaceKindScratch threads live in the ephemeral scratch root
	// (typically ~/.wuu/scratch/<date>) and have no registered project.
	WorkspaceKindScratch WorkspaceKind = "scratch"
)

type Thread struct {
	ID               string        `json:"id"`
	ParentID         string        `json:"parent_id,omitempty"`
	AgentPath        string        `json:"agent_path,omitempty"`
	Preview          string        `json:"preview"`
	Title            string        `json:"title,omitempty"`
	ModelProvider    string        `json:"model_provider"`
	Model            string        `json:"model"`
	CWD              string        `json:"cwd"`
	WorkspaceKind    WorkspaceKind `json:"workspace_kind,omitempty"`
	Status           ThreadStatus  `json:"status"`
	ReadOnly         bool          `json:"read_only,omitempty"`
	Ephemeral        bool          `json:"ephemeral,omitempty"`
	Pinned           bool          `json:"pinned,omitempty"`
	Archived         bool          `json:"archived,omitempty"`
	ForkedFromID     string        `json:"forked_from_id,omitempty"`
	ForkedFromTurnID string        `json:"forked_from_turn_id,omitempty"`
	ForkedFromItemID string        `json:"forked_from_item_id,omitempty"`
	Worktree         *WorktreeInfo `json:"worktree,omitempty"`
	CreatedAt        time.Time     `json:"created_at"`
	UpdatedAt        time.Time     `json:"updated_at"`
	Turns            []Turn        `json:"turns"`
	ChildAgents      []Agent       `json:"child_agents,omitempty"`
	// ListeningPorts is the latest deduped, sorted list of localhost
	// ports the agent asked the desktop to surface (via
	// report_listening_ports). The desktop uses the first entry to
	// auto-open the in-app browser when this thread becomes active,
	// and renders the full list as clickable chips in the sidebar.
	ListeningPorts []int               `json:"listening_ports,omitempty"`
	BrowserState   *ThreadBrowserState `json:"browser_state,omitempty"`
}

type WorktreeInfo struct {
	Path         string   `json:"path"`
	BaseHEAD     string   `json:"base_head,omitempty"`
	BaseRepo     string   `json:"base_repo,omitempty"`
	Dirty        bool     `json:"dirty,omitempty"`
	ChangedFiles []string `json:"changed_files,omitempty"`
}

type ThreadBrowserState struct {
	CurrentURL        string `json:"current_url,omitempty"`
	PrimaryPreviewURL string `json:"primary_preview_url,omitempty"`
	LinkedProcessID   string `json:"linked_process_id,omitempty"`
}

type Turn struct {
	ID                  string        `json:"id"`
	Items               []ThreadItem  `json:"items"`
	ItemsView           TurnItemsView `json:"items_view"`
	Status              TurnStatus    `json:"status"`
	Error               *TurnError    `json:"error,omitempty"`
	FinishReason        string        `json:"finish_reason,omitempty"`
	StopReason          string        `json:"stop_reason,omitempty"`
	Truncated           bool          `json:"truncated,omitempty"`
	StartedAt           *time.Time    `json:"started_at,omitempty"`
	CompletedAt         *time.Time    `json:"completed_at,omitempty"`
	DurationMS          *int64        `json:"duration_ms,omitempty"`
	InputTokens         int           `json:"input_tokens,omitempty"`
	OutputTokens        int           `json:"output_tokens,omitempty"`
	ContextTokens       int           `json:"context_tokens,omitempty"`
	CacheCreationTokens int           `json:"cache_creation_tokens,omitempty"`
	CacheReadTokens     int           `json:"cache_read_tokens,omitempty"`
	UsageModel          string        `json:"usage_model,omitempty"`
}

type TurnError struct {
	Message string `json:"message"`
	// Structured error fields, filled in by BuildTurnError from the typed
	// error (HTTPError, StreamError) and the agentcontrol.ClassifyError
	// classifier. The front-end prefers these over the raw `message` for
	// the visible chip text; the message still rides along for the
	// hover tooltip and the "copy debug info" payload.
	Code       string           `json:"code,omitempty"`
	Category   string           `json:"category,omitempty"`
	Provider   string           `json:"provider,omitempty"`
	StatusCode int              `json:"status_code,omitempty"`
	Action     *TurnErrorAction `json:"action,omitempty"`
}

type ThreadItemType string

const (
	ThreadItemUserMessage       ThreadItemType = "user_message"
	ThreadItemAgentMessage      ThreadItemType = "agent_message"
	ThreadItemReasoning         ThreadItemType = "reasoning"
	ThreadItemToolCall          ThreadItemType = "tool_call"
	ThreadItemCollabAgentTool   ThreadItemType = "collab_agent_tool_call"
	ThreadItemContextCompaction ThreadItemType = "context_compaction"
	ThreadItemError             ThreadItemType = "error"
)

type ThreadItemStatus string

const (
	ThreadItemStatusInProgress ThreadItemStatus = "in_progress"
	ThreadItemStatusCompleted  ThreadItemStatus = "completed"
	ThreadItemStatusFailed     ThreadItemStatus = "failed"
)

type ThreadItemPhase string

const (
	// Empty phase means unknown while text is streaming. This mirrors Codex's
	// nullable message phase: only committed messages are classified.
	ThreadItemPhaseCommentary  ThreadItemPhase = "commentary"
	ThreadItemPhaseFinalAnswer ThreadItemPhase = "final_answer"
)

type ThreadItem struct {
	ID           string                     `json:"id"`
	SourceID     string                     `json:"source_id,omitempty"`
	Type         ThreadItemType             `json:"type"`
	Status       ThreadItemStatus           `json:"status,omitempty"`
	Phase        ThreadItemPhase            `json:"phase,omitempty"`
	Role         string                     `json:"role,omitempty"`
	Text         string                     `json:"text,omitempty"`
	Images       []ThreadItemImage          `json:"images,omitempty"`
	Files        []ThreadItemFile           `json:"files,omitempty"`
	Name         string                     `json:"name,omitempty"`
	Arguments    string                     `json:"arguments,omitempty"`
	Display      *providers.ToolCallDisplay `json:"display,omitempty"`
	Result       string                     `json:"result,omitempty"`
	Error        string                     `json:"error,omitempty"`
	Reason       string                     `json:"reason,omitempty"`
	FinishReason string                     `json:"finish_reason,omitempty"`
	StopReason   string                     `json:"stop_reason,omitempty"`
	Truncated    bool                       `json:"truncated,omitempty"`
}

type ThreadItemImage struct {
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type ThreadItemFile struct {
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
	Filename  string `json:"filename,omitempty"`
}

type ItemStartedNotification struct {
	ThreadID    string     `json:"thread_id"`
	TurnID      string     `json:"turn_id"`
	Item        ThreadItem `json:"item"`
	StartedAtMS int64      `json:"started_at_ms"`
}

type ItemCompletedNotification struct {
	ThreadID      string     `json:"thread_id"`
	TurnID        string     `json:"turn_id"`
	Item          ThreadItem `json:"item"`
	CompletedAtMS int64      `json:"completed_at_ms"`
}

type AgentMessageDeltaNotification struct {
	ThreadID string `json:"thread_id"`
	TurnID   string `json:"turn_id"`
	ItemID   string `json:"item_id"`
	Delta    string `json:"delta"`
}

type AgentMessageReplaceNotification struct {
	ThreadID string `json:"thread_id"`
	TurnID   string `json:"turn_id"`
	ItemID   string `json:"item_id"`
	Text     string `json:"text"`
}

type ReasoningDeltaNotification struct {
	ThreadID string `json:"thread_id"`
	TurnID   string `json:"turn_id"`
	ItemID   string `json:"item_id"`
	Delta    string `json:"delta"`
}

type ReasoningReplaceNotification struct {
	ThreadID string `json:"thread_id"`
	TurnID   string `json:"turn_id"`
	ItemID   string `json:"item_id"`
	Text     string `json:"text"`
}

type ToolCallDeltaNotification struct {
	ThreadID string `json:"thread_id"`
	TurnID   string `json:"turn_id"`
	ItemID   string `json:"item_id"`
	Delta    string `json:"delta"`
}

type ToolCallOutputNotification struct {
	ThreadID string `json:"thread_id"`
	TurnID   string `json:"turn_id"`
	ItemID   string `json:"item_id"`
	Delta    string `json:"delta"`
}

type StreamEventPayload struct {
	Type           providers.StreamEventType        `json:"type"`
	Content        string                           `json:"content,omitempty"`
	Message        *providers.ChatMessage           `json:"message,omitempty"`
	ToolCall       *providers.ToolCall              `json:"tool_call,omitempty"`
	ToolResult     string                           `json:"tool_result,omitempty"`
	PlanUpdate     *providers.PlanUpdate            `json:"plan_update,omitempty"`
	Lifecycle      *StreamLifecyclePayload          `json:"lifecycle,omitempty"`
	RequestContext *providers.RequestContextSummary `json:"request_context,omitempty"`
	ProviderState  *providers.ProviderStateSummary  `json:"provider_state,omitempty"`
	Usage          *providers.TokenUsage            `json:"usage,omitempty"`
	StopReason     string                           `json:"stop_reason,omitempty"`
	FinishReason   string                           `json:"finish_reason,omitempty"`
	Truncated      bool                             `json:"truncated,omitempty"`
	Error          string                           `json:"error,omitempty"`
}

type StreamLifecyclePayload struct {
	Phase       string `json:"phase"`
	Attempt     int    `json:"attempt,omitempty"`
	MaxAttempts int    `json:"max_attempts,omitempty"`
	RetryCount  int    `json:"retry_count,omitempty"`
	MaxRetries  int    `json:"max_retries,omitempty"`
	RetryInMS   int64  `json:"retry_in_ms,omitempty"`
	ElapsedMS   int64  `json:"elapsed_ms,omitempty"`
	BudgetMS    int64  `json:"budget_ms,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

// SettingsUsageRange selects which time window the settings/usage RPC
// covers. Empty string is treated as "all" by the appserver.
type SettingsUsageRange string

const (
	SettingsUsageRangeAll SettingsUsageRange = "all"
	SettingsUsageRange7d  SettingsUsageRange = "7d"
	SettingsUsageRange30d SettingsUsageRange = "30d"
	SettingsUsageRange90d SettingsUsageRange = "90d"
)

// SettingsUsageQuery is the input for the settings/usage RPC. Range
// selects the time window; empty defaults to "all".
type SettingsUsageQuery struct {
	Range SettingsUsageRange `json:"range,omitempty"`
}

// SettingsUsageMetrics is the headline number block shown at the top of the
// desktop usage page. Totals are weighted by token count across every
// token_usage row whose timestamp falls inside the requested range. Turns
// counts primary-session conversations, Agents counts subagent runs. Both
// turn and agent buckets share a per-row key so a single model row can
// surface either kind without losing fidelity.
type SettingsUsageMetrics struct {
	PromptTokens        int       `json:"prompt_tokens"`
	ContextTokens       int       `json:"context_tokens"`
	InputTokens         int       `json:"input_tokens"`
	OutputTokens        int       `json:"output_tokens"`
	CacheReadTokens     int       `json:"cache_read_tokens"`
	CacheCreationTokens int       `json:"cache_creation_tokens"`
	CacheHitRate        float64   `json:"cache_hit_rate"`
	Turns               int       `json:"turns"`
	Agents              int       `json:"agents"`
	DateRange           [2]string `json:"date_range"`
	ActiveDays          int       `json:"active_days"`
}

// SettingsUsageDay is one calendar day of token activity, bucketed by the
// token_usage row's At timestamp (UTC). Days are emitted in ascending
// date order; gaps in the visible window are filled in by the desktop.
type SettingsUsageDay struct {
	Date                string  `json:"date"`
	InputTokens         int     `json:"input_tokens"`
	OutputTokens        int     `json:"output_tokens"`
	CacheCreationTokens int     `json:"cache_creation_tokens"`
	CacheReadTokens     int     `json:"cache_read_tokens"`
	CacheHitRate        float64 `json:"cache_hit_rate"`
	Turns               int     `json:"turns"`
	Agents              int     `json:"agents"`
}

// SettingsUsageEntry is one recent token-spending record surfaced in the
// "最近记录" list. Source identifies whether the row came from a primary
// session turn or a subagent run; Title is rendered as the entry headline.
type SettingsUsageEntry struct {
	ID                  string `json:"id"`
	Source              string `json:"source"` // "turn" | "agent"
	Title               string `json:"title"`
	Provider            string `json:"provider"`
	Model               string `json:"model"`
	At                  string `json:"at"`
	InputTokens         int    `json:"input_tokens"`
	OutputTokens        int    `json:"output_tokens"`
	CacheCreationTokens int    `json:"cache_creation_tokens"`
	CacheReadTokens     int    `json:"cache_read_tokens"`
}

// SettingsUsageResponse is the single source of truth for the desktop
// usage page. Range mirrors the requested window. Metrics is the headline
// number block, ModelBreakdowns is the per-model table sorted by total
// context tokens descending (legacy rows with empty provider+model are
// surfaced as "(unknown)"), Days is the calendar-day series for the
// heatmap (gaps filled by the desktop), and Entries is the most recent
// N token_usage rows within the range for the "最近记录" list.
type SettingsUsageResponse struct {
	Range           SettingsUsageRange   `json:"range"`
	TotalSessions   int                  `json:"total_sessions"`
	GeneratedAt     string               `json:"generated_at"`
	Metrics         SettingsUsageMetrics `json:"metrics"`
	ModelBreakdowns []insight.ModelUsage `json:"model_breakdowns"`
	Days            []SettingsUsageDay   `json:"days"`
	Entries         []SettingsUsageEntry `json:"entries"`
}
