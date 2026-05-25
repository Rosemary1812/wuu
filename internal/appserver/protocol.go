package appserver

import (
	"encoding/json"
	"time"

	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/tools"
)

const (
	ProtocolVersion = "wuu-app-server/v0.1"

	MethodInitialize        = "initialize"
	MethodConfigRead        = "config/read"
	MethodConfigModelUpdate = "config/model/update"
	MethodConfigCodexModels = "config/codex/models"
	MethodThreadStart       = "thread/start"
	MethodThreadResume      = "thread/resume"
	MethodThreadFork        = "thread/fork"
	MethodThreadList        = "thread/list"
	MethodThreadPin         = "thread/pin"
	MethodThreadArchive     = "thread/archive"
	MethodTurnStart         = "turn/start"
	MethodTurnInterrupt     = "turn/interrupt"
	MethodShutdown          = "shutdown"

	MethodToolRequestUserInput = "item/tool/requestUserInput"

	NotificationThreadStarted = "thread/started"
	NotificationThreadResumed = "thread/resumed"
	NotificationTurnStarted   = "turn/started"
	NotificationTurnEvent     = "turn/event"
	NotificationTurnError     = "turn/error"
	NotificationTurnCompleted = "turn/completed"

	NotificationItemStarted       = "item/started"
	NotificationItemCompleted     = "item/completed"
	NotificationAgentMessageDelta = "item/agentMessage/delta"
	NotificationReasoningDelta    = "item/reasoning/delta"
	NotificationToolCallDelta     = "item/toolCall/delta"
	NotificationToolCallOutput    = "item/toolCall/outputDelta"
	NotificationAgentUpdated      = "agent/updated"
	NotificationAgentMailbox      = "agent/mailbox"
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

type InitializeResult struct {
	ProtocolVersion string            `json:"protocol_version"`
	Provider        string            `json:"provider"`
	Model           string            `json:"model"`
	Effort          string            `json:"effort,omitempty"`
	WorkspaceRoot   string            `json:"workspace_root"`
	Providers       []ProviderSummary `json:"providers,omitempty"`
}

type ConfigReadResult struct {
	Provider      string            `json:"provider"`
	Model         string            `json:"model"`
	Effort        string            `json:"effort,omitempty"`
	ConfigPath    string            `json:"config_path"`
	WorkspaceRoot string            `json:"workspace_root"`
	SessionDir    string            `json:"session_dir"`
	Providers     []ProviderSummary `json:"providers,omitempty"`
}

type ConfigModelUpdateParams struct {
	Provider string  `json:"provider,omitempty"`
	Model    string  `json:"model"`
	Effort   *string `json:"effort,omitempty"`
}

type ConfigModelUpdateResult struct {
	Provider  string            `json:"provider"`
	Model     string            `json:"model"`
	Effort    string            `json:"effort,omitempty"`
	Providers []ProviderSummary `json:"providers,omitempty"`
}

type ConfigCodexModelsParams struct {
	Provider string `json:"provider,omitempty"`
}

type ConfigCodexModelsResult struct {
	Provider string              `json:"provider"`
	Model    string              `json:"model"`
	Effort   string              `json:"effort,omitempty"`
	Models   []CodexModelSummary `json:"models"`
}

type CodexModelSummary struct {
	Slug                  string   `json:"slug"`
	DisplayName           string   `json:"display_name,omitempty"`
	DefaultReasoningLevel string   `json:"default_reasoning_level,omitempty"`
	SupportedReasoning    []string `json:"supported_reasoning,omitempty"`
	SupportedInAPI        bool     `json:"supported_in_api"`
}

type ProviderSummary struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Model string `json:"model"`
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
}

type ThreadForkResult struct {
	Thread Thread `json:"thread"`
}

