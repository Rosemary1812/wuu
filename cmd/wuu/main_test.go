package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/config"
	"github.com/blueberrycongee/wuu/internal/evalharness"
	looprunner "github.com/blueberrycongee/wuu/internal/loop"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/runtime"
	"github.com/blueberrycongee/wuu/internal/sessiontrace"
	"github.com/blueberrycongee/wuu/internal/statepath"
	"github.com/blueberrycongee/wuu/internal/tools"
	"github.com/blueberrycongee/wuu/internal/workflow"
)

func TestRunVersionAliasForwardsJSONFlag(t *testing.T) {
	output := captureStdout(t, func() {
		if err := run([]string{"--version", "--json"}); err != nil {
			t.Fatalf("run returned error: %v", err)
		}
	})

	var payload map[string]any
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("expected JSON output, got %q: %v", output, err)
	}
	if _, ok := payload["version"]; !ok {
		t.Fatalf("expected version field in JSON output: %v", payload)
	}
}

func TestRunVersionAliasForwardsLongFlag(t *testing.T) {
	output := captureStdout(t, func() {
		if err := run([]string{"-v", "--long"}); err != nil {
			t.Fatalf("run returned error: %v", err)
		}
	})

	if !strings.Contains(output, "version:") {
		t.Fatalf("expected long version output, got %q", output)
	}
	if !strings.Contains(output, "commit:") {
		t.Fatalf("expected long version output to include commit, got %q", output)
	}
}

func TestRunWithoutArgsPrintsUsage(t *testing.T) {
	output := captureStdout(t, func() {
		if err := run(nil); err != nil {
			t.Fatalf("run returned error: %v", err)
		}
	})
	if !strings.Contains(output, "GUI-first") || strings.Contains(output, "wuu tui") {
		t.Fatalf("unexpected usage output: %q", output)
	}
}

func TestRunTUICommandIsRemoved(t *testing.T) {
	err := run([]string{"tui"})
	if err == nil || !strings.Contains(err.Error(), "TUI has been removed") {
		t.Fatalf("expected removed TUI error, got %v", err)
	}
}

