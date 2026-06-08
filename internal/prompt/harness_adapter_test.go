package prompt

import (
	"strings"
	"testing"
)

func TestHarnessFamilyForModel(t *testing.T) {
	tests := []struct {
		provider string
		model    string
		want     HarnessFamily
	}{
		{provider: "anthropic", model: "claude-sonnet-4-5", want: HarnessFamilyClaude},
		{provider: "openai", model: "gpt-5-codex", want: HarnessFamilyCodex},
		{provider: "openai", model: "gpt-5.5", want: HarnessFamilyGPT},
		{provider: "google", model: "gemini-2.5-pro", want: HarnessFamilyGemini},
		{provider: "moonshot", model: "kimi-k2", want: HarnessFamilyKimi},
		{provider: "custom", model: "local-model", want: HarnessFamilyPortable},
	}
	for _, tt := range tests {
		if got := HarnessFamilyForModel(tt.provider, tt.model); got != tt.want {
			t.Fatalf("HarnessFamilyForModel(%q, %q) = %s, want %s", tt.provider, tt.model, got, tt.want)
		}
	}
}

func TestHarnessAdapterTextSelectsModelFamilyGuidance(t *testing.T) {
	claude := HarnessAdapterText("anthropic", "claude-sonnet-4-5")
	for _, want := range []string{"# Harness Adapter", "dynamic workflow path", "run_workflow", "subagents"} {
		if !strings.Contains(claude, want) {
			t.Fatalf("claude adapter missing %q:\n%s", want, claude)
		}
	}

	codex := HarnessAdapterText("openai", "gpt-5-codex")
	for _, want := range []string{"tool-contract-driven", "saved .js workflow", "review or goal tracking"} {
		if !strings.Contains(codex, want) {
			t.Fatalf("codex adapter missing %q:\n%s", want, codex)
		}
	}

	portable := HarnessAdapterText("custom", "local-model")
	for _, want := range []string{"portable harness path", "tool descriptions exactly", "spawn_agent"} {
		if !strings.Contains(portable, want) {
			t.Fatalf("portable adapter missing %q:\n%s", want, portable)
		}
	}
}

func TestBuilderAddHarnessAdapter(t *testing.T) {
	var b Builder
	b.AddSection("base", "base prompt", true)
	b.AddHarnessAdapter("openai", "gpt-5-codex")
	result := b.Build()
	if !strings.Contains(result, "base prompt") || !strings.Contains(result, "# Harness Adapter") {
		t.Fatalf("builder should include base and harness adapter:\n%s", result)
	}
}
