package agentcontrol

import (
	"time"

	"github.com/blueberrycongee/wuu/internal/agentthread"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/subagent"
)

type AgentMailboxMessage struct {
	Type         string    `json:"type"`
	TaskID       string    `json:"task_id,omitempty"`
	AgentID      string    `json:"agent_id"`
	AgentPath    string    `json:"agent_path,omitempty"`
	ParentID     string    `json:"parent_id,omitempty"`
	TaskName     string    `json:"task_name,omitempty"`
	AgentType    string    `json:"agent_type,omitempty"`
	Status       string    `json:"status"`
	Description  string    `json:"description,omitempty"`
	Result       string    `json:"result,omitempty"`
	Error        string    `json:"error,omitempty"`
	ErrorClass   string    `json:"error_class,omitempty"`
	InputTokens  int       `json:"input_tokens,omitempty"`
	OutputTokens int       `json:"output_tokens,omitempty"`
	ReportPath   string    `json:"report_path,omitempty"`
	Artifacts    []string  `json:"artifacts,omitempty"`
	StartedAt    time.Time `json:"started_at,omitempty"`
	CompletedAt  time.Time `json:"completed_at,omitempty"`
	DurationMS   int64     `json:"duration_ms,omitempty"`
}

func NewAgentMailboxMessage(snap subagent.SubAgentSnapshot) AgentMailboxMessage {
	return NewAgentMailboxMessageWithReport(snap, "", nil)
}

func NewAgentMailboxMessageWithReport(snap subagent.SubAgentSnapshot, reportPath string, artifacts []string) AgentMailboxMessage {
	msg := AgentMailboxMessage{
		Type:         "agent_result",
		TaskID:       snap.ID,
		AgentID:      snap.ID,
		AgentPath:    snap.AgentPath,
		ParentID:     snap.ParentID,
		TaskName:     snap.TaskName,
		AgentType:    snap.Type,
		Status:       string(snap.Status),
		Description:  snap.Description,
		Result:       snap.Result,
		InputTokens:  snap.InputTokens,
		OutputTokens: snap.OutputTokens,
		ReportPath:   reportPath,
		Artifacts:    artifacts,
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
	content := agentthread.SubagentNotificationContent(snap.AgentPath, NewAgentMailboxMessage(snap))
	return agentthread.NewInterAgentCommunication(
		parseAgentPathOrRoot(snap.AgentPath),
		agentthread.RootAgentPath(),
		content,
		false,
	).String()
}

func AgentMailboxChatMessage(snap subagent.SubAgentSnapshot) providers.ChatMessage {
	return providers.ChatMessage{
		Role:    "assistant",
		Content: FormatAgentMailboxMessage(snap),
	}
}
