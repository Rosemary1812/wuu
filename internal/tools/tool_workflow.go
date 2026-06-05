package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/workflow"
)

// ---------------------------------------------------------------------------
// list_workflows
// ---------------------------------------------------------------------------

type ListWorkflowsTool struct{ env *Env }

func NewListWorkflowsTool(env *Env) *ListWorkflowsTool { return &ListWorkflowsTool{env: env} }

func (t *ListWorkflowsTool) Name() string            { return "list_workflows" }
func (t *ListWorkflowsTool) IsReadOnly() bool        { return true }
func (t *ListWorkflowsTool) IsConcurrencySafe() bool { return true }

func (t *ListWorkflowsTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "list_workflows",
		Description: "List reusable workflow definitions discovered from project and user workflow directories. " +
			"Use this when the user asks for a repeatable, long-running, scheduled, or multi-agent task and you need " +
			"to choose a saved workflow before creating a durable workflow run.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}
}

func (t *ListWorkflowsTool) Execute(_ context.Context, _ string) (string, error) {
	items := make([]map[string]any, 0, len(t.env.Workflows))
	for _, wf := range t.env.Workflows {
		item := map[string]any{
			"name":                   wf.Name,
			"description":            wf.Description,
			"when_to_use":            wf.WhenToUse,
			"source":                 wf.Source,
			"path":                   wf.Path,
			"argument_hint":          wf.ArgumentHint,
			"user_invocable":         wf.UserInvocable,
			"disable_model_invoke":   wf.DisableModelInvoke,
			"version":                wf.Version,
			"max_agents":             wf.MaxAgents,
			"max_concurrency":        wf.MaxConcurrency,
			"profiles":               wf.Profiles,
			"allow_profile_creation": wf.AllowProfileCreation,
			"memory_policy":          wf.MemoryPolicy,
		}
		items = append(items, item)
	}
	return mustJSON(map[string]any{
		"workflows": items,
		"count":     len(items),
	})
}

// ---------------------------------------------------------------------------
// load_workflow
// ---------------------------------------------------------------------------

type LoadWorkflowTool struct{ env *Env }

func NewLoadWorkflowTool(env *Env) *LoadWorkflowTool { return &LoadWorkflowTool{env: env} }

func (t *LoadWorkflowTool) Name() string            { return "load_workflow" }
func (t *LoadWorkflowTool) IsReadOnly() bool        { return true }
func (t *LoadWorkflowTool) IsConcurrencySafe() bool { return true }

func (t *LoadWorkflowTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "load_workflow",
		Description: "Load the full body of a reusable workflow definition. Workflow definitions are portable " +
			"orchestration assets, similar to skills, but they are used to create durable workflow runs. The returned " +
			"body may contain ${ARGUMENTS}, ${CLAUDE_WORKFLOW_DIR}, and ${CLAUDE_SESSION_ID} substitutions.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "Workflow name. Leading slash is optional.",
				},
				"arguments": map[string]any{
					"type":        "string",
					"description": "Optional argument string substituted into ${ARGUMENTS}.",
				},
			},
			"required": []string{"name"},
		},
	}
}

func (t *LoadWorkflowTool) Execute(_ context.Context, argsJSON string) (string, error) {
	var args struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	if strings.TrimSpace(args.Name) == "" {
		return "", errors.New("load_workflow requires name")
	}
	wf, ok := t.env.FindWorkflow(args.Name)
	if !ok {
		return "", fmt.Errorf("workflow %q not found. available: %s", args.Name, strings.Join(t.env.WorkflowNames(), ", "))
	}
	body := t.env.ProcessWorkflowBody(wf, args.Arguments)
	return mustJSON(map[string]any{
		"name":                   wf.Name,
		"description":            wf.Description,
		"when_to_use":            wf.WhenToUse,
		"source":                 wf.Source,
		"path":                   wf.Path,
		"dir":                    wf.Dir,
		"argument_hint":          wf.ArgumentHint,
		"version":                wf.Version,
		"max_agents":             wf.MaxAgents,
		"max_concurrency":        wf.MaxConcurrency,
		"profiles":               wf.Profiles,
		"allow_profile_creation": wf.AllowProfileCreation,
		"memory_policy":          wf.MemoryPolicy,
		"suggested_phase_names":  extractWorkflowPhaseNames(body),
		"content":                body,
	})
}

// ---------------------------------------------------------------------------
// create_workflow
// ---------------------------------------------------------------------------

type CreateWorkflowTool struct{ env *Env }

func NewCreateWorkflowTool(env *Env) *CreateWorkflowTool { return &CreateWorkflowTool{env: env} }

func (t *CreateWorkflowTool) Name() string            { return "create_workflow" }
func (t *CreateWorkflowTool) IsReadOnly() bool        { return false }
func (t *CreateWorkflowTool) IsConcurrencySafe() bool { return true }

