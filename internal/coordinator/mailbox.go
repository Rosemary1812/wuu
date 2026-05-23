package coordinator

import (
	"encoding/json"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/subagent"
)

const AgentMailboxMessageName = "wuu_agent_mailbox"

type AgentMailboxMessage struct {
	Type         string    `json:"type"`
	AgentID      string    `json:"agent_id"`
	AgentPath    string    `json:"agent_path,omitempty"`
	TaskName     string    `json:"task_name,omitempty"`
	AgentType    string    `json:"agent_type,omitempty"`
	Status       string    `json:"status"`
	Description  string    `json:"description,omitempty"`
	Result       string    `json:"result,omitempty"`
	Error        string    `json:"error,omitempty"`
	ErrorClass   string    `json:"error_class,omitempty"`
	InputTokens  int       `json:"input_tokens,omitempty"`
	OutputTokens int       `json:"output_tokens,omitempty"`
	StartedAt    time.Time `json:"started_at,omitempty"`
	CompletedAt  time.Time `json:"completed_at,omitempty"`
	DurationMS   int64     `json:"duration_ms,omitempty"`
}

func NewAgentMailboxMessage(snap subagent.SubAgentSnapshot) AgentMailboxMessage {
	msg := AgentMailboxMessage{
		Type:         "agent_result",
		AgentID:      snap.ID,
		AgentPath:    snap.AgentPath,
		TaskName:     snap.TaskName,
		AgentType:    snap.Type,
		Status:       string(snap.Status),
		Description:  snap.Description,
		Result:       snap.Result,
		InputTokens:  snap.InputTokens,
		OutputTokens: snap.OutputTokens,
		StartedAt:    snap.StartedAt,
		CompletedAt:  snap.CompletedAt,
	}
	if snap.Error != nil {
		msg.Error = snap.Error.Error()
		msg.ErrorClass = string(ClassifyError(snap.Error))
	}
	if !snap.CompletedAt.IsZero() && !snap.StartedAt.IsZero() {
		msg.DurationMS = snap.CompletedAt.Sub(snap.StartedAt).Milliseconds()
	}
	return msg
}

func FormatAgentMailboxMessage(snap subagent.SubAgentSnapshot) string {
	data, err := json.Marshal(NewAgentMailboxMessage(snap))
	if err != nil {
		return `{"type":"agent_result","error":"marshal failed"}`
	}
	return string(data)
}

func AgentMailboxChatMessage(snap subagent.SubAgentSnapshot) providers.ChatMessage {
	return providers.ChatMessage{
		Role:    "user",
		Name:    AgentMailboxMessageName,
		Content: FormatAgentMailboxMessage(snap),
	}
}
