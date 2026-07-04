package tools

import (
	"context"
	"strings"
	"sync"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/workflow"
)

// Background workflow lifecycle bookkeeping (repair plan 2026-07-04 item #8,
// invariant 2: every background task is observable and recoverable).
//
// A script-driver workflow runs on a detached goroutine. Its "started"
// marker is the run record the workflow store already persists before the
// goroutine launches (Status=running + StartedAt); this file adds the two
// missing halves:
//
//   - a process-local live registry so a startup sweep can tell "running
//     because a goroutine in this process is executing it" apart from
//     "running because the process that ran it died";
//   - the sweep itself, which settles orphaned script runs as
//     failed(interrupted) the first time a store directory is opened in
//     this process.
var workflowRunLifecycle = struct {
	mu sync.Mutex
	// sweptDirs remembers which store dirs this process already reconciled.
	sweptDirs map[string]bool
	// liveRuns counts in-flight background script goroutines per run id.
	liveRuns map[string]int
}{
	sweptDirs: make(map[string]bool),
	liveRuns:  make(map[string]int),
}

func markWorkflowRunLive(runID string) {
	workflowRunLifecycle.mu.Lock()
	defer workflowRunLifecycle.mu.Unlock()
	workflowRunLifecycle.liveRuns[runID]++
}

func releaseWorkflowRunLive(runID string) {
	workflowRunLifecycle.mu.Lock()
	defer workflowRunLifecycle.mu.Unlock()
	if workflowRunLifecycle.liveRuns[runID] <= 1 {
		delete(workflowRunLifecycle.liveRuns, runID)
		return
	}
	workflowRunLifecycle.liveRuns[runID]--
}

func workflowRunLive(runID string) bool {
	workflowRunLifecycle.mu.Lock()
	defer workflowRunLifecycle.mu.Unlock()
	return workflowRunLifecycle.liveRuns[runID] > 0
}

// claimWorkflowStoreSweep reports whether the caller should sweep the given
// store dir (first open in this process wins).
func claimWorkflowStoreSweep(dir string) bool {
	workflowRunLifecycle.mu.Lock()
	defer workflowRunLifecycle.mu.Unlock()
	if workflowRunLifecycle.sweptDirs[dir] {
		return false
	}
	workflowRunLifecycle.sweptDirs[dir] = true
	return true
}

// workflowStoreForEnv is the workflow tools' store accessor: it resolves the
// durable store and, on the first access to that store directory in this
// process, reconciles crash-orphaned background runs.
func workflowStoreForEnv(env *Env) (*workflow.Store, error) {
	store, err := env.WorkflowStore()
	if err != nil {
		return nil, err
	}
	reconcileOrphanWorkflowRuns(store)
	return store, nil
}

// reconcileOrphanWorkflowRuns settles script-driver runs a dead process left
// in running state: with no live goroutine in this process (and none able to
// survive a restart), such a run can never finish, so it is marked
// failed(interrupted) and its bound goal is synced. Agent-managed runs are
// exempt — they are advanced by conversation turns and legitimately stay
// running across restarts. Recurring semantics for missed background work
// are out of scope here (see internal/cron).
func reconcileOrphanWorkflowRuns(store *workflow.Store) {
	if store == nil || strings.TrimSpace(store.Dir()) == "" {
		return
	}
	if !claimWorkflowStoreSweep(store.Dir()) {
		return
	}
	runs, err := store.ListRuns()
	if err != nil {
		providers.DebugLogf("workflow reconcile: list runs in %s failed: %v", store.Dir(), err)
		return
	}
	for _, run := range runs {
		if run.Driver != workflow.RunDriverScript || run.Status != workflow.RunStateRunning {
			continue
		}
		if workflowRunLive(run.ID) {
			continue
		}
		message := "interrupted: no live script executor for this run; the process exited while the workflow script was running"
		updated, err := store.UpdateRunStatus(run.ID, workflow.RunStateFailed, message)
		if err != nil {
			providers.DebugLogf("workflow reconcile: settle orphaned run %s failed: %v", run.ID, err)
			continue
		}
		providers.DebugLogf("workflow reconcile: run %s marked failed (%s)", run.ID, message)
		if _, err := syncWorkflowGoalStatus(updated); err != nil {
			providers.DebugLogf("workflow reconcile: goal sync for run %s failed: %v", run.ID, err)
		}
	}
}

// runWorkflowScriptInBackground launches a workflow script runtime on a
// detached goroutine with lifecycle bookkeeping: the run is registered live
// so the startup sweep never mistakes it for a crash orphan, the bound goal
// is synced when the script ends, and failures are logged instead of being
// swallowed (the durable run state itself is settled inside
// ScriptRuntime.Run, which fails the run on script errors).
func runWorkflowScriptInBackground(runtime *workflow.ScriptRuntime, runID, action string) {
	markWorkflowRunLive(runID)
	go func() {
		defer releaseWorkflowRunLive(runID)
		finished, err := runtime.Run(context.Background())
		if err != nil {
			providers.DebugLogf("%s: background workflow run %s failed: %v", action, runID, err)
		}
		if _, syncErr := syncWorkflowGoalStatus(finished); syncErr != nil {
			providers.DebugLogf("%s: background workflow run %s goal-status sync failed: %v", action, runID, syncErr)
		}
	}()
}
