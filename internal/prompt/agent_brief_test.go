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
		"Scope",
		"Non-goals",
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
		"Workflow Context Extension",
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
}
