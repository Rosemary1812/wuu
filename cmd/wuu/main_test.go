package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/runtime"
)

func TestResolveTUIThemeMode_UsesAutoWhenHomeMissing(t *testing.T) {
	theme, err := resolveTUIThemeMode("", "")
	if err != nil {
		t.Fatalf("resolveTUIThemeMode returned error: %v", err)
	}
	if theme != "auto" {
		t.Fatalf("expected auto theme when HOME is missing, got %q", theme)
	}
}

func TestResolveTUIThemeMode_PrefersOverrideWhenHomeMissing(t *testing.T) {
	theme, err := resolveTUIThemeMode("", "dark")
	if err != nil {
		t.Fatalf("resolveTUIThemeMode returned error: %v", err)
	}
	if theme != "dark" {
		t.Fatalf("expected override theme, got %q", theme)
	}
}

func TestResolveTUIThemeMode_ReturnsLoadErrorsWhenHomePresent(t *testing.T) {
	home := t.TempDir()
	prefsPath := home + "/.config/wuu/preferences.json"
	if err := os.MkdirAll(home+"/.config/wuu", 0o755); err != nil {
		t.Fatalf("mkdir prefs dir: %v", err)
	}
	if err := os.WriteFile(prefsPath, []byte("{"), 0o644); err != nil {
		t.Fatalf("write prefs: %v", err)
	}

	_, err := resolveTUIThemeMode(home, "")
	if err == nil {
		t.Fatal("expected invalid global preferences to return an error")
	}
	if !strings.Contains(err.Error(), "load global preferences") {
		t.Fatalf("expected load global preferences error, got %v", err)
	}
}

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
		!strings.Contains(output, "mcp_readonly_concurrency") ||
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

func TestMissingRequiredTools(t *testing.T) {
	missing := missingRequiredTools([]string{"tool_search", "list_cron"}, []string{"tool_search", "write_file"})
	if len(missing) != 1 || missing[0] != "list_cron" {
		t.Fatalf("unexpected missing tools: %+v", missing)
	}
	if got := missingRequiredTools([]string{"tool_search"}, []string{"tool_search"}); len(got) != 0 {
		t.Fatalf("expected no missing tools, got %+v", got)
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
