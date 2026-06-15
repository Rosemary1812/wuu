package prompt

import (
	"strings"
	"testing"
)

func TestAgentBriefContractTextIncludesCoreFields(t *testing.T) {
	text := AgentBriefContractText()
	for _, want := range []string{
		"Base Agent Brief Contract",
		"Task",
		"Background",
		"Role",
		"Identity / memory",
		"Scope / ownership",
		"owned files or modules",
		"Non-goals",
		"out of scope",
		"Acceptance criteria",
		"Deliverables",
		"Reporting",
		"Constraints",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("agent brief contract missing %q:\n%s", want, text)
		}
	}
}

func TestWorkflowBriefExtensionTextIncludesOnlyWorkflowContext(t *testing.T) {
	text := WorkflowBriefExtensionText()
	for _, want := range []string{
		"Workflow context",
		"Workflow Run",
		"Phase",
		"Team Member",
		"Mode",
		"Agent Profile",
		"State binding",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("workflow brief extension missing %q:\n%s", want, text)
		}
	}
	for _, bad := range []string{"Workflow Context Extension", "Profile Extension", "Ephemeral Extension"} {
		if strings.Contains(text, bad) {
			t.Fatalf("workflow brief should avoid extension naming %q:\n%s", bad, text)
		}
	}
}
