package appserver

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/harness"
	"github.com/blueberrycongee/wuu/internal/statepath"
	"github.com/blueberrycongee/wuu/internal/workflow"
)

func TestLoopSnapshotReturnsWorkflowAndThreadHarnessState(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu-state")

	workflowStore := workflow.NewStore(rt.StateDir)
	run, err := workflowStore.CreateRun(workflow.Run{
		ID:             "wf-1",
		DefinitionName: "delivery",
		Status:         workflow.RunStateRunning,
		LoopID:         "wf-1",
		LoopDir:        filepath.Join(rt.StateDir, "loops", "wf-1"),
	})
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if err := workflowStore.UpsertAgentRun(workflow.AgentRun{
		ID:            "worker-1",
		WorkflowRunID: run.ID,
		Status:        workflow.AgentRunStateCompleted,
		ChangedFiles:  []string{"internal/appserver/loop_handlers.go"},
	}); err != nil {
		t.Fatalf("UpsertAgentRun: %v", err)
	}

	harnessStore := harness.NewStore(filepath.Join(statepath.SessionArtifactDir(rt.StateDir, "thread-1"), "harness"))
	if err := harnessStore.UpsertTask(harness.Task{
		ID:        "task-1",
		Name:      "review",
		Role:      "reviewer",
		LoopID:    "wf-1",
		LoopDir:   filepath.Join(rt.StateDir, "loops", "wf-1"),
		Status:    harness.TaskStatusFailed,
		Error:     "review failed",
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertTask: %v", err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"loop/snapshot","params":{"thread_id":"thread-1"}}`)); err != nil {
		t.Fatalf("loop/snapshot: %v", err)
	}

	msgs := parseOutput(t, out.String())
	msg := responseByID(t, msgs, "1")
	result := remarshal[LoopSnapshotResult](t, msg["result"])
	if len(result.Snapshot.Workflows) != 1 || result.Snapshot.Workflows[0].ID != "wf-1" {
		t.Fatalf("workflow snapshot = %+v", result.Snapshot.Workflows)
	}
	if result.Snapshot.Workflows[0].LoopDir == "" {
		t.Fatalf("workflow snapshot missing loop dir: %+v", result.Snapshot.Workflows[0])
	}
	if len(result.Snapshot.Harness.Tasks) != 1 || result.Snapshot.Harness.Tasks[0].ID != "task-1" {
		t.Fatalf("harness snapshot = %+v", result.Snapshot.Harness)
	}
	if result.Snapshot.Harness.Tasks[0].LoopID != "wf-1" {
		t.Fatalf("harness task missing loop binding: %+v", result.Snapshot.Harness.Tasks[0])
	}
	if len(result.Snapshot.Attention) == 0 {
		t.Fatalf("expected failed harness task in attention: %+v", result.Snapshot)
	}
}
