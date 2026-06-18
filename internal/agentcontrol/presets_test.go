package agentcontrol

import (
	"strings"
	"testing"
)

// The presets are constants meant to be pasted into worker prompts
// verbatim. These tests pin the load-bearing lines so a casual edit
// can't accidentally weaken the failure-mode-specific guarantees
// the prompts encode (see presets.go for the rationale on each).

func TestVerificationPreset_HasFrameInversion(t *testing.T) {
	if !strings.Contains(VerificationPreset, "not just confirm") {
		t.Error("VerificationPreset is missing the frame-inversion line")
	}
	if !strings.Contains(VerificationPreset, "try to break it") {
		t.Error("VerificationPreset is missing the break-it directive")
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

func TestSystemPromptPreamble_ContainsAgentToolRules(t *testing.T) {
	preamble := SystemPromptPreamble()
	for _, want := range []string{
		"optional Agent tool",
		"main agent owns the user conversation",
		"subagent_type",
		"general-purpose",
		"verification",
		"fork-self",
		"run_in_background",
		"send_message",
		"followup_task",
		"await_agents",
		"parallel",
	} {
		if !strings.Contains(preamble, want) {
			t.Errorf("SystemPromptPreamble missing agent-tool concept %q", want)
		}
	}
	if strings.Contains(preamble, "You are an orchestration agent") {
		t.Fatalf("SystemPromptPreamble should not make orchestration the main identity:\n%s", preamble)
	}
	for _, old := range []string{"## Agent Types", "fork_turns", "worker: general-purpose", "research: read-only"} {
		if strings.Contains(preamble, old) {
			t.Fatalf("SystemPromptPreamble should not retain old agent taxonomy %q:\n%s", old, preamble)
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
		"inter-agent notifications",
		"based on your findings",
		"self-contained",
	} {
		if !strings.Contains(preamble, want) {
			t.Errorf("SystemPromptPreamble missing agent result handling %q", want)
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
		"delegation materially improves",
		"critical path",
		"blocks your immediate next step",
	} {
		if !strings.Contains(preamble, want) {
			t.Errorf("SystemPromptPreamble missing delegation discipline %q", want)
		}
	}
}

func TestComposeWorkerSystemPrompt_ContainsWorkerOverride(t *testing.T) {
	wt, err := LookupWorkerType(DefaultSubagentType)
	if err != nil {
		t.Fatalf("LookupWorkerType(general-purpose): %v", err)
	}
	got := composeWorkerSystemPrompt("You are wuu, a pragmatic CLI coding assistant.", wt, "/tmp/repo", IsolationInplace)
	if !strings.Contains(got, "Worker override:") {
		t.Fatalf("worker system prompt missing override marker: %q", got)
	}
	if !strings.Contains(got, "If a tool is in your tool list") {
		t.Fatalf("worker system prompt must restore access to worker tools: %q", got)
	}
	if !strings.Contains(got, "cannot spawn or manage other agents") {
		t.Fatalf("worker system prompt must match worker tool filtering: %q", got)
	}
	if strings.Contains(got, "You may spawn further sub-agents") {
		t.Fatalf("worker system prompt must not promise recursive delegation: %q", got)
	}
}

func TestComposeWorkerSystemPrompt_TeachesNonInteractiveGit(t *testing.T) {
	wt, err := LookupWorkerType(DefaultSubagentType)
	if err != nil {
		t.Fatalf("LookupWorkerType(general-purpose): %v", err)
	}
	got := composeWorkerSystemPrompt("", wt, "/tmp/repo", IsolationInplace)
	for _, want := range []string{
		"Treat shell commands as non-interactive",
		"Use run_shell for normal git status/diff/log/add/commit workflows",
		"stage intended files with explicit paths",
		"git restore --staged",
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
