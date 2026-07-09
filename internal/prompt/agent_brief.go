package prompt

import "strings"

func AgentBriefContractSummary() string {
	return "Use the Base Agent Brief Contract: invocation mode, task, background, role, identity/memory, scope, non-goals, starting points, acceptance criteria, deliverables, reporting, and constraints. Fresh workers need a self-contained brief; forks need only the incremental focus and deliverable."
}

func ProfileBriefExtensionSummary() string {
	return "If agent_profile is set, state the profile name, use saved profile memory when relevant, and surface reusable lessons as memory candidates instead of saving temporary progress."
}

func EphemeralBriefExtensionSummary() string {
	return "If agent_profile is omitted, state that the worker is temporary and should rely only on shared/project context."
}

func AgentBriefContractText() string {
	return strings.Join([]string{
		"Base Agent Brief Contract:",
		"- Invocation mode: state whether this is a fresh worker with no parent history or a fork that inherits the parent context.",
		"- Task: the concrete work to perform.",
		"- Background: relevant context the worker needs. For fresh workers, make this self-contained and include only useful trace refs; do not assume prior conversation.",
		"- Role: the perspective or responsibility to use.",
		"- Identity / memory: whether this is a named profile or a temporary worker.",
		"- Scope / ownership: what is included, including owned files or modules for code-edit tasks.",
		"- Non-goals: what to avoid, including nearby files or modules that are out of scope when relevant.",
		"- Starting points: files, commands, docs, or prior findings to inspect first.",
		"- Acceptance criteria: how to know the task is done.",
		"- Deliverables: expected output, artifacts, or changed files.",
		"- Reporting: how to hand results back. Fork briefs should be incremental: focus, scope, non-goals, and deliverable rather than a pasted transcript.",
		"- Constraints: safety, permissions, isolation, time, or test limits.",
		"- Recovery boundary: use HelpMe instead of ordinary spawn_agent when the real need is a fresh second opinion after repeated failure or polluted context.",
	}, "\n")
}
