package worktree

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initRepo creates a minimal git repo at dir with one commit.
func initRepo(t *testing.T, dir string) {
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
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "init")
}

func TestNewManager_NotGitRepo(t *testing.T) {
	dir := t.TempDir() // not a git repo
	_, err := NewManager(dir, filepath.Join(dir, "wt"))
	if err == nil {
		t.Fatal("expected error for non-git directory")
	}
}

func TestNewManager_GitRepo(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	m, err := NewManager(dir, filepath.Join(dir, ".wuu", "worktrees"))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if m.parentRepo == "" {
		t.Fatal("parentRepo not set")
	}
}

func TestCreateAndCleanup(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	m, err := NewManager(dir, filepath.Join(dir, ".wuu", "worktrees"))
	if err != nil {
		t.Fatal(err)
	}

	wt, err := m.Create("sess-1", "worker-A", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if wt.Path == "" {
		t.Fatal("worktree path empty")
	}
	if _, err := os.Stat(wt.Path); err != nil {
		t.Fatalf("worktree not on disk: %v", err)
	}
	// README.md from initial commit should exist in the worktree.
	if _, err := os.Stat(filepath.Join(wt.Path, "README.md")); err != nil {
		t.Fatalf("expected README.md in worktree, got: %v", err)
	}
	if wt.HEAD == "" {
		t.Fatal("HEAD not recorded")
	}

	if err := m.Cleanup(wt); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if _, err := os.Stat(wt.Path); !os.IsNotExist(err) {
		t.Fatalf("worktree should be removed, got: %v", err)
	}
}

func TestCleanupReportsLockedRegistryEntryAfterFallback(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	m, err := NewManager(dir, filepath.Join(dir, ".wuu", "worktrees"))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	wt, err := m.Create("sess-locked", "worker-locked", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		cmd := exec.Command("git", "worktree", "unlock", wt.Path)
		cmd.Dir = dir
		_ = cmd.Run()
		_ = m.Cleanup(wt)
	})

	// A locked worktree makes `remove --force` fail. The existing fallback
	// removes the directory manually, while `git worktree prune` deliberately
	// keeps the locked registry entry and still exits successfully.
	cmd := exec.Command("git", "worktree", "lock", "--reason", "cleanup-test", wt.Path)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("lock worktree: %v\n%s", err, out)
	}

	cleanupErr := m.Cleanup(wt)
	if cleanupErr == nil {
		t.Fatal("Cleanup succeeded while Git still retained the locked worktree")
	}
	if !strings.Contains(cleanupErr.Error(), "Git registry still contains") {
		t.Fatalf("Cleanup error = %v, want registry inconsistency", cleanupErr)
	}
	if _, err := os.Lstat(wt.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("manual fallback should have removed the worktree path, got %v", err)
	}
	registered, err := m.worktreeRegistered(wt.Path)
	if err != nil {
		t.Fatalf("check worktree registry: %v", err)
	}
	if !registered {
		t.Fatal("locked worktree registry entry unexpectedly disappeared")
	}

	cmd = exec.Command("git", "worktree", "unlock", wt.Path)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("unlock missing worktree entry: %v\n%s", err, out)
	}
	if err := m.Cleanup(wt); err != nil {
		t.Fatalf("Cleanup after unlock: %v", err)
	}
	registered, err = m.worktreeRegistered(wt.Path)
	if err != nil {
		t.Fatalf("check final worktree registry: %v", err)
	}
	if registered {
		t.Fatal("worktree registry entry remains after successful cleanup")
	}
}