func TestRunInitWritesDefaultConfig(t *testing.T) {
	workdir := t.TempDir()
	t.Chdir(workdir)
	output := captureStdout(t, func() {
		if err := run([]string{"init", "--force"}); err != nil {
			t.Fatalf("run returned error: %v", err)
		}
	})
	if !strings.Contains(output, "created") {
		t.Fatalf("expected created output, got %q", output)
	}
	data, err := os.ReadFile(filepath.Join(workdir, ".wuu.json"))
	if err != nil {
		t.Fatalf("expected config file: %v", err)
	}
	var cfg config.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("expected JSON config: %v", err)
	}
	if cfg.DefaultProvider == "" || len(cfg.Providers) == 0 {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestRunGoalDemoWritesDurableState(t *testing.T) {
	workdir := t.TempDir()
	wuuHome := filepath.Join(t.TempDir(), "wuu-home")
	t.Setenv("WUU_HOME", wuuHome)
	if err := os.WriteFile(filepath.Join(workdir, "marker.txt"), []byte("ok"), 0644); err != nil {
		t.Fatal(err)
	}

	output := captureStdout(t, func() {
		if err := run([]string{
			"goal", "demo",
			"--workdir", workdir,
			"--id", "loop-test",
			"--goal", "prove durable goal",
			"--verify-command", "test -f marker.txt",
		}); err != nil {
			t.Fatalf("run goal demo: %v", err)
		}
	})
	if !strings.Contains(output, "status: completed") {
		t.Fatalf("expected completed summary, got %q", output)
	}
	loopDir := cliLoopDir(t, wuuHome, workdir, "loop-test")
	for _, rel := range []string{
		"state.json",
		"events.jsonl",
		"views/progress.md",
		"views/decisions.md",
		"views/failures.md",
		"views/approvals.md",
		"artifacts/research.md",
		"artifacts/plan.md",
		"artifacts/todo.md",
		"artifacts/verification.md",
		"artifacts/review.md",
		"artifacts/integration.md",
		"artifacts/final.md",
	} {
		if _, err := os.Stat(filepath.Join(loopDir, rel)); err != nil {
			t.Fatalf("expected %s: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(workdir, ".loop")); !os.IsNotExist(err) {
		t.Fatalf("project root .loop should not be created, stat err=%v", err)
	}

	status := captureStdout(t, func() {
		if err := run([]string{"goal", "status", "--workdir", workdir, "--id", "loop-test", "--json"}); err != nil {
			t.Fatalf("run goal status: %v", err)
		}
	})
	var state struct {
		ID          string `json:"id"`
		Status      string `json:"status"`
		Final       string `json:"final_artifact"`
		TestResults []struct {
			Passed bool `json:"passed"`
		} `json:"test_results"`
	}
	if err := json.Unmarshal([]byte(status), &state); err != nil {
		t.Fatalf("parse goal status JSON: %v\n%s", err, status)
	}
	if state.ID != "loop-test" || state.Status != "completed" || state.Final == "" {
		t.Fatalf("unexpected goal state: %+v", state)
	}
	if len(state.TestResults) != 1 || !state.TestResults[0].Passed {
		t.Fatalf("expected one passing test result, got %+v", state.TestResults)
	}
}

func TestRunGoalDemoRecordsVerificationFailure(t *testing.T) {
	workdir := t.TempDir()
	wuuHome := filepath.Join(t.TempDir(), "wuu-home")
	t.Setenv("WUU_HOME", wuuHome)

	output := captureStdout(t, func() {
		if err := run([]string{
			"goal", "demo",
			"--workdir", workdir,
			"--id", "loop-fail",
			"--goal", "capture failure",
			"--verify-command", "test -f missing.txt",
		}); err != nil {
			t.Fatalf("run goal demo: %v", err)
		}
	})
	if !strings.Contains(output, "status: needs_human") {
		t.Fatalf("expected needs_human summary, got %q", output)
	}
	loopDir := cliLoopDir(t, wuuHome, workdir, "loop-fail")
	failures, err := os.ReadFile(filepath.Join(loopDir, "views", "failures.md"))
	if err != nil {
		t.Fatalf("read failures: %v", err)
	}
	if !strings.Contains(string(failures), "verification_command_failed") {
		t.Fatalf("expected verification failure, got %s", failures)
	}
	if _, err := os.Stat(filepath.Join(workdir, ".loop")); !os.IsNotExist(err) {
		t.Fatalf("project root .loop should not be created, stat err=%v", err)
	}
}

func TestRunLoopCommandRemainsGoalAlias(t *testing.T) {
	workdir := t.TempDir()
	wuuHome := filepath.Join(t.TempDir(), "wuu-home")
	t.Setenv("WUU_HOME", wuuHome)

	output := captureStdout(t, func() {
		if err := run([]string{
			"loop", "demo",
			"--workdir", workdir,
			"--id", "loop-alias",
			"--goal", "prove old command alias",
		}); err != nil {
			t.Fatalf("run loop alias: %v", err)
		}
	})
	if !strings.Contains(output, "goal_id: loop-alias") {
		t.Fatalf("expected goal summary from loop alias, got %q", output)
	}
}

func cliLoopDir(t *testing.T, wuuHome, workdir, loopID string) string {
	t.Helper()
	workspaceStateDir, err := statepath.WorkspaceDir(wuuHome, workdir)
	if err != nil {
		t.Fatalf("WorkspaceDir: %v", err)
	}
	return statepath.LoopDir(workspaceStateDir, loopID)
}

func TestLoadOrCreateAppServerConfigCreatesStarterConfig(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()

	cfg, configPath, err := loadOrCreateAppServerConfig(root, home)
	if err != nil {
		t.Fatalf("loadOrCreateAppServerConfig: %v", err)
	}

	expectedPath := filepath.Join(home, ".config", "wuu", "config.json")
	if configPath != expectedPath {
		t.Fatalf("expected config path %q, got %q", expectedPath, configPath)
	}
	if cfg.DefaultProvider != "openai-codex" {
		t.Fatalf("expected openai-codex default provider, got %q", cfg.DefaultProvider)
	}
	if len(cfg.Providers) != 1 {
		t.Fatalf("expected one starter provider, got %+v", cfg.Providers)
	}
	if _, ok := cfg.Providers["openai-codex"]; !ok {
		t.Fatalf("starter config missing openai-codex provider: %+v", cfg.Providers)
	}

	loaded, loadedPath, err := config.LoadFrom(root, home)
	if err != nil {
		t.Fatalf("reload starter config: %v", err)
	}
	if loadedPath != configPath || loaded.DefaultProvider != "openai-codex" {
		t.Fatalf("unexpected reloaded config: path=%q cfg=%+v", loadedPath, loaded)
	}
}

func TestRunModelsRejectsUnsupportedProvider(t *testing.T) {
	workdir := t.TempDir()
	configPath := workdir + "/.wuu.json"
	data := `{
  "default_provider": "main",
  "providers": {
    "main": {
      "type": "openai-compatible",
      "base_url": "https://example.com/v1",
      "api_key": "sk-test",
      "model": "gpt-test"
    }
  },
  "agent": {
    "system_prompt": "test"
  }
}`
	if err := os.WriteFile(configPath, []byte(data), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	err := run([]string{"models", "--workdir", workdir})
	if err == nil {
		t.Fatal("expected unsupported provider error")
	}
	if !strings.Contains(err.Error(), "openai-codex providers only") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunEvalListDoesNotRequireConfig(t *testing.T) {
	output := captureStdout(t, func() {
		if err := run([]string{"eval", "--list"}); err != nil {
			t.Fatalf("run returned error: %v", err)
		}
	})

	if !strings.Contains(output, "test_failure_fix") ||
		!strings.Contains(output, "git_test_failure_fix") ||
		!strings.Contains(output, "multi_file_pricing") ||
		!strings.Contains(output, "long_process_output") ||
		!strings.Contains(output, "tool_search_deferred") ||
		!strings.Contains(output, "stale_read_guard") ||
		!strings.Contains(output, "mcp_readonly_concurrency") ||
		!strings.Contains(output, "mcp_live_discovery") ||
		!strings.Contains(output, "multi_agent_worker") ||
		!strings.Contains(output, "checkpoint_rollback") {
		t.Fatalf("expected built-in eval tasks, got %q", output)
	}
}

func TestResolveEvalTasksSelectsCommaSeparatedIDs(t *testing.T) {
	tasks, err := resolveEvalTasks("test_failure_fix,multi_file_pricing")
	if err != nil {
		t.Fatalf("resolveEvalTasks: %v", err)
	}
	if len(tasks) != 2 || tasks[0].ID != "test_failure_fix" || tasks[1].ID != "multi_file_pricing" {
		t.Fatalf("unexpected tasks: %+v", tasks)
	}
}

func TestResolveEvalTasksRejectsUnknownTask(t *testing.T) {
	_, err := resolveEvalTasks("missing")
	if err == nil || !strings.Contains(err.Error(), "unknown eval task") {
		t.Fatalf("expected unknown task error, got %v", err)
	}
}

func TestRunEvalLiveCodexOAuthSkipsWithoutCredentials(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex-missing"))

	output := captureStdout(t, func() {
		if err := run([]string{"eval", "--live-codex-oauth"}); err != nil {
			t.Fatalf("run returned error: %v", err)
		}
	})

	if !strings.Contains(output, "SKIP live Codex OAuth eval") {
		t.Fatalf("expected skip output, got %q", output)
	}
}

func TestMissingRequiredTools(t *testing.T) {
	missing := missingRequiredTools([]string{"tool_search", "list_cron"}, []string{"tool_search", "write_file"})
	if len(missing) != 1 || missing[0] != "list_cron" {
		t.Fatalf("unexpected missing tools: %+v", missing)
	}
	if got := missingRequiredTools([]string{"tool_search"}, []string{"tool_search"}); len(got) != 0 {
		t.Fatalf("expected no missing tools, got %+v", got)
	}

	forbidden := forbiddenToolsUsed([]string{"create_workflow", "run_workflow"}, []string{"start_workflow", "create_workflow", "create_workflow"})
	if len(forbidden) != 1 || forbidden[0] != "create_workflow" {
		t.Fatalf("unexpected forbidden tools: %+v", forbidden)
	}
}

func TestToolNameSequencePreservesRepeatedCalls(t *testing.T) {
	got := toolNameSequence([]tools.ToolExecutionRecord{
		{Name: "read_file"},
		{Name: "checkpoint"},
		{Name: "apply_patch"},
		{Name: "checkpoint"},
		{Name: "apply_patch"},
	})
	want := []string{"read_file", "checkpoint", "apply_patch", "checkpoint", "apply_patch"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tool sequence mismatch:\n got: %+v\nwant: %+v", got, want)
	}
}

func TestMissingRequiredToolErrors(t *testing.T) {
	required := []evalharness.ToolErrorRequirement{
		{ToolName: "edit_file", ErrorContains: "changed since last read"},
	}
	records := []tools.ToolExecutionRecord{
		{Name: "edit_file", Success: false, Error: "file changed since last read. Use read_file again before editing"},
	}
	if got := missingRequiredToolErrors(required, records); len(got) != 0 {
		t.Fatalf("expected no missing errors, got %+v", got)
	}

	missing := missingRequiredToolErrors(required, []tools.ToolExecutionRecord{
		{Name: "edit_file", Success: false, Error: "old_text not found"},
	})
	if len(missing) != 1 || missing[0] != "edit_file:changed since last read" {
		t.Fatalf("unexpected missing errors: %+v", missing)
	}
}

func TestMissingRequiredToolCalls(t *testing.T) {
	messages := []providers.ChatMessage{
		{
			Role: "assistant",
			ToolCalls: []providers.ToolCall{
				{Name: "checkpoint", Arguments: `{"action":"create","checkpoint_id":"before_bad_edit","paths":["target.txt","scratch.txt"]}`},
				{Name: "write_file", Arguments: `{"path":"checkpoint_result.txt","content":"CHECKPOINT_ROLLBACK_DONE\n"}`},
			},
		},
	}
	required := []evalharness.ToolCallRequirement{
		{ToolName: "checkpoint", ArgumentEquals: map[string]string{"action": "create", "checkpoint_id": "before_bad_edit"}, ArgsContains: []string{"scratch.txt"}},
		{ToolName: "write_file", ArgumentEquals: map[string]string{"path": "checkpoint_result.txt"}},
	}
	if got := missingRequiredToolCalls(required, messages); len(got) != 0 {
		t.Fatalf("expected no missing tool calls, got %+v", got)
	}

	missing := missingRequiredToolCalls([]evalharness.ToolCallRequirement{
		{ToolName: "checkpoint", ArgumentEquals: map[string]string{"action": "restore", "checkpoint_id": "before_bad_edit"}},
	}, messages)
	if len(missing) != 1 || missing[0] != "checkpoint action=restore checkpoint_id=before_bad_edit" {
		t.Fatalf("unexpected missing tool calls: %+v", missing)
	}
}

func TestMissingRequiredToolSequence(t *testing.T) {
	messages := []providers.ChatMessage{
		{
			Role: "assistant",
			ToolCalls: []providers.ToolCall{
				{Name: "checkpoint", Arguments: `{"action":"create","checkpoint_id":"before_bad_edit","paths":["target.txt","scratch.txt"]}`},
				{Name: "apply_patch", Arguments: "*** Update File: target.txt\n*** Add File: scratch.txt\n"},
			},
		},
		{
			Role: "assistant",
			ToolCalls: []providers.ToolCall{
				{Name: "checkpoint", Arguments: `{"action":"restore","checkpoint_id":"before_bad_edit"}`},
				{Name: "apply_patch", Arguments: "*** Add File: checkpoint_result.txt\n"},
			},
		},
	}
	required := []evalharness.ToolCallRequirement{
		{ToolName: "checkpoint", ArgumentEquals: map[string]string{"action": "create", "checkpoint_id": "before_bad_edit"}},
		{ToolName: "apply_patch", ArgsContains: []string{"target.txt", "scratch.txt"}},
		{ToolName: "checkpoint", ArgumentEquals: map[string]string{"action": "restore", "checkpoint_id": "before_bad_edit"}},
		{ToolName: "apply_patch", ArgsContains: []string{"checkpoint_result.txt"}},
	}
	if got := missingRequiredToolSequence(required, messages); len(got) != 0 {
		t.Fatalf("expected no missing tool sequence, got %+v", got)
	}

	outOfOrder := []providers.ChatMessage{{
		Role: "assistant",
		ToolCalls: []providers.ToolCall{
			{Name: "checkpoint", Arguments: `{"action":"restore","checkpoint_id":"before_bad_edit"}`},
			{Name: "checkpoint", Arguments: `{"action":"create","checkpoint_id":"before_bad_edit"}`},
		},
	}}
	missing := missingRequiredToolSequence(required[:2], outOfOrder)
	if len(missing) != 1 || missing[0] != "apply_patch contains=target.txt contains=scratch.txt" {
		t.Fatalf("unexpected missing sequence: %+v", missing)
	}
}

func TestEvalSafePreviewRedactsSecretsAndTruncates(t *testing.T) {
	got := evalSafePreview("access_token=secret-value-1234567890 keep", 200)
	if strings.Contains(got, "secret-value") {
		t.Fatalf("secret leaked in preview: %q", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("redaction marker missing: %q", got)
	}
	truncated := evalSafePreview(strings.Repeat("x", 40), 20)
	if !strings.Contains(truncated, "truncated") {
		t.Fatalf("expected truncation marker: %q", truncated)
	}
}

func TestEvalToolObservationsAreMetadataOnly(t *testing.T) {
	records := []tools.ToolExecutionRecord{{
		Name:                 "run_shell",
		CallID:               "call_1",
		ArgumentsSHA256:      strings.Repeat("c", 64),
		ResultAction:         "restore",
		Kind:                 tools.ToolKindShell,
		Exposure:             tools.ToolExposureDirect,
		Risk:                 tools.ToolRiskHigh,
		ClassificationReason: "destructive shell command",
		PolicyAction:         tools.ToolPolicyAllow,
		PolicyReason:         "risk policy",
		ReadOnly:             false,
		ConcurrencySafe:      false,
		DurationMS:           42,
		RevisionBefore:       "git:before:worktree:aaa",
		RevisionAfter:        "git:after:worktree:bbb",
		Success:              false,
		Error:                "authorization: bearer abc123",
		ErrorKind:            "approval_required",
		RawOutputBytes:       1024,
		ReturnedOutputBytes:  256,
		ResultBudgeted:       true,
		ResultRef:            "/tmp/wuu/tool-results/call_1.txt",
		ArtifactRefs:         []string{"/tmp/wuu/tool-results/call_1.txt", "/tmp/wuu/tool-results/run-test-logs/call_1.log"},
		ApprovalRef:          "/tmp/wuu/approvals/call_1.json",
		PatchRiskSummary:     &tools.ToolPatchRisk{FileCount: 2, HunkCount: 2, AddedLines: 8, DeletedLines: 3, Actions: map[string]int{"update": 2}, MultiFile: true, RiskLevel: "medium"},
	}}

	got := evalToolObservations(records)
	if len(got) != 1 {
		t.Fatalf("expected one observation, got %+v", got)
	}
	if got[0].Name != "run_shell" || got[0].Kind != "shell" || got[0].RawOutputBytes != 1024 || !got[0].ResultBudgeted {
		t.Fatalf("metadata not preserved: %+v", got[0])
	}
	if got[0].ArgumentsSHA256 != records[0].ArgumentsSHA256 {
		t.Fatalf("argument fingerprint not preserved: %+v", got[0])
	}
	if got[0].ResultAction != "restore" {
		t.Fatalf("result action not preserved: %+v", got[0])
	}
	if got[0].PolicyReason != "risk policy" {
		t.Fatalf("policy reason not preserved: %+v", got[0])
	}
	if got[0].ClassificationReason != "destructive shell command" {
		t.Fatalf("classification reason not preserved: %+v", got[0])
	}
	if got[0].RevisionBefore != records[0].RevisionBefore || got[0].RevisionAfter != records[0].RevisionAfter {
		t.Fatalf("revisions not preserved: %+v", got[0])
	}
	if strings.Contains(got[0].Error, "abc123") {
		t.Fatalf("error secret leaked: %q", got[0].Error)
	}
	if got[0].ErrorKind != "approval_required" {
		t.Fatalf("error kind not preserved: %+v", got[0])
	}
	if got[0].ResultRef != records[0].ResultRef {
		t.Fatalf("result ref not preserved: %+v", got[0])
	}
	if !reflect.DeepEqual(got[0].ArtifactRefs, records[0].ArtifactRefs) {
		t.Fatalf("artifact refs not preserved: %+v", got[0])
	}
	if got[0].ApprovalRef != records[0].ApprovalRef {
		t.Fatalf("approval ref not preserved: %+v", got[0])
	}
	if got[0].PatchRiskSummary == nil ||
		got[0].PatchRiskSummary.RiskLevel != "medium" ||
		got[0].PatchRiskSummary.Actions["update"] != 2 ||
		!got[0].PatchRiskSummary.MultiFile {
		t.Fatalf("patch risk summary not preserved: %+v", got[0].PatchRiskSummary)
	}
	if got[0].ResultEnvelope == nil || got[0].ResultEnvelope.DataRef != records[0].ResultRef {
		t.Fatalf("result envelope missing ref: %+v", got[0].ResultEnvelope)
	}
	if got[0].ResultEnvelope.Revision != records[0].RevisionAfter {
		t.Fatalf("result envelope missing revision: %+v", got[0].ResultEnvelope)
	}
	artifactRefs, ok := got[0].ResultEnvelope.Data["artifact_refs"].([]string)
	if !ok || !reflect.DeepEqual(artifactRefs, records[0].ArtifactRefs) {
		t.Fatalf("result envelope missing artifact refs: %+v", got[0].ResultEnvelope)
	}
	if got[0].ResultEnvelope.Data["approval_ref"] != records[0].ApprovalRef {
		t.Fatalf("result envelope missing approval ref: %+v", got[0].ResultEnvelope)
	}
	if got[0].ResultEnvelope.Data["error_kind"] != records[0].ErrorKind {
		t.Fatalf("result envelope missing error kind: %+v", got[0].ResultEnvelope)
	}
	if got[0].ResultEnvelope.Data["arguments_sha256"] != records[0].ArgumentsSHA256 {
		t.Fatalf("result envelope missing argument fingerprint: %+v", got[0].ResultEnvelope)
	}
	if got[0].ResultEnvelope.Data["result_action"] != records[0].ResultAction {
		t.Fatalf("result envelope missing result action: %+v", got[0].ResultEnvelope)
	}
	rawEnvelope, err := json.Marshal(got[0].ResultEnvelope)
	if err != nil {
		t.Fatalf("marshal result envelope: %v", err)
	}
	if strings.Contains(string(rawEnvelope), "abc123") || strings.Contains(string(rawEnvelope), "authorization") {
		t.Fatalf("result envelope leaked raw error: %s", string(rawEnvelope))
	}
}

func TestEvalToolInventoryObservationsAreSchemaFree(t *testing.T) {
	infos := []tools.ToolInfo{{
		Name:            "read_file",
		Kind:            tools.ToolKindFile,
		Exposure:        tools.ToolExposureDirect,
		Risk:            tools.ToolRiskLow,
		ReadOnly:        true,
		ConcurrencySafe: true,
		Reason:          "safe metadata without schema",
	}}

	got := evalToolInventoryObservations(infos)
	if len(got) != 1 {
		t.Fatalf("expected one tool inventory item, got %+v", got)
	}
	if got[0].Name != "read_file" || got[0].Kind != "file" || got[0].Exposure != "direct" || got[0].Risk != "low" || !got[0].ReadOnly || !got[0].ConcurrencySafe {
		t.Fatalf("tool inventory metadata not preserved: %+v", got[0])
	}
	raw, err := json.Marshal(got[0])
	if err != nil {
		t.Fatalf("marshal tool inventory: %v", err)
	}
	for _, forbidden := range []string{"description", "input_schema", "parameters", "properties"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("tool inventory leaked schema-like field %q: %s", forbidden, string(raw))
		}
	}
}

func TestEvalModelProfileObservation(t *testing.T) {
	got := evalModelProfileObservation(&runtime.Session{
		ProviderName: "openai",
		Model:        "gpt-5-codex",
	})

	if got == nil {
		t.Fatal("expected model profile observation")
	}
	if got.ProviderName != "openai" || got.Model != "gpt-5-codex" || got.Family != "codex" {
		t.Fatalf("unexpected model profile identity: %+v", got)
	}
	if got.DefaultWriteMode != "patch" || !got.FreeformTool || !got.AllowParallelReadOnly {
		t.Fatalf("unexpected model profile strategy: %+v", got)
	}
}

func TestEvalContextBlockObservationsSummarizeRuntimeBlocks(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/eval\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nconst token = \"super-secret-token\"\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	kit, err := tools.New(root)
	if err != nil {
		t.Fatalf("tools.New: %v", err)
	}
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "read_file",
		Arguments: `{"path":"main.go","offset":1,"limit":3}`,
	}); err != nil {
		t.Fatalf("read_file: %v", err)
	}

	got := evalContextBlockObservations(&runtime.Session{
		RootDir: root,
		Toolkit: kit,
	})
	byKind := make(map[string]evalharness.ContextBlockObservation)
	for _, block := range got {
		if block.Kind == "" {
			t.Fatalf("context block kind missing: %+v", block)
		}
		if block.ContentBytes <= 0 || strings.TrimSpace(block.ContentPreview) == "" {
			t.Fatalf("context block missing preview metadata: %+v", block)
		}
		if strings.Contains(block.ContentPreview, "super-secret-token") {
			t.Fatalf("context block preview leaked file body: %+v", block)
		}
		byKind[block.Kind] = block
	}
	for _, kind := range []string{"ENVIRONMENT", "REPO_MAP", "ACTIVE_FILES", "TOOL_RESULT_SUMMARY"} {
		if _, ok := byKind[kind]; !ok {
			t.Fatalf("missing context block kind %s in %+v", kind, got)
		}
	}
	active := byKind["ACTIVE_FILES"]
	if active.Source != "read_file" || active.TokenBudget == 0 || !strings.Contains(active.ContentPreview, "main.go") {
		t.Fatalf("active files block missing read metadata: %+v", active)
	}
}

func TestEvalWorkflowObservationsIncludeTeamArbitration(t *testing.T) {
	store := workflow.NewStore(t.TempDir())
	if _, err := store.CreateRun(workflow.Run{
		ID:         "team-run",
		Driver:     workflow.RunDriverAgentManaged,
		Entrypoint: workflow.RunEntrypointNaturalLanguageAgent,
		Status:     workflow.RunStateRunning,
	}); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	for _, agent := range []workflow.AgentRun{
		{
			ID:            "agent-a",
			WorkflowRunID: "team-run",
			Status:        workflow.AgentRunStateCompleted,
			ReportMissing: true,
			ChangedFiles:  []string{"shared.go"},
		},
		{
			ID:            "agent-b",
			WorkflowRunID: "team-run",
			Status:        workflow.AgentRunStateFailed,
			ChangedFiles:  []string{"shared.go"},
		},
	} {
		if err := store.UpsertAgentRun(agent); err != nil {
			t.Fatalf("UpsertAgentRun(%s): %v", agent.ID, err)
		}
	}

	snapshot := looprunner.SnapshotSystem(looprunner.SnapshotOptions{WorkflowStore: store})
	if len(snapshot.Warnings) > 0 {
		t.Fatalf("unexpected warnings: %+v", snapshot.Warnings)
	}
	attention := evalLoopAttentionObservations(snapshot.Attention)
	if len(attention) == 0 {
		t.Fatalf("loop attention should capture workflow arbitration issues: %+v", snapshot.Attention)
	}
	got := evalWorkflowObservations(snapshot.Workflows)
	if len(got) != 1 {
		t.Fatalf("expected one workflow observation, got %+v", got)
	}
	if got[0].Driver != workflow.RunDriverAgentManaged || got[0].Entrypoint != workflow.RunEntrypointNaturalLanguageAgent {
		t.Fatalf("workflow observation missing driver fields: %+v", got[0])
	}
	wantRunDir := filepath.Join(store.Dir(), "workflows", "team-run")
	if got[0].RunDir != wantRunDir || got[0].EventLogPath != filepath.Join(wantRunDir, "events.jsonl") {
		t.Fatalf("workflow observation missing artifact paths: %+v", got[0])
	}
	arbitration := got[0].TeamArbitration
	if arbitration.Status != "attention_required" {
		t.Fatalf("unexpected arbitration status: %+v", arbitration)
	}
	if len(arbitration.MissingReports) != 1 || arbitration.MissingReports[0] != "agent-a" {
		t.Fatalf("missing reports not preserved: %+v", arbitration)
	}
	if len(arbitration.FailedAgentRuns) != 1 || arbitration.FailedAgentRuns[0] != "agent-b" {
		t.Fatalf("failed runs not preserved: %+v", arbitration)
	}
	if len(arbitration.ChangedFileOverlaps) != 1 || arbitration.ChangedFileOverlaps[0].File != "shared.go" {
		t.Fatalf("changed-file overlap not preserved: %+v", arbitration)
	}
}

func TestPersistEvalTraceWritesSessionArtifact(t *testing.T) {
	sessionDir := t.TempDir()
	result := evalharness.Result{
		TaskID:   "task-1",
		TaskName: "Task One",
		Observability: &evalharness.Observability{
			SessionDir:         sessionDir,
			FinalAnswerPreview: "done",
			ModelProfile:       &evalharness.ModelProfileObservation{ProviderName: "openai", Model: "gpt-5-codex", Family: "codex"},
		},
	}

	persistEvalTrace(&result)

	if result.Observability.TracePath == "" {
		t.Fatalf("trace path not recorded: %+v", result.Observability)
	}
	data, err := os.ReadFile(result.Observability.TracePath)
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}
	if !strings.Contains(string(data), `"type":"model_profile"`) || !strings.Contains(string(data), `"type":"final"`) {
		t.Fatalf("trace missing expected events:\n%s", string(data))
	}
}

func TestPersistCLIRunTraceWritesSessionArtifact(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "target.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "old.txt"), []byte("old\n"), 0o644); err != nil {
		t.Fatalf("write old fixture: %v", err)
	}
	kit, err := tools.New(root)
	if err != nil {
		t.Fatalf("tools.New: %v", err)
	}
	sessionDir := filepath.Join(t.TempDir(), "session-artifacts")
	kit.SetSessionDir(sessionDir)
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		ID:        "old-call",
		Name:      "read_file",
		Arguments: `{"path":"old.txt"}`,
	}); err != nil {
		t.Fatalf("old read: %v", err)
	}
	toolRecordStart := len(kit.ToolTelemetry())
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		ID:        "new-call",
		Name:      "read_file",
		Arguments: `{"path":"target.txt"}`,
	}); err != nil {
		t.Fatalf("new read: %v", err)
	}
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		ID:        "new-call-repeat",
		Name:      "read_file",
		Arguments: `{"path":"target.txt"}`,
	}); err != nil {
		t.Fatalf("repeat read: %v", err)
	}

	startedAt := time.Now().UTC().Add(-time.Second)
	completedAt := time.Now().UTC()
	tracePath, err := persistCLIRunTrace(
		&runtime.Session{ProviderName: "openai", Toolkit: kit},
		&agent.StreamRunner{Model: "gpt-test", APIModel: "gpt-test-api"},
		"cli-session-1",
		startedAt,
		completedAt,
		agent.LoopResult{Content: "done", InputTokens: 11, OutputTokens: 7},
		nil,
		toolRecordStart,
		[]sessiontrace.RequestContextRecord{{
			StepIndex:         0,
			TransientMessages: 1,
			ContentBytes:      120,
			BlockKinds:        []string{"ENVIRONMENT", "TOOL_POLICY"},
		}},
	)
	if err != nil {
		t.Fatalf("persistCLIRunTrace: %v", err)
	}
	if tracePath != sessiontrace.Path(sessionDir) {
		t.Fatalf("trace path = %q, want %q", tracePath, sessiontrace.Path(sessionDir))
	}
	summary, err := sessiontrace.ReplayTrace(tracePath)
	if err != nil {
		t.Fatalf("replay session trace: %v", err)
	}
	if summary.Mode != "session_trace_replay" || summary.LatestTurn == nil || summary.LatestTurn.ThreadID != "cli-session-1" {
		t.Fatalf("unexpected replay summary: %+v", summary)
	}
	if summary.LatestTurn.InputTokens != 11 || summary.LatestTurn.OutputTokens != 7 || summary.Final == nil || summary.Final.FinalAnswerPreview != "done" {
		t.Fatalf("trace did not preserve final metadata: %+v", summary)
	}
	if summary.LatestTurn.ModelProfile == nil ||
		summary.LatestTurn.ModelProfile.Family != "gpt" ||
		summary.LatestTurn.ModelProfile.DefaultWriteMode != "patch" {
		t.Fatalf("trace should include model profile strategy: %+v", summary.LatestTurn.ModelProfile)
	}
	if len(summary.ToolNames) != 2 || summary.ToolNames[0] != "read_file" || summary.ToolNames[1] != "read_file" {
		t.Fatalf("trace should include only this run's tool record: %+v", summary.ToolNames)
	}
	if summary.ToolSummary == nil ||
		len(summary.ToolSummary.RepeatedArguments) != 1 ||
		summary.ToolSummary.RepeatedArguments[0].ToolName != "read_file" ||
		summary.ToolSummary.RepeatedArguments[0].Count != 2 ||
		summary.ToolSummary.RepeatedArguments[0].ArgumentsSHA256 == "" {
		t.Fatalf("trace should summarize repeated tool arguments: %+v", summary.ToolSummary)
	}
	if len(summary.ContextRequests) != 1 ||
		summary.ContextRequests[0].TransientMessages != 1 ||
		!containsString(summary.ContextBlockKinds, "TOOL_POLICY") {
		t.Fatalf("trace should preserve request context metadata: %+v", summary)
	}
}

