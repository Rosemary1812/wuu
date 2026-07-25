package prompts

import (
	"strings"
	"testing"
)

func TestSystemMainKeepsOnlyMainAgentCoordinationRules(t *testing.T) {
	prompt := SystemMain()
	for _, want := range []string{
		"completed subagent task does not mean the overall task is complete",
		"review, integrate, and verify the result",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("SystemMain missing %q:\n%s", want, prompt)
		}
	}
	for _, internalTerm := range []string{"hidden context", "result card", "conversation participant"} {
		if strings.Contains(prompt, internalTerm) {
			t.Fatalf("SystemMain should not expose UI or runtime term %q:\n%s", internalTerm, prompt)
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

func TestSystemGuidesNaturalUserCenteredReplies(t *testing.T) {
	prompt := System()
	for _, want := range []string{
		"user's mental model",
		"Skip ritual openings",
		"Treat the user as an equal",
		"Default to natural prose",
		"genuinely complex answer easier to scan",
		"do not split a short answer into sections",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("System missing %q:\n%s", want, prompt)
		}
	}
}