func TestCleanupPropagatesPruneFailure(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	m, err := NewManager(dir, filepath.Join(dir, ".wuu", "worktrees"))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	wt, err := m.Create("sess-prune", "worker-prune", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	originalParent := m.parentRepo
	t.Cleanup(func() {
		m.parentRepo = originalParent
		_ = m.Cleanup(wt)
	})

	// Make every Git cleanup command fail deterministically without relying on
	// filesystem permissions. The manual disk fallback still succeeds, leaving
	// the original repository's registry entry behind.
	m.parentRepo = filepath.Join(dir, "missing-parent-repo")
	cleanupErr := m.Cleanup(wt)
	m.parentRepo = originalParent
	if cleanupErr == nil {
		t.Fatal("Cleanup swallowed git worktree prune failure")
	}
	if !strings.Contains(cleanupErr.Error(), "git worktree prune") {
		t.Fatalf("Cleanup error = %v, want prune failure", cleanupErr)
	}
	if _, err := os.Lstat(wt.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("manual fallback should have removed the worktree path, got %v", err)
	}
	registered, err := m.worktreeRegistered(wt.Path)
	if err != nil {
		t.Fatalf("check worktree registry: %v", err)
	}
	if !registered {
		t.Fatal("worktree registry entry unexpectedly disappeared after prune failure")
	}

	if err := m.Cleanup(wt); err != nil {
		t.Fatalf("Cleanup after restoring parent repo: %v", err)
	}
}

func TestCreate_DuplicateFails(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	m, _ := NewManager(dir, filepath.Join(dir, ".wuu", "worktrees"))

	wt, err := m.Create("sess", "dup", "")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Cleanup(wt)

	if _, err := m.Create("sess", "dup", ""); err == nil {
		t.Fatal("expected duplicate Create to fail")
	}
}

func TestCleanupSession(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	m, _ := NewManager(dir, filepath.Join(dir, ".wuu", "worktrees"))

	for _, wid := range []string{"a", "b", "c"} {
		if _, err := m.Create("sess-X", wid, ""); err != nil {
			t.Fatalf("Create %s: %v", wid, err)
		}
	}

	list, _ := m.List("sess-X")
	if len(list) != 3 {
		t.Fatalf("expected 3 worktrees, got %d", len(list))
	}

	if err := m.CleanupSession("sess-X"); err != nil {
		t.Fatalf("CleanupSession: %v", err)
	}

	list, _ = m.List("sess-X")
	if len(list) != 0 {
		t.Fatalf("expected 0 worktrees after cleanup, got %d", len(list))
	}
}

func TestCreate_FromBaseRepo(t *testing.T) {
	// Make a base repo, commit to it, then create a chained worktree
	// from a worktree of it.
	dir := t.TempDir()
	initRepo(t, dir)
	m, _ := NewManager(dir, filepath.Join(dir, ".wuu", "worktrees"))

	wtA, err := m.Create("sess", "A", "")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Cleanup(wtA)

	// Make a change in worktree A and commit it.
	if err := os.WriteFile(filepath.Join(wtA.Path, "new.txt"), []byte("from A"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"add", "new.txt"},
		{"commit", "-q", "-m", "from A"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = wtA.Path
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v in worktree A: %v\n%s", args, err, out)
		}
	}

	// Now spawn worker B based on worktree A.
	wtB, err := m.Create("sess", "B", wtA.Path)
	if err != nil {
		t.Fatalf("Create chained: %v", err)
	}
	defer m.Cleanup(wtB)

	// Worker B should see the file A added.
	if _, err := os.Stat(filepath.Join(wtB.Path, "new.txt")); err != nil {
		t.Fatalf("chained worktree should contain A's commit: %v", err)
	}
}

func TestList_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	m, _ := NewManager(dir, filepath.Join(dir, ".wuu", "worktrees"))

	list, err := m.List("nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("expected empty list, got %d", len(list))
	}
}

