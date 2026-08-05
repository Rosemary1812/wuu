package appserver

import (
	"encoding/json"
	"time"

	"github.com/blueberrycongee/wuu/internal/activity"
	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/automation"
	"github.com/blueberrycongee/wuu/internal/capability"
	"github.com/blueberrycongee/wuu/internal/channels"
	"github.com/blueberrycongee/wuu/internal/execution"
	"github.com/blueberrycongee/wuu/internal/extensions"
	"github.com/blueberrycongee/wuu/internal/insight"
	"github.com/blueberrycongee/wuu/internal/modelroles"
	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/toolresult"
)

const (
	ProtocolVersion = "wuu-app-server/v0.1"

	MethodInitialize              = "initialize"
	MethodConfigRead              = "config/read"
	MethodConfigModelUpdate       = "config/model/update"
	MethodConfigAdvancedUpdate    = "config/advanced/update"
	MethodConfigGeneralUpdate     = "config/general/update"
	MethodExtensionCatalogRefresh = "extension/catalog/refresh"
	MethodExtensionPackageUpdate  = "extension/package/update"
	MethodConfigCodexModels       = "config/codex/models"
	MethodConfigCatalogRefresh    = "config/model-catalog/refresh"
	MethodConfigProviderRemove    = "config/provider/remove"
	MethodSkillList               = "skill/list"
	MethodChannelBootstrap        = "channel/bootstrap"
	MethodChannelAgentList        = "channel/agent/list"
	MethodChannelAgentInsights    = "channel/agent/insights"
	MethodChannelAgentCreate      = "channel/agent/create"
	MethodChannelAgentUpdate      = "channel/agent/update"
	MethodChannelAgentDelete      = "channel/agent/delete"
	MethodChannelAgentStart       = "channel/agent/start"
	MethodChannelAgentReset       = "channel/agent/reset"
	MethodChannelRoomList         = "channel/room/list"
	MethodChannelRoomCreate       = "channel/room/create"
	MethodChannelRoomUpdate       = "channel/room/update"
	MethodChannelRoomDelete       = "channel/room/delete"
	MethodChannelRoomRead         = "channel/room/read"
	MethodChannelMessageList      = "channel/message/list"
	MethodChannelMessageSend      = "channel/message/send"
	MethodChannelTaskCreate       = "channel/task/create"
	MethodChannelTaskUpdate       = "channel/task/update"
	MethodChannelMentionStatus    = "channel/human-mention/status"
	MethodChannelMentionAck       = "channel/human-mention/ack"
	MethodAutomationList          = "automation/list"
	MethodAutomationRuns          = "automation/run/list"
	MethodAutomationCreate        = "automation/create"
	MethodAutomationUpdate        = "automation/update"
	MethodAutomationRemove        = "automation/remove"
	// MethodGoalActiveSummary returns the lightweight composer-banner view
	// of the most-recently-updated non-terminal goal in the requested thread
	// scope. The mutation methods are user-owned controls for the active
	// runtime Goal; full workflow/agent detail stays on the agent tool loop.
	MethodGoalActiveSummary        = "goal/active-summary"
	MethodGoalPause                = "goal/pause"
	MethodGoalResume               = "goal/resume"
	MethodGoalClear                = "goal/clear"
	MethodGoalUpdateText           = "goal/update-text"
	MethodThreadStart              = "thread/start"
	MethodThreadResume             = "thread/resume"
	MethodThreadFork               = "thread/fork"
	MethodThreadEditMessage        = "thread/edit-message"
	MethodThreadContextComposition = "thread/context-composition"
	MethodSideThreadOpen           = "sideThread/open"
	MethodSideThreadGetHistory     = "sideThread/getHistory"
	MethodSideThreadSend           = "sideThread/sendMessage"
	MethodSideThreadInterrupt      = "sideThread/interrupt"
	MethodSideThreadReset          = "sideThread/reset"
	MethodThreadList               = "thread/list"
	MethodThreadListArchived       = "thread/listArchived"
	MethodThreadSearch             = "thread/search"
	MethodThreadPreview            = "thread/preview"
	MethodThreadPin                = "thread/pin"
	MethodThreadArchive            = "thread/archive"
	MethodThreadCompactStart       = "thread/compact/start"
	MethodThreadRegenerateTitle    = "thread/regenerate-title"
	MethodThreadRename             = "thread/rename"
	MethodThreadDelete             = "thread/delete"
	MethodWorkspaceStateCleanup    = "workspace/state/cleanup"
	// Memory panel RPCs (设置 → 记忆). Wire contract fixed ahead of
	// implementation by docs/plans/2026-07-04-memory-redesign.md §8.2 and
	// mirrored field-for-field by desktop/src/shared/protocol.ts.
	MethodMemoryOverview   = "memory/overview"
	MethodMemoryChat       = "memory/chat"
	MethodMemoryRead       = "memory/read"
	MethodTextPolish       = "text/polish"
	MethodGitCommitMessage = "git/commit-message"
	MethodTurnStart        = "turn/start"
	MethodTurnQueue        = "turn/queue"
	MethodTurnUpdateQueued = "turn/update-queued"
	MethodTurnDequeue      = "turn/dequeue"
	MethodTurnSteer        = "turn/steer"
	MethodTurnUnsteer      = "turn/unsteer"
	MethodTurnInterrupt    = "turn/interrupt"
	MethodRunStart         = "run/start"
	MethodRunInterrupt     = "run/interrupt"
	MethodProcessList      = "process/list"
	MethodProcessRead      = "process/read"
	MethodProcessWrite     = "process/write"
	MethodProcessResize    = "process/resize"
	MethodProcessStop      = "process/stop"
	MethodMCPList          = "mcp/list"
	MethodMCPConnect       = "mcp/connect"
	MethodMCPDisconnect    = "mcp/disconnect"
	MethodMCPRefresh       = "mcp/refresh"
	MethodMCPAuthStart     = "mcp/auth/start"
	MethodMCPAuthStatus    = "mcp/auth/status"
	MethodMCPAuthFinish    = "mcp/auth/finish"
	MethodMCPAuthRemove    = "mcp/auth/remove"
	MethodActivityList     = "activity/list"
	MethodActivityTakeover = "activity/takeover"
	MethodActivityRelease  = "activity/release"
	MethodActivityStop     = "activity/stop"
	MethodShutdown         = "shutdown"
	// MethodSettingsUsage returns the aggregated per-provider/model token
	// usage snapshot for the desktop settings page. Range filter selects
	// the time window ("all", "7d", "30d", "90d"); empty defaults to "all".
	MethodSettingsUsage        = "settings/usage"
	MethodDevicePushRegister   = "device/push_register"
	MethodDevicePushUnregister = "device/push_unregister"

	// browser/* are server-initiated requests: the core sends them TO the
	// desktop client and awaits a Response over the reverse-RPC channel
	// (Server.callClient). They are deliberately NOT registered in
	// handleLine's method switch — that switch dispatches client→core
	// requests, whereas these travel core→desktop. The desktop host owns the
	// hidden WebContentsView backing each tab and answers these on its main
	// process. tab_id is minted core-side (browserBridge.randomID) so parallel
	// cores never collide on a tab identifier.
	MethodBrowserCDP           = "browser/cdp"
	MethodBrowserScreenshot    = "browser/screenshot"
	MethodBrowserOpenTab       = "browser/open_tab"
	MethodBrowserCloseTab      = "browser/close_tab"
	MethodBrowserSetVisibility = "browser/set_visibility"
	MethodBrowserListTabs      = "browser/list_tabs"

	NotificationThreadStarted          = "thread/started"
	NotificationThreadResumed          = "thread/resumed"
	NotificationThreadUpdated          = "thread/updated"
	NotificationTurnStarted            = "turn/started"
	NotificationTurnQueued             = "turn/queued"
	NotificationTurnDequeued           = "turn/dequeued"
	NotificationTurnHeld               = "turn/held"
	NotificationTurnEvent              = "turn/event"
	NotificationTurnError              = "turn/error"
	NotificationTurnCompleted          = "turn/completed"
	NotificationRunStarted             = "run/started"
	NotificationRunUpdated             = "run/updated"
	NotificationSideThreadEvent        = "sideThread/event"
	NotificationActivityStarted        = "activity/started"
	NotificationActivityUpdated        = "activity/updated"
	NotificationActivityControlChanged = "activity/control_changed"
	NotificationActivityStopped        = "activity/stopped"
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

// RuntimeHostSummary identifies the process behind this app-server connection.
// Product identities such as organization, seat, and agent definition remain
// control-plane concerns and are intentionally not part of this core contract.
type RuntimeHostSummary struct {
	Kind       string `json:"kind"`
	InstanceID string `json:"instance_id,omitempty"`
}

// InitializeParams describes the client attached to the UI-neutral core.
// Capabilities are opt-in: an omitted capability must never cause the core to
// send a request that the client cannot handle.
type InitializeParams struct {
	ProtocolVersion string             `json:"protocol_version,omitempty"`
	Client          ClientInfo         `json:"client,omitempty"`
	Capabilities    ClientCapabilities `json:"capabilities,omitempty"`
}

type ClientInfo struct {
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
}

type ClientCapabilities struct {
	ReverseRPC ReverseRPCCapabilities `json:"reverse_rpc,omitempty"`
}

type ReverseRPCCapabilities struct {
	Methods []string `json:"methods,omitempty"`
}

type InitializeResult struct {
	Status             string                       `json:"status"`
	Issues             []RuntimeIssue               `json:"issues,omitempty"`
	ProtocolVersion    string                       `json:"protocol_version"`
	Core               CoreBuildInfo                `json:"core"`
	Provider           string                       `json:"provider"`
	Model              string                       `json:"model"`
	Effort             string                       `json:"effort,omitempty"`
	Variant            string                       `json:"variant,omitempty"`
	Ultra              bool                         `json:"ultra"`
	MaxParallel        int                          `json:"max_parallel"`
	RuntimeHost        RuntimeHostSummary           `json:"runtime_host"`
	WorkspaceRoot      string                       `json:"workspace_root"`
	Permissions        PermissionSummary            `json:"permissions"`
	ExtensionTrust     ExtensionTrustSummary        `json:"extension_trust"`
	ExtensionInventory []ExtensionInventoryRecord   `json:"extension_inventory,omitempty"`
	ModelProfile       *ModelProfileSummary         `json:"model_profile,omitempty"`
	ToolSurface        *ToolSurfaceSummary          `json:"tool_surface,omitempty"`
	ModelRoles         []ModelRoleSummary           `json:"model_roles,omitempty"`
	ModelAliases       map[string]ModelAliasSummary `json:"model_aliases,omitempty"`
	Providers          []ProviderSummary            `json:"providers,omitempty"`
	AdvancedSettings   AdvancedSettingsSummary      `json:"advanced_settings"`
	GeneralSettings    GeneralSettingsSummary       `json:"general_settings"`
	Features           FeatureFlags                 `json:"features"`
}

type FeatureFlags struct {
	HelpMe bool `json:"helpme"`
	// Browser advertises that this client can host the embedded browser
	// backend (hidden WebContentsView + CDP bridge). Mirrored by
	// desktop/src/shared/protocol.ts. Filled by config_handlers.handleInitialize.
	Browser bool `json:"browser"`
}

// clientResponse is the inbound envelope for a Response the desktop client
// writes back to a server-initiated request. It exists separately from Response
// only because Response.Result is `any` (an outbound-encode shape) while the
// reverse-RPC delivery path must re-decode Result as raw JSON without losing it.
// handleLine second-unmarshals a Method=="" line into this before routing it to
// the waiting caller. Mirrored by desktop/src/shared/protocol.ts.
type clientResponse struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *ResponseError  `json:"error,omitempty"`
}

