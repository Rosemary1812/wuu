package agentcontrol

import (
	"strings"
	"testing"
)

// These tests pin the load-bearing lines of the model-facing prompt
// constants so a casual edit can't accidentally weaken them.

func TestSystemPromptPreamble_ContainsAgentToolRules(t *testing.T) {
	preamble := SystemPromptPreamble()
	for _, want := range []string{
		"optional Agent tool",
		"main agent owns the user conversation",
		"subagent_type",
		"general-purpose",
		"fork-self",
		"run_in_background",
		"send_message",
		"trigger_turn",
		"subagent_status",
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

func TestComposeWorkerSystemPrompt_IsCapabilityNeutralForTerminalWork(t *testing.T) {
	wt, err := LookupWorkerType(DefaultSubagentType)
	if err != nil {
		t.Fatalf("LookupWorkerType(general-purpose): %v", err)
	}
	got := composeWorkerSystemPrompt("", wt, "/tmp/repo", IsolationInplace)
	for _, want := range []string{
		"Treat command execution as non-interactive",
		"active tool surface exposes it",
		"command execution is unavailable",
		"Profile-specific tool-surface guidance",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("worker system prompt missing capability-neutral command guidance %q", want)
		}
	}
	for _, old := range []string{
		"Use run_shell",
		"structured git tool",
		"run_test",
		"Bash is the terminal entry point",
		"git status",
		"git diff",
		"git commit",
		"git restore --staged",
		"`git commit -m`",
		"`git commit -e`",
		"`git rebase -i`",
		"`git add -i`",
	} {
		if strings.Contains(got, old) {
			t.Fatalf("worker system prompt must not teach legacy command path %q:\n%s", old, got)
		}
	}
}
