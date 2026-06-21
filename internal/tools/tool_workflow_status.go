package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/statepath"
	"github.com/blueberrycongee/wuu/internal/workflow"
)

// ---------------------------------------------------------------------------
// workflow_status
// ---------------------------------------------------------------------------

type WorkflowStatusTool struct{ env *Env }

func NewWorkflowStatusTool(env *Env) *WorkflowStatusTool { return &WorkflowStatusTool{env: env} }

func (t *WorkflowStatusTool) Name() string            { return "workflow_status" }
func (t *WorkflowStatusTool) IsReadOnly() bool        { return true }
func (t *WorkflowStatusTool) IsConcurrencySafe() bool { return true }

func (t *WorkflowStatusTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "workflow_status",
		Description: "Inspect durable Workflow Run state. Pass run_id to inspect one run, or omit run_id to list recent runs. " +
			"Use this after start_workflow, after awaiting workflow agents, before resuming a paused run, and before final synthesis.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"run_id": map[string]any{
					"type":        "string",
					"description": "Workflow run id. Omit to list recent runs.",
				},
				"include_events": map[string]any{
					"type":        "boolean",
					"description": "Whether to include recent event log entries for a specific run. Defaults false.",
				},
				"event_limit": map[string]any{
					"type":        "integer",
					"description": "Maximum event entries to return when include_events is true. Defaults 20.",
				},
			},
		},
	}
}

