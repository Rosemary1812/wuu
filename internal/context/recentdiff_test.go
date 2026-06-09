package context

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecentDiffBlockReturnsCompactDirtyGitSummary(t *testing.T) {
	root := t.TempDir()
	initRecentDiffGitRepo(t, root)
	writeRecentDiffFile(t, filepath.Join(root, "a.txt"), "before\n")
	runRecentDiffGit(t, root, "add", "a.txt")
	runRecentDiffGit(t, root, "commit", "-m", "initial")

	writeRecentDiffFile(t, filepath.Join(root, "a.txt"), "after\n")
	writeRecentDiffFile(t, filepath.Join(root, "b.txt"), "new\n")
	writeRecentDiffFile(t, filepath.Join(root, ".env"), "API_KEY=secret-value-1234567890\n")

	block, ok := RecentDiffBlock(root, RecentDiffOptions{MaxFiles: 10})
	if !ok {
		t.Fatal("expected recent diff block for dirty repo")
	}
	if block.Kind != BlockRecentDiff || block.Source != "runtime.recent_diff" {
		t.Fatalf("unexpected block metadata: %+v", block)
	}
	for _, want := range []string{
		"base_revision:",
		"changed_files: 3",
		"unstaged_shortstat: 1 file changed, 1 insertion(+), 1 deletion(-)",
		"files:",
		"- M a.txt",
		"- ?? b.txt",
		"- ?? [REDACTED sensitive path]",
		"diff bodies are omitted",
	} {
		if !strings.Contains(block.Content, want) {
			t.Fatalf("recent diff block missing %q:\n%s", want, block.Content)
		}
	}
	if strings.Contains(block.Content, ".env") || strings.Contains(block.Content, "secret-value") {
		t.Fatalf("recent diff block leaked sensitive path/content:\n%s", block.Content)
	}
}

func TestRecentDiffBlockReturnsFalseForCleanOrNonGitWorkspace(t *testing.T) {
	if block, ok := RecentDiffBlock(t.TempDir(), RecentDiffOptions{}); ok {
		t.Fatalf("non-git workspace should not produce recent diff block: %+v", block)
	}

	root := t.TempDir()
	initRecentDiffGitRepo(t, root)
	writeRecentDiffFile(t, filepath.Join(root, "a.txt"), "clean\n")
	runRecentDiffGit(t, root, "add", "a.txt")
	runRecentDiffGit(t, root, "commit", "-m", "initial")
	if block, ok := RecentDiffBlock(root, RecentDiffOptions{}); ok {
		t.Fatalf("clean git workspace should not produce recent diff block: %+v", block)
	}
}

func initRecentDiffGitRepo(t *testing.T, root string) {
	t.Helper()
	runRecentDiffGit(t, root, "init")
	runRecentDiffGit(t, root, "config", "user.email", "test@example.com")
	runRecentDiffGit(t, root, "config", "user.name", "Test User")
}

func runRecentDiffGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, string(out))
	}
}

func writeRecentDiffFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir parent: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}
