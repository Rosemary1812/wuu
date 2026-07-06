package tools

import (
	"context"
	"testing"

	"github.com/blueberrycongee/wuu/internal/capability"
	"github.com/blueberrycongee/wuu/internal/modelprofile"
)

// hasCapability reports whether the given capability appears in the
// provided slice.
func hasCapability(caps []capability.Capability, want capability.Capability) bool {
	for _, c := range caps {
		if c == want {
			return true
		}
	}
	return false
}

// TestAdvancedToolsHiddenFromModelSurfaces verifies that the
// structured git tool — the one that Codex-style harnesses collapse
// into bash — stays registered in the toolkit so internal callers can
// still reach it, but never appears on a model-visible tool surface or
// a compiled profile hidden-tool contract.
//
// bash-first redesign: the model only ever sees bash. The former
// run_shell / run_test / managed-process tools were removed entirely;
// git remains registry-only. The compiler omits git from profile
// surfaces, and toolExposure keeps the registry-only implementation
// out of Definitions.
func TestAdvancedToolsHiddenFromModelSurfaces(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// The advanced command tool that the bash-first surface demotes to
	// internal. The list is the authoritative source: toolExposure, the
	// model-profile compiler, and this test must agree.
	advancedTools := []string{
		"git",
	}

	// Baseline: without a profile, the legacy direct-tool surface
	// must not include any of the advanced tools. toolExposure
	// returns Hidden for them, so they never leak into Definitions.
	legacy := kit.Definitions()
	for _, name := range advancedTools {
		if containsProfileDef(legacy, name) {
			t.Errorf("legacy surface must not advertise %s, got %v", name, sortedProfileDefNames(legacy))
		}
	}

	// Per-profile: the compiler does not include advanced command
	// tools in either visible tools or hidden profile output.
	profiles := []struct {
		provider string
		model    string
	}{
		{provider: "openai", model: "gpt-5-codex"},
		{provider: "openai", model: "gpt-5.5"},
		{provider: "anthropic", model: "claude-sonnet-4-5"},
		{provider: "google", model: "gemini-2.5-pro"},
		{provider: "ollama", model: "llama-coder"},
	}
	for _, tt := range profiles {
		kit.SetActiveProfile(modelprofile.Resolve(tt.provider, tt.model), true)
		surface := kit.ActiveSurface()
		defs := kit.Definitions()
		for _, name := range advancedTools {
			if _, ok := surface.HiddenTools[name]; ok {
				t.Errorf("%s/%s: %s must not remain in surface.HiddenTools",
					tt.provider, tt.model, name)
			}
			if containsProfileDef(defs, name) {
				t.Errorf("%s/%s: %s must not appear in Definitions (advanced), got %v",
					tt.provider, tt.model, name, sortedProfileDefNames(defs))
			}
		}
	}
}

// TestAdvancedToolsRemainReachableViaRegistry confirms that
// hiding the git tool from the model surface does not remove it
// from the registry. Internal callers can still look it up by name
// and execute it.
func TestAdvancedToolsRemainReachableViaRegistry(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, name := range []string{"git"} {
		tool := kit.LookupTool(name)
		if tool == nil {
			t.Errorf("registry must still contain %s for internal callers, got nil", name)
			continue
		}
		if tool.Name() != name {
			t.Errorf("registry lookup for %s returned %q", name, tool.Name())
		}
	}

	// The removed legacy command tools must not resolve at all.
	for _, name := range []string{"run_shell", "run_test", "start_process", "list_processes", "read_process_output", "write_stdin", "stop_process"} {
		if tool := kit.LookupTool(name); tool != nil {
			t.Errorf("removed legacy tool %s must not resolve via registry", name)
		}
	}

	// The Execute path bypasses the model surface filter — the
	// surface only hides tools from Definitions. Confirm the
	// internal path resolves git.
	if _, err := kit.executeByName(context.Background(), "git", `{"subcommand":"status"}`); err != nil {
		t.Logf("git Execute error (expected for unit test without project setup): %v", err)
	}
}

// executeByName resolves a tool by name and dispatches the call
// through the registry, bypassing the model surface filter. It
// exists so the registry-reachability test does not have to
// construct a full providers.ToolCall by hand.
func (t *Toolkit) executeByName(ctx context.Context, name, args string) (string, error) {
	tool := t.LookupTool(name)
	if tool == nil {
		return "", nil
	}
	return tool.Execute(ctx, args)
}
