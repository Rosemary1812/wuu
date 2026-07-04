package tools

import (
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/workflow"
)

func workflowReconcileEnv(t *testing.T) *Env {
	t.Helper()
	return &Env{
		RootDir:   t.TempDir(),
		StateDir:  t.TempDir(),
		SessionID: "thread-wf-reconcile",
	}
}

func seedWorkflowRun(t *testing.T, env *Env, id, driver string, status workflow.RunState) *workflow.Store {
	t.Helper()
	store, err := env.WorkflowStore()
	if err != nil {
		t.Fatalf("WorkflowStore: %v", err)
	}
	if _, err := store.CreateRun(workflow.Run{ID: id, Driver: driver, Status: status}); err != nil {
		t.Fatalf("CreateRun %s: %v", id, err)
	}
	return store
}

// TestWorkflowStoreForEnv_SettlesOrphanedScriptRun is the crash story for
// repair item #8: a background workflow script goroutine died with its
// process and the run froze at running. The first store access in a new
// process must settle it as failed(interrupted); agent-managed runs, which
// legitimately stay running across restarts, are untouched.
func TestWorkflowStoreForEnv_SettlesOrphanedScriptRun(t *testing.T) {
	env := workflowReconcileEnv(t)
	store := seedWorkflowRun(t, env, "workflow-orphan-script", workflow.RunDriverScript, workflow.RunStateRunning)
	if _, err := store.CreateRun(workflow.Run{ID: "workflow-agent-managed", Driver: workflow.RunDriverAgentManaged, Status: workflow.RunStateRunning}); err != nil {
		t.Fatalf("CreateRun agent-managed: %v", err)
	}
	if _, err := store.CreateRun(workflow.Run{ID: "workflow-done", Driver: workflow.RunDriverScript, Status: workflow.RunStateCompleted}); err != nil {
		t.Fatalf("CreateRun completed: %v", err)
	}

	swept, err := workflowStoreForEnv(env)
	if err != nil {
		t.Fatalf("workflowStoreForEnv: %v", err)
	}

	orphan, err := swept.LoadRun("workflow-orphan-script")
	if err != nil {
		t.Fatalf("LoadRun orphan: %v", err)
	}
	if orphan.Status != workflow.RunStateFailed {
		t.Fatalf("orphaned script run should settle failed, got %s", orphan.Status)
	}
	if !strings.Contains(orphan.Error, "interrupted") {
		t.Fatalf("orphan error should carry the interruption reason, got %q", orphan.Error)
	}
	if orphan.CompletedAt.IsZero() {
		t.Fatal("settled orphan should record a completion time")
	}

	if run, _ := swept.LoadRun("workflow-agent-managed"); run.Status != workflow.RunStateRunning {
		t.Fatalf("agent-managed running run must survive the sweep, got %s", run.Status)
	}
	if run, _ := swept.LoadRun("workflow-done"); run.Status != workflow.RunStateCompleted {
		t.Fatalf("terminal run must survive the sweep, got %s", run.Status)
	}
}

// TestReconcileOrphanWorkflowRuns_SkipsLiveRuns asserts the process-local
// live registry protects a script run that a goroutine in this process is
// actually executing.
func TestReconcileOrphanWorkflowRuns_SkipsLiveRuns(t *testing.T) {
	env := workflowReconcileEnv(t)
	store := seedWorkflowRun(t, env, "workflow-live-script", workflow.RunDriverScript, workflow.RunStateRunning)

	markWorkflowRunLive("workflow-live-script")
	defer releaseWorkflowRunLive("workflow-live-script")

	reconcileOrphanWorkflowRuns(store)

	if run, _ := store.LoadRun("workflow-live-script"); run.Status != workflow.RunStateRunning {
		t.Fatalf("live run must survive the sweep, got %s", run.Status)
	}
}

// TestReconcileOrphanWorkflowRuns_SweepsEachDirOnce locks the load-bearing
// once-per-process semantics: the sweep only classifies runs that predate
// this process. A run created after the first sweep (its goroutine may be
// between CreateRun and the live-registry mark) must never be reaped by a
// later store access.
func TestReconcileOrphanWorkflowRuns_SweepsEachDirOnce(t *testing.T) {
	env := workflowReconcileEnv(t)
	if _, err := workflowStoreForEnv(env); err != nil {
		t.Fatalf("workflowStoreForEnv: %v", err)
	}

	store := seedWorkflowRun(t, env, "workflow-post-sweep", workflow.RunDriverScript, workflow.RunStateRunning)

	if _, err := workflowStoreForEnv(env); err != nil {
		t.Fatalf("workflowStoreForEnv second access: %v", err)
	}
	if run, _ := store.LoadRun("workflow-post-sweep"); run.Status != workflow.RunStateRunning {
		t.Fatalf("run created after the startup sweep must not be reaped, got %s", run.Status)
	}
}
