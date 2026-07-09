package goal

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/harness"
)

func TestSnapshotSystemProjectsGoalAndHarnessAttention(t *testing.T) {
	root := t.TempDir()
	goalRoot := filepath.Join(root, "state", "goals")
	goalStore := NewStore(filepath.Join(goalRoot, "goal-approval"))
	if _, err := goalStore.Init(Spec{ID: "goal-approval", Goal: "merge worker result"}); err != nil {
		t.Fatalf("Init goal: %v", err)
	}
	if _, _, err := goalStore.RequestApproval(ApprovalRequest{
		ID:              "approval-1",
		Title:           "Approve merge",
		RequestedAction: "merge worktree",
		Source:          "worktree",
		SourceID:        "worker-1",
	}); err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}

	harnessStore := harness.NewStore(filepath.Join(root, "harness"))
	if err := harnessStore.UpsertTask(harness.Task{
		ID:        "task-1",
		Role:      "worker",
		Status:    harness.TaskStatusFailed,
		Error:     "worker crashed",
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertTask: %v", err)
	}
	if _, err := harnessStore.SubmitReport(harness.Report{
		ID:        "report-1",
		TaskID:    "task-1",
		Outcome:   "partial",
		Summary:   "partially complete",
		Blockers:  []string{"tests failed"},
		Artifacts: []string{"artifact.md"},
	}); err != nil {
		t.Fatalf("SubmitReport: %v", err)
	}

	snapshot := SnapshotSystem(SnapshotOptions{
		GoalRoot:     goalRoot,
		HarnessStore: harnessStore,
		Now:          fixedClock(),
	})
	if len(snapshot.Goals) != 1 || len(snapshot.Approvals) != 1 {
		t.Fatalf("goal approvals missing from snapshot: %+v", snapshot)
	}
	if len(snapshot.Harness.Tasks) != 1 || len(snapshot.Harness.Reports) != 1 {
		t.Fatalf("Harness snapshot = %+v", snapshot.Harness)
	}
	if !attentionContains(snapshot.Attention, "harness", string(harness.TaskStatusFailed)) {
		t.Fatalf("missing harness failure attention: %+v", snapshot.Attention)
	}
	if !attentionContains(snapshot.Attention, "harness_report", "partial") {
		t.Fatalf("missing harness report attention: %+v", snapshot.Attention)
	}
	if !attentionContains(snapshot.Attention, "goal_approval", string(ApprovalStatusPending)) {
		t.Fatalf("missing goal approval attention: %+v", snapshot.Attention)
	}
}

func attentionContains(items []AttentionItem, source, status string) bool {
	for _, item := range items {
		if item.Source == source && item.Status == status {
			return true
		}
	}
	return false
}
