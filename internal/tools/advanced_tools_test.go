package tools

import (
	"context"
	"sort"
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

// hiddenToolNames returns a stable, sorted view of the names in a
// surface.HiddenTools map so failure messages are deterministic.
func hiddenToolNames(surfaceTools map[string]capability.Capability) []string {
	out := make([]string, 0, len(surfaceTools))
	for name := range surfaceTools {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// TestAdvancedToolsHiddenFromModelSurfaces verifies that the
// legacy run_test / start_process / git / managed-process tools
// — the ones that OpenCode and Codex-style harnesses collapse into
// bash + tool_search — stay registered in the toolkit so internal
// callers (progressive disclosure, replay, the bash result
// post-processor) can still reach them, but never appear on a
// model-visible tool surface.
//
// Phase 5 of the bash-first redesign: the model never has to guess
// between run_test / start_process / git / list_processes. The
// compiler hides them in surface.Tools and toolExposure returns
// Hidden for the legacy fallback surface, so the legacy direct-tool
// surface (no profile set) also hides them.
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

	// Per-profile: the compiler keeps every advanced tool in the
	// hidden set on every standard profile, and the legacy surface
	// path agrees because toolExposure short-circuits before the
	// toolExposure-to-surface wiring runs.
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
		kit.SetActiveProfile(modelprofile.Resolve(tt.provider, tt.model))
		surface := kit.ActiveSurface()
		// run_test and git must be in the surface's HiddenTools
		// map on every profile. The model never sees them.
		for _, name := range []string{"run_test", "git"} {
			if _, ok := surface.HiddenTools[name]; !ok {
				t.Errorf("%s/%s: %s must be in surface.HiddenTools, got %v",
					tt.provider, tt.model, name, hiddenToolNames(surface.HiddenTools))
			}
		}
		// All managed-process tools must be hidden on every
		// profile, including the local sandboxed one.
		defs := kit.Definitions()
		for _, name := range advancedTools {
			if containsProfileDef(defs, name) {
				t.Errorf("%s/%s: %s must not appear in Definitions (advanced), got %v",
					tt.provider, tt.model, name, sortedProfileDefNames(defs))
			}
		}
	}
}

// TestAdvancedToolsRemainReachableViaRegistry confirms that
// hiding the legacy tools from the model surface does not remove
// them from the registry. Internal callers (tool_search activation,
// replay, the bash post-processor that adds test summaries) still
// need to look them up by name and execute them.
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
