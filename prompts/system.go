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

// SystemMain returns the main-agent-only path-selection map. The tools
// and durable-state concepts listed here (update_plan, create_goal,
// start_workflow, spawn_agent, helpme, inception, write_memory, read_memory) are
// not on a worker's tool surface, so this section must not be embedded
// in a subagent's system prompt. config.DefaultSystemPrompt() joins
// System() and SystemMain() for the main agent; config.WorkerSystemPrompt()
// returns only System() for spawned subagents.
func SystemMain() string {
	return strings.TrimSpace(systemMain)
}