type ThreadListResult struct {
	Threads []Thread `json:"threads"`
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

type TurnStartParams struct {
	ThreadID string           `json:"thread_id"`
	Prompt   string           `json:"prompt"`
	Images   []TurnStartImage `json:"images,omitempty"`
}

type TurnStartImage struct {
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type TurnStartResult struct {
	Turn Turn `json:"turn"`
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

type TurnStartedNotification struct {
	ThreadID string `json:"thread_id"`
	Turn     Turn   `json:"turn"`
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
	Turn     Turn   `json:"turn"`
}

type TurnCompletedNotification struct {
	ThreadID     string `json:"thread_id"`
	Turn         Turn   `json:"turn"`
	Content      string `json:"content"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
}

type Agent struct {
	ID                 string    `json:"id"`
	Type               string    `json:"type"`
	TaskName           string    `json:"task_name,omitempty"`
	AgentPath          string    `json:"agent_path,omitempty"`
	ParentID           string    `json:"parent_id,omitempty"`
	Description        string    `json:"description,omitempty"`
	Status             string    `json:"status"`
	Result             string    `json:"result,omitempty"`
	Error              string    `json:"error,omitempty"`
	InputTokens        int       `json:"input_tokens,omitempty"`
	OutputTokens       int       `json:"output_tokens,omitempty"`
	NestedCount        int       `json:"nested_count,omitempty"`
	NestedRunningCount int       `json:"nested_running_count,omitempty"`
	StartedAt          time.Time `json:"started_at"`
	CompletedAt        time.Time `json:"completed_at,omitempty"`
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

type Thread struct {
	ID               string       `json:"id"`
	ParentID         string       `json:"parent_id,omitempty"`
	AgentPath        string       `json:"agent_path,omitempty"`
	Preview          string       `json:"preview"`
	ModelProvider    string       `json:"model_provider"`
	Model            string       `json:"model"`
	CWD              string       `json:"cwd"`
	Status           ThreadStatus `json:"status"`
	ReadOnly         bool         `json:"read_only,omitempty"`
	Pinned           bool         `json:"pinned,omitempty"`
	Archived         bool         `json:"archived,omitempty"`
	ForkedFromID     string       `json:"forked_from_id,omitempty"`
	ForkedFromTurnID string       `json:"forked_from_turn_id,omitempty"`
	ForkedFromItemID string       `json:"forked_from_item_id,omitempty"`
	CreatedAt        time.Time    `json:"created_at"`
	UpdatedAt        time.Time    `json:"updated_at"`
	Turns            []Turn       `json:"turns"`
	ChildAgents      []Agent      `json:"child_agents,omitempty"`
}

type Turn struct {
	ID          string        `json:"id"`
	Items       []ThreadItem  `json:"items"`
	ItemsView   TurnItemsView `json:"items_view"`
	Status      TurnStatus    `json:"status"`
	Error       *TurnError    `json:"error,omitempty"`
	StartedAt   *time.Time    `json:"started_at,omitempty"`
	CompletedAt *time.Time    `json:"completed_at,omitempty"`
	DurationMS  *int64        `json:"duration_ms,omitempty"`
}

type TurnError struct {
	Message string `json:"message"`
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

type ThreadItem struct {
	ID        string            `json:"id"`
	Type      ThreadItemType    `json:"type"`
	Status    ThreadItemStatus  `json:"status,omitempty"`
	Role      string            `json:"role,omitempty"`
	Text      string            `json:"text,omitempty"`
	Images    []ThreadItemImage `json:"images,omitempty"`
	Name      string            `json:"name,omitempty"`
	Arguments string            `json:"arguments,omitempty"`
	Result    string            `json:"result,omitempty"`
	Error     string            `json:"error,omitempty"`
}

type ThreadItemImage struct {
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
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

type ReasoningDeltaNotification struct {
	ThreadID string `json:"thread_id"`
	TurnID   string `json:"turn_id"`
	ItemID   string `json:"item_id"`
	Delta    string `json:"delta"`
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

type ToolRequestUserInputParams struct {
	ThreadID  string                  `json:"thread_id,omitempty"`
	Questions []tools.AskUserQuestion `json:"questions"`
}

type StreamEventPayload struct {
	Type       providers.StreamEventType `json:"type"`
	Content    string                    `json:"content,omitempty"`
	Message    *providers.ChatMessage    `json:"message,omitempty"`
	ToolCall   *providers.ToolCall       `json:"tool_call,omitempty"`
	ToolResult string                    `json:"tool_result,omitempty"`
	PlanUpdate *providers.PlanUpdate     `json:"plan_update,omitempty"`
	Usage      *providers.TokenUsage     `json:"usage,omitempty"`
	StopReason string                    `json:"stop_reason,omitempty"`
	Truncated  bool                      `json:"truncated,omitempty"`
	Error      string                    `json:"error,omitempty"`
}