func TestRunEvalReplayTraceJSON(t *testing.T) {
	tracePath := filepath.Join(t.TempDir(), "eval-trace.jsonl")
	if err := evalharness.WriteTrace(tracePath, evalharness.Result{
		TaskID:             "task-1",
		TaskName:           "Task One",
		Success:            true,
		VerificationReason: "passed",
		Observability: &evalharness.Observability{
			SessionID:          "eval-task-1",
			SessionDir:         filepath.Dir(tracePath),
			TracePath:          tracePath,
			FinalAnswerPreview: "done",
			ModelProfile:       &evalharness.ModelProfileObservation{ProviderName: "openai", Model: "gpt-5-codex", Family: "codex"},
			ContextBlocks: []evalharness.ContextBlockObservation{{
				Kind:           "TOOL_RESULT_SUMMARY",
				Source:         "tool_telemetry",
				TokenBudget:    800,
				ContentPreview: "recent_tool_calls:",
			}},
			ToolRecords: []evalharness.ToolObservation{{
				Name:            "read_file",
				ArgumentsSHA256: strings.Repeat("c", 64),
				Success:         true,
			}, {
				Name:            "read_file",
				ArgumentsSHA256: strings.Repeat("c", 64),
				Success:         true,
			}},
		},
	}); err != nil {
		t.Fatalf("write trace: %v", err)
	}
	outputPath := filepath.Join(t.TempDir(), "replay.json")

	output := captureStdout(t, func() {
		if err := run([]string{"eval", "--replay-trace", tracePath, "--json", "--output", outputPath}); err != nil {
			t.Fatalf("run returned error: %v", err)
		}
	})

	var stdoutSummary evalharness.TraceReplaySummary
	if err := json.Unmarshal([]byte(output), &stdoutSummary); err != nil {
		t.Fatalf("expected replay JSON output, got %q: %v", output, err)
	}
	if stdoutSummary.Task == nil || stdoutSummary.Task.ID != "task-1" || stdoutSummary.Final == nil || !stdoutSummary.Final.Success {
		t.Fatalf("unexpected stdout replay summary: %+v", stdoutSummary)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read replay output: %v", err)
	}
	var fileSummary evalharness.TraceReplaySummary
	if err := json.Unmarshal(data, &fileSummary); err != nil {
		t.Fatalf("parse replay output file: %v", err)
	}
	if fileSummary.Mode != "deterministic_trace_replay" || len(fileSummary.ToolNames) != 2 || fileSummary.ToolNames[0] != "read_file" || fileSummary.ToolNames[1] != "read_file" {
		t.Fatalf("unexpected replay output file: %+v", fileSummary)
	}
	if len(fileSummary.ContextBlocks) != 1 ||
		fileSummary.ContextBlocks[0].Kind != "TOOL_RESULT_SUMMARY" ||
		fileSummary.ContextBlocks[0].ContentPreview != "recent_tool_calls:" {
		t.Fatalf("replay output should include context block observations: %+v", fileSummary.ContextBlocks)
	}
	if fileSummary.ToolSummary == nil ||
		len(fileSummary.ToolSummary.RepeatedArguments) != 1 ||
		fileSummary.ToolSummary.RepeatedArguments[0].ToolName != "read_file" ||
		fileSummary.ToolSummary.RepeatedArguments[0].ArgumentsSHA256 != strings.Repeat("c", 64) ||
		fileSummary.ToolSummary.RepeatedArguments[0].Count != 2 {
		t.Fatalf("replay output should include repeated argument summary: %+v", fileSummary.ToolSummary)
	}
}

