package appserver

import (
	"encoding/json"

	"github.com/blueberrycongee/wuu/internal/providers"
)

const (
	ProtocolVersion = "wuu-app-server/v0.1"

	MethodInitialize    = "initialize"
	MethodConfigRead    = "config/read"
	MethodThreadStart   = "thread/start"
	MethodThreadResume  = "thread/resume"
	MethodThreadList    = "thread/list"
	MethodTurnStart     = "turn/start"
	MethodTurnInterrupt = "turn/interrupt"
	MethodShutdown      = "shutdown"

	NotificationThreadStarted = "thread/started"
	NotificationThreadResumed = "thread/resumed"
	NotificationTurnStarted   = "turn/started"
	NotificationTurnEvent     = "turn/event"
	NotificationTurnError     = "turn/error"
	NotificationTurnCompleted = "turn/completed"
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

type InitializeResult struct {
	ProtocolVersion string `json:"protocol_version"`
	Provider        string `json:"provider"`
	Model           string `json:"model"`
	WorkspaceRoot   string `json:"workspace_root"`
}

type ConfigReadResult struct {
	Provider      string `json:"provider"`
	Model         string `json:"model"`
	ConfigPath    string `json:"config_path"`
	WorkspaceRoot string `json:"workspace_root"`
	SessionDir    string `json:"session_dir"`
}

type ThreadStartResult struct {
	ThreadID string `json:"thread_id"`
}

type ThreadResumeParams struct {
	SessionID string `json:"session_id,omitempty"`
}

type ThreadResumeResult struct {
	ThreadID     string `json:"thread_id"`
	MessageCount int    `json:"message_count"`
}

type ThreadInfo struct {
	ThreadID     string `json:"thread_id"`
	MessageCount int    `json:"message_count"`
	Running      bool   `json:"running"`
	CurrentTurn  string `json:"current_turn"`
}

type ThreadListResult struct {
	Threads []ThreadInfo `json:"threads"`
}

type TurnStartParams struct {
	ThreadID string `json:"thread_id"`
	Prompt   string `json:"prompt"`
}

type TurnStartResult struct {
	TurnID string `json:"turn_id"`
}

type TurnInterruptParams struct {
	ThreadID string `json:"thread_id"`
}

type OKResult struct {
	OK bool `json:"ok"`
}

type ThreadStartedNotification struct {
	ThreadID string `json:"thread_id"`
}

type ThreadResumedNotification struct {
	ThreadID     string `json:"thread_id"`
	MessageCount int    `json:"message_count"`
}

type TurnStartedNotification struct {
	ThreadID string `json:"thread_id"`
	TurnID   string `json:"turn_id"`
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
}

type TurnCompletedNotification struct {
	ThreadID     string `json:"thread_id"`
	TurnID       string `json:"turn_id"`
	Content      string `json:"content"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
}

type StreamEventPayload struct {
	Type       providers.StreamEventType `json:"type"`
	Content    string                    `json:"content,omitempty"`
	Message    *providers.ChatMessage    `json:"message,omitempty"`
	ToolCall   *providers.ToolCall       `json:"tool_call,omitempty"`
	ToolResult string                    `json:"tool_result,omitempty"`
	Usage      *providers.TokenUsage     `json:"usage,omitempty"`
	StopReason string                    `json:"stop_reason,omitempty"`
	Truncated  bool                      `json:"truncated,omitempty"`
	Error      string                    `json:"error,omitempty"`
}
