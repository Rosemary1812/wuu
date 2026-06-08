package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSnapshotCacheHit(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := cwd
	for {
		if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Skip("not in a git repo")
		}
		root = parent
	}

	// Clear cache.
	snapshotCache.mu.Lock()
	snapshotCache.captured = time.Time{}
	snapshotCache.mu.Unlock()

	// First call warms cache.
	info1 := Snapshot(root)
	if info1.CWD == "" {
		t.Fatal("expected non-empty CWD")
	}

	// Second call should hit cache.
	info2 := Snapshot(root)
	if info2.GitBranch != info1.GitBranch {
		t.Fatalf("cache miss: branch changed from %q to %q", info1.GitBranch, info2.GitBranch)
	}
	if info2.GitStatus != info1.GitStatus {
		t.Fatalf("cache miss: status changed from %q to %q", info1.GitStatus, info2.GitStatus)
	}
}

func TestSnapshotCacheDifferentCWD(t *testing.T) {
	// Ensure cache stores per-CWD.
	snapshotCache.mu.Lock()
	snapshotCache.captured = time.Time{}
	snapshotCache.mu.Unlock()

	_ = Snapshot("/tmp/fake-a")
	info2 := Snapshot("/tmp/fake-b")

	// Second call with different CWD must not reuse first CWD.
	if info2.CWD != "/tmp/fake-b" {
		t.Fatalf("expected CWD /tmp/fake-b, got %q", info2.CWD)
	}
}

func TestCompileBlocksRendersTypedContext(t *testing.T) {
	got := CompileBlocks([]Block{
		{Kind: BlockProjectRules, Title: "Rules", Source: "AGENTS.md", Content: "Use gofmt.", TokenBudget: 200},
		{Kind: BlockRecentDiff, Content: "   "},
		{Kind: BlockTestFailures, Content: "go test failed"},
	})

	for _, want := range []string{
		"[PROJECT_RULES]",
		"title: Rules",
		"source: AGENTS.md",
		"token_budget: 200",
		"Use gofmt.",
		"[TEST_FAILURES]",
		"go test failed",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("compiled context missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "[RECENT_DIFF]") {
		t.Fatalf("empty blocks should be skipped:\n%s", got)
	}
}

func TestFormatSystemReminderUsesTypedEnvironmentBlock(t *testing.T) {
	got := FormatSystemReminder(EnvInfo{
		CWD:       "/repo",
		Date:      "2026-06-09",
		GitBranch: "main",
		GitStatus: "clean",
	}, "# Extra\n\nUse targeted tests.")

	for _, want := range []string{
		"<system-reminder>",
		"[ENVIRONMENT]",
		"title: Runtime environment",
		"source: runtime.snapshot",
		"# Environment",
		"- CWD: /repo",
		"[ADDITIONAL_CONTEXT]",
		"Use targeted tests.",
		"</system-reminder>",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("system reminder missing %q:\n%s", want, got)
		}
	}
}