func TestRunEvalReplayTraceTextPrintsPolicyBlocks(t *testing.T) {
	tracePath := filepath.Join(t.TempDir(), "eval-trace.jsonl")
	approvalRef := "/tmp/wuu/session/approvals/call-process.json"
	if err := evalharness.WriteTrace(tracePath, evalharness.Result{
		TaskID:             "task-1",
		TaskName:           "Task One",
		Success:            true,
		ForbiddenToolsUsed: []string{"create_workflow"},
		WorkflowIssues:     []string{"run-1:missing_reports=beta-writer"},
		VerificationEvidence: []evalharness.VerificationEvidence{{
			Check:   "go tests",
			Passed:  true,
			Command: "go test ./...",
		}},
		Observability: &evalharness.Observability{
			ToolRecords: []evalharness.ToolObservation{{
				Name:         "run_test",
				CallID:       "call-test",
				ResultAction: "run",
				Success:      true,
			}, {
				Name:            "start_process",
				CallID:          "call-process",
				Kind:            "process",
				Risk:            "high",
				PolicyAction:    "require_approval",
				ErrorKind:       "approval_required",
				ApprovalRef:     approvalRef,
				ArgumentsSHA256: strings.Repeat("e", 64),
				Success:         false,
			}},
			LoopAttention: []evalharness.LoopAttentionObservation{{
				Source:  "workflow_agent",
				ID:      "beta-writer",
				Status:  "missing_report",
				Message: "workflow run run-1 is missing agent report",
			}},
			WorkflowRuns: []evalharness.WorkflowRunObservation{{
				ID:           "run-1",
				RunDir:       "/tmp/wuu/workflows/run-1",
				EventLogPath: "/tmp/wuu/workflows/run-1/events.jsonl",
				Driver:       "agent_managed",
				Status:       "completed",
				AgentRuns: []evalharness.WorkflowAgentRunObservation{{
					ID:           "alpha-writer",
					TaskName:     "write alpha marker",
					AgentProfile: "marker-writer",
					Status:       "completed",
					ReportPath:   "/tmp/wuu/workflows/run-1/agents/alpha/report.md",
					WorktreePath: "/tmp/wuu/worktrees/alpha",
					ChangedFiles: []string{"team_alpha.txt"},
				}, {
					ID:            "beta-writer",
					TaskName:      "write beta marker",
					Status:        "awaiting_report",
					ReportMissing: true,
					ChangedFiles:  []string{"team_alpha.txt", "team_beta.txt"},
				}},
				TeamArbitration: evalharness.WorkflowTeamArbitration{
					Status:         "awaiting_reports",
					MissingReports: []string{"beta-writer"},
					ChangedFileOverlaps: []evalharness.WorkflowChangedFileOverlapObservation{{
						File:        "team_alpha.txt",
						AgentRunIDs: []string{"alpha-writer", "beta-writer"},
					}},
					NextActions: []string{"await_reports"},
				},
			}},
		},
	}); err != nil {
		t.Fatalf("write trace: %v", err)
	}

	output := captureStdout(t, func() {
		if err := run([]string{"eval", "--replay-trace", tracePath}); err != nil {
			t.Fatalf("run returned error: %v", err)
		}
	})

	if !strings.Contains(output, "policy_blocks: start_process:require_approval:approval_required:call_id=call-process:approval_ref="+approvalRef) {
		t.Fatalf("replay text output missing policy blocks:\n%s", output)
	}
	if !strings.Contains(output, "workflow_runs: run-1:driver=agent_managed:status=completed:event_log=/tmp/wuu/workflows/run-1/events.jsonl:run_dir=/tmp/wuu/workflows/run-1") {
		t.Fatalf("replay text output missing workflow artifact paths:\n%s", output)
	}
	if !strings.Contains(output, "loop_attention: workflow_agent:id=beta-writer:status=missing_report:message=workflow run run-1 is missing agent report") {
		t.Fatalf("replay text output missing loop attention:\n%s", output)
	}
	if !strings.Contains(output, "workflow_agents: run-1/alpha-writer:task=write alpha marker:profile=marker-writer:status=completed:report=/tmp/wuu/workflows/run-1/agents/alpha/report.md:worktree=/tmp/wuu/worktrees/alpha:changed=team_alpha.txt") {
		t.Fatalf("replay text output missing workflow agent report paths:\n%s", output)
	}
	if !strings.Contains(output, "run-1/beta-writer:task=write beta marker:status=awaiting_report:report_missing=true:changed=team_alpha.txt|team_beta.txt") {
		t.Fatalf("replay text output missing workflow agent missing-report state:\n%s", output)
	}
	if !strings.Contains(output, "workflow_arbitration: run-1:status=awaiting_reports:missing_reports=beta-writer:overlaps=team_alpha.txt=alpha-writer+beta-writer:next=await_reports") {
		t.Fatalf("replay text output missing workflow arbitration summary:\n%s", output)
	}
	if !strings.Contains(output, "forbidden_tools: create_workflow") {
		t.Fatalf("replay text output missing forbidden tools:\n%s", output)
	}
	if !strings.Contains(output, "workflow_issues: run-1:missing_reports=beta-writer") {
		t.Fatalf("replay text output missing workflow issues:\n%s", output)
	}
	if !strings.Contains(output, "validation: status=incomplete tools=1 evidence=1 missing=2 failures=0") {
		t.Fatalf("replay text output missing validation summary:\n%s", output)
	}
	if !strings.Contains(output, "validation_evidence: go tests:passed:command=go test ./...") {
		t.Fatalf("replay text output missing validation evidence:\n%s", output)
	}
	if !strings.Contains(output, "validation_missing: forbidden_tool:create_workflow") {
		t.Fatalf("replay text output missing validation missing requirements:\n%s", output)
	}
	if !strings.Contains(output, "workflow_issue:run-1:missing_reports=beta-writer") {
		t.Fatalf("replay text output missing workflow validation issue:\n%s", output)
	}
	if strings.Contains(output, strings.Repeat("e", 64)) {
		t.Fatalf("replay text output should not print argument fingerprints by default:\n%s", output)
	}
}

