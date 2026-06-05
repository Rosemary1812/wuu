package prompt

import "strings"

func AgentBriefContractSummary() string {
	return "Use the Base Agent Brief Contract: task, background, role, identity/memory, scope, non-goals, starting points, acceptance criteria, deliverables, reporting, and constraints."
}

func WorkflowBriefExtensionSummary() string {
	return "For workflow work, add the Workflow Context Extension: workflow_run_id, phase_id, team_member_id, team mode, agent_profile when present, and how the result binds back to workflow state."
}

func ProfileBriefExtensionSummary() string {
	return "If agent_profile is set, add the Profile Extension: treat it as the long-lived identity, use profile memory when relevant, and surface reusable lessons as memory candidates instead of saving temporary progress."
}

func EphemeralBriefExtensionSummary() string {
	return "If agent_profile is omitted, add the Ephemeral Extension: state that the worker is temporary, has no long-lived identity, and should rely only on shared/project context."
}

func AgentBriefContractText() string {
	return strings.Join([]string{
		"Base Agent Brief Contract:",
		"- Task: the concrete work to perform.",
		"- Background: relevant context the worker needs.",
		"- Role: the perspective or responsibility to use.",
		"- Identity / memory: whether this is a named profile or a temporary worker.",
		"- Scope: what is included.",
		"- Non-goals: what to avoid.",
		"- Starting points: files, commands, docs, or prior findings to inspect first.",
		"- Acceptance criteria: how to know the task is done.",
		"- Deliverables: expected output, artifacts, or changed files.",
		"- Reporting: how to hand results back.",
		"- Constraints: safety, permissions, isolation, time, or test limits.",
	}, "\n")
}

func WorkflowBriefExtensionText() string {
	return strings.Join([]string{
		"Workflow Context Extension:",
		"- Workflow Run: durable run id.",
		"- Phase: phase id/name this work belongs to.",
		"- Team Member: recorded team member id and role.",
		"- Mode: reuse_profile, create_profile, or ephemeral.",
		"- Agent Profile: profile name when mode uses a durable identity.",
		"- State binding: require agent_report, then the orchestrator records results with workflow_control.",
	}, "\n")
}