func (t *WorkflowStatusTool) Execute(_ context.Context, argsJSON string) (string, error) {
	var args struct {
		RunID         string `json:"run_id"`
		IncludeEvents bool   `json:"include_events"`
		EventLimit    int    `json:"event_limit"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	store, err := t.env.WorkflowStore()
	if err != nil {
		return "", fmt.Errorf("resolve workflow store: %w", err)
	}
	runID := strings.TrimSpace(args.RunID)
	if runID == "" {
		runs, err := store.ListRuns()
		if err != nil {
			return "", err
		}
		return mustJSON(map[string]any{
			"action": "workflow_status",
			"runs":   reverseWorkflowRunsWithDriverDefaults(runs),
			"count":  len(runs),
			"next_steps": []string{
				"Pass run_id to workflow_status to inspect a specific Workflow Run.",
				"Use start_workflow with driver=auto when starting new workflow work.",
			},
		})
	}

	run, err := store.LoadRun(runID)
	if err != nil {
		return "", err
	}
	run = workflowRunWithDriverDefaults(run)
	agents, err := store.ListAgentRuns(runID)
	if err != nil {
		return "", err
	}
	memoryCandidates, err := store.ListMemoryCandidates(runID)
	if err != nil {
		return "", err
	}
	fileCheckpoints, err := store.ListFileCheckpoints(runID)
	if err != nil {
		return "", err
	}
	teamPlan, err := store.LoadTeamPlan(runID)
	if err != nil {
		return "", err
	}
	arbitration := workflow.AnalyzeTeamArbitration(agents)
	result := map[string]any{
		"action":            "workflow_status",
		"run":               run,
		"agent_runs":        agents,
		"workflow_team":     teamPlan,
		"team_arbitration":  arbitration,
		"memory_candidates": memoryCandidates,
		"file_checkpoints":  fileCheckpoints,
		"next_steps":        workflowStatusNextSteps(run, agents, teamPlan, arbitration),
	}
	if run.DefinitionName != "" {
		if def, ok := t.env.FindWorkflow(run.DefinitionName); ok {
			profileResolution, err := resolveWorkflowProfilesForDefinition(def, false)
			if err != nil {
				return "", err
			}
			result["profile_resolution"] = profileResolution
		}
	}
	if args.IncludeEvents {
		events, err := store.ListEvents(runID)
		if err != nil {
			return "", err
		}
		result["events"] = tailWorkflowEvents(events, args.EventLimit)
	}
	return mustJSON(result)
}

func resolveWorkflowProfilesForDefinition(def workflow.Definition, createMissing bool) ([]workflow.ProfileResolution, error) {
	if len(def.Profiles) == 0 {
		return nil, nil
	}
	wuuHome, err := statepath.Home("")
	if err != nil {
		return nil, err
	}
	return workflow.ResolveProfiles(workflow.ProfileResolutionOptions{
		WuuHome:       wuuHome,
		Definition:    def,
		CreateMissing: createMissing,
	})
}

func workflowProfileResolutionNames(resolutions []workflow.ProfileResolution) []string {
	names := make([]string, 0, len(resolutions))
	for _, resolution := range resolutions {
		if strings.TrimSpace(resolution.Name) != "" {
			names = append(names, resolution.Name)
		}
	}
	return names
}

func parseInitialWorkflowStatus(raw string) (workflow.RunState, error) {
	switch strings.TrimSpace(raw) {
	case "", string(workflow.RunStateRunning):
		return workflow.RunStateRunning, nil
	case string(workflow.RunStateDraft):
		return workflow.RunStateDraft, nil
	case string(workflow.RunStateApprovalPending):
		return workflow.RunStateApprovalPending, nil
	default:
		return "", fmt.Errorf("invalid initial_status %q", raw)
	}
}

func parseWorkflowRunState(raw string) (workflow.RunState, error) {
	switch strings.TrimSpace(raw) {
	case string(workflow.RunStateDraft):
		return workflow.RunStateDraft, nil
	case string(workflow.RunStateApprovalPending):
		return workflow.RunStateApprovalPending, nil
	case string(workflow.RunStateRunning):
		return workflow.RunStateRunning, nil
	case string(workflow.RunStatePaused):
		return workflow.RunStatePaused, nil
	case string(workflow.RunStateCompleted):
		return workflow.RunStateCompleted, nil
	case string(workflow.RunStateFailed):
		return workflow.RunStateFailed, nil
	case string(workflow.RunStateCancelled):
		return workflow.RunStateCancelled, nil
	default:
		return "", fmt.Errorf("invalid workflow run status %q", raw)
	}
}

func parseWorkflowPhaseState(raw string) (workflow.PhaseState, error) {
	switch strings.TrimSpace(raw) {
	case string(workflow.PhaseStatePending):
		return workflow.PhaseStatePending, nil
	case string(workflow.PhaseStateRunnable):
		return workflow.PhaseStateRunnable, nil
	case string(workflow.PhaseStateRunning):
		return workflow.PhaseStateRunning, nil
	case string(workflow.PhaseStateCompleted):
		return workflow.PhaseStateCompleted, nil
	case string(workflow.PhaseStateBlocked):
		return workflow.PhaseStateBlocked, nil
	case string(workflow.PhaseStateFailed):
		return workflow.PhaseStateFailed, nil
	case string(workflow.PhaseStateSkipped):
		return workflow.PhaseStateSkipped, nil
	default:
		return "", fmt.Errorf("invalid workflow phase status %q", raw)
	}
}

func parseWorkflowAgentRunState(raw string) (workflow.AgentRunState, error) {
	switch strings.TrimSpace(raw) {
	case string(workflow.AgentRunStateQueued):
		return workflow.AgentRunStateQueued, nil
	case string(workflow.AgentRunStateStarting):
		return workflow.AgentRunStateStarting, nil
	case string(workflow.AgentRunStateRunning):
		return workflow.AgentRunStateRunning, nil
	case string(workflow.AgentRunStateAwaitingReport):
		return workflow.AgentRunStateAwaitingReport, nil
	case string(workflow.AgentRunStateCompleted):
		return workflow.AgentRunStateCompleted, nil
	case string(workflow.AgentRunStateFailed):
		return workflow.AgentRunStateFailed, nil
	case string(workflow.AgentRunStateRetrying):
		return workflow.AgentRunStateRetrying, nil
	case string(workflow.AgentRunStateCancelled):
		return workflow.AgentRunStateCancelled, nil
	default:
		return "", fmt.Errorf("invalid workflow agent run status %q", raw)
	}
}

func parseWorkflowMemoryCandidateStatus(raw string) (workflow.MemoryCandidateStatus, error) {
	switch strings.TrimSpace(raw) {
	case string(workflow.MemoryCandidatePending):
		return workflow.MemoryCandidatePending, nil
	case string(workflow.MemoryCandidateAccepted):
		return workflow.MemoryCandidateAccepted, nil
	case string(workflow.MemoryCandidateRejected):
		return workflow.MemoryCandidateRejected, nil
	default:
		return "", fmt.Errorf("invalid workflow memory candidate status %q", raw)
	}
}

func buildWorkflowPhases(inputs []workflowPhaseInput, plan string, runStatus workflow.RunState) []workflow.Phase {
	names := make([]workflowPhaseInput, 0, len(inputs))
	for _, input := range inputs {
		if strings.TrimSpace(input.Name) == "" {
			continue
		}
		names = append(names, input)
	}
	if len(names) == 0 {
		for _, name := range extractWorkflowPhaseNames(plan) {
			names = append(names, workflowPhaseInput{Name: name})
		}
	}
	if len(names) == 0 && strings.TrimSpace(plan) != "" {
		names = append(names, workflowPhaseInput{ID: "workflow", Name: "Workflow"})
	}

	used := make(map[string]int, len(names))
	phases := make([]workflow.Phase, 0, len(names))
	for i, input := range names {
		id := workflowPhaseID(input.ID, input.Name, used)
		status := workflow.PhaseStatePending
		if runStatus == workflow.RunStateRunning && i == 0 {
			status = workflow.PhaseStateRunnable
		}
		phases = append(phases, workflow.Phase{
			ID:     id,
			Name:   strings.TrimSpace(input.Name),
			Status: status,
		})
	}
	return phases
}

func workflowPhaseID(rawID, name string, used map[string]int) string {
	base := slugWorkflowID(rawID)
	if base == "" {
		base = slugWorkflowID(name)
	}
	if base == "" {
		base = "phase"
	}
	count := used[base]
	used[base] = count + 1
	if count == 0 {
		return base
	}
	return fmt.Sprintf("%s_%d", base, count+1)
}

func slugWorkflowID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastUnderscore := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore && b.Len() > 0 {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(b.String(), "_")
}

func extractWorkflowPhaseNames(body string) []string {
	lines := strings.Split(body, "\n")
	inPhases := false
	phaseHeadingLevel := 0
	var names []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			level := leadingHashCount(trimmed)
			heading := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			if strings.EqualFold(heading, "Phases") {
				inPhases = true
				phaseHeadingLevel = level
				continue
			}
			if inPhases && level <= phaseHeadingLevel {
				break
			}
		}
		if !inPhases {
			continue
		}
		if name := markdownListItemText(trimmed); name != "" {
			names = append(names, name)
			if len(names) >= 100 {
				break
			}
		}
	}
	return names
}

func leadingHashCount(value string) int {
	count := 0
	for _, r := range value {
		if r != '#' {
			break
		}
		count++
	}
	return count
}

func markdownListItemText(value string) string {
	if strings.HasPrefix(value, "- ") || strings.HasPrefix(value, "* ") {
		return strings.TrimSpace(value[2:])
	}
	idx := 0
	for idx < len(value) && value[idx] >= '0' && value[idx] <= '9' {
		idx++
	}
	if idx == 0 || idx >= len(value) {
		return ""
	}
	if value[idx] != '.' && value[idx] != ')' {
		return ""
	}
	return strings.TrimSpace(value[idx+1:])
}

func workflowDefinitionPath(rootDir, scope, name, kind string) (string, string, error) {
	dirName := slugWorkflowID(name)
	if dirName == "" {
		return "", "", fmt.Errorf("invalid workflow name %q", name)
	}
	filename := "WORKFLOW.md"
	if kind == workflow.DefinitionKindScript {
		filename = "WORKFLOW.js"
	}
	switch strings.TrimSpace(scope) {
	case "", "project":
		return filepath.Join(workflow.ProjectWorkflowPath(rootDir), dirName, filename), "project", nil
	case "user":
		wuuHome, err := statepath.Home("")
		if err != nil {
			return "", "", err
		}
		return filepath.Join(workflow.UserWorkflowPath(wuuHome), dirName, filename), "user", nil
	default:
		return "", "", fmt.Errorf("invalid workflow scope %q", scope)
	}
}

func workflowDefinitionKind(def workflow.Definition) string {
	if def.Kind == workflow.DefinitionKindScript {
		return workflow.DefinitionKindScript
	}
	return workflow.DefinitionKindMarkdown
}

func workflowDefinitionNextSteps(def workflow.Definition) []string {
	if workflowDefinitionKind(def) == workflow.DefinitionKindScript {
		return []string{
			"Use start_workflow with definition_name and driver=auto to start this script-driven workflow.",
			"Use run_workflow directly only when the task explicitly requires the script path.",
			"Let the script driver own phase/spawn/await/synthesis control; inspect workflow_status for durable run state.",
		}
	}
	return []string{
		"Use start_workflow with definition_name and driver=auto to start an agent-managed Workflow Run.",
		"Use create_workflow directly only when the task explicitly requires the agent-managed path.",
		"After start_workflow returns driver=agent_managed, list Agent Profiles, record the Workflow Team, spawn agents, await results, and bind them back with workflow_control.",
	}
}

func normalizeWorkflowDefinitionKind(raw string) (string, error) {
	switch strings.TrimSpace(raw) {
	case "", workflow.DefinitionKindMarkdown:
		return workflow.DefinitionKindMarkdown, nil
	case workflow.DefinitionKindScript:
		return workflow.DefinitionKindScript, nil
	default:
		return "", fmt.Errorf("invalid workflow kind %q", raw)
	}
}

func renderWorkflowDefinitionMarkdown(args saveWorkflowArgs, name, body string) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", workflowFrontmatterScalar(name))
	if strings.TrimSpace(args.Description) != "" {
		fmt.Fprintf(&b, "description: %s\n", workflowFrontmatterScalar(args.Description))
	}
	if strings.TrimSpace(args.WhenToUse) != "" {
		fmt.Fprintf(&b, "when-to-use: %s\n", workflowFrontmatterScalar(args.WhenToUse))
	}
	if strings.TrimSpace(args.ArgumentHint) != "" {
		fmt.Fprintf(&b, "argument-hint: %s\n", workflowFrontmatterScalar(args.ArgumentHint))
	}
	if strings.TrimSpace(args.Version) != "" {
		fmt.Fprintf(&b, "version: %s\n", workflowFrontmatterScalar(args.Version))
	}
	if args.MaxAgents > 0 {
		fmt.Fprintf(&b, "max-agents: %d\n", args.MaxAgents)
	}
	if args.MaxConcurrency > 0 {
		fmt.Fprintf(&b, "max-concurrency: %d\n", args.MaxConcurrency)
	}
	if len(args.Profiles) > 0 {
		b.WriteString("profiles:\n")
		for _, profile := range args.Profiles {
			profileName := strings.TrimSpace(profile.Name)
			if profileName == "" {
				continue
			}
			fmt.Fprintf(&b, "  - name: %s\n", workflowFrontmatterScalar(profileName))
			if profile.Required {
				b.WriteString("    required: true\n")
			}
		}
	}
	if strings.TrimSpace(args.AllowProfileCreation) != "" {
		fmt.Fprintf(&b, "allow-profile-creation: %s\n", workflowFrontmatterScalar(args.AllowProfileCreation))
	}
	if strings.TrimSpace(args.MemoryPolicy) != "" {
		fmt.Fprintf(&b, "memory-policy: %s\n", workflowFrontmatterScalar(args.MemoryPolicy))
	}
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimSpace(body))
	b.WriteString("\n")
	return b.String()
}

func renderWorkflowDefinitionScript(args saveWorkflowArgs, name, body string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "// name: %s\n", workflowFrontmatterScalar(name))
	if strings.TrimSpace(args.Description) != "" {
		fmt.Fprintf(&b, "// description: %s\n", workflowFrontmatterScalar(args.Description))
	}
	if strings.TrimSpace(args.WhenToUse) != "" {
		fmt.Fprintf(&b, "// when-to-use: %s\n", workflowFrontmatterScalar(args.WhenToUse))
	}
	if strings.TrimSpace(args.ArgumentHint) != "" {
		fmt.Fprintf(&b, "// argument-hint: %s\n", workflowFrontmatterScalar(args.ArgumentHint))
	}
	if strings.TrimSpace(args.Version) != "" {
		fmt.Fprintf(&b, "// version: %s\n", workflowFrontmatterScalar(args.Version))
	}
	if args.MaxAgents > 0 {
		fmt.Fprintf(&b, "// max-agents: %d\n", args.MaxAgents)
	}
	if args.MaxConcurrency > 0 {
		fmt.Fprintf(&b, "// max-concurrency: %d\n", args.MaxConcurrency)
	}
	if len(args.Profiles) > 0 {
		names := make([]string, 0, len(args.Profiles))
		for _, profile := range args.Profiles {
			if strings.TrimSpace(profile.Name) != "" {
				names = append(names, strings.TrimSpace(profile.Name))
			}
		}
		if len(names) > 0 {
			fmt.Fprintf(&b, "// profiles: %s\n", strings.Join(names, ", "))
		}
	}
	if strings.TrimSpace(args.AllowProfileCreation) != "" {
		fmt.Fprintf(&b, "// allow-profile-creation: %s\n", workflowFrontmatterScalar(args.AllowProfileCreation))
	}
	if strings.TrimSpace(args.MemoryPolicy) != "" {
		fmt.Fprintf(&b, "// memory-policy: %s\n", workflowFrontmatterScalar(args.MemoryPolicy))
	}
	b.WriteString("\n")
	b.WriteString(strings.TrimSpace(body))
	b.WriteString("\n")
	return b.String()
}

func workflowFrontmatterScalar(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if value == "" {
		return `""`
	}
	return value
}

func reusableWorkflowBodyFromRun(run workflow.Run) string {
	var b strings.Builder
	b.WriteString("## Intent\n\n")
	if strings.TrimSpace(run.Arguments) != "" {
		fmt.Fprintf(&b, "Repeat the process used for: %s\n", strings.TrimSpace(run.Arguments))
	} else {
		b.WriteString("Repeat this workflow for similar future work.\n")
	}
	if len(run.Phases) > 0 {
		b.WriteString("\n## Phases\n\n")
		for i, phase := range run.Phases {
			name := strings.TrimSpace(phase.Name)
			if name == "" {
				name = phase.ID
			}
			fmt.Fprintf(&b, "%d. %s\n", i+1, name)
		}
	}
	b.WriteString("\n## Output\n\n")
	b.WriteString("The final report must include shipped behavior, changed files, verification, open risks, and memory candidates.\n")
	return b.String()
}

func upsertWorkflowDefinition(existing []workflow.Definition, def workflow.Definition) []workflow.Definition {
	for i := range existing {
		if existing[i].Name == def.Name {
			next := make([]workflow.Definition, len(existing))
			copy(next, existing)
			next[i] = def
			return next
		}
	}
	next := make([]workflow.Definition, 0, len(existing)+1)
	next = append(next, existing...)
	next = append(next, def)
	return next
}

func renderWorkflowPlan(def workflow.Definition, arguments, plan string, phases []workflow.Phase) string {
	var b strings.Builder
	b.WriteString("# Workflow Run Plan\n\n")
	if def.Name != "" {
		fmt.Fprintf(&b, "- Definition: %s\n", def.Name)
	}
	if strings.TrimSpace(arguments) != "" {
		fmt.Fprintf(&b, "- Arguments: %s\n", strings.TrimSpace(arguments))
	}
	if len(phases) > 0 {
		b.WriteString("\n## Phases\n\n")
		for i, phase := range phases {
			fmt.Fprintf(&b, "%d. %s (`%s`)\n", i+1, phase.Name, phase.ID)
		}
	}
	if strings.TrimSpace(plan) != "" {
		b.WriteString("\n## Plan Body\n\n")
		b.WriteString(strings.TrimSpace(plan))
		b.WriteString("\n")
	}
	return b.String()
}

func workflowNextSteps(status workflow.RunState) []string {
	switch status {
	case workflow.RunStateDraft:
		return []string{"Review or refine the plan, then transition the run to running before spawning agents."}
	case workflow.RunStateApprovalPending:
		return []string{"Ask for the required approval, then resume the workflow run when approved."}
	case workflow.RunStatePaused:
		return []string{
			"Inspect workflow_status for pause_reason, resume_hint, blocked phases, and Agent Runs.",
			"Resolve the blocker or rollback concern, then use workflow_control action=resume_run.",
			"If a specific Agent Run should be retried, use workflow_control action=retry_agent_run before spawning or messaging it again.",
		}
	default:
		return []string{
			"Call list_agent_profiles, choose reuse_profile/create_profile/ephemeral members for this run, then record the Workflow Team with workflow_control action=record_workflow_team.",
			"Write each team member prompt from the Base Agent Brief Contract and include workflow run, phase, team member, and result-binding context.",
			"Spawn reuse_profile/create_profile members with spawn_agent.agent_profile set to the recorded profile; spawn ephemeral members without agent_profile. Pass the workflow's goal_id and goal_dir to spawn_agent so agent_report updates the goal state.",
			"Require each workflow agent to call agent_report before treating its work as complete.",
			"Create file checkpoints before risky direct edits that may need rollback.",
			"Use await_agents when synthesis depends on agent output, then workflow_control action=record_await_results to bind results to the workflow run.",
			"Use workflow_control action=generate_final_report after all blocking phases and agent runs are complete.",
		}
	}
}

func runWorkflowNextSteps(status workflow.RunState) []string {
	switch status {
	case workflow.RunStatePaused:
		return []string{
			"Inspect workflow_status for pause_reason, resume_hint, and profile_resolution.",
			"Create or choose the required Agent Profiles, then use workflow_control action=resume_run.",
		}
	case workflow.RunStateCompleted:
		return []string{
			"Inspect workflow_status for the final report, phases, Agent Runs, and event log.",
			"Use save_workflow with run_id if this ad hoc script should become a reusable workflow.",
		}
	case workflow.RunStateFailed:
		return []string{
			"Inspect workflow_status and the saved workflow.js artifact for the script failure.",
			"Fix the script or blocked agent output, then start a new workflow run.",
		}
	case workflow.RunStateCancelled:
		return []string{"Inspect workflow_status for cancellation context before starting a new workflow run."}
	default:
		return []string{
			"Use workflow_status to monitor phases, Agent Runs, and the event log while the script runs.",
			"Use workflow_control action=pause_run/resume_run if the script needs to pause between orchestration steps.",
			"Use awaitAgents in the script for fan-in and synthesize for the final report.",
		}
	}
}

func workflowStatusNextSteps(run workflow.Run, agents []workflow.AgentRun, teamPlan workflow.TeamPlan, arbitration workflow.TeamArbitration) []string {
	switch run.Status {
	case workflow.RunStateDraft, workflow.RunStateApprovalPending, workflow.RunStatePaused:
		return workflowNextSteps(run.Status)
	case workflow.RunStateCompleted:
		return []string{
			"Inspect final_report_path, agent reports, and events as the durable record of the completed workflow.",
			"Use save_workflow with run_id only if this run should become a reusable workflow definition.",
		}
	case workflow.RunStateFailed:
		return []string{
			"Inspect run error, failed Agent Runs, file checkpoints, and events before deciding whether to retry, rollback, or start a new run.",
		}
	case workflow.RunStateCancelled:
		return []string{
			"Treat this run as closed unless the user asks to start a replacement Workflow Run.",
		}
	}

	if strings.TrimSpace(run.ScriptPath) != "" {
		if arbitration.Status == "attention_required" {
			return append(copyWorkflowSteps(arbitration.NextActions), "After resolving script worker issues, inspect workflow_status again for final_report_path or remaining events.")
		}
		return []string{
			"The script driver owns phase/spawn/await/synthesis control; inspect events and final_report_path, and pause the run if it appears stuck.",
		}
	}

	if len(agents) > 0 && arbitration.Status == "attention_required" {
		return append(copyWorkflowSteps(arbitration.NextActions), "After resolving arbitration issues, update phase state and inspect workflow_status again before synthesis.")
	}
	if len(teamPlan.Members) == 0 {
		return []string{
			"Call list_agent_profiles, choose reuse_profile/create_profile/ephemeral members for this run, then record the Workflow Team with workflow_control action=record_workflow_team.",
		}
	}
	if len(agents) == 0 {
		return []string{
			"Spawn the recorded Workflow Team members with spawn_agent, setting agent_profile only for reuse_profile/create_profile members.",
			"After workers finish, bind outputs back with workflow_control action=record_await_results.",
		}
	}
	if err := validateWorkflowReadyToComplete(run, agents); err == nil {
		return []string{
			"Use workflow_control action=generate_final_report or write_final_report with complete_run=true.",
		}
	}
	return []string{
		"Continue runnable or active phases, await or record worker results, then inspect workflow_status again before synthesis.",
	}
}

func copyWorkflowSteps(steps []string) []string {
	out := make([]string, len(steps))
	copy(out, steps)
	return out
}

func reverseWorkflowRuns(runs []workflow.Run) []workflow.Run {
	out := make([]workflow.Run, len(runs))
	for i := range runs {
		out[i] = runs[len(runs)-1-i]
	}
	return out
}

func reverseWorkflowRunsWithDriverDefaults(runs []workflow.Run) []workflow.Run {
	out := reverseWorkflowRuns(runs)
	for i := range out {
		out[i] = workflowRunWithDriverDefaults(out[i])
	}
	return out
}

func workflowRunWithDriverDefaults(run workflow.Run) workflow.Run {
	if strings.TrimSpace(run.Driver) == "" {
		if strings.TrimSpace(run.ScriptPath) != "" {
			run.Driver = workflow.RunDriverScript
		} else {
			run.Driver = workflow.RunDriverAgentManaged
		}
	}
	if strings.TrimSpace(run.Entrypoint) == "" {
		run.Entrypoint = workflow.RunEntrypointNaturalLanguageAgent
	}
	return run
}

func tailWorkflowEvents(events []workflow.Event, limit int) []workflow.Event {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if len(events) <= limit {
		return events
	}
	return events[len(events)-limit:]
}
