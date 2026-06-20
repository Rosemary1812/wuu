package tools

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"

	memstore "github.com/blueberrycongee/wuu/internal/memory/store"
	"github.com/blueberrycongee/wuu/internal/modelprofile"
	"github.com/blueberrycongee/wuu/internal/providers"
)

// containsProfileDef reports whether the given name appears in the
// visible tool definitions returned by Toolkit.Definitions().
func containsProfileDef(defs []providers.ToolDefinition, name string) bool {
	for _, d := range defs {
		if d.Name == name {
			return true
		}
	}
	return false
}

// sortedProfileDefNames returns a stable, sorted slice of the
// visible tool names so failure messages are deterministic.
func sortedProfileDefNames(defs []providers.ToolDefinition) []string {
	out := make([]string, 0, len(defs))
	for _, d := range defs {
		out = append(out, d.Name)
	}
	sort.Strings(out)
	return out
}

func TestSetActiveProfileCompilesAndExposesBashForAllStandardProfiles(t *testing.T) {
	root := t.TempDir()
	for _, tt := range []struct {
		provider string
		model    string
	}{
		{provider: "openai", model: "gpt-5-codex"},
		{provider: "openai", model: "gpt-5.5"},
		{provider: "anthropic", model: "claude-sonnet-4-5"},
		{provider: "google", model: "gemini-2.5-pro"},
	} {
		kit, err := New(root)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		kit.SetActiveProfile(modelprofile.Resolve(tt.provider, tt.model))
		surface := kit.ActiveSurface()
		if surface.ProfileName == "" {
			t.Fatalf("%s/%s: expected a compiled surface", tt.provider, tt.model)
		}
		defs := kit.Definitions()
		if !containsProfileDef(defs, "bash") {
			t.Fatalf("%s/%s: Definitions must include bash, got %v", tt.provider, tt.model, sortedProfileDefNames(defs))
		}
	}
}

func TestCompiledProfileVisibleDefinitionsDoNotTeachLegacyCommandTools(t *testing.T) {
	for _, tt := range []struct {
		provider string
		model    string
	}{
		{provider: "openai", model: "gpt-5-codex"},
		{provider: "anthropic", model: "claude-sonnet-4-5"},
	} {
		kit, err := New(t.TempDir())
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		kit.SetActiveProfile(modelprofile.Resolve(tt.provider, tt.model))
		for _, def := range kit.Definitions() {
			text := visibleDefinitionText(def)
			for _, old := range []string{
				"run_shell",
				"run_test",
				"start_process",
				"list_processes",
				"read_process_output",
				"write_stdin",
				"stop_process",
				"structured git tool",
			} {
				if strings.Contains(text, old) {
					t.Fatalf("%s/%s visible tool %s must not teach legacy command path %q:\n%s", tt.provider, tt.model, def.Name, old, text)
				}
			}
		}
	}
}

func visibleDefinitionText(def providers.ToolDefinition) string {
	schema, _ := json.Marshal(def.InputSchema)
	return def.Name + "\n" + def.Description + "\n" + string(schema)
}

func TestActiveProfileAllowsDeferredMCPThroughToolSearch(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.registry = NewRegistry(
		NewToolSearchTool(kit),
		&stubTool{
			name:   "mcp_docs_search",
			def:    providers.ToolDefinition{Name: "mcp_docs_search", Description: "Search docs through MCP"},
			result: `{"action":"mcp_docs_search"}`,
		},
	)
	kit.SetActiveProfile(modelprofile.Resolve("openai", "gpt-5-codex"))

	if containsProfileDef(kit.Definitions(), "mcp_docs_search") {
		t.Fatal("MCP tool should not be visible before tool_search")
	}
	_, err = kit.Execute(context.Background(), providers.ToolCall{Name: "mcp_docs_search", Arguments: `{}`})
	if err == nil || !strings.Contains(err.Error(), "deferred") {
		t.Fatalf("unloaded MCP tool should ask for tool_search, got %v", err)
	}

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "tool_search",
		Arguments: `{"query":"docs search"}`,
	})
	if err != nil {
		t.Fatalf("tool_search: %v", err)
	}
	var parsed struct {
		ExposedTools []string `json:"exposed_tools"`
	}
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("parse tool_search: %v", err)
	}
	if !containsString(parsed.ExposedTools, "mcp_docs_search") {
		t.Fatalf("tool_search did not expose MCP tool: %s", resp)
	}
	if !containsProfileDef(kit.Definitions(), "mcp_docs_search") {
		t.Fatal("activated MCP tool should be visible under MCP-capable active profile")
	}
	out, err := kit.Execute(context.Background(), providers.ToolCall{Name: "mcp_docs_search", Arguments: `{}`})
	if err != nil {
		t.Fatalf("activated MCP tool should execute: %v", err)
	}
	if !strings.Contains(out, "mcp_docs_search") {
		t.Fatalf("unexpected MCP result: %s", out)
	}
}

