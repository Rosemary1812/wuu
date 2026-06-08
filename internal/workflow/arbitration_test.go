package workflow

import "testing"

func TestAnalyzeTeamArbitrationClear(t *testing.T) {
	got := AnalyzeTeamArbitration([]AgentRun{{
		ID:         "agent-a",
		Status:     AgentRunStateCompleted,
		ReportPath: "reports/agent-a.md",
	}})
	if got.Status != "clear" {
		t.Fatalf("Status = %q, want clear: %+v", got.Status, got)
	}
	if len(got.NextActions) != 1 {
		t.Fatalf("expected clear next action: %+v", got.NextActions)
	}
}

func TestAnalyzeTeamArbitrationFindsIssues(t *testing.T) {
	got := AnalyzeTeamArbitration([]AgentRun{
		{
			ID:           "agent-open",
			Status:       AgentRunStateRunning,
			ChangedFiles: []string{"shared.go"},
		},
		{
			ID:            "agent-missing",
			Status:        AgentRunStateCompleted,
			ReportMissing: true,
			ChangedFiles:  []string{"shared.go"},
		},
		{
			ID:     "agent-failed",
			Status: AgentRunStateFailed,
		},
	})

	if got.Status != "attention_required" {
		t.Fatalf("Status = %q, want attention_required: %+v", got.Status, got)
	}
	if len(got.OpenAgentRuns) != 1 || got.OpenAgentRuns[0] != "agent-open" {
		t.Fatalf("unexpected open runs: %+v", got.OpenAgentRuns)
	}
	if len(got.MissingReports) != 1 || got.MissingReports[0] != "agent-missing" {
		t.Fatalf("unexpected missing reports: %+v", got.MissingReports)
	}
	if len(got.FailedAgentRuns) != 1 || got.FailedAgentRuns[0] != "agent-failed" {
		t.Fatalf("unexpected failed runs: %+v", got.FailedAgentRuns)
	}
	if len(got.ChangedFileOverlaps) != 1 ||
		got.ChangedFileOverlaps[0].File != "shared.go" ||
		len(got.ChangedFileOverlaps[0].AgentRunIDs) != 2 {
		t.Fatalf("unexpected changed-file overlaps: %+v", got.ChangedFileOverlaps)
	}
	if len(got.NextActions) != 4 {
		t.Fatalf("expected issue-specific next actions: %+v", got.NextActions)
	}
}
