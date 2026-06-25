package appserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	goalrunner "github.com/blueberrycongee/wuu/internal/goal"
	"github.com/blueberrycongee/wuu/internal/goalruntime"
	"github.com/blueberrycongee/wuu/internal/harness"
	"github.com/blueberrycongee/wuu/internal/statepath"
	"github.com/blueberrycongee/wuu/internal/workflow"
)

func (s *Server) handleGoalSnapshot(req Request) error {
	var params GoalSnapshotParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.writeResponse(req.ID, nil, fmt.Errorf("parse goal snapshot params: %w", err))
		}
	}

	workflowStore, err := s.goalWorkflowStore(params.ThreadID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	harnessStore, err := s.goalHarnessStore(params.ThreadID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}

	snapshot := goalrunner.SnapshotSystem(goalrunner.SnapshotOptions{
		GoalRoot:      statepath.GoalRoot(filepath.Clean(workflowStore.Dir())),
		WorkflowStore: workflowStore,
		HarnessStore:  harnessStore,
	})
	return s.writeResponse(req.ID, GoalSnapshotResult{Snapshot: snapshot}, nil)
}

func (s *Server) handleGoalWorktreeReview(req Request) error {
	var params GoalWorktreeReviewParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.writeResponse(req.ID, nil, fmt.Errorf("parse goal worktree review params: %w", err))
		}
	}
	stateDir, err := s.workspaceStateDir()
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	review, err := goalrunner.ReviewWorktree(goalrunner.WorktreeReviewOptions{
		ParentRepo:   s.rt.RootDir,
		WorktreeRoot: statepath.WorktreeRoot(stateDir),
		WorktreePath: params.WorktreePath,
		TargetRepo:   s.rt.RootDir,
		MaxDiffBytes: params.MaxDiffBytes,
	})
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, GoalWorktreeReviewResult{Review: review}, nil)
}

func (s *Server) handleGoalWorktreeCleanup(req Request) error {
	var params GoalWorktreeCleanupParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.writeResponse(req.ID, nil, fmt.Errorf("parse goal worktree cleanup params: %w", err))
		}
	}
	stateDir, err := s.workspaceStateDir()
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	cleanup, err := goalrunner.CleanupWorktreeIfClean(goalrunner.WorktreeCleanupOptions{
		ParentRepo:                 s.rt.RootDir,
		WorktreeRoot:               statepath.WorktreeRoot(stateDir),
		WorktreePath:               params.WorktreePath,
		ConfirmUserApproved:        params.ConfirmUserApproved,
		ConfirmRemoveCleanWorktree: params.ConfirmRemoveCleanWorktree,
	})
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, GoalWorktreeCleanupResult{Cleanup: cleanup}, nil)
}

func (s *Server) handleGoalWorktreeRollback(req Request) error {
	var params GoalWorktreeRollbackParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.writeResponse(req.ID, nil, fmt.Errorf("parse goal worktree rollback params: %w", err))
		}
	}
	stateDir, err := s.workspaceStateDir()
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	rollback, err := goalrunner.RollbackWorktree(goalrunner.WorktreeRollbackOptions{
		ParentRepo:                    s.rt.RootDir,
		WorktreeRoot:                  statepath.WorktreeRoot(stateDir),
		WorktreePath:                  params.WorktreePath,
		ConfirmUserApproved:           params.ConfirmUserApproved,
		ConfirmDiscardWorktreeChanges: params.ConfirmDiscardWorktreeChanges,
	})
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, GoalWorktreeRollbackResult{Rollback: rollback}, nil)
}

func (s *Server) handleGoalWorktreeMerge(req Request) error {
	var params GoalWorktreeMergeParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.writeResponse(req.ID, nil, fmt.Errorf("parse goal worktree merge params: %w", err))
		}
	}
	stateDir, err := s.workspaceStateDir()
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	merge, err := goalrunner.MergeWorktree(goalrunner.WorktreeMergeOptions{
		ParentRepo:                s.rt.RootDir,
		WorktreeRoot:              statepath.WorktreeRoot(stateDir),
		WorktreePath:              params.WorktreePath,
		TargetRepo:                s.rt.RootDir,
		ConfirmUserApproved:       params.ConfirmUserApproved,
		ConfirmApplyWorktreeDiff:  params.ConfirmApplyWorktreeDiff,
		ConfirmTargetRepoMutation: params.ConfirmTargetRepoMutation,
	})
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, GoalWorktreeMergeResult{Merge: merge}, nil)
}

