package appserver

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/blueberrycongee/wuu/internal/harness"
	looprunner "github.com/blueberrycongee/wuu/internal/loop"
	"github.com/blueberrycongee/wuu/internal/statepath"
	"github.com/blueberrycongee/wuu/internal/workflow"
)

func (s *Server) handleLoopSnapshot(req Request) error {
	var params LoopSnapshotParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.writeResponse(req.ID, nil, fmt.Errorf("parse loop snapshot params: %w", err))
		}
	}

	workflowStore, err := s.loopWorkflowStore()
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	harnessStore, err := s.loopHarnessStore(params.ThreadID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}

	snapshot := looprunner.SnapshotSystem(looprunner.SnapshotOptions{
		LoopRoot:      filepath.Join(filepath.Clean(workflowStore.Dir()), "loops"),
		WorkflowStore: workflowStore,
		HarnessStore:  harnessStore,
	})
	return s.writeResponse(req.ID, LoopSnapshotResult{Snapshot: snapshot}, nil)
}

func (s *Server) handleLoopWorktreeReview(req Request) error {
	var params LoopWorktreeReviewParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.writeResponse(req.ID, nil, fmt.Errorf("parse loop worktree review params: %w", err))
		}
	}
	stateDir, err := s.workspaceStateDir()
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	review, err := looprunner.ReviewWorktree(looprunner.WorktreeReviewOptions{
		ParentRepo:   s.rt.RootDir,
		WorktreeRoot: statepath.WorktreeRoot(stateDir),
		WorktreePath: params.WorktreePath,
		TargetRepo:   s.rt.RootDir,
		MaxDiffBytes: params.MaxDiffBytes,
	})
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, LoopWorktreeReviewResult{Review: review}, nil)
}

func (s *Server) handleLoopWorktreeCleanup(req Request) error {
	var params LoopWorktreeCleanupParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.writeResponse(req.ID, nil, fmt.Errorf("parse loop worktree cleanup params: %w", err))
		}
	}
	stateDir, err := s.workspaceStateDir()
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	cleanup, err := looprunner.CleanupWorktreeIfClean(looprunner.WorktreeCleanupOptions{
		ParentRepo:                 s.rt.RootDir,
		WorktreeRoot:               statepath.WorktreeRoot(stateDir),
		WorktreePath:               params.WorktreePath,
		ConfirmUserApproved:        params.ConfirmUserApproved,
		ConfirmRemoveCleanWorktree: params.ConfirmRemoveCleanWorktree,
	})
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, LoopWorktreeCleanupResult{Cleanup: cleanup}, nil)
}

func (s *Server) handleLoopWorktreeRollback(req Request) error {
	var params LoopWorktreeRollbackParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.writeResponse(req.ID, nil, fmt.Errorf("parse loop worktree rollback params: %w", err))
		}
	}
	stateDir, err := s.workspaceStateDir()
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	rollback, err := looprunner.RollbackWorktree(looprunner.WorktreeRollbackOptions{
		ParentRepo:                    s.rt.RootDir,
		WorktreeRoot:                  statepath.WorktreeRoot(stateDir),
		WorktreePath:                  params.WorktreePath,
		ConfirmUserApproved:           params.ConfirmUserApproved,
		ConfirmDiscardWorktreeChanges: params.ConfirmDiscardWorktreeChanges,
	})
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, LoopWorktreeRollbackResult{Rollback: rollback}, nil)
}

func (s *Server) handleLoopWorktreeMerge(req Request) error {
	var params LoopWorktreeMergeParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.writeResponse(req.ID, nil, fmt.Errorf("parse loop worktree merge params: %w", err))
		}
	}
	stateDir, err := s.workspaceStateDir()
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	merge, err := looprunner.MergeWorktree(looprunner.WorktreeMergeOptions{
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
	return s.writeResponse(req.ID, LoopWorktreeMergeResult{Merge: merge}, nil)
}

func (s *Server) handleLoopApprovalResolve(req Request) error {
	var params LoopApprovalResolveParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.writeResponse(req.ID, nil, fmt.Errorf("parse loop approval resolve params: %w", err))
		}
	}
	if !params.ConfirmUserApproved {
		return s.writeResponse(req.ID, nil, fmt.Errorf("loop approval resolve requires confirm_user_approved=true"))
	}
	store, err := s.loopStoreForID(params.LoopID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	_, approval, err := store.ResolveApproval(looprunner.ApprovalResolution{
		ID:         params.ApprovalID,
		Approved:   params.Approved,
		Rejected:   params.Rejected,
		ResolvedBy: params.ResolvedBy,
		Resolution: params.Resolution,
	})
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, LoopApprovalResolveResult{Approval: approval}, nil)
}

func (s *Server) loopWorkflowStore() (*workflow.Store, error) {
	stateDir, err := s.workspaceStateDir()
	if err != nil {
		return nil, err
	}
	return workflow.NewStore(stateDir), nil
}

func (s *Server) loopHarnessStore(threadID string) (*harness.Store, error) {
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

func (s *Server) loopStoreForID(loopID string) (*looprunner.Store, error) {
	loopID = strings.TrimSpace(loopID)
	if loopID == "" {
		return nil, fmt.Errorf("loop_id is required")
	}
	if loopID == "." || loopID == ".." || filepath.Base(loopID) != loopID || strings.ContainsAny(loopID, `/\`) {
		return nil, fmt.Errorf("loop_id must be a loop id, not a path")
	}
	stateDir, err := s.workspaceStateDir()
	if err != nil {
		return nil, err
	}
	return looprunner.NewStore(filepath.Join(stateDir, "loops", loopID)), nil
}
