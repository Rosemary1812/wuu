package coordinator

import (
	"strings"
	"testing"
)

// The presets are constants meant to be pasted into worker prompts
// verbatim. These tests pin the load-bearing lines so a casual edit
// can't accidentally weaken the failure-mode-specific guarantees
// the prompts encode (see presets.go for the rationale on each).

func TestVerificationPreset_HasFrameInversion(t *testing.T) {
	if !strings.Contains(VerificationPreset, "NOT to confirm") {
		t.Error("VerificationPreset is missing the frame-inversion line")
	}
	if !strings.Contains(VerificationPreset, "TRY TO BREAK IT") {
		t.Error("VerificationPreset is missing the TRY TO BREAK IT directive")
	}
}

func TestVerificationPreset_RequiresEvidence(t *testing.T) {
	if !strings.Contains(VerificationPreset, "Never write") && !strings.Contains(VerificationPreset, "PASS") {
		t.Error("VerificationPreset must require evidence for PASS verdicts")
	}
}

func TestVerificationPreset_VerdictFormat(t *testing.T) {
	for _, want := range []string{"VERDICT: PASS", "VERDICT: FAIL", "VERDICT: PARTIAL"} {
		if !strings.Contains(VerificationPreset, want) {
			t.Errorf("VerificationPreset missing verdict literal %q", want)
		}
	}
}

func TestResearchPreset_IsReadOnly(t *testing.T) {
	if !strings.Contains(ResearchPreset, "Do NOT modify") {
		t.Error("ResearchPreset must forbid modifications")
	}
	if !strings.Contains(ResearchPreset, "mutate state") {
		t.Error("ResearchPreset must forbid state-mutating commands")
	}
}

func TestResearchPreset_RequiresFileLineCitations(t *testing.T) {
	if !strings.Contains(ResearchPreset, "file:line") {
		t.Error("ResearchPreset must require file:line citations")
	}
}

func TestResearchPreset_OutputShape(t *testing.T) {
	for _, want := range []string{"## Answer", "## Evidence", "## Notes"} {
		if !strings.Contains(ResearchPreset, want) {
			t.Errorf("ResearchPreset missing required section %q", want)
		}
	}
}

func TestSystemPromptPreamble_ContainsOrchestrationRules(t *testing.T) {
	preamble := SystemPromptPreamble()
	for _, want := range []string{
		"coordinator",
		"worker",
		"spawn_agent",
		"fork_agent",
		"send_message",
		"followup_task",
		"parallel",
	} {
		if !strings.Contains(preamble, want) {
			t.Errorf("SystemPromptPreamble missing orchestration concept %q", want)
		}
	}
}

func TestSystemPromptPreamble_ContainsConcurrencyRules(t *testing.T) {
	preamble := SystemPromptPreamble()
	for _, want := range []string{
		"parallel",
		"one at a time",
		"conflicts",
	} {
		if !strings.Contains(preamble, want) {
			t.Errorf("SystemPromptPreamble missing concurrency rule %q", want)
		}
	}
}

func TestSystemPromptPreamble_ContainsWorkerResultHandling(t *testing.T) {
	preamble := SystemPromptPreamble()
	for _, want := range []string{
		"Workers cannot see your conversation history",
		"based on your findings",
		"self-contained",
	} {
		if !strings.Contains(preamble, want) {
			t.Errorf("SystemPromptPreamble missing worker result handling %q", want)
		}
	}
}

func TestSystemPromptPreamble_ContainsHonestyRules(t *testing.T) {
	preamble := SystemPromptPreamble()
	for _, want := range []string{
		"stuck",
		"failure",
		"close_agent",
	} {
		if !strings.Contains(preamble, want) {
			t.Errorf("SystemPromptPreamble missing honesty rule %q", want)
		}
	}
}

func TestSystemPromptPreamble_ContainsDelegationDiscipline(t *testing.T) {
	preamble := SystemPromptPreamble()
	for _, want := range []string{
		"trivial tasks",
		"higher-level work",
		"critical path",
		"blocks your immediate next step",
	} {
		if !strings.Contains(preamble, want) {
			t.Errorf("SystemPromptPreamble missing delegation discipline %q", want)
		}
	}
}

func TestComposeWorkerSystemPrompt_ContainsWorkerOverride(t *testing.T) {
	wt, err := LookupWorkerType("worker")
	if err != nil {
		t.Fatalf("LookupWorkerType(worker): %v", err)
	}
	got := composeWorkerSystemPrompt("You are wuu, a pragmatic CLI coding assistant.", wt, "/tmp/repo", IsolationInplace)
	if !strings.Contains(got, "Worker override:") {
		t.Fatalf("worker system prompt missing override marker: %q", got)
	}
	if !strings.Contains(got, "If a tool is in your tool list") {
		t.Fatalf("worker system prompt must restore access to worker tools: %q", got)
	}
}

func TestComposeWorkerSystemPrompt_TeachesNonInteractiveGit(t *testing.T) {
	wt, err := LookupWorkerType("worker")
	if err != nil {
		t.Fatalf("LookupWorkerType(worker): %v", err)
	}
	got := composeWorkerSystemPrompt("", wt, "/tmp/repo", IsolationInplace)
	for _, want := range []string{
		"Treat shell commands as non-interactive",
		"`git commit -m`",
		"`git commit -e`",
		"`git rebase -i`",
		"`git add -i`",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("worker system prompt missing non-interactive git guidance %q", want)
		}
	}
}
