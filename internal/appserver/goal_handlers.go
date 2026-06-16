package appserver

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

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
