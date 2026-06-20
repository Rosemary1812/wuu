package goal

import (
	"fmt"
	"sort"
	"strings"
)

type ContextScope string

const (
	ContextScopeGoalOnly ContextScope = "goal_only"
	ContextScopeRelevant ContextScope = "relevant"
	ContextScopeDiff     ContextScope = "diff"
	ContextScopeFull     ContextScope = "full"
)

type RoleConfig struct {
	Name            string       `json:"name"`
	Role            string       `json:"role"`
	SystemPrompt    string       `json:"system_prompt"`
	AllowedTools    []string     `json:"allowed_tools,omitempty"`
	ContextScope    ContextScope `json:"context_scope"`
	OutputSchema    string       `json:"output_schema"`
	SuccessCriteria []string     `json:"success_criteria,omitempty"`
}

func BuiltinRoles() []RoleConfig {
	roles := []RoleConfig{
		{
			Name:         "lead",
			Role:         "Lead / Planner",
			ContextScope: ContextScopeFull,
			AllowedTools: []string{"read_file", "grep", "glob", "repo_map", "load_skill", "list_workflows", "start_workflow", "workflow_status", "workflow_control", "spawn_agent", "await_agents", "agent_report"},
			SystemPrompt: "Own the goal, maintain durable state, split work into phases, and require reviewer or verifier evidence before completion.",
			OutputSchema: "report: summary, decisions, assignments, blockers, verification policy, next steps",
			SuccessCriteria: []string{
				"Durable state is current.",
				"Workers have concrete acceptance criteria.",
				"Completion is backed by independent review or verification.",
			},
		},
		{
			Name:         "planner",
			Role:         "Planner",
			ContextScope: ContextScopeRelevant,
			AllowedTools: []string{"read_file", "grep", "glob", "repo_map", "load_skill", "list_workflows", "update_plan", "agent_report"},
			SystemPrompt: "Turn researched context into an executable plan with scope, non-goals, risks, verification, and rollback notes. Do not edit product files.",
			OutputSchema: "plan: steps, files, risks, verification, rollback, open questions",
			SuccessCriteria: []string{
				"Plan is specific enough for a worker.",
				"Plan includes verification and rollback.",
			},
		},
		{
			Name:         "researcher",
			Role:         "Researcher",
			ContextScope: ContextScopeRelevant,
			AllowedTools: []string{"read_file", "grep", "glob", "repo_map", "ast_search", "semantic_search", "bash", "load_skill", "agent_report"},
			SystemPrompt: "Read the code and summarize constraints, ownership boundaries, relevant files, and unknowns. Do not edit files.",
			OutputSchema: "research: relevant files, findings, constraints, risks, suggested next reads",
			SuccessCriteria: []string{
				"Findings cite files or commands.",
				"Unknowns are explicit.",
			},
		},
		{
			Name:         "worker",
			Role:         "Worker",
			ContextScope: ContextScopeRelevant,
			AllowedTools: nil,
			SystemPrompt: "Implement the assigned scoped change and its tests. Report changed files and verification evidence. Do not declare the whole goal complete.",
			OutputSchema: "handoff: changed_files, work_done, tests, risks, next_steps",
			SuccessCriteria: []string{
				"Implementation matches assigned scope.",
				"Relevant tests or verification were run.",
			},
		},
		{
			Name:         "reviewer",
			Role:         "Reviewer",
			ContextScope: ContextScopeDiff,
			AllowedTools: []string{"read_file", "grep", "glob", "bash", "load_skill", "agent_report"},
			SystemPrompt: "Review the diff for real bugs, missed requirements, unsafe behavior, and missing verification. Do not edit files.",
			OutputSchema: "review: verdict, findings, evidence, residual risk",
			SuccessCriteria: []string{
				"Findings are actionable and evidence-backed.",
				"No style-only issues are reported.",
			},
		},
		{
			Name:         "qa",
			Role:         "QA / Verifier",
			ContextScope: ContextScopeRelevant,
			AllowedTools: []string{"read_file", "grep", "glob", "bash", "agent_report"},
			SystemPrompt: "Verify the real user path with commands, builds, tests, browser or process checks when available. Do not edit files.",
			OutputSchema: "verification: verdict, commands, outputs, failures, reproduction notes",
			SuccessCriteria: []string{
				"Verdict is backed by fresh evidence.",
				"Failures include logs and reproduction steps.",
			},
		},
		{
			Name:         "debugger",
			Role:         "Debugger",
			ContextScope: ContextScopeRelevant,
			AllowedTools: []string{"read_file", "grep", "glob", "repo_map", "bash", "agent_report"},
			SystemPrompt: "Analyze failure logs and identify root cause before proposing or making a fix. Prefer evidence over guesses.",
			OutputSchema: "debug: symptom, evidence, root cause, fix options, next command",
			SuccessCriteria: []string{
				"Root cause is tied to observed evidence.",
				"Next action is concrete.",
			},
		},
		{
			Name:         "integrator",
			Role:         "Integrator",
			ContextScope: ContextScopeFull,
			AllowedTools: []string{"read_file", "grep", "glob", "bash", "workflow_status", "workflow_control", "agent_report"},
			SystemPrompt: "Combine approved outputs, detect conflicts, preserve provenance, and write the final handoff. Do not bypass failed verification.",
			OutputSchema: "integration: merged artifacts, conflicts, final verification, final report",
			SuccessCriteria: []string{
				"Only approved work is integrated.",
				"Conflicts and residual risks are recorded.",
			},
		},
	}
	sort.Slice(roles, func(i, j int) bool { return roles[i].Name < roles[j].Name })
	return roles
}

func FindRole(name string) (RoleConfig, bool) {
	name = strings.TrimSpace(strings.ToLower(name))
	for _, role := range BuiltinRoles() {
		if role.Name == name {
			return role, true
		}
	}
	return RoleConfig{}, false
}

func MustRole(name string) (RoleConfig, error) {
	role, ok := FindRole(name)
	if !ok {
		return RoleConfig{}, fmt.Errorf("unknown goal role %q", name)
	}
	return role, nil
}