func (s *Server) handleGoalApprovalResolve(req Request) error {
	var params GoalApprovalResolveParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.writeResponse(req.ID, nil, fmt.Errorf("parse goal approval resolve params: %w", err))
		}
	}
	if !params.ConfirmUserApproved {
		return s.writeResponse(req.ID, nil, fmt.Errorf("goal approval resolve requires confirm_user_approved=true"))
	}
	store, err := s.goalStoreForID(params.GoalID, params.ThreadID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	_, approval, err := store.ResolveApproval(goalrunner.ApprovalResolution{
		ID:         params.ApprovalID,
		Approved:   params.Approved,
		Rejected:   params.Rejected,
		ResolvedBy: params.ResolvedBy,
		Resolution: params.Resolution,
	})
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, GoalApprovalResolveResult{Approval: approval}, nil)
}

// handleGoalActiveSummary returns the lightweight composer-banner view of
// the most recently updated non-terminal goal. The renderer only needs id,
// text (single-line), status, step, started_at, updated_at — full goal
// state (tasks, approvals, workflow phases) is intentionally omitted so the
// renderer cannot rebuild the deleted right-side Goal panel from this
// surface. started_at feeds the composer banner's elapsed-time counter and
// must come from the canonical goal creation timestamp so the timer survives
// a desktop reload.
func (s *Server) handleGoalActiveSummary(req Request) error {
	var params GoalActiveSummaryParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.writeResponse(req.ID, nil, fmt.Errorf("parse goal active summary params: %w", err))
		}
	}
	summary, err := s.findActiveGoalSummary(params.ThreadID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, GoalActiveSummaryResult{Summary: summary}, nil)
}

func (s *Server) handleGoalPause(req Request) error {
	var params GoalPauseParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.writeResponse(req.ID, nil, fmt.Errorf("parse goal pause params: %w", err))
		}
	}
	if !params.ConfirmUserApproved {
		return s.writeResponse(req.ID, nil, fmt.Errorf("goal pause requires confirm_user_approved=true"))
	}
	_, runtime, err := s.runtimeGoalForControl(params.GoalID, params.ThreadID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if runtime == nil {
		return s.writeResponse(req.ID, nil, fmt.Errorf("active runtime goal not found"))
	}
	if _, err := runtime.SetUserStatus(goalruntime.StatusPaused, time.Now().UTC()); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, GoalPauseResult{OK: true}, nil)
}

func (s *Server) handleGoalResume(req Request) error {
	var params GoalResumeParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.writeResponse(req.ID, nil, fmt.Errorf("parse goal resume params: %w", err))
		}
	}
	if !params.ConfirmUserApproved {
		return s.writeResponse(req.ID, nil, fmt.Errorf("goal resume requires confirm_user_approved=true"))
	}
	_, runtime, err := s.runtimeGoalForControl(params.GoalID, params.ThreadID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if runtime == nil {
		return s.writeResponse(req.ID, nil, fmt.Errorf("active runtime goal not found"))
	}
	if _, err := runtime.SetUserStatus(goalruntime.StatusActive, time.Now().UTC()); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, GoalResumeResult{OK: true}, nil)
}

func (s *Server) handleGoalClear(req Request) error {
	var params GoalClearParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.writeResponse(req.ID, nil, fmt.Errorf("parse goal clear params: %w", err))
		}
	}
	if !params.ConfirmUserApproved {
		return s.writeResponse(req.ID, nil, fmt.Errorf("goal clear requires confirm_user_approved=true"))
	}
	_, runtime, err := s.runtimeGoalForControl(params.GoalID, params.ThreadID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if runtime == nil {
		return s.writeResponse(req.ID, nil, fmt.Errorf("active runtime goal not found"))
	}
	if err := runtime.Clear(); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, GoalClearResult{OK: true}, nil)
}

// handleGoalCancel marks the named goal as cancelled. Terminal-status
// goals (completed/failed/cancelled) refuse the request to keep the
// renderer's banner from racing against a finished goal.
func (s *Server) handleGoalCancel(req Request) error {
	var params GoalCancelParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.writeResponse(req.ID, nil, fmt.Errorf("parse goal cancel params: %w", err))
		}
	}
	if !params.ConfirmUserApproved {
		return s.writeResponse(req.ID, nil, fmt.Errorf("goal cancel requires confirm_user_approved=true"))
	}
	_, runtime, err := s.runtimeGoalForControl(params.GoalID, params.ThreadID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if runtime == nil {
		return s.writeResponse(req.ID, nil, fmt.Errorf("active runtime goal not found"))
	}
	if _, err := runtime.SetUserStatus(goalruntime.StatusCancelled, time.Now().UTC()); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, GoalCancelResult{OK: true}, nil)
}

