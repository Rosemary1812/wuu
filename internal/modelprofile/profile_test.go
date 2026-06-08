package modelprofile

import "testing"

func TestResolveClassifiesModelFamilies(t *testing.T) {
	tests := []struct {
		provider string
		model    string
		want     Family
	}{
		{provider: "anthropic", model: "claude-sonnet-4-5", want: FamilyClaude},
		{provider: "openai", model: "gpt-5-codex", want: FamilyCodex},
		{provider: "openai", model: "gpt-5.5", want: FamilyGPT},
		{provider: "google", model: "gemini-2.5-pro", want: FamilyGemini},
		{provider: "moonshot", model: "kimi-k2", want: FamilyKimi},
		{provider: "deepseek", model: "deepseek-v3.2", want: FamilyDeepSeek},
		{provider: "dashscope", model: "qwen3-coder-plus", want: FamilyQwen},
		{provider: "ollama", model: "llama-coder", want: FamilyLocal},
		{provider: "custom", model: "local-model", want: FamilyPortable},
	}
	for _, tt := range tests {
		if got := Resolve(tt.provider, tt.model).Family; got != tt.want {
			t.Fatalf("Resolve(%q, %q).Family = %s, want %s", tt.provider, tt.model, got, tt.want)
		}
	}
}

func TestResolveCodexProfileUsesPatchFirstHarness(t *testing.T) {
	profile := Resolve("openai", "gpt-5-codex")
	if profile.Workflow.DefaultWriteMode != WriteModePatch {
		t.Fatalf("DefaultWriteMode = %s, want %s", profile.Workflow.DefaultWriteMode, WriteModePatch)
	}
	if !profile.APIShape.FreeformTool || !profile.APIShape.DeveloperRole {
		t.Fatalf("expected Codex profile to support freeform tools and developer role: %+v", profile.APIShape)
	}
	if profile.Code.PreferredPatchGrammar != "codex_apply_patch" || profile.Code.PatchReliability < 5 {
		t.Fatalf("unexpected Codex code profile: %+v", profile.Code)
	}
	if !profile.Workflow.AllowParallelReadOnly {
		t.Fatalf("Codex profile should allow parallel read-only workers: %+v", profile.Workflow)
	}
}

func TestResolveGPT4AndOSSProfilesAvoidPatchMode(t *testing.T) {
	for _, model := range []string{"gpt-4.1-mini", "openai/gpt-oss-120b"} {
		profile := Resolve("openai", model)
		if profile.Workflow.DefaultWriteMode != WriteModeExactEdit {
			t.Fatalf("%s DefaultWriteMode = %s, want %s", model, profile.Workflow.DefaultWriteMode, WriteModeExactEdit)
		}
		if profile.APIShape.FreeformTool {
			t.Fatalf("%s should not default to freeform patch tools", model)
		}
	}
}

func TestResolveClaudeProfilePrefersReadEditPlan(t *testing.T) {
	profile := Resolve("anthropic", "claude-sonnet-4-5")
	if profile.Workflow.DefaultWriteMode != WriteModeExactEdit {
		t.Fatalf("DefaultWriteMode = %s, want %s", profile.Workflow.DefaultWriteMode, WriteModeExactEdit)
	}
	if !profile.Reasoning.PrefersExplicitPlan || !profile.Context.SupportsPromptCache {
		t.Fatalf("unexpected Claude reasoning/context profile: reasoning=%+v context=%+v", profile.Reasoning, profile.Context)
	}
	if profile.Code.ExactEditReliability <= profile.Code.PatchReliability {
		t.Fatalf("Claude profile should prefer exact edits over patches: %+v", profile.Code)
	}
}

func TestResolveStrictPortableCoderProfilesUseSchemaConservativePolicy(t *testing.T) {
	for _, tt := range []struct {
		provider string
		model    string
	}{
		{provider: "google", model: "gemini-2.5-pro"},
		{provider: "moonshot", model: "kimi-k2"},
		{provider: "dashscope", model: "qwen3-coder-plus"},
	} {
		profile := Resolve(tt.provider, tt.model)
		if profile.APIShape.ToolCalling != ToolCallingStrictJSON {
			t.Fatalf("%s/%s ToolCalling = %s, want %s", tt.provider, tt.model, profile.APIShape.ToolCalling, ToolCallingStrictJSON)
		}
		if profile.Workflow.DefaultWriteMode != WriteModeExactEdit {
			t.Fatalf("%s/%s DefaultWriteMode = %s, want %s", tt.provider, tt.model, profile.Workflow.DefaultWriteMode, WriteModeExactEdit)
		}
	}
}

func TestResolveLocalProfileLimitsAutonomy(t *testing.T) {
	profile := Resolve("ollama", "llama-coder")
	if profile.Workflow.DefaultMaxAutonomousSteps > 5 {
		t.Fatalf("local DefaultMaxAutonomousSteps = %d, want <= 5", profile.Workflow.DefaultMaxAutonomousSteps)
	}
	if profile.Workflow.AllowDirectShell {
		t.Fatalf("local profile should not allow direct shell by default: %+v", profile.Workflow)
	}
	if profile.Workflow.DefaultWriteMode != WriteModeWholeFileNewOnly {
		t.Fatalf("local DefaultWriteMode = %s, want %s", profile.Workflow.DefaultWriteMode, WriteModeWholeFileNewOnly)
	}
}
