package context

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestSnapshotIncludesOnlyLightweightRuntimeInfo(t *testing.T) {
	info := Snapshot("/tmp/project")
	if info.CWD != "/tmp/project" {
		t.Fatalf("expected CWD /tmp/project, got %q", info.CWD)
	}
	if info.Date == "" {
		t.Fatal("expected current date")
	}
	if info.GitBranch != "" || info.GitStatus != "" {
		t.Fatalf("default snapshot should not collect git state: %+v", info)
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

func TestCompileBlocksEnforcesTokenBudget(t *testing.T) {
	longContent := strings.Repeat("src/internal/really/long/path/to/file.go\n", 200)
	got := CompileBlocks([]Block{
		{Kind: BlockRepoMap, Title: "Repo map", Source: "runtime.repo_map", Content: longContent, TokenBudget: 40},
	})

	for _, want := range []string{
		"[REPO_MAP]",
		"token_budget: 40",
		"truncated: block content exceeded token_budget 40;",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("compiled context missing %q:\n%s", want, got)
		}
	}
	if len(got) >= len(longContent) {
		t.Fatalf("expected content to be truncated, got %d chars from %d-char input", len(got), len(longContent))
	}
	if strings.Count(got, "src/internal/really/long/path/to/file.go") >= 200 {
		t.Fatalf("expected repeated paths to be truncated:\n%s", got)
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

func TestSystemReminderBlockMessageNameIsStableAndInternal(t *testing.T) {
	block := Block{
		Kind:    BlockRepoMap,
		Title:   "Compact repository map",
		Source:  "runtime.repo_map",
		Content: "files_scanned: 3",
	}
	name := SystemReminderBlockMessageName(block, 0)
	if name == "" || len(name) > 64 {
		t.Fatalf("unexpected context message name length: %q", name)
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		t.Fatalf("context message name should be provider-safe, got %q", name)
	}
	changed := block
	changed.Content = "files_scanned: 99"
	if got := SystemReminderBlockMessageName(changed, 0); got != name {
		t.Fatalf("content changes should not change context message name: got %q want %q", got, name)
	}
	if got := SystemReminderBlockMessageName(block, 1); got == name {
		t.Fatalf("duplicate block ordinals should get distinct names: %q", got)
	}
	if !IsSystemReminder(name, "plain content") {
		t.Fatalf("split context message name should be recognized as internal")
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
		{name: "envelope with overlap sibling (named)", msgName: AgentNotificationMessageName, content: `{"author":"/root/worker","recipient":"/root","content":` + strconv.Quote(rawNotification) + `,"trigger_turn":true,"changed_file_overlap":["changed_file_overlap: foo.go touched by /root/a, /root/b"]}`, want: true},
		{name: "envelope with overlap sibling (unnamed)", content: `{"author":"/root/worker","recipient":"/root","content":` + strconv.Quote(rawNotification) + `,"trigger_turn":true,"changed_file_overlap":["changed_file_overlap: foo.go touched by /root/a, /root/b"]}`, want: true},
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