func TestSetActiveProfileLocalProfileDropsBash(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetActiveProfile(modelprofile.Resolve("ollama", "llama-coder"))
	defs := kit.Definitions()
	if containsProfileDef(defs, "bash") {
		t.Fatalf("local profile must not expose bash, got %v", sortedProfileDefNames(defs))
	}
}

func TestSetActiveProfileCodexExposesApplyPatchHidesEditAndWrite(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetActiveProfile(modelprofile.Resolve("openai", "gpt-5-codex"))
	defs := kit.Definitions()
	if !containsProfileDef(defs, "apply_patch") {
		t.Fatalf("Codex surface must include apply_patch, got %v", sortedProfileDefNames(defs))
	}
	for _, hidden := range []string{"edit_file", "write_file", "run_test", "git", "run_shell"} {
		if containsProfileDef(defs, hidden) {
			t.Fatalf("Codex surface must not advertise %s, got %v", hidden, sortedProfileDefNames(defs))
		}
	}
}

func TestSetActiveProfileAlignsDefinitionWithExecutionState(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetActiveProfile(modelprofile.Resolve("openai", "gpt-5-codex"))

	if !containsProfileDef(kit.Definitions(), "apply_patch") {
		t.Fatal("Codex surface should advertise apply_patch")
	}
	_, err = kit.Execute(context.Background(), providers.ToolCall{Name: "apply_patch", Arguments: `{}`})
	if err == nil {
		t.Fatal("expected apply_patch to reject missing patchText")
	}
	if strings.Contains(err.Error(), "disabled") {
		t.Fatalf("advertised apply_patch must not be disabled: %v", err)
	}

	_, err = kit.Execute(context.Background(), providers.ToolCall{Name: "edit_file", Arguments: `{}`})
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("hidden edit_file should be disabled under Codex surface, got %v", err)
	}
}

func TestActiveProfileDefinitionsRespectExplicitDisables(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetActiveProfile(modelprofile.Resolve("openai", "gpt-5-codex"))
	kit.DisableTools("spawn_agent")

	defs := kit.Definitions()
	if containsProfileDef(defs, "spawn_agent") {
		t.Fatalf("explicitly disabled spawn_agent leaked into active surface: %v", sortedProfileDefNames(defs))
	}
	if !containsProfileDef(defs, "apply_patch") {
		t.Fatalf("unrelated surface tool apply_patch should remain visible: %v", sortedProfileDefNames(defs))
	}
}

func TestActiveProfileBlocksHiddenToolExecution(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetActiveProfile(modelprofile.Resolve("openai", "gpt-5-codex"))

	_, err = kit.Execute(context.Background(), providers.ToolCall{Name: "run_shell", Arguments: `{"command":"echo hi"}`})
	if err == nil || !strings.Contains(err.Error(), "active model surface") {
		t.Fatalf("hidden run_shell should be blocked by active surface, got %v", err)
	}

	_, err = kit.Execute(context.Background(), providers.ToolCall{Name: "run_workflow", Arguments: `{}`})
	if err == nil || !strings.Contains(err.Error(), "deferred") {
		t.Fatalf("inactive deferred run_workflow should ask for tool_search, got %v", err)
	}
	kit.activateDeferredTools("run_workflow")
	_, err = kit.Execute(context.Background(), providers.ToolCall{Name: "run_workflow", Arguments: `{}`})
	if err == nil || strings.Contains(err.Error(), "deferred") || strings.Contains(err.Error(), "active model surface") {
		t.Fatalf("activated run_workflow should reach tool validation, got %v", err)
	}
}

func TestActiveProfileExposesMemoryToolsOnlyWithProvider(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetActiveProfile(modelprofile.Resolve("openai", "gpt-5-codex"))
	if containsProfileDef(kit.Definitions(), "read_memory") || containsProfileDef(kit.Definitions(), "write_memory") {
		t.Fatal("memory tools should stay hidden without a provider")
	}

	provider, err := memstore.NewFileProvider(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileProvider: %v", err)
	}
	kit.SetMemory(provider)
	defs := kit.Definitions()
	for _, want := range []string{"read_memory", "write_memory"} {
		if !containsProfileDef(defs, want) {
			t.Fatalf("memory provider should expose %s, got %v", want, sortedProfileDefNames(defs))
		}
	}
}

func TestSetActiveProfileClaudeExposesEditAndWriteHidesApplyPatch(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetActiveProfile(modelprofile.Resolve("anthropic", "claude-sonnet-4-5"))
	defs := kit.Definitions()
	for _, want := range []string{"bash", "edit_file", "write_file", "read_file", "grep", "glob"} {
		if !containsProfileDef(defs, want) {
			t.Fatalf("Claude surface must include %s, got %v", want, sortedProfileDefNames(defs))
		}
	}
	if containsProfileDef(defs, "apply_patch") {
		t.Fatalf("Claude surface must not advertise apply_patch, got %v", sortedProfileDefNames(defs))
	}
	for _, hidden := range []string{"run_test", "git", "run_shell"} {
		if containsProfileDef(defs, hidden) {
			t.Fatalf("Claude surface must not advertise %s, got %v", hidden, sortedProfileDefNames(defs))
		}
	}
}

