package prompts

import (
	"strings"
	"testing"
)

func TestSystemMainKeepsOnlyMainAgentCoordinationRules(t *testing.T) {
	prompt := SystemMain()
	for _, want := range []string{
		"completed subagent task does not mean the overall task is complete",
		"integrate the result and verify the overall work",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("SystemMain missing %q:\n%s", want, prompt)
		}
	}
	for _, duplicatedTerm := range []string{"hidden context", "result card", "conversation participant"} {
		if strings.Contains(prompt, duplicatedTerm) {
			t.Fatalf("SystemMain should leave per-message result-card guidance out of the stable prompt %q:\n%s", duplicatedTerm, prompt)
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