// handleGoalUpdateText rewrites the goal objective. The renderer is
// expected to obtain explicit user confirmation before invoking this;
// the server enforces confirm_user_approved as a guardrail.
func (s *Server) handleGoalUpdateText(req Request) error {
	var params GoalUpdateTextParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.writeResponse(req.ID, nil, fmt.Errorf("parse goal update text params: %w", err))
		}
	}
	if !params.ConfirmUserApproved {
		return s.writeResponse(req.ID, nil, fmt.Errorf("goal update text requires confirm_user_approved=true"))
	}
	text := strings.TrimSpace(params.Text)
	if text == "" {
		return s.writeResponse(req.ID, nil, fmt.Errorf("goal text is required"))
	}
	_, runtime, err := s.runtimeGoalForControl(params.GoalID, params.ThreadID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if runtime == nil {
		return s.writeResponse(req.ID, nil, fmt.Errorf("active runtime goal not found"))
	}
	if _, err := runtime.EditObjective(text, time.Now().UTC()); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, GoalUpdateTextResult{OK: true}, nil)
}

// findActiveGoalSummary returns the thread runtime Goal's lightweight summary.
// The composer banner intentionally ignores the older durable Goal ledger: that
// ledger is execution evidence and cannot drive runtime continuation.
func (s *Server) findActiveGoalSummary(threadID string) (*GoalActiveSummary, error) {
	runtimeGoal, foundRuntimeGoal, err := s.currentRuntimeGoal(threadID)
	if err != nil {
		return nil, err
	}
	if !foundRuntimeGoal || goalruntime.IsTerminalStatus(runtimeGoal.Status) {
		return nil, nil
	}
	return s.runtimeGoalSummary(runtimeGoal, threadID)
}

func (s *Server) runtimeGoalSummary(goal goalruntime.Goal, threadID string) (*GoalActiveSummary, error) {
	started := ""
	if !goal.CreatedAt.IsZero() {
		started = goal.CreatedAt.UTC().Format(time.RFC3339)
	}
	updated := ""
	if !goal.UpdatedAt.IsZero() {
		updated = goal.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return &GoalActiveSummary{
		ID:                      goal.GoalID,
		ThreadID:                strings.TrimSpace(threadID),
		Text:                    goalSummaryText(goal.Objective),
		Status:                  string(goal.Status),
		StartedAt:               started,
		UpdatedAt:               updated,
		StopReason:              runtimeGoalStopReason(goal.Status),
		TokensUsed:              goal.TokensUsed,
		TokenBudget:             goal.TokenBudget,
		TimeUsedSeconds:         goal.TimeUsedSeconds,
		GoalTurns:               goal.GoalTurns,
		Blocker:                 goal.BlockerAudit.Message,
		BlockerConsecutiveTurns: goal.BlockerAudit.ConsecutiveTurns,
		CanPause:                goal.Status == goalruntime.StatusActive,
		CanResume:               goal.Status == goalruntime.StatusPaused || goal.Status == goalruntime.StatusBlocked || goal.Status == goalruntime.StatusBudgetLimited || goal.Status == goalruntime.StatusUsageLimited,
		CanCancel:               !goalruntime.IsTerminalStatus(goal.Status),
		CanClear:                !goalruntime.IsTerminalStatus(goal.Status),
	}, nil
}

func runtimeGoalStopReason(status goalruntime.Status) string {
	switch status {
	case goalruntime.StatusPaused:
		return "paused"
	case goalruntime.StatusBlocked:
		return "blocked"
	case goalruntime.StatusBudgetLimited:
		return "budget_limited"
	case goalruntime.StatusUsageLimited:
		return "usage_limited"
	default:
		return ""
	}
}

// goalSummaryText collapses goal.Goal into the first-line form the
// banner renders. Width truncation is a renderer concern so editing a
// long first line does not accidentally persist a server-side truncation.
func goalSummaryText(goal string) string {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return ""
	}
	if idx := strings.IndexAny(goal, "\n\r"); idx >= 0 {
		goal = goal[:idx]
	}
	return goal
}

