// Package rollout provides incremental JSONL persistence for conversation
// sessions. Each session's events are appended line-by-line to a JSONL file
// under ~/.wuu/sessions/rollouts/<thread_id>.jsonl. A background writer
// goroutine owns the file handle; callers enqueue RolloutItems through Emit
// and synchronize via FlushIfMaterialized (called BEFORE tool execution so
// tool_call lands before the tool runs).
//
// Why this exists: the previous persistence path in
// internal/appserver/turn_handlers.go saved the entire turn only AFTER it
// completed. A network drop or process kill mid-turn lost every tool side
// effect (file edits, shell commands), forcing the user to retry from
// scratch — and retry is harmful when tools are non-idempotent (rm -rf,
// git push, deploy scripts). Mirrors Codex rollout/src/recorder.rs:877 ff.
package rollout

import (
	"encoding/json"
	"time"
)

// RolloutItem type constants — the discriminator tag for each variant.
// Each RolloutItem has exactly one non-nil pointer field matching Type.
const (
	TypeSessionMeta      = "session_meta"
	TypeUserMessage      = "user_message"
	TypeAssistantMessage = "assistant_message"
	TypeToolCall         = "tool_call"
	TypeToolResult       = "tool_result"
	TypeTurnMeta         = "turn_meta"
)

// RolloutItem is the JSONL line shape: one item per line, Type tag
// distinguishes variants. Pointer fields with omitempty keep each line
// compact and contain only the relevant variant. Pointer equality is
// intentional — Go marshals a nil pointer as JSON null, which omitempty
// then drops, leaving `{...,"type":"user_message"}` only on the wire.
type RolloutItem struct {
	Type             string            `json:"type"`
	SessionMeta      *SessionMeta      `json:"session_meta,omitempty"`
	UserMessage      *UserMessage      `json:"user_message,omitempty"`
	AssistantMessage *AssistantMessage `json:"assistant_message,omitempty"`
	ToolCall         *ToolCall         `json:"tool_call,omitempty"`
	ToolResult       *ToolResult       `json:"tool_result,omitempty"`
	TurnMeta         *TurnMeta         `json:"turn_meta,omitempty"`
}

// SessionMeta is the per-session header emitted once at file open. Carries
// provider/model so a reader can rebuild the session without out-of-band
// metadata.
type SessionMeta struct {
	SessionID    string    `json:"session_id"`
	ThreadID     string    `json:"thread_id"`
	ProviderName string    `json:"provider_name,omitempty"`
	Model        string    `json:"model,omitempty"`
	StartedAt    time.Time `json:"started_at"`
}

// UserMessage is a user-role message persisted to the rollout.
type UserMessage struct {
	Role    string    `json:"role"` // always "user"
	Content string    `json:"content"`
	At      time.Time `json:"at"`
}

// AssistantMessage is an assistant-role message persisted to the rollout.
// Tool calls are tracked via separate ToolCall items, not embedded here,
// so the file can replay tool_call and tool_result as paired events.
type AssistantMessage struct {
	Role    string    `json:"role"` // always "assistant"
	Content string    `json:"content"`
	At      time.Time `json:"at"`
}

// ToolCall is one tool invocation request, persisted BEFORE the tool runs.
// Args is raw JSON so it survives schema evolution without round-trip
// loss (the provider layer can ship richer argument shapes without
// touching the rollout schema).
type ToolCall struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

// ToolResult is one tool execution outcome, persisted AFTER the tool
// returns. Exactly one of Output / Error is meaningful per call (Output on
// success, Error on failure). The pair ToolCall[id] → ToolResult[id] is
// the retry dedup key — on restart, the resume loop skips tools whose
// result is already on disk.
type ToolResult struct {
	ID     string          `json:"id"`
	Output json.RawMessage `json:"output,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// TurnMeta is the per-turn summary emitted at end of turn. Status uses
// the same vocabulary as internal/appserver TurnStatus values.
type TurnMeta struct {
	Status       string     `json:"status"` // "completed" / "failed" / "interrupted"
	FinishReason string     `json:"finish_reason"`
	TokenUsage   TokenUsage `json:"token_usage"`
}

// TokenUsage mirrors providers.TokenUsage but with explicit JSON tags for
// JSONL stability. The providers type uses Go field names only and is
// intended for in-process use; this struct is the wire-stable form.
type TokenUsage struct {
	InputTokens         int `json:"input_tokens"`
	OutputTokens        int `json:"output_tokens"`
	CacheCreationTokens int `json:"cache_creation_tokens,omitempty"`
	CacheReadTokens     int `json:"cache_read_tokens,omitempty"`
}