func TestApplyEvalWorkflowIssuesFailsResult(t *testing.T) {
	result := evalharness.Result{
		TaskID:  "task-1",
		Success: true,
		Observability: &evalharness.Observability{
			WorkflowRuns: []evalharness.WorkflowRunObservation{{
				ID:     "run-1",
				Status: "completed",
				TeamArbitration: evalharness.WorkflowTeamArbitration{
					Status:         "attention_required",
					MissingReports: []string{"worker-1"},
				},
			}},
		},
	}

	applyEvalWorkflowIssues(&result)

	if result.Success {
		t.Fatalf("workflow issues should fail eval result: %+v", result)
	}
	if len(result.WorkflowIssues) != 2 ||
		result.WorkflowIssues[0] != "run-1:arbitration=attention_required" ||
		result.WorkflowIssues[1] != "run-1:missing_reports=worker-1" {
		t.Fatalf("workflow issues not summarized: %+v", result.WorkflowIssues)
	}
}

func TestApplyEvalWorkflowIssuesPrefersLoopAttention(t *testing.T) {
	result := evalharness.Result{
		TaskID:  "task-1",
		Success: true,
		Observability: &evalharness.Observability{
			LoopAttention: []evalharness.LoopAttentionObservation{{
				Source:  "workflow_agent",
				ID:      "worker-1",
				Status:  "missing_report",
				Message: "workflow run run-1 is missing agent report",
			}, {
				Source: "harness",
				ID:     "task-1",
				Status: "failed",
			}},
			WorkflowRuns: []evalharness.WorkflowRunObservation{{
				ID:     "run-1",
				Status: "completed",
			}},
		},
	}

	applyEvalWorkflowIssues(&result)

	if result.Success {
		t.Fatalf("loop attention issues should fail eval result: %+v", result)
	}
	want := []string{"harness:task-1:status=failed", "run-1:missing_reports=worker-1"}
	if strings.Join(result.WorkflowIssues, "\n") != strings.Join(want, "\n") {
		t.Fatalf("loop attention issues not summarized: %+v", result.WorkflowIssues)
	}
}

