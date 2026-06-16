package goal

import (
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/agentcontrol"
)

func TestAgentControlFailureSinkWritesGoalFailure(t *testing.T) {
	store := NewStore(goalTestDir(t, "goal-agentcontrol"))
	store.SetClock(fixedClock())
	if _, err := store.Init(Spec{
		ID:   "goal-agentcontrol",
		Goal: "capture agentcontrol failures",
	}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	sink := NewAgentControlFailureSink(store)
	err := sink.RecordAgentFailure(agentcontrol.AgentFailure{
		Source:     "harness_report",
		TaskID:     "agent-1",
		RunID:      "agent-1-run",
		Outcome:    "stuck",
		Message:    "tests failed",
		ReportPath: "harness/reports/agent-1.md",
		CreatedAt:  time.Date(2026, 6, 14, 1, 2, 3, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("RecordAgentFailure: %v", err)
	}
	if err := sink.RecordAgentFailure(agentcontrol.AgentFailure{
		Source:     "harness_report",
		TaskID:     "agent-1",
		RunID:      "agent-1-run",
		Outcome:    "stuck",
		Message:    "tests failed",
		ReportPath: "harness/reports/agent-1.md",
	}); err != nil {
		t.Fatalf("RecordAgentFailure duplicate: %v", err)
	}
	state, err := store.LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if len(state.Failures) != 1 || state.Failures[0].Source != "harness_report" || state.Failures[0].SourceID != "agent-1:agent-1-run:harness/reports/agent-1.md" {
		t.Fatalf("unexpected synced failures: %+v", state.Failures)
	}
	failures, err := store.FailureContext()
	if err != nil {
		t.Fatalf("FailureContext: %v", err)
	}
	if !strings.Contains(failures, "harness_report_stuck") || !strings.Contains(failures, "artifact=harness/reports/agent-1.md") {
		t.Fatalf("failure context missing source details:\n%s", failures)
	}
}

func TestAgentControlFailureSinkWritesGoalReportProgress(t *testing.T) {
	store := NewStore(goalTestDir(t, "goal-agent-report"))
	store.SetClock(fixedClock())
	if _, err := store.Init(Spec{
		ID:   "goal-agent-report",
		Goal: "capture agent report",
	}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	sink := NewAgentControlFailureSink(nil)
	err := sink.RecordAgentReport(agentcontrol.AgentReport{
		Source:       "harness_report",
		TaskID:       "agent-2",
		RunID:        "agent-2-run",
		GoalID:       "goal-agent-report",
		GoalDir:      store.Dir(),
		Outcome:      "completed",
		Summary:      "Implemented the worker change.",
		ReportPath:   "harness/reports/agent-2.md",
		ChangedFiles: []string{"internal/goal/report_sync.go"},
		Verification: []string{"go test ./internal/goal"},
		Artifacts:    []string{"harness/artifacts/agent-2.patch"},
		NextSteps:    []string{"review worker diff"},
		CreatedAt:    time.Date(2026, 6, 14, 1, 2, 3, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("RecordAgentReport: %v", err)
	}
	if err := sink.RecordAgentReport(agentcontrol.AgentReport{
		Source:       "harness_report",
		TaskID:       "agent-2",
		RunID:        "agent-2-run",
		GoalDir:      store.Dir(),
		Outcome:      "completed",
		Summary:      "Implemented the worker change.",
		ReportPath:   "harness/reports/agent-2.md",
		ChangedFiles: []string{"internal/goal/report_sync.go"},
		Verification: []string{"go test ./internal/goal"},
		Artifacts:    []string{"harness/artifacts/agent-2.patch"},
	}); err != nil {
		t.Fatalf("RecordAgentReport duplicate: %v", err)
	}
	state, err := store.LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if len(state.Progress) != 1 || state.Progress[0].SourceID != "agent-2:agent-2-run:harness/reports/agent-2.md" {
		t.Fatalf("unexpected progress: %+v", state.Progress)
	}
	if len(state.ModifiedFiles) != 1 || state.ModifiedFiles[0] != "internal/goal/report_sync.go" {
		t.Fatalf("unexpected modified files: %+v", state.ModifiedFiles)
	}
	if len(state.Artifacts) != 2 {
		t.Fatalf("expected report and evidence artifact refs, got %+v", state.Artifacts)
	}
	if len(state.TestResults) != 1 || !state.TestResults[0].Passed || !strings.Contains(state.TestResults[0].Output, "go test ./internal/goal") {
		t.Fatalf("unexpected test results: %+v", state.TestResults)
	}
	if len(state.NextSteps) != 1 || state.NextSteps[0] != "review worker diff" {
		t.Fatalf("unexpected next steps: %+v", state.NextSteps)
	}
}
