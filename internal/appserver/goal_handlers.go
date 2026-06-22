package appserver

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	goalrunner "github.com/blueberrycongee/wuu/internal/goal"
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

	workflowStore, err := s.goalWorkflowStore()
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
	store, err := s.goalStoreForID(params.GoalID)
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
// text (single-line), status, step, updated_at — full goal state (tasks,
// approvals, workflow phases) is intentionally omitted so the renderer
// cannot rebuild the deleted right-side Goal panel from this surface.
func (s *Server) handleGoalActiveSummary(req Request) error {
	summary, err := s.findActiveGoalSummary()
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, GoalActiveSummaryResult{Summary: summary}, nil)
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
	store, err := s.goalStoreForID(params.GoalID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	state, err := store.LoadState()
	if err != nil {
		return s.writeResponse(req.ID, nil, fmt.Errorf("load goal state: %w", err))
	}
	if goalStatusIsTerminal(state.Status) {
		return s.writeResponse(req.ID, nil, fmt.Errorf("goal %q is already %s", params.GoalID, state.Status))
	}
	if _, err := store.SetStatus(goalrunner.StatusCancelled, state.CurrentStep, "cancelled from composer banner"); err != nil {
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
	store, err := s.goalStoreForID(params.GoalID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if _, err := store.UpdateText(text); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, GoalUpdateTextResult{OK: true}, nil)
}

// findActiveGoalSummary walks the workspace's goal directory and returns
// the most recently updated non-terminal goal's lightweight summary. The
// walk is bounded: missing or malformed goal subdirectories are skipped
// rather than failing the whole call, since this powers a renderer banner
// and a transient disk error should not blank the composer surface.
func (s *Server) findActiveGoalSummary() (*GoalActiveSummary, error) {
	stateDir, err := s.workspaceStateDir()
	if err != nil {
		return nil, err
	}
	goalRoot := statepath.GoalRoot(stateDir)
	entries, err := os.ReadDir(goalRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read goal root: %w", err)
	}
	var best *goalrunner.State
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
			continue
		}
		store := goalrunner.NewStore(statepath.GoalDir(stateDir, name))
		state, err := store.LoadState()
		if err != nil {
			continue
		}
		if goalStatusIsTerminal(state.Status) {
			continue
		}
		if best == nil || state.UpdatedAt.After(best.UpdatedAt) {
			best = &state
		}
	}
	if best == nil {
		return nil, nil
	}
	updated := ""
	if !best.UpdatedAt.IsZero() {
		updated = best.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return &GoalActiveSummary{
		ID:        best.ID,
		Text:      goalSummaryText(best.Goal),
		Status:    string(best.Status),
		Step:      string(best.CurrentStep),
		UpdatedAt: updated,
	}, nil
}

// goalStatusIsTerminal reports whether a goal status can no longer change.
// The composer banner only shows non-terminal goals so the user always
// sees a goal that is still doing something.
func goalStatusIsTerminal(status goalrunner.Status) bool {
	switch status {
	case goalrunner.StatusCompleted, goalrunner.StatusFailed, goalrunner.StatusCancelled:
		return true
	default:
		return false
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

func (s *Server) goalWorkflowStore() (*workflow.Store, error) {
	stateDir, err := s.workspaceStateDir()
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
	if control := s.liveAgentControl(threadID); control != nil && control.HarnessStore() != nil {
		return control.HarnessStore(), nil
	}
	stateDir, err := s.workspaceStateDir()
	if err != nil {
		return nil, err
	}
	return harness.NewStore(filepath.Join(statepath.SessionArtifactDir(stateDir, threadID), "harness")), nil
}

func (s *Server) goalStoreForID(goalID string) (*goalrunner.Store, error) {
	goalID = strings.TrimSpace(goalID)
	if goalID == "" {
		return nil, fmt.Errorf("goal_id is required")
	}
	if goalID == "." || goalID == ".." || filepath.Base(goalID) != goalID || strings.ContainsAny(goalID, `/\`) {
		return nil, fmt.Errorf("goal_id must be a goal id, not a path")
	}
	stateDir, err := s.workspaceStateDir()
	if err != nil {
		return nil, err
	}
	return goalrunner.NewStore(statepath.GoalDir(stateDir, goalID)), nil
}
