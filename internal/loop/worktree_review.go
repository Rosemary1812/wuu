package loop

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/blueberrycongee/wuu/internal/worktree"
)

type WorktreeReviewOptions struct {
	ParentRepo   string
	WorktreeRoot string
	WorktreePath string
	TargetRepo   string
	MaxDiffBytes int
}

type WorktreeReview struct {
	WorktreePath  string                `json:"worktree_path"`
	TargetRepo    string                `json:"target_repo,omitempty"`
	Status        worktree.Status       `json:"status"`
	Diff          string                `json:"diff,omitempty"`
	DiffTruncated bool                  `json:"diff_truncated,omitempty"`
	MergePreview  worktree.MergePreview `json:"merge_preview"`
}

type WorktreeCleanupOptions struct {
	ParentRepo                 string
	WorktreeRoot               string
	WorktreePath               string
	ConfirmUserApproved        bool
	ConfirmRemoveCleanWorktree bool
}

type WorktreeCleanupResult struct {
	WorktreePath string          `json:"worktree_path"`
	Removed      bool            `json:"removed"`
	Kept         bool            `json:"kept"`
	StatusBefore worktree.Status `json:"status_before"`
}

func ReviewWorktree(opts WorktreeReviewOptions) (WorktreeReview, error) {
	parentRepo := strings.TrimSpace(opts.ParentRepo)
	if parentRepo == "" {
		return WorktreeReview{}, errors.New("parent repo is required")
	}
	worktreeRoot := strings.TrimSpace(opts.WorktreeRoot)
	if worktreeRoot == "" {
		return WorktreeReview{}, errors.New("worktree root is required")
	}
	worktreePath, err := validatedWorktreePath(worktreeRoot, opts.WorktreePath)
	if err != nil {
		return WorktreeReview{}, err
	}
	targetRepo := strings.TrimSpace(opts.TargetRepo)
	if targetRepo == "" {
		targetRepo = parentRepo
	}
	manager, err := worktree.NewManager(parentRepo, worktreeRoot)
	if err != nil {
		return WorktreeReview{}, err
	}
	review, err := manager.Review(worktree.Lease{Path: worktreePath}, targetRepo)
	if err != nil {
		return WorktreeReview{}, err
	}
	diff, truncated := truncateReviewDiff(review.Diff, opts.MaxDiffBytes)
	return WorktreeReview{
		WorktreePath:  worktreePath,
		TargetRepo:    targetRepo,
		Status:        review.Status,
		Diff:          diff,
		DiffTruncated: truncated,
		MergePreview:  review.MergePreview,
	}, nil
}

func validatedWorktreePath(root, path string) (string, error) {
	root = strings.TrimSpace(root)
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("worktree path is required")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve worktree root: %w", err)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve worktree path: %w", err)
	}
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil {
		return "", fmt.Errorf("relativize worktree path: %w", err)
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
		return "", fmt.Errorf("worktree path is outside managed root: %s", absPath)
	}
	return absPath, nil
}

func truncateReviewDiff(diff string, maxBytes int) (string, bool) {
	if maxBytes <= 0 || len(diff) <= maxBytes {
		return diff, false
	}
	return diff[:maxBytes] + "\n[diff truncated]\n", true
}

func CleanupWorktreeIfClean(opts WorktreeCleanupOptions) (WorktreeCleanupResult, error) {
	if !opts.ConfirmUserApproved || !opts.ConfirmRemoveCleanWorktree {
		return WorktreeCleanupResult{}, errors.New("loop worktree cleanup requires confirm_user_approved=true and confirm_remove_clean_worktree=true")
	}
	parentRepo := strings.TrimSpace(opts.ParentRepo)
	if parentRepo == "" {
		return WorktreeCleanupResult{}, errors.New("parent repo is required")
	}
	worktreeRoot := strings.TrimSpace(opts.WorktreeRoot)
	if worktreeRoot == "" {
		return WorktreeCleanupResult{}, errors.New("worktree root is required")
	}
	worktreePath, err := validatedWorktreePath(worktreeRoot, opts.WorktreePath)
	if err != nil {
		return WorktreeCleanupResult{}, err
	}
	manager, err := worktree.NewManager(parentRepo, worktreeRoot)
	if err != nil {
		return WorktreeCleanupResult{}, err
	}
	target := &worktree.Worktree{Path: worktreePath}
	status, err := manager.Status(target)
	if err != nil {
		return WorktreeCleanupResult{}, err
	}
	kept, err := manager.CleanupIfClean(target)
	if err != nil {
		return WorktreeCleanupResult{}, err
	}
	return WorktreeCleanupResult{
		WorktreePath: worktreePath,
		Removed:      !kept,
		Kept:         kept,
		StatusBefore: status,
	}, nil
}