func TestRunEvalReplaySessionTraceJSON(t *testing.T) {
	tracePath := filepath.Join(t.TempDir(), "session-trace.jsonl")
	if err := sessiontrace.AppendTurn(tracePath,
		sessiontrace.TurnRecord{
			ThreadID:     "thread-1",
			TurnID:       "turn-1",
			Status:       "completed",
			ProviderName: "openai",
			Model:        "gpt-test",
		},
		sessiontrace.FinalRecord{Status: "completed", FinalAnswerPreview: "done"},
		[]tools.ToolInfo{{Name: "semantic_search", Kind: tools.ToolKindSearch, Risk: tools.ToolRiskLow, ReadOnly: true}},
		[]tools.ToolExecutionRecord{{
			Name:            "semantic_search",
			ArgumentsSHA256: strings.Repeat("d", 64),
			Kind:            tools.ToolKindSearch,
			Success:         true,
		}, {
			Name:            "semantic_search",
			ArgumentsSHA256: strings.Repeat("d", 64),
			Kind:            tools.ToolKindSearch,
			Success:         true,
		}},
	); err != nil {
		t.Fatalf("write session trace: %v", err)
	}
	outputPath := filepath.Join(t.TempDir(), "session-replay.json")

	output := captureStdout(t, func() {
		if err := run([]string{"eval", "--replay-trace", tracePath, "--json", "--output", outputPath}); err != nil {
			t.Fatalf("run returned error: %v", err)
		}
	})

	var stdoutSummary sessiontrace.ReplaySummary
	if err := json.Unmarshal([]byte(output), &stdoutSummary); err != nil {
		t.Fatalf("expected session replay JSON output, got %q: %v", output, err)
	}
	if stdoutSummary.Mode != "session_trace_replay" || stdoutSummary.LatestTurn == nil || stdoutSummary.LatestTurn.ThreadID != "thread-1" {
		t.Fatalf("unexpected stdout session replay summary: %+v", stdoutSummary)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read session replay output: %v", err)
	}
	var fileSummary sessiontrace.ReplaySummary
	if err := json.Unmarshal(data, &fileSummary); err != nil {
		t.Fatalf("parse session replay output file: %v", err)
	}
	if len(fileSummary.ToolNames) != 2 || fileSummary.ToolNames[0] != "semantic_search" || fileSummary.ToolNames[1] != "semantic_search" || fileSummary.Final == nil || fileSummary.Final.Status != "completed" {
		t.Fatalf("unexpected session replay output file: %+v", fileSummary)
	}
	if fileSummary.ToolSummary == nil ||
		len(fileSummary.ToolSummary.RepeatedArguments) != 1 ||
		fileSummary.ToolSummary.RepeatedArguments[0].ToolName != "semantic_search" ||
		fileSummary.ToolSummary.RepeatedArguments[0].ArgumentsSHA256 != strings.Repeat("d", 64) ||
		fileSummary.ToolSummary.RepeatedArguments[0].Count != 2 {
		t.Fatalf("session replay output should include repeated argument summary: %+v", fileSummary.ToolSummary)
	}
}

