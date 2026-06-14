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
