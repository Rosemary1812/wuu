package prompts

import (
	_ "embed"
	"strings"
)

//go:embed system.md
var system string

//go:embed system_main.md
var systemMain string

// System returns the wuu base system prompt sections that apply to both
// the main agent and spawned subagents: identity, tone, workspace rules,
// task discipline, validation, tool use, final-answer shape, and
// anti-patterns.
func System() string {
	return strings.TrimSpace(system)
}

// SystemMain returns the main-agent-only path-selection map. Orchestration
// belongs to the brain: spawn_agent, helpme, and the subagent management
// suite are compiled out of worker surfaces, so this section must not be
// embedded in a subagent's system prompt. Of the other tools it mentions,
// update_plan and inception stay visible to workers (their worker-facing
// guidance lives in the tool descriptions), goal stays deferred behind
// tool_search on the worker surface, and read_memory/write_memory are
// retired everywhere (memory redesign). The workflow path (start_workflow)
// is deliberately NOT listed here: workflow tools are a named-agent-only
// capability, so ordinary project main agents must not be taught a tool
// they cannot call. That bullet is re-injected surface-gated by the prompt
// builder (prompt.Builder.AddWorkflowPathGuidance) only for surfaces that
// actually carry the workflow capability.
// config.DefaultSystemPrompt() joins System() and SystemMain() for the
// main agent; config.WorkerSystemPrompt() returns only System() for
// spawned subagents.
func SystemMain() string {
	return strings.TrimSpace(systemMain)
}
