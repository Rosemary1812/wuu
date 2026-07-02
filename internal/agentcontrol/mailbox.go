package agentcontrol

import (
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/agentthread"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/subagent"
)

type AgentMailboxMessage struct {
	Type            string    `json:"type"`
	ResultID        string    `json:"result_id,omitempty"`
	TaskID          string    `json:"task_id,omitempty"`
	AgentID         string    `json:"agent_id"`
	ParticipantID   string    `json:"participant_id,omitempty"`
	AgentPath       string    `json:"agent_path,omitempty"`
	ParentID        string    `json:"parent_id,omitempty"`
	TaskName        string    `json:"task_name,omitempty"`
	AgentType       string    `json:"agent_type,omitempty"`
	Status          string    `json:"status"`
	Description     string    `json:"description,omitempty"`
	Result          string    `json:"result,omitempty"`
	ResultPath      string    `json:"result_path,omitempty"`
	ResultBytes     int       `json:"result_bytes,omitempty"`
	ResultTruncated bool      `json:"result_truncated,omitempty"`
	Error           string    `json:"error,omitempty"`
	ErrorClass      string    `json:"error_class,omitempty"`
	InputTokens     int       `json:"input_tokens,omitempty"`
	OutputTokens    int       `json:"output_tokens,omitempty"`
	ReportPath      string    `json:"report_path,omitempty"`
	ReportMissing   bool      `json:"report_missing,omitempty"`
	Artifacts       []string  `json:"artifacts,omitempty"`
	StartedAt       time.Time `json:"started_at,omitempty"`
	CompletedAt     time.Time `json:"completed_at,omitempty"`
	DurationMS      int64     `json:"duration_ms,omitempty"`
}

func NewAgentMailboxMessage(snap subagent.SubAgentSnapshot) AgentMailboxMessage {
	return NewAgentMailboxMessageWithReport(snap, "", nil)
}

func NewAgentMailboxMessageWithReport(snap subagent.SubAgentSnapshot, reportPath string, artifacts []string) AgentMailboxMessage {
	ref := AgentResultReference{}
	if strings.TrimSpace(snap.Result) != "" {
		preview, truncated := previewAgentResult(snap.Result)
		ref = AgentResultReference{
			Preview:   preview,
			Bytes:     len([]byte(snap.Result)),
			Truncated: truncated,
		}
	}
	return NewAgentMailboxMessageWithReportAndResult(snap, reportPath, artifacts, ref)
}

func NewAgentMailboxMessageWithReportAndResult(snap subagent.SubAgentSnapshot, reportPath string, artifacts []string, ref AgentResultReference) AgentMailboxMessage {
	msg := AgentMailboxMessage{
		Type:            "agent_result",
		ResultID:        agentResultDeliveryID(snap),
		TaskID:          snap.ID,
		AgentID:         snap.ID,
		ParticipantID:   snap.ParticipantID,
		AgentPath:       snap.AgentPath,
		ParentID:        snap.ParentID,
		TaskName:        snap.TaskName,
		AgentType:       snap.Type,
		Status:          string(snap.Status),
		Description:     snap.Description,
		Result:          ref.Preview,
		ResultPath:      ref.Path,
		ResultBytes:     ref.Bytes,
		ResultTruncated: ref.Truncated,
		InputTokens:     snap.InputTokens,
		OutputTokens:    snap.OutputTokens,
		ReportPath:      reportPath,
		ReportMissing:   snap.Status == subagent.StatusCompleted && reportPath == "",
		Artifacts:       artifacts,
		StartedAt:       snap.StartedAt,
		CompletedAt:     snap.CompletedAt,
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