func TestRunEvalReplaySessionTraceTextPrintsPolicyBlocks(t *testing.T) {
	tracePath := filepath.Join(t.TempDir(), "session-trace.jsonl")
	if err := sessiontrace.AppendTurn(tracePath,
		sessiontrace.TurnRecord{
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			Status:   "completed",
		},
		sessiontrace.FinalRecord{Status: "completed", FinalAnswerPreview: "done"},
		nil,
		[]tools.ToolExecutionRecord{{
			Name:            "run_shell",
			CallID:          "call-shell",
			Kind:            tools.ToolKindShell,
			Risk:            tools.ToolRiskHigh,
			PolicyAction:    tools.ToolPolicyDeny,
			ErrorKind:       "policy_denied",
			ArgumentsSHA256: strings.Repeat("f", 64),
			Success:         false,
		}},
	); err != nil {
		t.Fatalf("write session trace: %v", err)
	}

	output := captureStdout(t, func() {
		if err := run([]string{"eval", "--replay-trace", tracePath}); err != nil {
			t.Fatalf("run returned error: %v", err)
		}
	})

	if !strings.Contains(output, "policy_blocks: run_shell:deny:policy_denied:call_id=call-shell") {
		t.Fatalf("session replay text output missing policy blocks:\n%s", output)
	}
	if strings.Contains(output, strings.Repeat("f", 64)) {
		t.Fatalf("session replay text output should not print argument fingerprints by default:\n%s", output)
	}
}

