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
// legacy run_test / start_process / git / managed-process tools
// — the ones that OpenCode and Codex-style harnesses collapse into
// bash — stay registered in the toolkit so internal callers can
// still reach them, but never appear on a model-visible tool
// surface or a compiled profile hidden-tool contract.
//
// Phase 5 of the bash-first redesign: the model never has to guess
// between run_test / start_process / git / list_processes. The
// compiler omits them from profile surfaces, and toolExposure keeps
// the registry-only implementations out of Definitions.
func TestAdvancedToolsHiddenFromModelSurfaces(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// The full set of advanced / legacy command tools that the
	// bash-first surface demotes to internal. The list is the
	// authoritative source: toolExposure, the model-profile
	// compiler, and this test must agree.
	advancedTools := []string{
		"run_shell",
		"run_test",
		"git",
		"start_process",
		"list_processes",
		"read_process_output",
		"write_stdin",
		"stop_process",
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
// hiding the legacy tools from the model surface does not remove
// them from the registry. Internal callers can still look them up
// by name and execute them.
func TestAdvancedToolsRemainReachableViaRegistry(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, name := range []string{"run_test", "git", "start_process"} {
		tool := kit.LookupTool(name)
		if tool == nil {
			t.Errorf("registry must still contain %s for internal callers, got nil", name)
			continue
		}
		if tool.Name() != name {
			t.Errorf("registry lookup for %s returned %q", name, tool.Name())
		}
	}

	// The Execute path bypasses the model surface filter — the
	// surface only hides tools from Definitions. Confirm the
	// internal path at least resolves the tool and gets a
	// non-nil response (the actual run may fail for a
	// misconfigured workspace; we only care that the dispatcher
	// is willing to invoke it).
	if _, err := kit.executeByName(context.Background(), "run_test", `{"command":"echo phase5-reachability"}`); err != nil {
		t.Logf("run_test Execute error (expected for unit test without project setup): %v", err)
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
