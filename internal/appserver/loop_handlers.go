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