func (s *Server) goalWorkflowStore(threadID string) (*workflow.Store, error) {
	stateDir, err := s.goalOrchestrationStateDir(threadID)
	if err != nil {
		return nil, err
	}
	return workflow.NewStore(stateDir), nil
}

func (s *Server) goalHarnessStore(threadID string) (*harness.Store, error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		if s != nil && s.rt != nil && s.rt.AgentControl != nil {
			return s.rt.AgentControl.HarnessStore(), nil
		}
		return nil, nil
	}
	if err := validateGoalThreadID(threadID); err != nil {
		return nil, err
	}
	if control := s.liveAgentControl(threadID); control != nil && control.HarnessStore() != nil {
		return control.HarnessStore(), nil
	}
	stateDir, err := s.goalOrchestrationStateDir(threadID)
	if err != nil {
		return nil, err
	}
	return harness.NewStore(filepath.Join(stateDir, "harness")), nil
}

func (s *Server) goalStoreForID(goalID, threadID string) (*goalrunner.Store, error) {
	goalID = strings.TrimSpace(goalID)
	if goalID == "" {
		return nil, fmt.Errorf("goal_id is required")
	}
	if goalID == "." || goalID == ".." || filepath.Base(goalID) != goalID || strings.ContainsAny(goalID, `/\`) {
		return nil, fmt.Errorf("goal_id must be a goal id, not a path")
	}
	stateDir, err := s.goalOrchestrationStateDir(threadID)
	if err != nil {
		return nil, err
	}
	return goalrunner.NewStore(statepath.GoalDir(stateDir, goalID)), nil
}

func (s *Server) runtimeGoalForControl(goalID, threadID string) (goalruntime.Goal, *goalruntime.Runtime, error) {
	runtimeGoal, found, err := s.currentRuntimeGoal(threadID)
	if err != nil || !found {
		return goalruntime.Goal{}, nil, err
	}
	goalID = strings.TrimSpace(goalID)
	if goalID != "" && runtimeGoal.GoalID != goalID {
		return goalruntime.Goal{}, nil, fmt.Errorf("active runtime goal %q does not match requested goal %q", runtimeGoal.GoalID, goalID)
	}
	runtime, ok, err := s.goalRuntimeForThread(threadID)
	if err != nil {
		return goalruntime.Goal{}, nil, err
	}
	if !ok {
		return goalruntime.Goal{}, nil, nil
	}
	return runtimeGoal, runtime, nil
}

func (s *Server) currentRuntimeGoal(threadID string) (goalruntime.Goal, bool, error) {
	runtime, ok, err := s.goalRuntimeForThread(threadID)
	if err != nil || !ok {
		return goalruntime.Goal{}, false, err
	}
	goal, err := runtime.CurrentGoal()
	if errors.Is(err, os.ErrNotExist) {
		return goalruntime.Goal{}, false, nil
	}
	if err != nil {
		return goalruntime.Goal{}, false, err
	}
	return goal, true, nil
}

func (s *Server) goalRuntimeForThread(threadID string) (*goalruntime.Runtime, bool, error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil, false, nil
	}
	if err := validateGoalThreadID(threadID); err != nil {
		return nil, false, err
	}
	if th := s.thread(threadID); th != nil {
		threadRuntime, err := s.ensureThreadRuntime(th)
		if err != nil {
			return nil, false, err
		}
		if threadRuntime != nil && threadRuntime.GoalRuntime != nil {
			return threadRuntime.GoalRuntime, true, nil
		}
	}
	stateDir, err := s.workspaceStateDir()
	if err != nil {
		return nil, false, err
	}
	return goalruntime.NewRuntime(goalruntime.NewStore(statepath.ThreadGoalRuntimePath(stateDir, threadID))), true, nil
}

func (s *Server) goalOrchestrationStateDir(threadID string) (string, error) {
	stateDir, err := s.workspaceStateDir()
	if err != nil {
		return "", err
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return stateDir, nil
	}
	if err := validateGoalThreadID(threadID); err != nil {
		return "", err
	}
	return statepath.SessionArtifactDir(stateDir, threadID), nil
}

func validateGoalThreadID(threadID string) error {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil
	}
	if threadID == "." || threadID == ".." || filepath.Base(threadID) != threadID || strings.ContainsAny(threadID, `/\`) {
		return fmt.Errorf("thread_id must be a session id, not a path")
	}
	return nil
}
