package context

import (
	"os"
	"path/filepath"
	"strconv"
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

func TestRepoMapBlockSummarizesWorkspace(t *testing.T) {
	root := t.TempDir()
	mustWriteContextTestFile(t, filepath.Join(root, "AGENTS.md"), "rules\n")
	mustWriteContextTestFile(t, filepath.Join(root, "go.mod"), "module example.com/repo\n")
	mustWriteContextTestFile(t, filepath.Join(root, "cmd/app/main.go"), "package main\n")
	mustWriteContextTestFile(t, filepath.Join(root, "cmd/app/main_test.go"), "package main\n")
	mustWriteContextTestFile(t, filepath.Join(root, "web/app.tsx"), "export const App = () => null\n")
	mustWriteContextTestFile(t, filepath.Join(root, "web/app.test.tsx"), "test('app', () => {})\n")
	mustWriteContextTestFile(t, filepath.Join(root, "node_modules/pkg/index.js"), "ignored\n")

	summary, ok, err := BuildRepoMap(root, RepoMapOptions{MaxListedFiles: 10})
	if err != nil {
		t.Fatalf("BuildRepoMap: %v", err)
	}
	if !ok {
		t.Fatal("expected repo map summary")
	}
	if summary.FilesScanned != 6 || summary.OmittedFiles != 0 {
		t.Fatalf("unexpected repo map summary counts: %+v", summary)
	}
	if len(summary.TestMappings) != 2 || summary.TestMappings[0].Source != "cmd/app/main.go" || summary.TestMappings[0].Test != "cmd/app/main_test.go" {
		t.Fatalf("unexpected repo map test mappings: %+v", summary.TestMappings)
	}
	if len(summary.RepresentativeFiles) == 0 || summary.RepresentativeFiles[0] != "AGENTS.md" {
		t.Fatalf("unexpected representative files: %+v", summary.RepresentativeFiles)
	}

	block, ok := RepoMapBlock(root, RepoMapOptions{MaxListedFiles: 10})
	if !ok {
		t.Fatal("expected repo map block")
	}
	if block.Kind != BlockRepoMap || block.Source != "runtime.repo_map" {
		t.Fatalf("unexpected repo map block metadata: %+v", block)
	}
	for _, want := range []string{
		"files_scanned: 6",
		"languages:",
		"- go: 2",
		"- typescript: 2",
		"test_files:",
		"- cmd/app/main_test.go",
		"- web/app.test.tsx",
		"test_mappings:",
		"- cmd/app/main.go -> cmd/app/main_test.go",
		"- web/app.tsx -> web/app.test.tsx",
		"representative_files:",
		"- AGENTS.md",
		"- go.mod",
		"- cmd/app/main.go",
	} {
		if !strings.Contains(block.Content, want) {
			t.Fatalf("repo map missing %q:\n%s", want, block.Content)
		}
	}
	if strings.Contains(block.Content, "node_modules") {
		t.Fatalf("repo map should skip node_modules:\n%s", block.Content)
	}
}

func mustWriteContextTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
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

func TestIsAgentNotificationDetectsNamedAndLegacyHandoffs(t *testing.T) {
	rawNotification := `<subagent_notification>
{"agent_path":"/root/worker","status":{"type":"agent_result","result":"done"}}
</subagent_notification>`
	envelope := `{"author":"/root/worker","recipient":"/root","content":` + strconv.Quote(rawNotification) + `,"trigger_turn":true}`

	cases := []struct {
		name    string
		msgName string
		content string
		want    bool
	}{
		{name: "named", msgName: AgentNotificationMessageName, content: "anything", want: true},
		{name: "raw notification", content: rawNotification, want: true},
		{name: "inter-agent envelope", content: envelope, want: true},
		{name: "normal user json", content: `{"content":"plain user text"}`, want: false},
		{name: "normal user text", content: "please inspect this", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsAgentNotification(tc.msgName, tc.content); got != tc.want {
				t.Fatalf("IsAgentNotification() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIsGoalContinuationDetectsInternalContinuation(t *testing.T) {
	cases := []struct {
		name    string
		msgName string
		content string
		want    bool
	}{
		{name: "named", msgName: GoalContinuationMessageName, content: "anything", want: true},
		{name: "envelope", content: "<goal_continuation>\nkeep going\n</goal_continuation>", want: true},
		{name: "normal user text", content: "please continue", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsGoalContinuation(tc.msgName, tc.content); got != tc.want {
				t.Fatalf("IsGoalContinuation() = %v, want %v", got, tc.want)
			}
		})
	}
}
