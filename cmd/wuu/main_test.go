package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/config"
	"github.com/blueberrycongee/wuu/internal/evalharness"
	"github.com/blueberrycongee/wuu/internal/runtime"
	"github.com/blueberrycongee/wuu/internal/tools"
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
		!strings.Contains(output, "multi_file_pricing") ||
		!strings.Contains(output, "long_process_output") ||
		!strings.Contains(output, "tool_search_deferred") ||
		!strings.Contains(output, "stale_read_guard") ||
		!strings.Contains(output, "mcp_readonly_concurrency") ||
		!strings.Contains(output, "mcp_live_discovery") ||
		!strings.Contains(output, "multi_agent_worker") {
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
		Name:                "run_shell",
		CallID:              "call_1",
		Kind:                tools.ToolKindShell,
		Exposure:            tools.ToolExposureDirect,
		Risk:                tools.ToolRiskHigh,
		PolicyAction:        tools.ToolPolicyAllow,
		ReadOnly:            false,
		ConcurrencySafe:     false,
		DurationMS:          42,
		Success:             false,
		Error:               "authorization: bearer abc123",
		RawOutputBytes:      1024,
		ReturnedOutputBytes: 256,
		ResultBudgeted:      true,
	}}

	got := evalToolObservations(records)
	if len(got) != 1 {
		t.Fatalf("expected one observation, got %+v", got)
	}
	if got[0].Name != "run_shell" || got[0].Kind != "shell" || got[0].RawOutputBytes != 1024 || !got[0].ResultBudgeted {
		t.Fatalf("metadata not preserved: %+v", got[0])
	}
	if strings.Contains(got[0].Error, "abc123") {
		t.Fatalf("error secret leaked: %q", got[0].Error)
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