func TestSetTemporaryEnvRestoresPreviousValue(t *testing.T) {
	t.Setenv("WUU_HOME", "/tmp/original-wuu-home")
	restore := setTemporaryEnv("WUU_HOME", "/tmp/eval-wuu-home")
	if got := os.Getenv("WUU_HOME"); got != "/tmp/eval-wuu-home" {
		t.Fatalf("WUU_HOME = %q, want temporary value", got)
	}
	restore()
	if got := os.Getenv("WUU_HOME"); got != "/tmp/original-wuu-home" {
		t.Fatalf("WUU_HOME = %q, want original value", got)
	}
}

func TestSetTemporaryEnvUnsetsMissingValue(t *testing.T) {
	t.Setenv("WUU_HOME", "placeholder")
	os.Unsetenv("WUU_HOME")
	restore := setTemporaryEnv("WUU_HOME", "/tmp/eval-wuu-home")
	if got := os.Getenv("WUU_HOME"); got != "/tmp/eval-wuu-home" {
		t.Fatalf("WUU_HOME = %q, want temporary value", got)
	}
	restore()
	if _, ok := os.LookupEnv("WUU_HOME"); ok {
		t.Fatal("WUU_HOME should be unset after restore")
	}
}

func TestResolveContextWindow_PrefersProviderOverride(t *testing.T) {
	if got := runtime.ResolveContextWindow("gpt-5.4", 777, 555); got != 777 {
		t.Fatalf("expected provider override, got %d", got)
	}
}

func TestResolveContextWindow_FallsBackToAgentOverride(t *testing.T) {
	if got := runtime.ResolveContextWindow("gpt-5.4", 0, 555); got != 555 {
		t.Fatalf("expected agent override, got %d", got)
	}
}

func TestResolveContextWindow_UsesModelRegistryByDefault(t *testing.T) {
	if got := runtime.ResolveContextWindow("gpt-5.4", 0, 0); got != 400000 {
		t.Fatalf("expected model registry context window, got %d", got)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	defer r.Close()

	os.Stdout = w
	defer func() {
		os.Stdout = oldStdout
	}()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read stdout: %v", err)
	}

	return strings.TrimSpace(buf.String())
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