func (t *CreateWorkflowTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "create_workflow",
		Description: "Create a durable Workflow Run from a reusable workflow definition or an ad hoc plan. " +
			"This records the workflow state, phase plan, event log, and plan artifact. It does not automatically " +
			"spawn agents; after creating a running workflow, use spawn_agent with agent_profile when named durable " +
			"profiles should work on phases, require agent_report before completion, then inspect progress with " +
			"workflow_status. Use this instead of manually carrying long-running workflow state in chat context.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"definition_name": map[string]any{
					"type":        "string",
					"description": "Optional saved workflow definition name. Omit for an ad hoc workflow.",
				},
				"arguments": map[string]any{
					"type":        "string",
					"description": "Arguments for the saved workflow, also stored on the run.",
				},
				"plan": map[string]any{
					"type":        "string",
					"description": "Markdown plan for this run. If omitted with definition_name, the workflow definition body is used.",
				},
				"phases": map[string]any{
					"type":        "array",
					"description": "Simple ordered phase plan. If omitted, phases are extracted from the plan or workflow body.",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"id":   map[string]any{"type": "string", "description": "Optional stable phase id."},
							"name": map[string]any{"type": "string", "description": "Human-readable phase name."},
						},
						"required": []string{"name"},
					},
				},
				"initial_status": map[string]any{
					"type":        "string",
					"enum":        []string{"draft", "approval_pending", "running"},
					"description": "Initial run state. Defaults to running.",
				},
				"run_id": map[string]any{
					"type":        "string",
					"description": "Optional caller-provided run id. Omit for an auto-generated id.",
				},
			},
		},
	}
}

type workflowPhaseInput struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (t *CreateWorkflowTool) Execute(_ context.Context, argsJSON string) (string, error) {
	var args struct {
		DefinitionName string               `json:"definition_name"`
		Arguments      string               `json:"arguments"`
		Plan           string               `json:"plan"`
		Phases         []workflowPhaseInput `json:"phases"`
		InitialStatus  string               `json:"initial_status"`
		RunID          string               `json:"run_id"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	status, err := parseInitialWorkflowStatus(args.InitialStatus)
	if err != nil {
		return "", err
	}

	var def workflow.Definition
	var body string
	if strings.TrimSpace(args.DefinitionName) != "" {
		found, ok := t.env.FindWorkflow(args.DefinitionName)
		if !ok {
			return "", fmt.Errorf("workflow %q not found. available: %s", args.DefinitionName, strings.Join(t.env.WorkflowNames(), ", "))
		}
		def = found
		body = t.env.ProcessWorkflowBody(def, args.Arguments)
	}

	plan := strings.TrimSpace(args.Plan)
	if plan == "" {
		plan = strings.TrimSpace(body)
	}
	phases := buildWorkflowPhases(args.Phases, plan, status)
	if len(phases) == 0 {
		return "", errors.New("create_workflow requires at least one phase or a plan with extractable phases")
	}
	if len(phases) > 100 {
		return "", errors.New("create_workflow supports at most 100 phases")
	}

	runID := strings.TrimSpace(args.RunID)
	if runID == "" {
		runID = "workflow-" + session.NewID()
	}
	store, err := t.env.WorkflowStore()
	if err != nil {
		return "", fmt.Errorf("resolve workflow store: %w", err)
	}
	run, err := store.CreateRun(workflow.Run{
		ID:             runID,
		DefinitionName: def.Name,
		DefinitionPath: def.Path,
		Arguments:      strings.TrimSpace(args.Arguments),
		Status:         status,
		Phases:         phases,
	})
	if err != nil {
		return "", err
	}
	planPath := ""
	if plan != "" {
		planPath, err = store.WritePlan(run.ID, renderWorkflowPlan(def, args.Arguments, plan, phases))
		if err != nil {
			return "", err
		}
		run.PlanPath = planPath
	}

	return mustJSON(map[string]any{
		"run_id":          run.ID,
		"status":          run.Status,
		"definition_name": run.DefinitionName,
		"definition_path": run.DefinitionPath,
		"plan_path":       planPath,
		"phases":          run.Phases,
		"next_steps":      workflowNextSteps(status),
		"workflow_status": map[string]string{"run_id": run.ID},
	})
}

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
			"Use this after create_workflow, after awaiting workflow agents, before resuming a paused run, and before final synthesis.",
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
			"runs":  reverseWorkflowRuns(runs),
			"count": len(runs),
		})
	}

	run, err := store.LoadRun(runID)
	if err != nil {
		return "", err
	}
	agents, err := store.ListAgentRuns(runID)
	if err != nil {
		return "", err
	}
	result := map[string]any{
		"run":        run,
		"agent_runs": agents,
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
	default:
		return []string{
			"Spawn workflow agents with spawn_agent, setting agent_profile for durable named profiles.",
			"Require each workflow agent to call agent_report before treating its work as complete.",
			"Use workflow_status to inspect durable progress before final synthesis.",
		}
	}
}

func reverseWorkflowRuns(runs []workflow.Run) []workflow.Run {
	out := make([]workflow.Run, len(runs))
	for i := range runs {
		out[i] = runs[len(runs)-1-i]
	}
	return out
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
