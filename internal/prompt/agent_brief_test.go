package prompt

import (
	"strings"
	"testing"
)

func TestAgentBriefContractTextIncludesCoreFields(t *testing.T) {
	text := AgentBriefContractText()
	for _, want := range []string{
		"Base Agent Brief Contract",
		"Invocation mode",
		"fresh worker",
		"fork",
		"Task",
		"Background",
		"self-contained",
		"Role",
		"Identity / memory",
		"Scope / ownership",
		"owned files or modules",
		"Non-goals",
		"out of scope",
		"Acceptance criteria",
		"Deliverables",
		"Reporting",
		"incremental",
		"HelpMe",
		"Constraints",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("agent brief contract missing %q:\n%s", want, text)
		}
	}
}

func TestAgentBriefContractSummaryStaysGeneral(t *testing.T) {
	summary := AgentBriefContractSummary()
	if !strings.Contains(summary, "scope, non-goals") {
		t.Fatalf("agent brief summary should keep general scope wording:\n%s", summary)
	}
	for _, want := range []string{"Fresh workers", "self-contained brief", "forks", "incremental focus"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("agent brief summary missing %q:\n%s", want, summary)
		}
	}
	if strings.Contains(summary, "ownership") {
		t.Fatalf("agent brief summary should not mention code-edit ownership:\n%s", summary)
	}
}
