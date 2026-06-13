package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
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