// Browser* are the wire payloads for the core→desktop browser/* requests.
// All field names are snake_case and mirrored field-for-field by
// packages/protocol/src/index.ts. None carry an activity_id: the desktop client
// auto-rejects server requests whose activity_id names a stopped activity
// (activityServerRequestRejection), which would wedge a CDP call the moment a
// tab's activity is torn down. Tab addressing is by tab_id alone.
type BrowserCDPParams struct {
	Workdir string          `json:"workdir"`
	TabID   string          `json:"tab_id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type BrowserCDPResult struct {
	// Result is the raw CDP command result when small enough to inline. When
	// the desktop size gate spills the payload to disk, Result is empty and
	// Path/Size describe the on-disk artifact instead.
	Result json.RawMessage `json:"result,omitempty"`
	Path   string          `json:"path,omitempty"`
	Size   int             `json:"size,omitempty"`
}

type BrowserScreenshotParams struct {
	Workdir  string `json:"workdir"`
	TabID    string `json:"tab_id"`
	DestPath string `json:"dest_path"`
	Format   string `json:"format,omitempty"`
}

type BrowserScreenshotResult struct {
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Path   string `json:"path"`
}

type BrowserOpenTabParams struct {
	Workdir    string `json:"workdir"`
	TabID      string `json:"tab_id"`
	InitialURL string `json:"initial_url,omitempty"`
}

type BrowserCloseTabParams struct {
	Workdir string `json:"workdir"`
	TabID   string `json:"tab_id"`
}

type BrowserSetVisibilityParams struct {
	Workdir string `json:"workdir"`
	TabID   string `json:"tab_id"`
	Visible bool   `json:"visible"`
}

type BrowserListTabsParams struct {
	Workdir string `json:"workdir"`
}

type BrowserListTabsResult struct {
	TabIDs []string `json:"tab_ids"`
}

type RuntimeIssue struct {
	Code     string `json:"code"`
	Provider string `json:"provider,omitempty"`
	Message  string `json:"message"`
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
	Provider           string                       `json:"provider"`
	Model              string                       `json:"model"`
	Effort             string                       `json:"effort,omitempty"`
	Variant            string                       `json:"variant,omitempty"`
	Ultra              bool                         `json:"ultra"`
	MaxParallel        int                          `json:"max_parallel"`
	ConfigPath         string                       `json:"config_path"`
	WorkspaceRoot      string                       `json:"workspace_root"`
	SessionDir         string                       `json:"session_dir"`
	Permissions        PermissionSummary            `json:"permissions"`
	ExtensionTrust     ExtensionTrustSummary        `json:"extension_trust"`
	ExtensionInventory []ExtensionInventoryRecord   `json:"extension_inventory,omitempty"`
	ModelProfile       *ModelProfileSummary         `json:"model_profile,omitempty"`
	ToolSurface        *ToolSurfaceSummary          `json:"tool_surface,omitempty"`
	ModelRoles         []ModelRoleSummary           `json:"model_roles,omitempty"`
	ModelAliases       map[string]ModelAliasSummary `json:"model_aliases,omitempty"`
	Providers          []ProviderSummary            `json:"providers,omitempty"`
	AdvancedSettings   AdvancedSettingsSummary      `json:"advanced_settings"`
	GeneralSettings    GeneralSettingsSummary       `json:"general_settings"`
}

type ConfigModelCatalogRefreshResult struct {
	ProviderCount int               `json:"provider_count"`
	ModelCount    int               `json:"model_count"`
	Providers     []ProviderSummary `json:"providers"`
}

type PermissionSummary struct {
	Mode string `json:"mode,omitempty"`
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

type MCPAuthStartResult struct {
	AuthorizationURL string   `json:"authorization_url"`
	State            string   `json:"state"`
	Scopes           []string `json:"scopes,omitempty"`
}

type MCPAuthStatusResult struct {
	Name          string   `json:"name"`
	Authenticated bool     `json:"authenticated"`
	ExpiresAt     string   `json:"expires_at,omitempty"`
	Scopes        []string `json:"scopes,omitempty"`
}

type MCPAuthFinishParams struct {
	Name  string `json:"name"`
	State string `json:"state"`
	Code  string `json:"code"`
}

type MCPAuthFinishResult struct {
	Auth   MCPAuthStatusResult `json:"auth"`
	Server MCPServerStatus     `json:"server"`
}

type MCPAuthRemoveResult struct {
	Auth   MCPAuthStatusResult `json:"auth"`
	Server MCPServerStatus     `json:"server"`
}

type ActivitySession struct {
	ID          string                `json:"id"`
	Kind        string                `json:"kind"`
	ThreadID    string                `json:"thread_id"`
	Workdir     string                `json:"workdir"`
	PluginID    string                `json:"plugin_id,omitempty"`
	Target      string                `json:"target,omitempty"`
	ProcessID   int                   `json:"process_id,omitempty"`
	WindowID    uint32                `json:"window_id,omitempty"`
	State       string                `json:"state"`
	Controller  string                `json:"controller"`
	Preview     string                `json:"preview,omitempty"`
	Error       string                `json:"error,omitempty"`
	Interaction *activity.Interaction `json:"interaction,omitempty"`
	CreatedAt   time.Time             `json:"created_at"`
	UpdatedAt   time.Time             `json:"updated_at"`
}

type ActivityListParams struct {
	ThreadID string `json:"thread_id"`
}

type ActivityListResult struct {
	Activities []ActivitySession `json:"activities"`
}

type ActivityActionParams struct {
	ThreadID   string `json:"thread_id"`
	ActivityID string `json:"activity_id"`
}

type ActivityActionResult struct {
	Activity ActivitySession `json:"activity"`
}

type ActivityReleaseResult struct {
	Activity   ActivitySession `json:"activity"`
	LeaseToken string          `json:"lease_token"`
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

type ExtensionState string

const (
	ExtensionStateActive   ExtensionState = "active"
	ExtensionStateReadOnly ExtensionState = "read_only"
	ExtensionStatePending  ExtensionState = "pending"
	ExtensionStateGranted  ExtensionState = "granted"
	ExtensionStateRejected ExtensionState = "rejected"
	ExtensionStateChanged  ExtensionState = "changed"
)

type ExtensionApprovalState string

const (
	ExtensionApprovalOfficial ExtensionApprovalState = "official"
	ExtensionApprovalPending  ExtensionApprovalState = "pending"
	ExtensionApprovalGranted  ExtensionApprovalState = "granted"
	ExtensionApprovalChanged  ExtensionApprovalState = "changed"
	ExtensionApprovalRejected ExtensionApprovalState = "rejected"
)

type ExtensionRuntimeState string

const (
	ExtensionRuntimeInactive ExtensionRuntimeState = "inactive"
	ExtensionRuntimeStarting ExtensionRuntimeState = "starting"
	ExtensionRuntimeActive   ExtensionRuntimeState = "active"
	ExtensionRuntimeFailed   ExtensionRuntimeState = "failed"
	ExtensionRuntimeStopping ExtensionRuntimeState = "stopping"
	ExtensionRuntimeStopped  ExtensionRuntimeState = "stopped"
)

type ExtensionCommandKind string

const (
	ExtensionCommandPromptTemplate ExtensionCommandKind = "prompt_template"
	ExtensionCommandRuntimeAction  ExtensionCommandKind = "runtime_action"
)

type ExtensionCommandDescriptor struct {
	ID          string               `json:"id"`
	Title       string               `json:"title"`
	Description string               `json:"description,omitempty"`
	Kind        ExtensionCommandKind `json:"kind"`
	Template    string               `json:"template,omitempty"`
	Contexts    []string             `json:"contexts,omitempty"`
	Aliases     []string             `json:"aliases,omitempty"`
	Keywords    []string             `json:"keywords,omitempty"`
}

type ExtensionContributions struct {
	Commands []ExtensionCommandDescriptor `json:"commands,omitempty"`
}

type ExtensionInventoryRecord struct {
	ID                   string                  `json:"id"`
	Name                 string                  `json:"name"`
	Description          string                  `json:"description,omitempty"`
	Kind                 extensions.Kind         `json:"kind"`
	Provenance           extensions.Provenance   `json:"provenance"`
	State                ExtensionState          `json:"state"`
	Executable           bool                    `json:"executable,omitempty"`
	Fingerprint          string                  `json:"fingerprint,omitempty"`
	GrantScope           extensions.GrantScope   `json:"grant_scope,omitempty"`
	RequestedPermissions []string                `json:"requested_permissions,omitempty"`
	UnsupportedFields    []string                `json:"unsupported_fields,omitempty"`
	ParentID             string                  `json:"parent_id,omitempty"`
	ApprovalState        ExtensionApprovalState  `json:"approval_state,omitempty"`
	RuntimeState         ExtensionRuntimeState   `json:"runtime_state,omitempty"`
	Enabled              *bool                   `json:"enabled,omitempty"`
	Contributions        *ExtensionContributions `json:"contributions,omitempty"`
}

type ExtensionPackageAction string

const (
	ExtensionPackageGrant   ExtensionPackageAction = "grant"
	ExtensionPackageReject  ExtensionPackageAction = "reject"
	ExtensionPackageRevoke  ExtensionPackageAction = "revoke"
	ExtensionPackageEnable  ExtensionPackageAction = "enable"
	ExtensionPackageDisable ExtensionPackageAction = "disable"
)

type ExtensionPackageUpdateParams struct {
	ID          string                 `json:"id"`
	Fingerprint string                 `json:"fingerprint,omitempty"`
	Action      ExtensionPackageAction `json:"action"`
}

type ExtensionPackageUpdateResult struct {
	ExtensionInventory []ExtensionInventoryRecord `json:"extension_inventory"`
}

type ExtensionCatalogRefreshResult struct {
	ExtensionInventory []ExtensionInventoryRecord `json:"extension_inventory"`
	Skills             []SkillSummary             `json:"skills"`
}

type ConfigModelUpdateParams struct {
	ThreadID       string  `json:"thread_id,omitempty"`
	Provider       string  `json:"provider,omitempty"`
	Model          string  `json:"model"`
	Effort         *string `json:"effort,omitempty"`
	Variant        *string `json:"variant,omitempty"`
	Ultra          *bool   `json:"ultra,omitempty"`
	PermissionMode *string `json:"permission_mode,omitempty"`
	BaseURL        *string `json:"base_url,omitempty"`
	APIKey         *string `json:"api_key,omitempty"`
	AuthToken      *string `json:"auth_token,omitempty"`
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
	Ultra            bool                    `json:"ultra"`
	MaxParallel      int                     `json:"max_parallel"`
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
// merge provider/model/permissions into the initialized state, so the shape
// intentionally matches that result.
type ConfigProviderRemoveResult struct {
	Provider         string                  `json:"provider"`
	Model            string                  `json:"model"`
	Variant          string                  `json:"variant,omitempty"`
	Permissions      PermissionSummary       `json:"permissions"`
	ExtensionTrust   ExtensionTrustSummary   `json:"extension_trust"`
	ModelProfile     *ModelProfileSummary    `json:"model_profile,omitempty"`
	ToolSurface      *ToolSurfaceSummary     `json:"tool_surface,omitempty"`
	ModelRoles       []ModelRoleSummary      `json:"model_roles,omitempty"`
	Providers        []ProviderSummary       `json:"providers,omitempty"`
	AdvancedSettings AdvancedSettingsSummary `json:"advanced_settings"`
}

type ConfigAdvancedUpdateParams struct {
	MaxSteps                *int                          `json:"max_steps,omitempty"`
	MaxContextTokens        *int                          `json:"max_context_tokens,omitempty"`
	Temperature             *float64                      `json:"temperature,omitempty"`
	CompactThresholdPct     *float64                      `json:"compact_threshold_pct,omitempty"`
	CompactKeepRecentTokens *int                          `json:"compact_keep_recent_tokens,omitempty"`
	DisableAutoCompact      *bool                         `json:"disable_auto_compact,omitempty"`
	ProviderContextWindow   *int                          `json:"provider_context_window,omitempty"`
	ModelAliases            *map[string]ModelAliasSummary `json:"model_aliases,omitempty"`
}

type ConfigAdvancedUpdateResult struct {
	AdvancedSettings AdvancedSettingsSummary      `json:"advanced_settings"`
	ModelAliases     map[string]ModelAliasSummary `json:"model_aliases,omitempty"`
	Providers        []ProviderSummary            `json:"providers,omitempty"`
}

type ConfigGeneralUpdateParams struct {
	AppendSystemPrompt    *string          `json:"append_system_prompt,omitempty"`
	GitAttributionEnabled *bool            `json:"git_attribution_enabled,omitempty"`
	MemoryDisable         *bool            `json:"memory_disable,omitempty"`
	MCPEnabledToggles     map[string]*bool `json:"mcp_enabled_toggles,omitempty"`
	DreamEnabled          *bool            `json:"dream_enabled,omitempty"`
	DreamIntervalDays     *int             `json:"dream_interval_days,omitempty"`
	DreamProvider         *string          `json:"dream_provider,omitempty"`
	DreamModel            *string          `json:"dream_model,omitempty"`
}

type ConfigGeneralUpdateResult struct {
	GeneralSettings GeneralSettingsSummary `json:"general_settings"`
}

type GeneralSettingsSummary struct {
	AppendSystemPrompt    string          `json:"append_system_prompt"`
	GitAttributionEnabled bool            `json:"git_attribution_enabled"`
	MemoryDisabled        bool            `json:"memory_disabled"`
	MCPServerEnabled      map[string]bool `json:"mcp_server_enabled"`
	DreamEnabled          bool            `json:"dream_enabled"`
	DreamIntervalDays     int             `json:"dream_interval_days"`
	DreamProvider         string          `json:"dream_provider,omitempty"`
	DreamModel            string          `json:"dream_model,omitempty"`
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

// GoalActiveSummary is the composer-banner view of the most recently
// updated non-terminal goal in one thread/session orchestration scope.
// The handler filters terminal statuses
// so the renderer can treat a nil summary as "no active goal" without
// re-checking status.
// Text is the first line of goal.Goal. The renderer owns visual ellipsis
// so editing a long first line never persists a server-side truncation.
// StartedAt and UpdatedAt remain available as goal metadata. TimeUsedSeconds
// contains completed execution, while RunningSince identifies the current
// in-flight slice; together they exclude time spent paused, blocked, or idle.
// The summary intentionally omits task / step / approvals to keep the composer
// surface quiet.
type GoalActiveSummary struct {
	ID                      string `json:"id"`
	ThreadID                string `json:"thread_id,omitempty"`
	Text                    string `json:"text"`
	Status                  string `json:"status"`
	Step                    string `json:"step,omitempty"`
	StartedAt               string `json:"started_at,omitempty"`
	UpdatedAt               string `json:"updated_at,omitempty"`
	RunningSince            string `json:"running_since,omitempty"`
	StopReason              string `json:"stop_reason,omitempty"`
	RecentProgress          string `json:"recent_progress,omitempty"`
	TokensUsed              int    `json:"tokens_used,omitempty"`
	TimeUsedSeconds         int64  `json:"time_used_seconds,omitempty"`
	GoalTurns               int    `json:"goal_turns,omitempty"`
	Blocker                 string `json:"blocker,omitempty"`
	BlockerConsecutiveTurns int    `json:"blocker_consecutive_turns,omitempty"`
	CanPause                bool   `json:"can_pause,omitempty"`
	CanResume               bool   `json:"can_resume,omitempty"`
	CanClear                bool   `json:"can_clear,omitempty"`
}

type GoalActiveSummaryParams struct {
	ThreadID string `json:"thread_id,omitempty"`
}

type GoalActiveSummaryResult struct {
	Summary *GoalActiveSummary `json:"summary,omitempty"`
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

type ManagedProcessSummary struct {
	ID             string    `json:"id"`
	OwnerKind      string    `json:"owner_kind"`
	OwnerID        string    `json:"owner_id"`
	Lifecycle      string    `json:"lifecycle"`
	Status         string    `json:"status"`
	PID            int       `json:"pid"`
	TTY            bool      `json:"tty,omitempty"`
	Command        string    `json:"command"`
	CWD            string    `json:"cwd"`
	StartedAt      time.Time `json:"started_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	StoppedAt      time.Time `json:"stopped_at,omitempty"`
	ExitCode       int       `json:"exit_code,omitempty"`
	LastError      string    `json:"last_error,omitempty"`
	InputAvailable bool      `json:"input_available,omitempty"`
}

