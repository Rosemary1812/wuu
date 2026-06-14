package loop

import (
	"fmt"
	"strings"

	"github.com/blueberrycongee/wuu/internal/agentcontrol"
)

type AgentControlFailureSink struct {
	Store *Store
}

func NewAgentControlFailureSink(store *Store) AgentControlFailureSink {
	return AgentControlFailureSink{Store: store}
}

func (s AgentControlFailureSink) RecordAgentFailure(in agentcontrol.AgentFailure) error {
	if s.Store == nil {
		return fmt.Errorf("loop agent failure sink requires a store")
	}
	source := strings.TrimSpace(in.Source)
	if source == "" {
		source = "agentcontrol"
	}
	sourceID := agentFailureSourceID(in)
	message := firstSnapshotText(in.Message, strings.Join(in.Blockers, "; "), in.Outcome)
	if message == "" {
		message = "agent failure"
	}
	_, err := s.Store.AddFailure(Failure{
		Step:      StepExecution,
		Kind:      failureKind(source, firstSnapshotText(in.Outcome, "failed")),
		Source:    source,
		SourceID:  sourceID,
		Message:   message,
		Artifact:  in.ReportPath,
		CreatedAt: in.CreatedAt,
	})
	return err
}

func agentFailureSourceID(in agentcontrol.AgentFailure) string {
	parts := []string{
		strings.TrimSpace(in.TaskID),
		strings.TrimSpace(in.RunID),
		strings.TrimSpace(in.ReportPath),
	}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		return strings.TrimSpace(in.AgentID)
	}
	return strings.Join(out, ":")
}
