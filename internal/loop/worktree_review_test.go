package loop

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/worktree"
)

func TestReviewWorktreeUsesManagedWorktreeRoot(t *testing.T) {
	root := t.TempDir()
	initReviewGitRepo(t, root)
	worktreeRoot := filepath.Join(root, ".wuu", "worktrees")
	manager, err := worktree.NewManager(root, worktreeRoot)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	lease, err := manager.CreateLease(worktree.LeaseOptions{
		SessionID: "session-1",
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

	review, err := ReviewWorktree(WorktreeReviewOptions{
		ParentRepo:   root,
		WorktreeRoot: worktreeRoot,
		WorktreePath: lease.Path,
		TargetRepo:   root,
	})
	if err != nil {
		t.Fatalf("ReviewWorktree: %v", err)
	}
	if !review.Status.Dirty || len(review.Status.ChangedFiles) != 1 || review.Status.ChangedFiles[0] != "README.md" {
		t.Fatalf("unexpected status: %+v", review.Status)
	}
	if !strings.Contains(review.Diff, "+changed") {
		t.Fatalf("diff missing worktree change:\n%s", review.Diff)
	}
	if !review.MergePreview.CanApply || len(review.MergePreview.ConflictFiles) != 0 {
		t.Fatalf("unexpected merge preview: %+v", review.MergePreview)
	}
}

func TestReviewWorktreeRejectsPathOutsideManagedRoot(t *testing.T) {
	root := t.TempDir()
	initReviewGitRepo(t, root)
	_, err := ReviewWorktree(WorktreeReviewOptions{
		ParentRepo:   root,
		WorktreeRoot: filepath.Join(root, ".wuu", "worktrees"),
		WorktreePath: root,
		TargetRepo:   root,
	})
	if err == nil || !strings.Contains(err.Error(), "outside managed root") {
		t.Fatalf("expected managed-root rejection, got %v", err)
	}
}

func TestCleanupWorktreeIfCleanRequiresApproval(t *testing.T) {
	root := t.TempDir()
	initReviewGitRepo(t, root)
	worktreeRoot := filepath.Join(root, ".wuu", "worktrees")
	manager, err := worktree.NewManager(root, worktreeRoot)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	lease, err := manager.CreateLease(worktree.LeaseOptions{SessionID: "session-1", TaskID: "task-1"})
	if err != nil {
		t.Fatalf("CreateLease: %v", err)
	}
	defer manager.Cleanup(&worktree.Worktree{Path: lease.Path})

	_, err = CleanupWorktreeIfClean(WorktreeCleanupOptions{
		ParentRepo:   root,
		WorktreeRoot: worktreeRoot,
		WorktreePath: lease.Path,
	})
	if err == nil || !strings.Contains(err.Error(), "confirm_user_approved") {
		t.Fatalf("expected approval error, got %v", err)
	}
	if _, statErr := os.Stat(lease.Path); statErr != nil {
		t.Fatalf("unapproved cleanup should not remove worktree: %v", statErr)
	}
}

func TestCleanupWorktreeIfCleanRemovesOnlyCleanWorktrees(t *testing.T) {
	root := t.TempDir()
	initReviewGitRepo(t, root)
	worktreeRoot := filepath.Join(root, ".wuu", "worktrees")
	manager, err := worktree.NewManager(root, worktreeRoot)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	cleanLease, err := manager.CreateLease(worktree.LeaseOptions{SessionID: "session-1", TaskID: "clean"})
	if err != nil {
		t.Fatalf("CreateLease clean: %v", err)
	}
	cleanResult, err := CleanupWorktreeIfClean(WorktreeCleanupOptions{
		ParentRepo:                 root,
		WorktreeRoot:               worktreeRoot,
		WorktreePath:               cleanLease.Path,
		ConfirmUserApproved:        true,
		ConfirmRemoveCleanWorktree: true,
	})
	if err != nil {
		t.Fatalf("CleanupWorktreeIfClean clean: %v", err)
	}
	if !cleanResult.Removed || cleanResult.Kept || cleanResult.StatusBefore.Dirty {
		t.Fatalf("unexpected clean cleanup result: %+v", cleanResult)
	}
	if _, statErr := os.Stat(cleanLease.Path); !os.IsNotExist(statErr) {
		t.Fatalf("clean worktree should be removed, stat err=%v", statErr)
	}

	dirtyLease, err := manager.CreateLease(worktree.LeaseOptions{SessionID: "session-1", TaskID: "dirty"})
	if err != nil {
		t.Fatalf("CreateLease dirty: %v", err)
	}
	defer manager.Cleanup(&worktree.Worktree{Path: dirtyLease.Path})
	if err := os.WriteFile(filepath.Join(dirtyLease.Path, "README.md"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatalf("write dirty worktree: %v", err)
	}
	dirtyResult, err := CleanupWorktreeIfClean(WorktreeCleanupOptions{
		ParentRepo:                 root,
		WorktreeRoot:               worktreeRoot,
		WorktreePath:               dirtyLease.Path,
		ConfirmUserApproved:        true,
		ConfirmRemoveCleanWorktree: true,
	})
	if err != nil {
		t.Fatalf("CleanupWorktreeIfClean dirty: %v", err)
	}
	if dirtyResult.Removed || !dirtyResult.Kept || !dirtyResult.StatusBefore.Dirty {
		t.Fatalf("unexpected dirty cleanup result: %+v", dirtyResult)
	}
	if _, statErr := os.Stat(dirtyLease.Path); statErr != nil {
		t.Fatalf("dirty worktree should be kept: %v", statErr)
	}
}

func TestRollbackWorktreeRequiresApproval(t *testing.T) {
	root := t.TempDir()
	initReviewGitRepo(t, root)
	worktreeRoot := filepath.Join(root, ".wuu", "worktrees")
	manager, err := worktree.NewManager(root, worktreeRoot)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	lease, err := manager.CreateLease(worktree.LeaseOptions{SessionID: "session-1", TaskID: "rollback-denied"})
	if err != nil {
		t.Fatalf("CreateLease: %v", err)
	}
	defer manager.Cleanup(&worktree.Worktree{Path: lease.Path})
	if err := os.WriteFile(filepath.Join(lease.Path, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatalf("write worktree file: %v", err)
	}

	_, err = RollbackWorktree(WorktreeRollbackOptions{
		ParentRepo:   root,
		WorktreeRoot: worktreeRoot,
		WorktreePath: lease.Path,
	})
	if err == nil || !strings.Contains(err.Error(), "confirm_discard_worktree_changes") {
		t.Fatalf("expected rollback approval error, got %v", err)
	}
	data, readErr := os.ReadFile(filepath.Join(lease.Path, "README.md"))
	if readErr != nil {
		t.Fatalf("read README after denied rollback: %v", readErr)
	}
	if string(data) != "changed\n" {
		t.Fatalf("denied rollback should not change file, got %q", string(data))
	}
}

func TestRollbackWorktreeDiscardsManagedWorktreeChanges(t *testing.T) {
	root := t.TempDir()
	initReviewGitRepo(t, root)
	worktreeRoot := filepath.Join(root, ".wuu", "worktrees")
	manager, err := worktree.NewManager(root, worktreeRoot)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	lease, err := manager.CreateLease(worktree.LeaseOptions{SessionID: "session-1", TaskID: "rollback"})
	if err != nil {
		t.Fatalf("CreateLease: %v", err)
	}
	defer manager.Cleanup(&worktree.Worktree{Path: lease.Path})
	if err := os.WriteFile(filepath.Join(lease.Path, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatalf("write tracked change: %v", err)
	}
	if err := os.WriteFile(filepath.Join(lease.Path, "scratch.txt"), []byte("temporary\n"), 0o644); err != nil {
		t.Fatalf("write untracked change: %v", err)
	}

	result, err := RollbackWorktree(WorktreeRollbackOptions{
		ParentRepo:                    root,
		WorktreeRoot:                  worktreeRoot,
		WorktreePath:                  lease.Path,
		ConfirmUserApproved:           true,
		ConfirmDiscardWorktreeChanges: true,
	})
	if err != nil {
		t.Fatalf("RollbackWorktree: %v", err)
	}
	if !result.RolledBack || !result.StatusBefore.Dirty || result.StatusAfter.Dirty {
		t.Fatalf("unexpected rollback result: %+v", result)
	}
	data, err := os.ReadFile(filepath.Join(lease.Path, "README.md"))
	if err != nil {
		t.Fatalf("read README after rollback: %v", err)
	}
	if string(data) != "hello\n" {
		t.Fatalf("tracked file was not restored, got %q", string(data))
	}
	if _, err := os.Stat(filepath.Join(lease.Path, "scratch.txt")); !os.IsNotExist(err) {
		t.Fatalf("untracked file should be removed, stat err=%v", err)
	}
}

func initReviewGitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
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
