package agentcontrol

import (
	"strings"
	"time"
)

type FailureSink interface {
	RecordAgentFailure(AgentFailure) error
}

type AgentFailure struct {
	Source       string
	TaskID       string
	RunID        string
	AgentID      string
	AgentPath    string
	Outcome      string
	Message      string
	ReportPath   string
	Blockers     []string
	ChangedFiles []string
	Verification []string
	CreatedAt    time.Time
}

func (c *AgentControl) recordAgentFailure(failure AgentFailure) error {
	if c == nil || c.failureSink == nil {
		return nil
	}
	if failure.CreatedAt.IsZero() {
		failure.CreatedAt = time.Now().UTC()
	}
	failure.Source = strings.TrimSpace(failure.Source)
	failure.TaskID = strings.TrimSpace(failure.TaskID)
	failure.RunID = strings.TrimSpace(failure.RunID)
	failure.AgentID = strings.TrimSpace(failure.AgentID)
	failure.AgentPath = strings.TrimSpace(failure.AgentPath)
	failure.Outcome = strings.TrimSpace(failure.Outcome)
	failure.Message = strings.TrimSpace(failure.Message)
	failure.ReportPath = strings.TrimSpace(failure.ReportPath)
	if failure.Message == "" {
		failure.Message = strings.Join(trimStringSlice(failure.Blockers), "; ")
	}
	if failure.Message == "" {
		failure.Message = failure.Outcome
	}
	return c.failureSink.RecordAgentFailure(failure)
}

func agentReportNeedsFailureSync(outcome string, blockers []string) bool {
	if len(trimStringSlice(blockers)) > 0 {
		return true
	}
	return harnessStatusFromReportOutcome(outcome) == "failed"
}
