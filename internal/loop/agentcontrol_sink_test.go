package loop

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/agentcontrol"
)

func TestAgentControlFailureSinkWritesLoopFailure(t *testing.T) {
	root := t.TempDir()
	store := NewStore(filepath.Join(root, ".loop"))
	store.SetClock(fixedClock())
	if _, err := store.Init(Spec{
		ID:   "loop-agentcontrol",
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