type ProcessListParams struct {
	ThreadID string `json:"thread_id"`
}

type ProcessListResult struct {
	Processes []ManagedProcessSummary `json:"processes"`
}

type ProcessStopParams struct {
	ThreadID  string `json:"thread_id"`
	ProcessID string `json:"process_id"`
}

type ProcessStopResult struct {
	Process ManagedProcessSummary `json:"process"`
}

type ProcessReadParams struct {
	ThreadID   string `json:"thread_id"`
	ProcessID  string `json:"process_id"`
	Offset     *int64 `json:"offset_bytes,omitempty"`
	MaxBytes   int    `json:"max_bytes,omitempty"`
	WaitMillis int    `json:"wait_ms,omitempty"`
}

type ProcessReadResult struct {
	Process     ManagedProcessSummary `json:"process"`
	Output      string                `json:"output"`
	Truncated   bool                  `json:"truncated"`
	StartOffset int64                 `json:"start_offset"`
	EndOffset   int64                 `json:"end_offset"`
	TotalBytes  int64                 `json:"total_bytes"`
	TimedOut    bool                  `json:"timed_out"`
}

type ProcessWriteParams struct {
	ThreadID  string `json:"thread_id"`
	ProcessID string `json:"process_id"`
	Input     string `json:"input"`
}

