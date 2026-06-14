package appserver

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/harness"
	"github.com/blueberrycongee/wuu/internal/statepath"
	"github.com/blueberrycongee/wuu/internal/workflow"
	"github.com/blueberrycongee/wuu/internal/worktree"
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

func TestLoopWorktreeReviewUsesManagedWorktreeManager(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu-state")
	initLoopHandlerGitRepo(t, rt.RootDir)
	manager, err := worktree.NewManager(rt.RootDir, statepath.WorktreeRoot(rt.StateDir))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	lease, err := manager.CreateLease(worktree.LeaseOptions{
		SessionID: "thread-1",
		TaskID:    "task-1",
		AgentID:   "agent-1",
	})
	if err != nil {
		t.Fatalf("CreateLease: %v", err)
	}
	defer manager.Cleanup(&worktree.Worktree{Path: lease.Path})
	if err := os.WriteFile(filepath.Join(lease.Path, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatalf("write worktree file: %v", err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	raw := `{"id":"review","method":"loop/worktree/review","params":{"worktree_path":` + quoteLoopHandlerJSON(lease.Path) + `}}`
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("loop/worktree/review: %v", err)
	}

	msgs := parseOutput(t, out.String())
	msg := responseByID(t, msgs, "review")
	result := remarshal[LoopWorktreeReviewResult](t, msg["result"])
	if result.Review.WorktreePath != lease.Path || !result.Review.Status.Dirty {
		t.Fatalf("unexpected worktree review: %+v", result.Review)
	}
	if !strings.Contains(result.Review.Diff, "+changed") {
		t.Fatalf("review diff missing worktree edit:\n%s", result.Review.Diff)
	}
	if !result.Review.MergePreview.CanApply {
		t.Fatalf("merge preview should be clean: %+v", result.Review.MergePreview)
	}
}

func TestLoopWorktreeReviewRejectsUnmanagedPath(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu-state")
	initLoopHandlerGitRepo(t, rt.RootDir)

	out := &lockedBuffer{}
	srv := New(rt, out)
	raw := `{"id":"review","method":"loop/worktree/review","params":{"worktree_path":` + quoteLoopHandlerJSON(rt.RootDir) + `}}`
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("loop/worktree/review: %v", err)
	}
	msgs := parseOutput(t, out.String())
	msg := responseByID(t, msgs, "review")
	if msg["error"] == nil {
		t.Fatalf("expected unmanaged path error, got %+v", msg)
	}
	if !strings.Contains(string(remarshalLoopHandlerRaw(t, msg["error"])), "outside managed root") {
		t.Fatalf("unexpected unmanaged path error: %+v", msg["error"])
	}
}

func TestLoopWorktreeCleanupRequiresApprovalAndRemovesCleanWorktree(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu-state")
	initLoopHandlerGitRepo(t, rt.RootDir)
	manager, err := worktree.NewManager(rt.RootDir, statepath.WorktreeRoot(rt.StateDir))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	lease, err := manager.CreateLease(worktree.LeaseOptions{
		SessionID: "thread-1",
		TaskID:    "cleanup",
	})
	if err != nil {
		t.Fatalf("CreateLease: %v", err)
	}
	defer manager.Cleanup(&worktree.Worktree{Path: lease.Path})

	out := &lockedBuffer{}
	srv := New(rt, out)
	raw := `{"id":"cleanup-denied","method":"loop/worktree/cleanup","params":{"worktree_path":` + quoteLoopHandlerJSON(lease.Path) + `}}`
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("loop/worktree/cleanup denied: %v", err)
	}
	denied := responseByID(t, parseOutput(t, out.String()), "cleanup-denied")
	if denied["error"] == nil || !strings.Contains(string(remarshalLoopHandlerRaw(t, denied["error"])), "confirm_user_approved") {
		t.Fatalf("expected approval error, got %+v", denied)
	}
	if _, statErr := os.Stat(lease.Path); statErr != nil {
		t.Fatalf("unapproved cleanup should not remove worktree: %v", statErr)
	}

	out = &lockedBuffer{}
	srv = New(rt, out)
	raw = `{"id":"cleanup","method":"loop/worktree/cleanup","params":{"worktree_path":` + quoteLoopHandlerJSON(lease.Path) + `,"confirm_user_approved":true,"confirm_remove_clean_worktree":true}}`
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("loop/worktree/cleanup: %v", err)
	}
	msg := responseByID(t, parseOutput(t, out.String()), "cleanup")
	result := remarshal[LoopWorktreeCleanupResult](t, msg["result"])
	if !result.Cleanup.Removed || result.Cleanup.Kept || result.Cleanup.StatusBefore.Dirty {
		t.Fatalf("unexpected cleanup result: %+v", result.Cleanup)
	}
	if _, statErr := os.Stat(lease.Path); !os.IsNotExist(statErr) {
		t.Fatalf("clean worktree should be removed, stat err=%v", statErr)
	}
}

func TestLoopWorktreeRollbackRequiresApprovalAndDiscardsChanges(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu-state")
	initLoopHandlerGitRepo(t, rt.RootDir)
	manager, err := worktree.NewManager(rt.RootDir, statepath.WorktreeRoot(rt.StateDir))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	lease, err := manager.CreateLease(worktree.LeaseOptions{
		SessionID: "thread-1",
		TaskID:    "rollback",
	})
	if err != nil {
		t.Fatalf("CreateLease: %v", err)
	}
	defer manager.Cleanup(&worktree.Worktree{Path: lease.Path})
	if err := os.WriteFile(filepath.Join(lease.Path, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatalf("write tracked change: %v", err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	raw := `{"id":"rollback-denied","method":"loop/worktree/rollback","params":{"worktree_path":` + quoteLoopHandlerJSON(lease.Path) + `}}`
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("loop/worktree/rollback denied: %v", err)
	}
	denied := responseByID(t, parseOutput(t, out.String()), "rollback-denied")
	if denied["error"] == nil || !strings.Contains(string(remarshalLoopHandlerRaw(t, denied["error"])), "confirm_discard_worktree_changes") {
		t.Fatalf("expected rollback approval error, got %+v", denied)
	}
	data, err := os.ReadFile(filepath.Join(lease.Path, "README.md"))
	if err != nil {
		t.Fatalf("read README after denied rollback: %v", err)
	}
	if string(data) != "changed\n" {
		t.Fatalf("denied rollback should keep worktree edit, got %q", string(data))
	}

	out = &lockedBuffer{}
	srv = New(rt, out)
	raw = `{"id":"rollback","method":"loop/worktree/rollback","params":{"worktree_path":` + quoteLoopHandlerJSON(lease.Path) + `,"confirm_user_approved":true,"confirm_discard_worktree_changes":true}}`
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("loop/worktree/rollback: %v", err)
	}
	msg := responseByID(t, parseOutput(t, out.String()), "rollback")
	result := remarshal[LoopWorktreeRollbackResult](t, msg["result"])
	if !result.Rollback.RolledBack || !result.Rollback.StatusBefore.Dirty || result.Rollback.StatusAfter.Dirty {
		t.Fatalf("unexpected rollback result: %+v", result.Rollback)
	}
	data, err = os.ReadFile(filepath.Join(lease.Path, "README.md"))
	if err != nil {
		t.Fatalf("read README after rollback: %v", err)
	}
	if string(data) != "hello\n" {
		t.Fatalf("rollback should restore tracked file, got %q", string(data))
	}
}

func initLoopHandlerGitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "init")
}

func quoteLoopHandlerJSON(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func remarshalLoopHandlerRaw(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal value: %v", err)
	}
	return data
}