func TestHasChanges(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	m, _ := NewManager(dir, filepath.Join(dir, ".wuu", "worktrees"))

	wt, err := m.Create("sess", "wA", "")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Cleanup(wt)

	// Pristine worktree should be clean.
	dirty, err := m.HasChanges(wt)
	if err != nil {
		t.Fatalf("HasChanges (clean): %v", err)
	}
	if dirty {
		t.Fatal("expected clean, got dirty")
	}

	// Dropping an untracked file should flip it to dirty.
	if err := os.WriteFile(filepath.Join(wt.Path, "scratch.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	dirty, err = m.HasChanges(wt)
	if err != nil {
		t.Fatalf("HasChanges (untracked): %v", err)
	}
	if !dirty {
		t.Fatal("expected dirty after untracked add")
	}
}

func TestCleanupIfClean(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	m, _ := NewManager(dir, filepath.Join(dir, ".wuu", "worktrees"))

	// Clean worktree: removed.
	wt1, err := m.Create("sess", "clean", "")
	if err != nil {
		t.Fatal(err)
	}
	kept, err := m.CleanupIfClean(wt1)
	if err != nil {
		t.Fatalf("CleanupIfClean clean: %v", err)
	}
	if kept {
		t.Fatal("clean worktree should not be kept")
	}
	if _, err := os.Stat(wt1.Path); !os.IsNotExist(err) {
		t.Fatalf("clean worktree should be gone, got: %v", err)
	}

	// Dirty worktree: kept.
	wt2, err := m.Create("sess", "dirty", "")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Cleanup(wt2)
	if err := os.WriteFile(filepath.Join(wt2.Path, "edit.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	kept, err = m.CleanupIfClean(wt2)
	if err != nil {
		t.Fatalf("CleanupIfClean dirty: %v", err)
	}
	if !kept {
		t.Fatal("dirty worktree should be kept")
	}
	if _, err := os.Stat(wt2.Path); err != nil {
		t.Fatalf("dirty worktree should still exist: %v", err)
	}
}

func TestCreateLeaseWritesManifestAndReview(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	m, err := NewManager(dir, filepath.Join(dir, ".wuu", "worktrees"))
	if err != nil {
		t.Fatal(err)
	}

	lease, err := m.CreateLease(LeaseOptions{
		SessionID:   "sess",
		TaskID:      "task-1",
		AgentID:     "agent-1",
		Branch:      "wuu/task-1",
		ManifestDir: filepath.Join(dir, ".wuu", "leases"),
	})
	if err != nil {
		t.Fatalf("CreateLease: %v", err)
	}
	defer m.Cleanup(&Worktree{Path: lease.Path})

	if lease.TaskID != "task-1" || lease.AgentID != "agent-1" || lease.BaseHEAD == "" || lease.Branch != "wuu/task-1" {
		t.Fatalf("lease identity not recorded: %+v", lease)
	}
	if _, err := os.Stat(lease.ManifestPath); err != nil {
		t.Fatalf("manifest should exist: %v", err)
	}

	if err := os.WriteFile(filepath.Join(lease.Path, "README.md"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lease.Path, "new.txt"), []byte("untracked"), 0o644); err != nil {
		t.Fatal(err)
	}
	review, err := m.Review(lease, dir)
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if !review.Status.Dirty || len(review.Status.ChangedFiles) != 2 {
		t.Fatalf("review should see changed tracked and untracked files: %+v", review.Status)
	}
	if review.Diff == "" || !review.MergePreview.CanApply {
		t.Fatalf("expected tracked diff with clean merge preview: %+v", review)
	}

	if err := m.WriteManifest(lease); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	data, err := os.ReadFile(lease.ManifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if string(data) == "" || !containsBytes(data, []byte(`"dirty": true`)) {
		t.Fatalf("manifest should record dirty state:\n%s", string(data))
	}
}

func TestApplyToTargetAppliesTrackedDiff(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	m, err := NewManager(dir, filepath.Join(dir, ".wuu", "worktrees"))
	if err != nil {
		t.Fatal(err)
	}
	lease, err := m.CreateLease(LeaseOptions{SessionID: "sess", TaskID: "apply"})
	if err != nil {
		t.Fatalf("CreateLease: %v", err)
	}
	defer m.Cleanup(&Worktree{Path: lease.Path})
	if err := os.WriteFile(filepath.Join(lease.Path, "README.md"), []byte("applied\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := m.ApplyToTarget(lease, dir)
	if err != nil {
		t.Fatalf("ApplyToTarget: %v", err)
	}
	if !result.Applied || len(result.ChangedFiles) != 1 || result.ChangedFiles[0] != "README.md" {
		t.Fatalf("unexpected apply result: %+v", result)
	}
	data, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatalf("read target file: %v", err)
	}
	if string(data) != "applied\n" {
		t.Fatalf("target file not updated: %q", string(data))
	}
}

func TestApplyToTargetRejectsUntrackedWorktreeFiles(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	m, err := NewManager(dir, filepath.Join(dir, ".wuu", "worktrees"))
	if err != nil {
		t.Fatal(err)
	}
	lease, err := m.CreateLease(LeaseOptions{SessionID: "sess", TaskID: "apply-untracked"})
	if err != nil {
		t.Fatalf("CreateLease: %v", err)
	}
	defer m.Cleanup(&Worktree{Path: lease.Path})
	if err := os.WriteFile(filepath.Join(lease.Path, "new.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = m.ApplyToTarget(lease, dir)
	if err == nil || !strings.Contains(err.Error(), "untracked files") {
		t.Fatalf("expected untracked rejection, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "new.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("untracked file should not be applied, stat err=%v", statErr)
	}
}

func TestRollbackLeaseResetsIsolatedWorktree(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	m, err := NewManager(dir, filepath.Join(dir, ".wuu", "worktrees"))
	if err != nil {
		t.Fatal(err)
	}
	lease, err := m.CreateLease(LeaseOptions{SessionID: "sess", TaskID: "rollback"})
	if err != nil {
		t.Fatalf("CreateLease: %v", err)
	}
	defer m.Cleanup(&Worktree{Path: lease.Path})

	if err := os.WriteFile(filepath.Join(lease.Path, "README.md"), []byte("bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lease.Path, "scratch.txt"), []byte("bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	status, err := m.Status(lease)
	if err != nil {
		t.Fatalf("Status dirty: %v", err)
	}
	if !status.Dirty {
		t.Fatal("expected dirty lease")
	}

	if err := m.RollbackLease(lease); err != nil {
		t.Fatalf("RollbackLease: %v", err)
	}
	status, err = m.Status(lease)
	if err != nil {
		t.Fatalf("Status clean: %v", err)
	}
	if status.Dirty {
		t.Fatalf("expected clean lease after rollback: %+v", status)
	}
	if _, err := os.Stat(filepath.Join(lease.Path, "scratch.txt")); !os.IsNotExist(err) {
		t.Fatalf("scratch should be removed, got %v", err)
	}
}

func containsBytes(haystack, needle []byte) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func TestIsGitRepo(t *testing.T) {
	notGit := t.TempDir()
	if IsGitRepo(notGit) {
		t.Error("temp dir should not be a git repo")
	}

	gitDir := t.TempDir()
	initRepo(t, gitDir)
	if !IsGitRepo(gitDir) {
		t.Error("initialized dir should be a git repo")
	}
}
