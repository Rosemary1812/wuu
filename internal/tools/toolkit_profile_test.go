package tools

import (
	"sort"
	"testing"

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
	// returns: bash and run_shell both visible, edit_file visible.
	kit.SetActiveProfile(modelprofile.Profile{})
	if kit.ActiveSurface().ProfileName != "" {
		t.Fatal("expected zero-value profile to clear the active surface")
	}
	defs := kit.Definitions()
	if !containsProfileDef(defs, "bash") {
		t.Fatalf("legacy surface must include bash, got %v", sortedProfileDefNames(defs))
	}
	if !containsProfileDef(defs, "run_shell") {
		t.Fatalf("legacy surface must include run_shell, got %v", sortedProfileDefNames(defs))
	}
	if !containsProfileDef(defs, "edit_file") {
		t.Fatalf("legacy surface must include edit_file, got %v", sortedProfileDefNames(defs))
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

func TestDefinitionsFilterMatchesSurfaceToolsExactly(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetActiveProfile(modelprofile.Resolve("anthropic", "claude-sonnet-4-5"))
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