type ProcessWriteResult struct {
	Process      ManagedProcessSummary `json:"process"`
	BytesWritten int                   `json:"bytes_written"`
}

type ProcessResizeParams struct {
	ThreadID  string `json:"thread_id"`
	ProcessID string `json:"process_id"`
	Cols      int    `json:"cols"`
	Rows      int    `json:"rows"`
}

type ProcessResizeResult struct {
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

type ModelAliasSummary struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Effort   string `json:"effort,omitempty"`
	Variant  string `json:"variant,omitempty"`
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
	Thread           Thread            `json:"thread"`
	HeldUserMessages []HeldUserMessage `json:"held_user_messages,omitempty"`
}

type ThreadForkParams struct {
	ThreadID string            `json:"thread_id"`
	TurnID   string            `json:"turn_id,omitempty"`
	ItemID   string            `json:"item_id,omitempty"`
	Target   *ThreadForkTarget `json:"target,omitempty"`
	Mode     string            `json:"mode,omitempty"`
}

type ThreadForkTarget struct {
	Seq      int            `json:"seq,omitempty"`
	Type     ThreadItemType `json:"type,omitempty"`
	SourceID string         `json:"source_id,omitempty"`
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

// A side thread is bound at most once to a main thread and remains hidden from
// the global session list. Its four JSON-RPC methods mirror the renderer IPC
// surface in packages/protocol/src/index.ts and the dispatch table in server.go.

type SideThreadOpenParams struct {
	MainThreadID string `json:"main_thread_id"`
}

type SideThreadOpenResult struct {
	Summary *SideThreadWireSummary `json:"summary"`
}

type SideThreadGetHistoryParams struct {
	MainThreadID string `json:"main_thread_id"`
}

type SideThreadGetHistoryResult struct {
	Summary  SideThreadWireSummary   `json:"summary"`
	Messages []SideThreadWireMessage `json:"messages"`
}

type SideThreadWireSummary struct {
	SideThreadID    string                     `json:"side_thread_id"`
	MainThreadID    string                     `json:"main_thread_id"`
	Status          string                     `json:"status"`
	Revision        uint64                     `json:"revision"`
	MainTaskSummary *SideThreadMainTaskSummary `json:"main_task_summary,omitempty"`
	CreatedAt       time.Time                  `json:"created_at"`
	UpdatedAt       time.Time                  `json:"updated_at"`
}

type SideThreadWireMessage struct {
	ID           string       `json:"id"`
	SideThreadID string       `json:"side_thread_id"`
	Role         string       `json:"role"`
	Text         string       `json:"text"`
	Items        []ThreadItem `json:"items,omitempty"`
	Status       string       `json:"status,omitempty"`
	ErrorMessage string       `json:"error_message,omitempty"`
	CreatedAt    time.Time    `json:"created_at"`
}

type SideThreadMainTaskSummary struct {
	Running         bool   `json:"running"`
	LastUserMessage string `json:"last_user_message,omitempty"`
}

type SideThreadSendParams struct {
	MainThreadID string `json:"main_thread_id"`
	Prompt       string `json:"prompt"`
}

type SideThreadSendResult struct {
	UserMessageID string                `json:"user_message_id"`
	Summary       SideThreadWireSummary `json:"summary"`
}

type SideThreadInterruptParams struct {
	MainThreadID string `json:"main_thread_id"`
}

type SideThreadInterruptResult struct {
	Ok bool `json:"ok"`
}

type SideThreadResetParams struct {
	MainThreadID string `json:"main_thread_id"`
}

type SideThreadResetResult struct {
	Ok bool `json:"ok"`
}

// SideThreadEventNotification is the wire union consumed by the renderer.
// Fields outside the selected Type are omitted.
type SideThreadEventNotification struct {
	Type         string                 `json:"type"`
	SideThreadID string                 `json:"side_thread_id"`
	MainThreadID string                 `json:"main_thread_id"`
	Revision     uint64                 `json:"revision,omitempty"`
	Summary      *SideThreadWireSummary `json:"summary,omitempty"`
	MessageID    string                 `json:"message_id,omitempty"`
	TextDelta    string                 `json:"text_delta,omitempty"`
	Message      *SideThreadWireMessage `json:"message,omitempty"`
	Items        []ThreadItem           `json:"items,omitempty"`
	ErrorMessage string                 `json:"error_message,omitempty"`
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

// ThreadPreviewParams asks the server to materialize the first N turns of a
// thread for the conversation-search preview pane. The renderer fetches this
// lazily when a search result is selected; the search response itself stays
// light (title + snippet only).
type ThreadPreviewParams struct {
	ThreadID string `json:"thread_id"`
	Limit    int    `json:"limit,omitempty"`
}

// ThreadPreviewResult carries the rendered preview turns. Empty (non-nil)
// slice means "thread exists but has no persisted turns yet"; a nil slice is
// reserved for the "thread not found / history missing" path so callers can
// tell the two apart if they care.
type ThreadPreviewResult struct {
	Turns []Turn `json:"turns"`
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

// ThreadDeleteParams is the input for the `thread/delete` method: the user
// permanently removes a conversation. Only archived or otherwise idle (not
// running) threads are eligible. Deletion removes the session row (chat
// history cascades via foreign keys), the workspace-scoped session artifact
// directory, and any fork worktree bound to the thread.
type ThreadDeleteParams struct {
	ThreadID string `json:"thread_id"`
}

type ThreadDeleteResult struct {
	ThreadID string `json:"thread_id"`
}

// WorkspaceStateCleanupParams is the input for the `workspace/state/cleanup`
// method: after the user removes a project from the sidebar, the desktop can
// offer a second, opt-in step that reclaims the removed workspace's local
// state directory (session artifacts, goals, worktrees, runtime files).
// Memory is never hard-deleted (self-consistency invariant 3): the memory
// directories are moved into a `.archived/` folder inside the same state
// directory instead. workspace_id (the desktop's stable project id) is
// preferred; workspace_path is the fallback for path-keyed state dirs.
type WorkspaceStateCleanupParams struct {
	WorkspaceID   string `json:"workspace_id,omitempty"`
	WorkspacePath string `json:"workspace_path,omitempty"`
}

type WorkspaceStateCleanupResult struct {
	StateDir string `json:"state_dir"`
	// Removed reports whether a state directory existed and its
	// non-memory contents were deleted.
	Removed bool `json:"removed"`
	// MemoryArchived reports whether at least one memory directory was
	// moved into .archived/ instead of being deleted.
	MemoryArchived bool `json:"memory_archived"`
}

// Memory panel wire types. Each struct's json tags mirror the Memory* types
// in desktop/src/shared/protocol.ts field-for-field; changing either side
// requires changing both (memory-redesign contract §8.2).

// Memory scope values: "user" targets the user notebook (~/.wuu/memory),
// "participant" targets a named agent's identity notebook
// (~/.wuu/participants/<id>/memory) and requires participant_id naming an
// active (non-retired) named participant.
const (
	MemoryScopeUser        = "user"
	MemoryScopeParticipant = "participant"
)

type MemoryOverviewParams struct {
	Scope         string `json:"scope"`
	ParticipantID string `json:"participant_id,omitempty"`
	ForceRefresh  bool   `json:"force_refresh,omitempty"`
}

// MemoryOverviewResult carries the structured essay the overview agent
// generated from the real notebook (one LLM pass). Cached indicates the
// backend served the notebook's 12-hour cache instead of regenerating.
type MemoryOverviewResult struct {
	EssayMD     string `json:"essay_md"`
	GeneratedAt string `json:"generated_at"`
	SourceMtime string `json:"source_mtime"`
	Cached      bool   `json:"cached"`
}

type MemoryChatParams struct {
	Scope         string `json:"scope"`
	ParticipantID string `json:"participant_id,omitempty"`
	Message       string `json:"message"`
}

type TextPolishParams struct {
	Text string `json:"text"`
}

type TextPolishResult struct {
	Text string `json:"text"`
}

// GitCommitMessageParams is the input for `git/commit-message`: the staged
// diff (already size-capped by the desktop) plus the staged file list so the
// model keeps an overview even when the diff itself is truncated.
type GitCommitMessageParams struct {
	Diff  string   `json:"diff"`
	Files []string `json:"files,omitempty"`
}

type GitCommitMessageResult struct {
	Message string `json:"message"`
}

// MemoryChangedFile is one real notebook file the manager agent touched.
// Action is "created", "modified", or "deleted".
type MemoryChangedFile struct {
	Path   string `json:"path"`
	Action string `json:"action"`
}

type MemoryChatResult struct {
	ReplyMD      string              `json:"reply_md"`
	ChangedFiles []MemoryChangedFile `json:"changed_files"`
}

type MemoryReadParams struct {
	Scope         string `json:"scope"`
	ParticipantID string `json:"participant_id,omitempty"`
}

// MemoryFileInfo describes one topic file in the notebook. Name,
// Description, and Type mirror the file's frontmatter (canonical types are
// user | feedback | reference | lesson, but Type stays a plain string so
// entries written by newer backends still render); Mtime is the file's
// modification time in RFC 3339.
type MemoryFileInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Mtime       string `json:"mtime"`
}

// MemoryReadResult is the raw MEMORY.md index plus the file inventory — no
// LLM involved; the panel's "查看原文" audit/fallback view.
type MemoryReadResult struct {
	IndexMD string           `json:"index_md"`
	Files   []MemoryFileInfo `json:"files"`
}

type TurnStartParams struct {
	ThreadID       string           `json:"thread_id"`
	Prompt         string           `json:"prompt"`
	Images         []TurnStartImage `json:"images,omitempty"`
	Files          []TurnStartFile  `json:"files,omitempty"`
	PermissionMode *string          `json:"permission_mode,omitempty"`
	ActiveDocument *ActiveDocument  `json:"active_document,omitempty"`
}

type ActiveDocument struct {
	Path string `json:"path"`
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

// Run is the public control and observation boundary for one agent invocation.
// Its Turns refer to canonical Thread/Turn records rather than copying them.
type Run = execution.Run

type RunStartParams struct {
	ThreadID       string            `json:"thread_id"`
	Prompt         string            `json:"prompt"`
	Images         []TurnStartImage  `json:"images,omitempty"`
	Files          []TurnStartFile   `json:"files,omitempty"`
	PermissionMode *string           `json:"permission_mode,omitempty"`
	Request        execution.Request `json:"request"`
	OutputSchema   json.RawMessage   `json:"output_schema,omitempty"`
}

type RunStartResult struct {
	Run Run `json:"run"`
}

type RunView struct {
	Run      Run     `json:"run"`
	Thread   *Thread `json:"thread,omitempty"`
	Attached bool    `json:"attached"`
}

type RunInterruptParams struct {
	RunID  string `json:"run_id"`
	Reason string `json:"reason,omitempty"`
}

type RunInterruptResult struct {
	Run Run `json:"run"`
}

type RunStartedNotification struct {
	Run Run `json:"run"`
}

type RunUpdatedNotification struct {
	Run Run `json:"run"`
}

type ThreadCompactStartParams struct {
	ThreadID string `json:"thread_id"`
}

type ThreadCompactStartResult struct {
	Turn Turn `json:"turn"`
}

type TurnQueueParams struct {
	ThreadID       string           `json:"thread_id"`
	Prompt         string           `json:"prompt"`
	Images         []TurnStartImage `json:"images,omitempty"`
	Files          []TurnStartFile  `json:"files,omitempty"`
	ClientID       string           `json:"client_id,omitempty"`
	PermissionMode *string          `json:"permission_mode,omitempty"`
	ActiveDocument *ActiveDocument  `json:"active_document,omitempty"`
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
	ActiveDocument *ActiveDocument  `json:"active_document,omitempty"`
}

type TurnSteerResult struct {
	TurnID string `json:"turn_id"`
}

type HeldUserMessage struct {
	ID       string           `json:"id"`
	ThreadID string           `json:"thread_id"`
	Origin   string           `json:"origin"`
	Prompt   string           `json:"prompt,omitempty"`
	Images   []TurnStartImage `json:"images,omitempty"`
	Files    []TurnStartFile  `json:"files,omitempty"`
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
	Thread           Thread            `json:"thread"`
	HeldUserMessages []HeldUserMessage `json:"held_user_messages,omitempty"`
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

type TurnHeldNotification struct {
	ThreadID string            `json:"thread_id"`
	Messages []HeldUserMessage `json:"messages"`
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
	// the shell can show a concise user-facing status while retaining
	// diagnostic facts. Empty fields fall back to the legacy
	// `error` string for older clients.
	Code       string `json:"code,omitempty"`
	Category   string `json:"category,omitempty"`
	Provider   string `json:"provider,omitempty"`
	StatusCode int    `json:"status_code,omitempty"`
	Turn       Turn   `json:"turn"`
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
	ThreadID                 string `json:"thread_id"`
	Turn                     Turn   `json:"turn"`
	Content                  string `json:"content"`
	InputTokens              int    `json:"input_tokens"`
	OutputTokens             int    `json:"output_tokens"`
	ContextTokens            int    `json:"context_tokens,omitempty"`
	CacheCreationTokens      int    `json:"cache_creation_tokens,omitempty"`
	CacheReadTokens          int    `json:"cache_read_tokens,omitempty"`
	FinishReason             string `json:"finish_reason,omitempty"`
	StopReason               string `json:"stop_reason,omitempty"`
	Truncated                bool   `json:"truncated,omitempty"`
	TracePath                string `json:"trace_path,omitempty"`
	AwaitingAutoContinuation bool   `json:"awaiting_auto_continuation,omitempty"`
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
	ModelAlias          string `json:"model_alias,omitempty"`
	ModelAliasFallback  bool   `json:"model_alias_fallback,omitempty"`
	Provider            string `json:"provider,omitempty"`
	Model               string `json:"model,omitempty"`
	APIModel            string `json:"api_model,omitempty"`
	Effort              string `json:"effort,omitempty"`
	Variant             string `json:"variant,omitempty"`
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
	// Participant identifies the persisted participant attached to this run.
	// Always populated on live snapshots: resolved from the
	// participant store when possible, synthesized from the snapshot's
	// type/task name otherwise.
	Participant *participant.Summary `json:"participant,omitempty"`
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

type TurnKind string

const (
	TurnKindUser     TurnKind = "user"
	TurnKindInternal TurnKind = "internal"
	TurnKindCompact  TurnKind = "compact"
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

type AutomationListResult struct {
	Tasks []automation.Task `json:"tasks"`
}

type AutomationRunsResult struct {
	Runs []automation.Run `json:"runs"`
}

type AutomationCreateParams struct {
	Title             string          `json:"title"`
	Prompt            string          `json:"prompt"`
	Schedule          string          `json:"schedule"`
	Timezone          string          `json:"timezone,omitempty"`
	Mode              automation.Mode `json:"mode,omitempty"`
	HeartbeatThreadID string          `json:"heartbeat_thread_id,omitempty"`
	WorkspaceID       string          `json:"workspace_id,omitempty"`
	WorkspacePath     string          `json:"workspace_path,omitempty"`
	Recurring         bool            `json:"recurring"`
	Paused            bool            `json:"paused,omitempty"`
}

type AutomationCreateResult struct {
	Task automation.Task `json:"task"`
}

type AutomationUpdateParams struct {
	ID                string           `json:"id"`
	Title             *string          `json:"title,omitempty"`
	Prompt            *string          `json:"prompt,omitempty"`
	Schedule          *string          `json:"schedule,omitempty"`
	Timezone          *string          `json:"timezone,omitempty"`
	Mode              *automation.Mode `json:"mode,omitempty"`
	HeartbeatThreadID *string          `json:"heartbeat_thread_id,omitempty"`
	Recurring         *bool            `json:"recurring,omitempty"`
	Paused            *bool            `json:"paused,omitempty"`
}

type AutomationUpdateResult struct {
	Task automation.Task `json:"task"`
}

type AutomationRemoveParams struct {
	ID string `json:"id"`
}

type Thread struct {
	ID               string        `json:"id"`
	Source           string        `json:"source,omitempty"`
	ParentID         string        `json:"parent_id,omitempty"`
	AgentPath        string        `json:"agent_path,omitempty"`
	Preview          string        `json:"preview"`
	Title            string        `json:"title,omitempty"`
	ModelProvider    string        `json:"model_provider"`
	Model            string        `json:"model"`
	ModelVariant     string        `json:"model_variant"`
	ModelEffort      string        `json:"model_effort"`
	PermissionMode   string        `json:"permission_mode"`
	CWD              string        `json:"cwd"`
	WorkspaceKind    WorkspaceKind `json:"workspace_kind,omitempty"`
	Status           ThreadStatus  `json:"status"`
	TreeInterrupted  bool          `json:"orchestration_interrupted,omitempty"`
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
}

type WorktreeInfo struct {
	Path         string   `json:"path"`
	BaseHEAD     string   `json:"base_head,omitempty"`
	BaseRepo     string   `json:"base_repo,omitempty"`
	Dirty        bool     `json:"dirty,omitempty"`
	ChangedFiles []string `json:"changed_files,omitempty"`
}

type Turn struct {
	ID   string   `json:"id"`
	Kind TurnKind `json:"kind,omitempty"`
	// ModelProvider and Model are captured when the turn begins. They stay
	// stable while a config update prepares the thread for its next turn.
	ModelProvider       string        `json:"model_provider,omitempty"`
	Model               string        `json:"model,omitempty"`
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

// MarshalJSON keeps the app-server contract stable: items is a required
// collection in every Turn payload, including a turn that has not produced
// its first item yet. A nil Go slice otherwise becomes JSON null, which is
// not a valid value for the protocol's ThreadItem[] field.
func (turn Turn) MarshalJSON() ([]byte, error) {
	type wireTurn Turn
	wire := wireTurn(turn)
	if wire.Items == nil {
		wire.Items = []ThreadItem{}
	}
	return json.Marshal(wire)
}

type TurnError struct {
	Message string `json:"message"`
	// Structured error fields, filled in by BuildTurnError from the typed
	// error (HTTPError, StreamError) and the agentcontrol.ClassifyError
	// classifier. The front-end prefers these over the raw `message` for
	// the visible system-event text; the message and machine code remain
	// diagnostic facts rather than UI actions.
	Code       string `json:"code,omitempty"`
	Category   string `json:"category,omitempty"`
	Provider   string `json:"provider,omitempty"`
	StatusCode int    `json:"status_code,omitempty"`
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
	ID string `json:"id"`
	// Seq is the message's stable per-thread address (session_messages.seq),
	// present on persisted chat messages.
	// 0/absent for synthetic or not-yet-persisted items.
	Seq          int                        `json:"seq,omitempty"`
	SourceID     string                     `json:"source_id,omitempty"`
	AgentID      string                     `json:"agent_id,omitempty"`
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
	ResultDetail *toolresult.Result         `json:"result_detail,omitempty"`
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
	ThreadID string             `json:"thread_id"`
	TurnID   string             `json:"turn_id"`
	ItemID   string             `json:"item_id"`
	Delta    string             `json:"delta"`
	Detail   *toolresult.Result `json:"tool_result_detail,omitempty"`
}

type StreamEventPayload struct {
	Type             providers.StreamEventType        `json:"type"`
	Content          string                           `json:"content,omitempty"`
	Message          *providers.ChatMessage           `json:"message,omitempty"`
	ToolCall         *providers.ToolCall              `json:"tool_call,omitempty"`
	ToolResult       string                           `json:"tool_result,omitempty"`
	ToolResultDetail *toolresult.Result               `json:"tool_result_detail,omitempty"`
	PlanUpdate       *providers.PlanUpdate            `json:"plan_update,omitempty"`
	Lifecycle        *StreamLifecyclePayload          `json:"lifecycle,omitempty"`
	RequestContext   *providers.RequestContextSummary `json:"request_context,omitempty"`
	ProviderState    *providers.ProviderStateSummary  `json:"provider_state,omitempty"`
	Usage            *providers.TokenUsage            `json:"usage,omitempty"`
	StopReason       string                           `json:"stop_reason,omitempty"`
	FinishReason     string                           `json:"finish_reason,omitempty"`
	Truncated        bool                             `json:"truncated,omitempty"`
	Error            string                           `json:"error,omitempty"`
}

type StreamLifecyclePayload struct {
	Phase           string                   `json:"phase"`
	OperationID     string                   `json:"operation_id,omitempty"`
	OperationKind   string                   `json:"operation_kind,omitempty"`
	WorkloadProfile string                   `json:"workload_profile,omitempty"`
	PayloadVersion  int                      `json:"payload_version,omitempty"`
	AttemptID       string                   `json:"attempt_id,omitempty"`
	Attempt         int                      `json:"attempt,omitempty"`
	MaxAttempts     int                      `json:"max_attempts,omitempty"`
	SubmissionID    string                   `json:"submission_id,omitempty"`
	SubmissionCount int                      `json:"submission_count,omitempty"`
	RetryCount      int                      `json:"retry_count,omitempty"`
	MaxRetries      int                      `json:"max_retries,omitempty"`
	RetryInMS       int64                    `json:"retry_in_ms,omitempty"`
	ElapsedMS       int64                    `json:"elapsed_ms,omitempty"`
	Reason          string                   `json:"reason,omitempty"`
	FailureCategory string                   `json:"failure_category,omitempty"`
	RecoveryAction  string                   `json:"recovery_action,omitempty"`
	BudgetDimension string                   `json:"budget_dimension,omitempty"`
	ReplayReason    string                   `json:"replay_reason,omitempty"`
	ResetPartial    bool                     `json:"reset_partial,omitempty"`
	Workflow        *WorkflowSnapshotPayload `json:"workflow,omitempty"`
}

type WorkflowSnapshotPayload struct {
	ID                         string `json:"id"`
	Operations                 uint64 `json:"operations"`
	Attempts                   uint64 `json:"attempts"`
	Submissions                uint64 `json:"submissions"`
	TransportSwitches          uint64 `json:"transport_switches"`
	CredentialRefreshes        uint64 `json:"credential_refreshes"`
	PayloadTransforms          uint64 `json:"payload_transforms"`
	ChildOperations            uint64 `json:"child_operations"`
	RecoveryWaitMS             uint64 `json:"recovery_wait_ms"`
	KnownSubmissions           uint64 `json:"known_submissions"`
	EstimatedSubmissions       uint64 `json:"estimated_submissions"`
	UnknownBillableSubmissions uint64 `json:"unknown_billable_submissions"`
	KnownInputTokens           int    `json:"known_input_tokens"`
	KnownOutputTokens          int    `json:"known_output_tokens"`
	EstimatedInputTokens       int    `json:"estimated_input_tokens"`
	EstimatedOutputTokens      int    `json:"estimated_output_tokens"`
}

// SettingsUsageQuery is the input for the settings/usage RPC. It carries
// no parameters: the snapshot always covers the full recorded history.
type SettingsUsageQuery struct{}

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

// SettingsUsageResponse is the single source of truth for the desktop
// usage page. Metrics is the headline number block, ModelBreakdowns is
// the per-model table sorted by total context tokens descending (legacy
// rows with empty provider+model are surfaced as "(unknown)"), and Days
// is the calendar-day series for the heatmap (gaps filled by the
// desktop).
type SettingsUsageResponse struct {
	TotalSessions   int                  `json:"total_sessions"`
	GeneratedAt     string               `json:"generated_at"`
	Metrics         SettingsUsageMetrics `json:"metrics"`
	ModelBreakdowns []insight.ModelUsage `json:"model_breakdowns"`
	SkillUsage      []insight.SkillUsage `json:"skill_usage"`
	Days            []SettingsUsageDay   `json:"days"`
}

type ChannelAgentListResult struct {
	Agents []channels.NamedAgent `json:"agents"`
}

type ChannelAgentLanguageUsage struct {
	Name  string  `json:"name"`
	Lines int     `json:"lines"`
	Share float64 `json:"share"`
}

type ChannelAgentInsight struct {
	AgentID            string                      `json:"agent_id"`
	WindowDays         int                         `json:"window_days"`
	FilesChanged       int                         `json:"files_changed"`
	Additions          int                         `json:"additions"`
	Deletions          int                         `json:"deletions"`
	InputTokens        int                         `json:"input_tokens"`
	OutputTokens       int                         `json:"output_tokens"`
	LastActiveAt       string                      `json:"last_active_at,omitempty"`
	Workspace          string                      `json:"workspace,omitempty"`
	Languages          []ChannelAgentLanguageUsage `json:"languages"`
	AttributionPartial bool                        `json:"attribution_partial"`
}

type ChannelAgentInsightsResult struct {
	GeneratedAt string                `json:"generated_at"`
	Insights    []ChannelAgentInsight `json:"insights"`
}

type ChannelBootstrapResult = channels.BootstrapResult

type ChannelAgentCreateParams struct {
	Name             string `json:"name"`
	AvatarKey        string `json:"avatar_key,omitempty"`
	AvatarImage      string `json:"avatar_image,omitempty"`
	ProviderOverride string `json:"provider_override,omitempty"`
	ModelOverride    string `json:"model_override,omitempty"`
	EffortOverride   string `json:"effort_override,omitempty"`
}

type ChannelAgentCreateResult struct {
	Agent channels.NamedAgent `json:"agent"`
}

type ChannelAgentUpdateParams struct {
	AgentID          string  `json:"agent_id"`
	Name             string  `json:"name"`
	AvatarKey        string  `json:"avatar_key,omitempty"`
	AvatarImage      *string `json:"avatar_image,omitempty"`
	ProviderOverride string  `json:"provider_override,omitempty"`
	ModelOverride    string  `json:"model_override,omitempty"`
	EffortOverride   string  `json:"effort_override,omitempty"`
}

type ChannelAgentUpdateResult struct {
	Agent channels.NamedAgent `json:"agent"`
}
type ChannelAgentDeleteParams struct {
	AgentID string `json:"agent_id"`
}
type ChannelAgentDeleteResult struct {
	Deleted bool `json:"deleted"`
}

type ChannelAgentStartParams struct {
	AgentID string `json:"agent_id"`
}

type ChannelAgentStartResult struct {
	Agent     channels.NamedAgent `json:"agent"`
	WakeState channels.WakeState  `json:"wake_state"`
	Started   bool                `json:"started"`
	ThreadID  string              `json:"thread_id"`
}

type ChannelAgentResetParams struct {
	AgentID string `json:"agent_id"`
}

type ChannelAgentResetResult struct {
	Agent     channels.NamedAgent `json:"agent"`
	WakeState channels.WakeState  `json:"wake_state"`
	Requested bool                `json:"requested"`
	ThreadID  string              `json:"thread_id"`
}

type ChannelRoomListResult struct {
	Rooms []channels.Room `json:"rooms"`
}

type ChannelRoomCreateParams struct {
	Name        string   `json:"name"`
	AvatarImage string   `json:"avatar_image,omitempty"`
	AgentIDs    []string `json:"agent_ids,omitempty"`
}

type ChannelRoomCreateResult struct {
	Room channels.Room `json:"room"`
}

type ChannelRoomUpdateParams struct {
	RoomID      string    `json:"room_id"`
	Name        *string   `json:"name,omitempty"`
	AvatarImage *string   `json:"avatar_image,omitempty"`
	AgentIDs    *[]string `json:"agent_ids,omitempty"`
}

type ChannelRoomUpdateResult struct {
	Room channels.Room `json:"room"`
}

type ChannelRoomDeleteParams struct {
	RoomID string `json:"room_id"`
}
type ChannelRoomDeleteResult struct {
	Deleted bool `json:"deleted"`
}

type ChannelRoomReadParams struct {
	RoomID string `json:"room_id"`
}

type ChannelRoomReadResult struct {
	Read bool `json:"read"`
}

type ChannelMessageListParams struct {
	RoomID   string `json:"room_id"`
	AfterSeq int64  `json:"after_seq,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

type ChannelMessageListResult struct {
	Messages []channels.Message `json:"messages"`
}

type ChannelMessageSendParams struct {
	RoomID   string           `json:"room_id"`
	ThreadID string           `json:"thread_id,omitempty"`
	ReplyTo  string           `json:"reply_to,omitempty"`
	Body     string           `json:"body"`
	Images   []TurnStartImage `json:"images,omitempty"`
	Files    []TurnStartFile  `json:"files,omitempty"`
}

type ChannelMessageSendResult struct {
	Message channels.Message `json:"message"`
}

type ChannelTaskCreateParams struct {
	RoomID  string `json:"room_id"`
	Title   string `json:"title"`
	OwnerID string `json:"owner_id"`
}

type ChannelTaskCreateResult struct {
	Task channels.Message `json:"task"`
}

type ChannelTaskUpdateParams struct {
	TaskID  string `json:"task_id"`
	State   string `json:"state,omitempty"`
	OwnerID string `json:"owner_id,omitempty"`
}

type ChannelTaskUpdateResult struct {
	Task channels.Message `json:"task"`
}

type ChannelHumanMentionStatusResult struct {
	Count int `json:"count"`
}

type ChannelHumanMentionAckResult struct {
	Acknowledged int `json:"acknowledged"`
}
