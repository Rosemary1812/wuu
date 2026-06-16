package loop

import (
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
	store, ok, err := s.resolveStore(in.LoopDir)
	if err != nil || !ok {
		return err
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
	_, err = store.AddFailure(Failure{
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

func (s AgentControlFailureSink) RecordAgentReport(in agentcontrol.AgentReport) error {
	store, ok, err := s.resolveStore(in.LoopDir)
	if err != nil || !ok {
		return err
	}
	source := strings.TrimSpace(in.Source)
	if source == "" {
		source = "harness_report"
	}
	_, err = store.RecordExternalReport(ExternalReport{
		Source:       source,
		SourceID:     agentReportSourceID(in),
		Outcome:      in.Outcome,
		Summary:      in.Summary,
		ReportPath:   in.ReportPath,
		Blockers:     in.Blockers,
		ChangedFiles: in.ChangedFiles,
		Verification: in.Verification,
		Artifacts:    in.Artifacts,
		NextSteps:    in.NextSteps,
		CreatedAt:    in.CreatedAt,
	})
	return err
}

func (s AgentControlFailureSink) resolveStore(loopDir string) (*Store, bool, error) {
	return resolveExternalLoopStore(s.Store, loopDir, "agentcontrol")
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

func agentReportSourceID(in agentcontrol.AgentReport) string {
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
