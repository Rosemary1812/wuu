package loop

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/harness"
	"github.com/blueberrycongee/wuu/internal/workflow"
)

func TestSnapshotSystemProjectsWorkflowAndHarnessAttention(t *testing.T) {
	root := t.TempDir()
	loopRoot := filepath.Join(root, "state", "loops")
	loopStore := NewStore(filepath.Join(loopRoot, "loop-approval"))
	if _, err := loopStore.Init(Spec{ID: "loop-approval", Goal: "merge worker result"}); err != nil {
		t.Fatalf("Init loop: %v", err)
	}
	if _, _, err := loopStore.RequestApproval(ApprovalRequest{
		ID:              "approval-1",
		Title:           "Approve merge",
		RequestedAction: "merge worktree",
		Source:          "worktree",
		SourceID:        "worker-1",
	}); err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}

	workflowStore := workflow.NewStore(filepath.Join(root, "state"))
	run, err := workflowStore.CreateRun(workflow.Run{
		ID:             "wf-1",
		DefinitionName: "feature-delivery",
		Status:         workflow.RunStateRunning,
		Phases: []workflow.Phase{{
			ID:     "implementation",
			Name:   "Implementation",
			Status: workflow.PhaseStateRunning,
		}},
	})
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	for _, agent := range []workflow.AgentRun{
		{
			ID:            "worker-1",
			WorkflowRunID: run.ID,
			PhaseID:       "implementation",
			Status:        workflow.AgentRunStateCompleted,
			ReportPath:    "worker-1.md",
			ChangedFiles:  []string{"app.go"},
		},
		{
			ID:            "worker-2",
			WorkflowRunID: run.ID,
			PhaseID:       "implementation",
			Status:        workflow.AgentRunStateCompleted,
			ReportMissing: true,
			ChangedFiles:  []string{"app.go"},
		},
	} {
		if err := workflowStore.UpsertAgentRun(agent); err != nil {
			t.Fatalf("UpsertAgentRun(%s): %v", agent.ID, err)
		}
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
		LoopRoot:      loopRoot,
		WorkflowStore: workflowStore,
		HarnessStore:  harnessStore,
		Now:           fixedClock(),
	})
	if len(snapshot.Loops) != 1 || len(snapshot.Approvals) != 1 {
		t.Fatalf("loop approvals missing from snapshot: %+v", snapshot)
	}
	if len(snapshot.Workflows) != 1 {
		t.Fatalf("Workflows = %+v", snapshot.Workflows)
	}
	if len(snapshot.Workflows[0].AgentRuns) != 2 {
		t.Fatalf("AgentRuns = %+v", snapshot.Workflows[0].AgentRuns)
	}
	if snapshot.Workflows[0].Arbitration.Status == "" {
		t.Fatalf("missing arbitration: %+v", snapshot.Workflows[0].Arbitration)
	}
	if len(snapshot.Harness.Tasks) != 1 || len(snapshot.Harness.Reports) != 1 {
		t.Fatalf("Harness snapshot = %+v", snapshot.Harness)
	}
	if !attentionContains(snapshot.Attention, "workflow_agent", "missing_report") {
		t.Fatalf("missing workflow missing-report attention: %+v", snapshot.Attention)
	}
	if !attentionContains(snapshot.Attention, "workflow_conflict", "changed_file_overlap") {
		t.Fatalf("missing workflow conflict attention: %+v", snapshot.Attention)
	}
	if !attentionContains(snapshot.Attention, "harness", string(harness.TaskStatusFailed)) {
		t.Fatalf("missing harness failure attention: %+v", snapshot.Attention)
	}
	if !attentionContains(snapshot.Attention, "harness_report", "partial") {
		t.Fatalf("missing harness report attention: %+v", snapshot.Attention)
	}
	if !attentionContains(snapshot.Attention, "loop_approval", string(ApprovalStatusPending)) {
		t.Fatalf("missing loop approval attention: %+v", snapshot.Attention)
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