func TestSetActiveProfileGPT4FallsBackToEditFile(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetActiveProfile(modelprofile.Resolve("openai", "gpt-4.1-mini"))
	defs := kit.Definitions()
	if !containsProfileDef(defs, "edit_file") || !containsProfileDef(defs, "write_file") {
		t.Fatalf("gpt-4 profile must expose edit_file + write_file, got %v", sortedProfileDefNames(defs))
	}
	if containsProfileDef(defs, "apply_patch") {
		t.Fatalf("gpt-4 profile must not expose apply_patch, got %v", sortedProfileDefNames(defs))
	}
}

func TestSetActiveProfileZeroValueRestoresLegacySurface(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Codex first: the model sees only apply_patch, not edit_file.
	kit.SetActiveProfile(modelprofile.Resolve("openai", "gpt-5-codex"))
	if containsProfileDef(kit.Definitions(), "edit_file") {
		t.Fatal("expected Codex surface to hide edit_file")
	}
	// Clear the profile and verify the legacy direct-tool surface
	// returns: bash is the only visible shell entry point. The
	// legacy run_shell name is now an internal implementation
	// and is hidden from every surface.
	kit.SetActiveProfile(modelprofile.Profile{})
	if kit.ActiveSurface().ProfileName != "" {
		t.Fatal("expected zero-value profile to clear the active surface")
	}
	defs := kit.Definitions()
	if !containsProfileDef(defs, "bash") {
		t.Fatalf("legacy surface must include bash, got %v", sortedProfileDefNames(defs))
	}
	if containsProfileDef(defs, "run_shell") {
		t.Fatalf("legacy surface must hide run_shell, got %v", sortedProfileDefNames(defs))
	}
	if !containsProfileDef(defs, "edit_file") {
		t.Fatalf("legacy surface must include edit_file, got %v", sortedProfileDefNames(defs))
	}
}

func TestCloneForRootPreservesActiveProfileSurface(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetActiveProfile(modelprofile.Resolve("openai", "gpt-5-codex"))

	clone, err := kit.CloneForRoot(t.TempDir())
	if err != nil {
		t.Fatalf("CloneForRoot: %v", err)
	}
	if clone.ActiveSurface().ProfileName != kit.ActiveSurface().ProfileName {
		t.Fatalf("clone surface = %q, want %q", clone.ActiveSurface().ProfileName, kit.ActiveSurface().ProfileName)
	}
	defs := clone.Definitions()
	if !containsProfileDef(defs, "apply_patch") || containsProfileDef(defs, "edit_file") {
		t.Fatalf("clone should keep Codex edit surface, got %v", sortedProfileDefNames(defs))
	}
	_, err = clone.Execute(context.Background(), providers.ToolCall{Name: "apply_patch", Arguments: `{}`})
	if err == nil {
		t.Fatal("expected apply_patch to reject missing patchText")
	}
	if strings.Contains(err.Error(), "disabled") {
		t.Fatalf("clone advertised apply_patch must not be disabled: %v", err)
	}
}

func TestActiveProfileReturnsInstalledProfile(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if kit.ActiveProfile().ProviderName != "" {
		t.Fatal("expected zero-value profile before SetActiveProfile")
	}
	want := modelprofile.Resolve("anthropic", "claude-sonnet-4-5")
	kit.SetActiveProfile(want)
	got := kit.ActiveProfile()
	if got.ProviderName != want.ProviderName || got.Model != want.Model {
		t.Fatalf("ActiveProfile = %+v, want provider=%s model=%s", got, want.ProviderName, want.Model)
	}
}

func TestActiveSurfaceReturnsCopy(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetActiveProfile(modelprofile.Resolve("openai", "gpt-5-codex"))

	surface := kit.ActiveSurface()
	delete(surface.Tools, "apply_patch")
	surface.Capabilities = nil

	if !containsProfileDef(kit.Definitions(), "apply_patch") {
		t.Fatal("mutating ActiveSurface result must not mutate toolkit surface")
	}
	if len(kit.ActiveSurface().Capabilities) == 0 {
		t.Fatal("mutating ActiveSurface capability slice must not mutate toolkit surface")
	}
}

func TestDefinitionsFilterMatchesSurfaceToolsExactly(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	provider, err := memstore.NewFileProvider(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileProvider: %v", err)
	}
	kit.SetMemory(provider)
	kit.SetActiveProfile(modelprofile.Resolve("anthropic", "claude-sonnet-4-5"))
	kit.activateDeferredTools("schedule_cron", "cancel_cron", "list_cron", "run_workflow", "create_workflow")
	surface := kit.ActiveSurface()
	defs := kit.Definitions()
	visible := make(map[string]struct{}, len(defs))
	for _, d := range defs {
		visible[d.Name] = struct{}{}
	}
	for name := range visible {
		if _, ok := surface.Tools[name]; !ok {
			t.Fatalf("Definitions returned %q but surface.Tools does not list it", name)
		}
	}
	for name := range surface.Tools {
		if _, ok := visible[name]; !ok {
			t.Fatalf("surface.Tools has %q but Definitions did not include it", name)
		}
	}
}
