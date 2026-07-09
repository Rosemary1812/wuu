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
			"Keep the same task behavior across providers",
			"runtime goals, task rail, and subagents are wuu coordination primitives",
			"Do not choose direct local work, runtime goals, task-rail delegation, or subagents based on provider/model family or brand.",
			"Use a runtime goal when the user-visible objective needs continuation across turns",
			"task rail or subagents for delegated or independent parallel work",
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
			"natural-language agent loop as the unified entry point",
			"provider-branded product modes",
			"task-handling options inside wuu",
			"direct work, subagents, or workflows",
			"workflows only when",
			"matching saved workflow",
		} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("%s adapter should not include family-specific guidance %q:\n%s", name, forbidden, text)
			}
		}
		if strings.Contains(strings.ToLower(text), "workflow") {
			t.Fatalf("%s adapter should not mention workflows as a task path:\n%s", name, text)
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
