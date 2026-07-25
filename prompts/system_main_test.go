package prompts

import (
	"strings"
	"testing"
)

func TestSystemMainKeepsOnlyMainAgentCoordinationRules(t *testing.T) {
	prompt := SystemMain()
	for _, want := range []string{
		"participant posted a result card",
		"Reference the card",
		"plan, goal, or delegated result",
		"evidence for the broader objective",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("SystemMain missing %q:\n%s", want, prompt)
		}
	}
	for _, toolName := range []string{"tool_search", "update_plan", "spawn_agent", "helpme", "inception"} {
		if strings.Contains(prompt, toolName) {
			t.Fatalf("SystemMain should leave %q guidance to its surface-gated tool description:\n%s", toolName, prompt)
		}
	}
	if len(prompt) > 1024 {
		t.Fatalf("SystemMain should remain a small main-only coordination section, got %d bytes", len(prompt))
	}
}
