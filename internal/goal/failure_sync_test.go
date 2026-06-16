package goal

import (
	"strings"
	"testing"
)

func TestSyncSnapshotFailuresRecordsExternalAttentionOnce(t *testing.T) {
	store := NewStore(goalTestDir(t, "goal-sync"))
	store.SetClock(fixedClock())
	if _, err := store.Init(Spec{
		ID:   "goal-sync",
		Goal: "sync external failures",
	}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	snapshot := SystemSnapshot{
		Attention: []AttentionItem{
			{
				Source:  "workflow_agent",
				ID:      "agent-a",
				Status:  "missing_report",
				Message: "worker did not submit report",
				Path:    "workflows/run-1/agents/agent-a.json",
			},
			{
				Source:  "harness_report",
				ID:      "report-a",
				Status:  "partial",
				Message: "tests failed",
				Path:    "harness/reports/agent-a.md",
			},
		},
	}
	result, err := SyncSnapshotFailures(store, snapshot)
	if err != nil {
		t.Fatalf("SyncSnapshotFailures: %v", err)
	}
	if result.Added != 2 || result.Skipped != 0 {
		t.Fatalf("unexpected first sync result: %+v", result)
	}
	result, err = SyncSnapshotFailures(store, snapshot)
	if err != nil {
		t.Fatalf("SyncSnapshotFailures second: %v", err)
	}
	if result.Added != 0 || result.Skipped != 2 {
		t.Fatalf("unexpected second sync result: %+v", result)
	}
	state, err := store.LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if len(state.Failures) != 2 {
		t.Fatalf("expected two failures, got %+v", state.Failures)
	}
	failures, err := store.FailureContext()
	if err != nil {
		t.Fatalf("FailureContext: %v", err)
	}
	for _, want := range []string{"workflow_agent_missing_report", "harness_report_partial", "source_id=agent-a:missing_report:workflows/run-1/agents/agent-a.json"} {
		if !strings.Contains(failures, want) {
			t.Fatalf("failure context missing %q:\n%s", want, failures)
		}
	}
}
