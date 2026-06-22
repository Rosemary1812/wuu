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

	goalrunner "github.com/blueberrycongee/wuu/internal/goal"
	"github.com/blueberrycongee/wuu/internal/harness"
	"github.com/blueberrycongee/wuu/internal/statepath"
	"github.com/blueberrycongee/wuu/internal/workflow"
	"github.com/blueberrycongee/wuu/internal/worktree"
)

func TestGoalSnapshotReturnsWorkflowAndThreadHarnessState(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu-state")

	workflowStore := workflow.NewStore(rt.StateDir)
	run, err := workflowStore.CreateRun(workflow.Run{
		ID:             "wf-1",
		DefinitionName: "delivery",
		Status:         workflow.RunStateRunning,
		GoalID:         "wf-1",
		GoalDir:        filepath.Join(rt.StateDir, "goals", "wf-1"),
	})
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if err := workflowStore.UpsertAgentRun(workflow.AgentRun{
		ID:            "worker-1",
		WorkflowRunID: run.ID,
		Status:        workflow.AgentRunStateCompleted,
		ChangedFiles:  []string{"internal/appserver/goal_handlers.go"},
	}); err != nil {
		t.Fatalf("UpsertAgentRun: %v", err)
	}

	harnessStore := harness.NewStore(filepath.Join(statepath.SessionArtifactDir(rt.StateDir, "thread-1"), "harness"))
	if err := harnessStore.UpsertTask(harness.Task{
		ID:        "task-1",
		Name:      "review",
		Role:      "reviewer",
		GoalID:    "wf-1",
		GoalDir:   filepath.Join(rt.StateDir, "goals", "wf-1"),
		Status:    harness.TaskStatusFailed,
		Error:     "review failed",
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertTask: %v", err)
	}
	goalStore := goalrunner.NewStore(filepath.Join(rt.StateDir, "goals", "wf-1"))
	if _, err := goalStore.Init(goalrunner.Spec{ID: "wf-1", Goal: "delivery goal"}); err != nil {
		t.Fatalf("Init goal: %v", err)
	}
	if _, _, err := goalStore.RequestApproval(goalrunner.ApprovalRequest{
		ID:              "approval-1",
		Title:           "Approve integration",
		RequestedAction: "merge worker diff",
	}); err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"goal/snapshot","params":{"thread_id":"thread-1"}}`)); err != nil {
		t.Fatalf("goal/snapshot: %v", err)
	}

	msgs := parseOutput(t, out.String())
	msg := responseByID(t, msgs, "1")
	result := remarshal[GoalSnapshotResult](t, msg["result"])
	if len(result.Snapshot.Workflows) != 1 || result.Snapshot.Workflows[0].ID != "wf-1" {
		t.Fatalf("workflow snapshot = %+v", result.Snapshot.Workflows)
	}
	if result.Snapshot.Workflows[0].GoalDir == "" {
		t.Fatalf("workflow snapshot missing goal dir: %+v", result.Snapshot.Workflows[0])
	}
	if len(result.Snapshot.Harness.Tasks) != 1 || result.Snapshot.Harness.Tasks[0].ID != "task-1" {
		t.Fatalf("harness snapshot = %+v", result.Snapshot.Harness)
	}
	if result.Snapshot.Harness.Tasks[0].GoalID != "wf-1" {
		t.Fatalf("harness task missing goal binding: %+v", result.Snapshot.Harness.Tasks[0])
	}
	if len(result.Snapshot.Attention) == 0 {
		t.Fatalf("expected failed harness task in attention: %+v", result.Snapshot)
	}
	if len(result.Snapshot.Approvals) != 1 || result.Snapshot.Approvals[0].ID != "approval-1" {
		t.Fatalf("approval snapshot = %+v", result.Snapshot.Approvals)
	}
}

func TestGoalWorktreeReviewUsesManagedWorktreeManager(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu-state")
	initGoalHandlerGitRepo(t, rt.RootDir)
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
	raw := `{"id":"review","method":"goal/worktree/review","params":{"worktree_path":` + quoteGoalHandlerJSON(lease.Path) + `}}`
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("goal/worktree/review: %v", err)
	}

	msgs := parseOutput(t, out.String())
	msg := responseByID(t, msgs, "review")
	result := remarshal[GoalWorktreeReviewResult](t, msg["result"])
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

func TestGoalWorktreeReviewRejectsUnmanagedPath(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu-state")
	initGoalHandlerGitRepo(t, rt.RootDir)

	out := &lockedBuffer{}
	srv := New(rt, out)
	raw := `{"id":"review","method":"goal/worktree/review","params":{"worktree_path":` + quoteGoalHandlerJSON(rt.RootDir) + `}}`
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("goal/worktree/review: %v", err)
	}
	msgs := parseOutput(t, out.String())
	msg := responseByID(t, msgs, "review")
	if msg["error"] == nil {
		t.Fatalf("expected unmanaged path error, got %+v", msg)
	}
	if !strings.Contains(string(remarshalGoalHandlerRaw(t, msg["error"])), "outside managed root") {
		t.Fatalf("unexpected unmanaged path error: %+v", msg["error"])
	}
}

func TestGoalWorktreeCleanupRequiresApprovalAndRemovesCleanWorktree(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu-state")
	initGoalHandlerGitRepo(t, rt.RootDir)
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
	raw := `{"id":"cleanup-denied","method":"goal/worktree/cleanup","params":{"worktree_path":` + quoteGoalHandlerJSON(lease.Path) + `}}`
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("goal/worktree/cleanup denied: %v", err)
	}
	denied := responseByID(t, parseOutput(t, out.String()), "cleanup-denied")
	if denied["error"] == nil || !strings.Contains(string(remarshalGoalHandlerRaw(t, denied["error"])), "confirm_user_approved") {
		t.Fatalf("expected approval error, got %+v", denied)
	}
	if _, statErr := os.Stat(lease.Path); statErr != nil {
		t.Fatalf("unapproved cleanup should not remove worktree: %v", statErr)
	}

	out = &lockedBuffer{}
	srv = New(rt, out)
	raw = `{"id":"cleanup","method":"goal/worktree/cleanup","params":{"worktree_path":` + quoteGoalHandlerJSON(lease.Path) + `,"confirm_user_approved":true,"confirm_remove_clean_worktree":true}}`
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("goal/worktree/cleanup: %v", err)
	}
	msg := responseByID(t, parseOutput(t, out.String()), "cleanup")
	result := remarshal[GoalWorktreeCleanupResult](t, msg["result"])
	if !result.Cleanup.Removed || result.Cleanup.Kept || result.Cleanup.StatusBefore.Dirty {
		t.Fatalf("unexpected cleanup result: %+v", result.Cleanup)
	}
	if _, statErr := os.Stat(lease.Path); !os.IsNotExist(statErr) {
		t.Fatalf("clean worktree should be removed, stat err=%v", statErr)
	}
}

func TestGoalWorktreeRollbackRequiresApprovalAndDiscardsChanges(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu-state")
	initGoalHandlerGitRepo(t, rt.RootDir)
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
	raw := `{"id":"rollback-denied","method":"goal/worktree/rollback","params":{"worktree_path":` + quoteGoalHandlerJSON(lease.Path) + `}}`
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("goal/worktree/rollback denied: %v", err)
	}
	denied := responseByID(t, parseOutput(t, out.String()), "rollback-denied")
	if denied["error"] == nil || !strings.Contains(string(remarshalGoalHandlerRaw(t, denied["error"])), "confirm_discard_worktree_changes") {
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
	raw = `{"id":"rollback","method":"goal/worktree/rollback","params":{"worktree_path":` + quoteGoalHandlerJSON(lease.Path) + `,"confirm_user_approved":true,"confirm_discard_worktree_changes":true}}`
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("goal/worktree/rollback: %v", err)
	}
	msg := responseByID(t, parseOutput(t, out.String()), "rollback")
	result := remarshal[GoalWorktreeRollbackResult](t, msg["result"])
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

func TestGoalWorktreeMergeRequiresApprovalAndAppliesDiff(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu-state")
	initGoalHandlerGitRepo(t, rt.RootDir)
	manager, err := worktree.NewManager(rt.RootDir, statepath.WorktreeRoot(rt.StateDir))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	lease, err := manager.CreateLease(worktree.LeaseOptions{
		SessionID: "thread-1",
		TaskID:    "merge",
	})
	if err != nil {
		t.Fatalf("CreateLease: %v", err)
	}
	defer manager.Cleanup(&worktree.Worktree{Path: lease.Path})
	if err := os.WriteFile(filepath.Join(lease.Path, "README.md"), []byte("merged\n"), 0o644); err != nil {
		t.Fatalf("write tracked change: %v", err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	raw := `{"id":"merge-denied","method":"goal/worktree/merge","params":{"worktree_path":` + quoteGoalHandlerJSON(lease.Path) + `}}`
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("goal/worktree/merge denied: %v", err)
	}
	denied := responseByID(t, parseOutput(t, out.String()), "merge-denied")
	if denied["error"] == nil || !strings.Contains(string(remarshalGoalHandlerRaw(t, denied["error"])), "confirm_apply_worktree_diff") {
		t.Fatalf("expected merge approval error, got %+v", denied)
	}
	data, err := os.ReadFile(filepath.Join(rt.RootDir, "README.md"))
	if err != nil {
		t.Fatalf("read README after denied merge: %v", err)
	}
	if string(data) != "hello\n" {
		t.Fatalf("denied merge should keep target file, got %q", string(data))
	}

	out = &lockedBuffer{}
	srv = New(rt, out)
	raw = `{"id":"merge","method":"goal/worktree/merge","params":{"worktree_path":` + quoteGoalHandlerJSON(lease.Path) + `,"confirm_user_approved":true,"confirm_apply_worktree_diff":true,"confirm_target_repo_mutation":true}}`
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("goal/worktree/merge: %v", err)
	}
	msg := responseByID(t, parseOutput(t, out.String()), "merge")
	result := remarshal[GoalWorktreeMergeResult](t, msg["result"])
	if !result.Merge.Applied || !result.Merge.Preview.CanApply || len(result.Merge.ChangedFiles) != 1 {
		t.Fatalf("unexpected merge result: %+v", result.Merge)
	}
	data, err = os.ReadFile(filepath.Join(rt.RootDir, "README.md"))
	if err != nil {
		t.Fatalf("read README after merge: %v", err)
	}
	if string(data) != "merged\n" {
		t.Fatalf("merge should update target file, got %q", string(data))
	}
}

func TestGoalApprovalResolveRequiresConfirmationAndUpdatesGoalState(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu-state")
	store := goalrunner.NewStore(filepath.Join(rt.StateDir, "goals", "goal-approval"))
	if _, err := store.Init(goalrunner.Spec{ID: "goal-approval", Goal: "resolve approval"}); err != nil {
		t.Fatalf("Init goal: %v", err)
	}
	if _, _, err := store.RequestApproval(goalrunner.ApprovalRequest{
		ID:              "approval-1",
		Title:           "Apply worktree diff",
		RequestedAction: "merge",
	}); err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	raw := `{"id":"approval-denied","method":"goal/approval/resolve","params":{"goal_id":"goal-approval","approval_id":"approval-1","approved":true}}`
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("goal/approval/resolve denied: %v", err)
	}
	denied := responseByID(t, parseOutput(t, out.String()), "approval-denied")
	if denied["error"] == nil || !strings.Contains(string(remarshalGoalHandlerRaw(t, denied["error"])), "confirm_user_approved") {
		t.Fatalf("expected approval confirmation error, got %+v", denied)
	}
	state, err := store.LoadState()
	if err != nil {
		t.Fatalf("LoadState after denied resolve: %v", err)
	}
	if len(state.Approvals) != 1 || state.Approvals[0].Status != goalrunner.ApprovalStatusPending {
		t.Fatalf("denied resolve should keep approval pending: %+v", state.Approvals)
	}

	out = &lockedBuffer{}
	srv = New(rt, out)
	raw = `{"id":"approval-path","method":"goal/approval/resolve","params":{"goal_id":"..","approval_id":"approval-1","approved":true,"confirm_user_approved":true}}`
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("goal/approval/resolve path guard: %v", err)
	}
	pathGuard := responseByID(t, parseOutput(t, out.String()), "approval-path")
	if pathGuard["error"] == nil || !strings.Contains(string(remarshalGoalHandlerRaw(t, pathGuard["error"])), "not a path") {
		t.Fatalf("expected goal id path guard error, got %+v", pathGuard)
	}

	out = &lockedBuffer{}
	srv = New(rt, out)
	raw = `{"id":"approval","method":"goal/approval/resolve","params":{"goal_id":"goal-approval","approval_id":"approval-1","approved":true,"resolved_by":"lead","resolution":"diff reviewed","confirm_user_approved":true}}`
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("goal/approval/resolve: %v", err)
	}
	msg := responseByID(t, parseOutput(t, out.String()), "approval")
	result := remarshal[GoalApprovalResolveResult](t, msg["result"])
	if result.Approval.Status != goalrunner.ApprovalStatusApproved || result.Approval.ResolvedBy != "lead" {
		t.Fatalf("unexpected approval result: %+v", result.Approval)
	}
	state, err = store.LoadState()
	if err != nil {
		t.Fatalf("LoadState after resolve: %v", err)
	}
	if state.NeedsHuman || state.Approvals[0].Status != goalrunner.ApprovalStatusApproved {
		t.Fatalf("resolve should update goal state: %+v", state)
	}
}

func initGoalHandlerGitRepo(t *testing.T, dir string) {
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

func quoteGoalHandlerJSON(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func remarshalGoalHandlerRaw(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal value: %v", err)
	}
	return data
}

func TestGoalActiveSummaryReturnsMostRecentNonTerminalGoal(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu-state")

	stateDir := rt.StateDir
	older := goalrunner.NewStore(statepath.GoalDir(stateDir, "older"))
	if _, err := older.Init(goalrunner.Spec{ID: "older", Goal: "older goal"}); err != nil {
		t.Fatalf("Init older: %v", err)
	}
	if _, err := older.AddProgress(goalrunner.StepResearch, "older progress"); err != nil {
		t.Fatalf("AddProgress older: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	newer := goalrunner.NewStore(statepath.GoalDir(stateDir, "newer"))
	if _, err := newer.Init(goalrunner.Spec{ID: "newer", Goal: "newer goal"}); err != nil {
		t.Fatalf("Init newer: %v", err)
	}
	if _, err := newer.AddProgress(goalrunner.StepExecution, "newer progress"); err != nil {
		t.Fatalf("AddProgress newer: %v", err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	if err := srv.handleLine(context.Background(), []byte(`{"id":"sum","method":"goal/active-summary"}`)); err != nil {
		t.Fatalf("goal/active-summary: %v", err)
	}
	msgs := parseOutput(t, out.String())
	msg := responseByID(t, msgs, "sum")
	result := remarshal[GoalActiveSummaryResult](t, msg["result"])
	if result.Summary == nil {
		t.Fatalf("expected active summary, got %+v", result)
	}
	if result.Summary.ID != "newer" {
		t.Fatalf("expected most-recent active goal id=newer, got %+v", result.Summary)
	}
	if result.Summary.Text != "newer goal" {
		t.Fatalf("summary text mismatch: %+v", result.Summary)
	}
	if result.Summary.Status != string(goalrunner.StatusRunning) {
		t.Fatalf("summary status mismatch: %+v", result.Summary)
	}
	if result.Summary.Step != string(goalrunner.StepExecution) {
		t.Fatalf("summary step mismatch: %+v", result.Summary)
	}
	if result.Summary.UpdatedAt == "" {
		t.Fatalf("summary updated_at empty: %+v", result.Summary)
	}
}

func TestGoalActiveSummarySkipsTerminalGoals(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu-state")

	store := goalrunner.NewStore(statepath.GoalDir(rt.StateDir, "done"))
	if _, err := store.Init(goalrunner.Spec{ID: "done", Goal: "done goal"}); err != nil {
		t.Fatalf("Init done: %v", err)
	}
	if _, err := store.SetStatus(goalrunner.StatusCompleted, goalrunner.StepSummary, "done"); err != nil {
		t.Fatalf("SetStatus done: %v", err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	if err := srv.handleLine(context.Background(), []byte(`{"id":"sum","method":"goal/active-summary"}`)); err != nil {
		t.Fatalf("goal/active-summary: %v", err)
	}
	msgs := parseOutput(t, out.String())
	msg := responseByID(t, msgs, "sum")
	result := remarshal[GoalActiveSummaryResult](t, msg["result"])
	if result.Summary != nil {
		t.Fatalf("expected nil summary when only terminal goal exists, got %+v", result.Summary)
	}
}

func TestGoalActiveSummaryCollapsesMultilineText(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu-state")

	store := goalrunner.NewStore(statepath.GoalDir(rt.StateDir, "multi"))
	if _, err := store.Init(goalrunner.Spec{ID: "multi", Goal: "first line\nsecond line\nthird"}); err != nil {
		t.Fatalf("Init multi: %v", err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	if err := srv.handleLine(context.Background(), []byte(`{"id":"sum","method":"goal/active-summary"}`)); err != nil {
		t.Fatalf("goal/active-summary: %v", err)
	}
	msgs := parseOutput(t, out.String())
	msg := responseByID(t, msgs, "sum")
	result := remarshal[GoalActiveSummaryResult](t, msg["result"])
	if result.Summary == nil {
		t.Fatalf("expected summary, got %+v", result)
	}
	if result.Summary.Text != "first line" {
		t.Fatalf("expected text collapsed to first line, got %+v", result.Summary)
	}
}

func TestGoalActiveSummaryLeavesLongTextForRendererEllipsis(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu-state")

	longGoal := strings.Repeat("a", 320)
	store := goalrunner.NewStore(statepath.GoalDir(rt.StateDir, "long"))
	if _, err := store.Init(goalrunner.Spec{ID: "long", Goal: longGoal}); err != nil {
		t.Fatalf("Init long: %v", err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	if err := srv.handleLine(context.Background(), []byte(`{"id":"sum","method":"goal/active-summary"}`)); err != nil {
		t.Fatalf("goal/active-summary: %v", err)
	}
	msgs := parseOutput(t, out.String())
	msg := responseByID(t, msgs, "sum")
	result := remarshal[GoalActiveSummaryResult](t, msg["result"])
	if result.Summary == nil {
		t.Fatalf("expected summary, got %+v", result)
	}
	if result.Summary.Text != longGoal {
		t.Fatalf("expected untruncated summary text, got %d chars", len(result.Summary.Text))
	}
}

func TestGoalCancelRequiresConfirmation(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu-state")

	store := goalrunner.NewStore(statepath.GoalDir(rt.StateDir, "live"))
	if _, err := store.Init(goalrunner.Spec{ID: "live", Goal: "live goal"}); err != nil {
		t.Fatalf("Init live: %v", err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	raw := `{"id":"cancel","method":"goal/cancel","params":{"goal_id":"live"}}`
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("goal/cancel: %v", err)
	}
	msgs := parseOutput(t, out.String())
	msg := responseByID(t, msgs, "cancel")
	if msg["error"] == nil {
		t.Fatalf("expected error when confirm_user_approved missing, got %+v", msg)
	}
	state, err := store.LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if state.Status == goalrunner.StatusCancelled {
		t.Fatalf("goal must not be cancelled without confirmation: %+v", state.Status)
	}
}

func TestGoalCancelMarksRunningGoalCancelled(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu-state")

	store := goalrunner.NewStore(statepath.GoalDir(rt.StateDir, "live"))
	if _, err := store.Init(goalrunner.Spec{ID: "live", Goal: "live goal"}); err != nil {
		t.Fatalf("Init live: %v", err)
	}
	if _, err := store.AddProgress(goalrunner.StepExecution, "running"); err != nil {
		t.Fatalf("AddProgress: %v", err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	raw := `{"id":"cancel","method":"goal/cancel","params":{"goal_id":"live","confirm_user_approved":true}}`
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("goal/cancel: %v", err)
	}
	msgs := parseOutput(t, out.String())
	msg := responseByID(t, msgs, "cancel")
	if msg["error"] != nil {
		t.Fatalf("unexpected error: %+v", msg["error"])
	}
	result := remarshal[GoalCancelResult](t, msg["result"])
	if !result.OK {
		t.Fatalf("expected OK result, got %+v", result)
	}
	state, err := store.LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if state.Status != goalrunner.StatusCancelled {
		t.Fatalf("expected status=cancelled, got %s", state.Status)
	}
}

func TestGoalCancelRefusesTerminalGoal(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu-state")

	store := goalrunner.NewStore(statepath.GoalDir(rt.StateDir, "done"))
	if _, err := store.Init(goalrunner.Spec{ID: "done", Goal: "done goal"}); err != nil {
		t.Fatalf("Init done: %v", err)
	}
	if _, err := store.SetStatus(goalrunner.StatusCompleted, goalrunner.StepSummary, ""); err != nil {
		t.Fatalf("SetStatus done: %v", err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	raw := `{"id":"cancel","method":"goal/cancel","params":{"goal_id":"done","confirm_user_approved":true}}`
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("goal/cancel: %v", err)
	}
	msgs := parseOutput(t, out.String())
	msg := responseByID(t, msgs, "cancel")
	if msg["error"] == nil {
		t.Fatalf("expected error cancelling terminal goal, got %+v", msg)
	}
}

func TestGoalUpdateTextRequiresConfirmation(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu-state")

	store := goalrunner.NewStore(statepath.GoalDir(rt.StateDir, "live"))
	if _, err := store.Init(goalrunner.Spec{ID: "live", Goal: "live goal"}); err != nil {
		t.Fatalf("Init live: %v", err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	raw := `{"id":"upd","method":"goal/update-text","params":{"goal_id":"live","text":"new goal"}}`
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("goal/update-text: %v", err)
	}
	msgs := parseOutput(t, out.String())
	msg := responseByID(t, msgs, "upd")
	if msg["error"] == nil {
		t.Fatalf("expected error when confirm_user_approved missing, got %+v", msg)
	}
	state, err := store.LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if state.Goal != "live goal" {
		t.Fatalf("goal text must not change without confirmation, got %q", state.Goal)
	}
}

func TestGoalUpdateTextRejectsEmptyText(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu-state")

	store := goalrunner.NewStore(statepath.GoalDir(rt.StateDir, "live"))
	if _, err := store.Init(goalrunner.Spec{ID: "live", Goal: "live goal"}); err != nil {
		t.Fatalf("Init live: %v", err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	raw := `{"id":"upd","method":"goal/update-text","params":{"goal_id":"live","text":"   ","confirm_user_approved":true}}`
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("goal/update-text: %v", err)
	}
	msgs := parseOutput(t, out.String())
	msg := responseByID(t, msgs, "upd")
	if msg["error"] == nil {
		t.Fatalf("expected error for empty text, got %+v", msg)
	}
}

func TestGoalUpdateTextRewritesGoalAndEmitsEvent(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu-state")

	store := goalrunner.NewStore(statepath.GoalDir(rt.StateDir, "live"))
	if _, err := store.Init(goalrunner.Spec{ID: "live", Goal: "live goal"}); err != nil {
		t.Fatalf("Init live: %v", err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	raw := `{"id":"upd","method":"goal/update-text","params":{"goal_id":"live","text":"updated goal","confirm_user_approved":true}}`
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("goal/update-text: %v", err)
	}
	msgs := parseOutput(t, out.String())
	msg := responseByID(t, msgs, "upd")
	if msg["error"] != nil {
		t.Fatalf("unexpected error: %+v", msg["error"])
	}
	result := remarshal[GoalUpdateTextResult](t, msg["result"])
	if !result.OK {
		t.Fatalf("expected OK result, got %+v", result)
	}
	state, err := store.LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if state.Goal != "updated goal" {
		t.Fatalf("expected goal rewritten, got %q", state.Goal)
	}
	events, err := store.Events()
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	foundEdit := false
	for _, ev := range events {
		if ev.Type == "goal_text_updated" {
			foundEdit = true
		}
	}
	if !foundEdit {
		t.Fatalf("expected goal_text_updated event, got %+v", events)
	}
}
