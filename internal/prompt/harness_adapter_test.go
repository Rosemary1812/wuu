package prompt

import (
	"strings"
	"testing"
)

func TestHarnessAdapterTextUsesProviderAgnosticGuidance(t *testing.T) {
	claude := HarnessAdapterText("anthropic", "claude-sonnet-4-5")
	codex := HarnessAdapterText("openai", "gpt-5-codex")
	local := HarnessAdapterText("ollama", "llama-coder")

	for name, text := range map[string]string{
		"claude": claude,
		"codex":  codex,
		"local":  local,
	} {
		for _, want := range []string{
			"# Harness Adapter",
			"same product regardless of provider, model family, or BYOK backend",
			"natural-language agent loop as the unified entry point",
			"Do not choose direct work, subagents, or workflows based on provider/model family or brand.",
			"Choose execution shape from the user's task",
			"Treat provider/model differences as compatibility details only",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s adapter missing %q:\n%s", name, want, text)
			}
		}

		for _, forbidden := range []string{
			"Profile family",
			"Default write mode",
			"script driver",
			"tool-contract-driven",
			"portable harness path",
			"conservative local-model harness path",
			"Follow the model-family guidance",
		} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("%s adapter should not include family-specific guidance %q:\n%s", name, forbidden, text)
			}
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
