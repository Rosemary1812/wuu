package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/cron"
	prompttext "github.com/blueberrycongee/wuu/internal/prompt"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/statepath"
	"github.com/blueberrycongee/wuu/internal/workflow"
)

func taskStorePath(stateDir string) string {
	return statepath.ScheduledTasksPath(stateDir)
}

type ScheduleCronTool struct{ env *Env }

func NewScheduleCronTool(env *Env) *ScheduleCronTool { return &ScheduleCronTool{env: env} }
func (t *ScheduleCronTool) Name() string             { return "schedule_cron" }
func (t *ScheduleCronTool) IsReadOnly() bool         { return false }
func (t *ScheduleCronTool) IsConcurrencySafe() bool  { return false }

func (t *ScheduleCronTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "schedule_cron",
		Description: "Create a scheduled task that runs a prompt or saved workflow at cron intervals. " +
			"Pass `workflow_name` to schedule a saved workflow (use this for repeatable, multi-agent work); the task's prompt will trigger `start_workflow`. " +
			"Otherwise pass `prompt` for an ad-hoc task. " +
			"The task can be recurring (runs repeatedly until deleted or expired) or one-shot (runs once).",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"cron": map[string]any{
					"type":        "string",
					"description": "5-field cron expression in local time (min hour dom month dow). Example: */5 * * * *",
				},
				"prompt": map[string]any{
					"type":        "string",
					"description": "The prompt to execute each time the task fires. Required unless workflow_name is set.",
				},
				"workflow_name": map[string]any{
					"type":        "string",
					"description": "Optional saved workflow definition name to run when the task fires.",
				},
				"workflow_arguments": map[string]any{
					"type":        "string",
					"description": "Arguments passed to the saved workflow when it fires.",
				},
				"recurring": map[string]any{
					"type":        "boolean",
					"description": "If true, the task repeats until deleted or it expires (7 days). If false, it runs once.",
				},
				"durable": map[string]any{
					"type":        "boolean",
					"description": "If true, persist to disk and survive restarts. If false (default), session-only.",
				},
			},
			"required": []string{"cron"},
		},
	}
}

func (t *ScheduleCronTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		Cron              string `json:"cron"`
		Prompt            string `json:"prompt"`
		WorkflowName      string `json:"workflow_name"`
		WorkflowArguments string `json:"workflow_arguments"`
		Recurring         bool   `json:"recurring"`
		Durable           bool   `json:"durable"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	args.Cron = strings.TrimSpace(args.Cron)
	args.Prompt = strings.TrimSpace(args.Prompt)
	args.WorkflowName = strings.TrimSpace(args.WorkflowName)
	args.WorkflowArguments = strings.TrimSpace(args.WorkflowArguments)
	workflowKind := ""
	if args.Cron == "" {
		return "", fmt.Errorf("schedule_cron requires cron")
	}
	if args.Prompt == "" && args.WorkflowName == "" {
		return "", fmt.Errorf("schedule_cron requires prompt or workflow_name")
	}
	if args.WorkflowName != "" {
		wf, ok := t.env.FindWorkflow(args.WorkflowName)
		if !ok {
			return "", fmt.Errorf("workflow %q not found. available: %s", args.WorkflowName, strings.Join(t.env.WorkflowNames(), ", "))
		}
		args.WorkflowName = wf.Name
		workflowKind = workflowDefinitionKind(wf)
		args.Prompt = scheduledWorkflowPrompt(wf.Name, workflowKind, args.WorkflowArguments, args.Prompt)
	}

	ce, err := cron.ParseCronExpression(args.Cron)
	if err != nil {
		return "", fmt.Errorf("invalid cron expression: %w", err)
	}

	next, err := ce.NextRun(time.Now())
	if err != nil {
		return "", fmt.Errorf("cron has no valid future run: %w", err)
	}
	if next.After(time.Now().AddDate(1, 0, 0)) {
		return "", fmt.Errorf("cron next run is more than 1 year away")
	}

	stateDir, err := t.env.WorkspaceStateDir()
	if err != nil {
		return "", err
	}
	fileStore := cron.NewTaskStore(taskStorePath(stateDir))
	sessionStore := cron.NewSessionTaskStore(stateDir)
	fileTasks, _ := fileStore.List()
	sessionTasks, _ := sessionStore.List()
	if len(fileTasks)+len(sessionTasks) >= cron.MaxJobs {
		return "", fmt.Errorf("maximum number of scheduled tasks reached (%d)", cron.MaxJobs)
	}

	metadata := map[string]string{"kind": "prompt"}
	if args.WorkflowName != "" {
		metadata = map[string]string{
			"kind":               "workflow",
			"workflow_name":      args.WorkflowName,
			"workflow_arguments": args.WorkflowArguments,
			"workflow_kind":      workflowKind,
		}
	}
	task := cron.Task{
		ID:        cron.GenerateTaskID(),
		Cron:      args.Cron,
		Prompt:    args.Prompt,
		Metadata:  metadata,
		CreatedAt: time.Now().UnixMilli(),
		Recurring: args.Recurring,
	}

	storeLabel := "session-only"
	storeErr := sessionStore.Add(task)
	if args.Durable {
		storeLabel = "durable"
		storeErr = fileStore.Add(task)
	}
	if storeErr != nil {
		return "", fmt.Errorf("failed to save task: %w", storeErr)
	}

	result := map[string]any{
		"action":             "schedule_cron",
		"id":                 task.ID,
		"schedule":           args.Cron,
		"prompt":             args.Prompt,
		"kind":               taskKind(task),
		"workflow_name":      task.Metadata["workflow_name"],
		"workflow_kind":      task.Metadata["workflow_kind"],
		"workflow_arguments": task.Metadata["workflow_arguments"],
		"type":               map[bool]string{true: "recurring", false: "one-shot"}[args.Recurring],
		"durability":         storeLabel,
	}
	return mustJSON(result)
}

func scheduledWorkflowPrompt(name, kind, arguments, extraPrompt string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Scheduled task fired. Start the saved workflow %q.", name)
	if args := strings.TrimSpace(arguments); args != "" {
		fmt.Fprintf(&sb, "\n\nWorkflow arguments:\n%s", args)
	}
	switch kind {
	case workflow.DefinitionKindScript:
		sb.WriteString("\n\nUse load_workflow to inspect the saved script definition, then call start_workflow with definition_name, workflow arguments, and driver=auto. Do not call run_workflow directly unless a later recovery step explicitly requires the script path.")
		sb.WriteString(" Let start_workflow choose the run ID unless the user explicitly provided one. The script driver writes the same Workflow Run, Workflow Team, Agent Run, and final-report state while it owns phases, worker spawns, awaits, and synthesis.")
	default:
		sb.WriteString("\n\nUse load_workflow to inspect the saved workflow definition, then call start_workflow with definition_name, workflow arguments, and driver=auto. Let start_workflow choose the run ID unless the user explicitly provided one.")
		sb.WriteString(" After creating the run, call list_agent_profiles, decide which roles reuse an existing Agent Profile, create a new recurring profile, or stay ephemeral, then record the Workflow Team with workflow_control action=record_workflow_team before spawning agents.")
		sb.WriteString(" Write each team member prompt from the shared Base Agent Brief Contract; include workflow run, phase, team member, and result-binding context because this is workflow work.")
	}
	sb.WriteString("\n\n")
	sb.WriteString(prompttext.AgentBriefContractSummary())
	sb.WriteString(" ")
	sb.WriteString(prompttext.WorkflowBriefExtensionSummary())
	if extra := strings.TrimSpace(extraPrompt); extra != "" {
		fmt.Fprintf(&sb, "\n\nAdditional instruction:\n%s", extra)
	}
	return sb.String()
}

func taskKind(task cron.Task) string {
	if task.Metadata["kind"] == "workflow" {
		return "workflow"
	}
	return "prompt"
}

type CancelCronTool struct{ env *Env }

func NewCancelCronTool(env *Env) *CancelCronTool  { return &CancelCronTool{env: env} }
func (t *CancelCronTool) Name() string            { return "cancel_cron" }
func (t *CancelCronTool) IsReadOnly() bool        { return false }
func (t *CancelCronTool) IsConcurrencySafe() bool { return false }

func (t *CancelCronTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name:        "cancel_cron",
		Description: "Cancel (delete) a scheduled task by its ID.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{
					"type":        "string",
					"description": "The task ID to cancel.",
				},
			},
			"required": []string{"id"},
		},
	}
}

func (t *CancelCronTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		ID string `json:"id"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	if args.ID == "" {
		return "", fmt.Errorf("cancel_cron requires id")
	}

	stateDir, err := t.env.WorkspaceStateDir()
	if err != nil {
		return "", err
	}
	fileStore := cron.NewTaskStore(taskStorePath(stateDir))
	sessionStore := cron.NewSessionTaskStore(stateDir)
	if err := fileStore.Remove(args.ID); err != nil {
		return "", fmt.Errorf("failed to cancel task: %w", err)
	}
	if err := sessionStore.Remove(args.ID); err != nil {
		return "", fmt.Errorf("failed to cancel task: %w", err)
	}

	result := map[string]any{"action": "cancel_cron", "cancelled": args.ID}
	return mustJSON(result)
}

type ListCronTool struct{ env *Env }

func NewListCronTool(env *Env) *ListCronTool    { return &ListCronTool{env: env} }
func (t *ListCronTool) Name() string            { return "list_cron" }
func (t *ListCronTool) IsReadOnly() bool        { return true }
func (t *ListCronTool) IsConcurrencySafe() bool { return true }

func (t *ListCronTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name:        "list_cron",
		Description: "List all scheduled tasks with their IDs, schedules, and prompts.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}
}

func (t *ListCronTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	stateDir, err := t.env.WorkspaceStateDir()
	if err != nil {
		return "", err
	}
	fileStore := cron.NewTaskStore(taskStorePath(stateDir))
	sessionStore := cron.NewSessionTaskStore(stateDir)
	fileTasks, err := fileStore.List()
	if err != nil {
		return "", fmt.Errorf("failed to list tasks: %w", err)
	}
	sessionTasks, err := sessionStore.List()
	if err != nil {
		return "", fmt.Errorf("failed to list tasks: %w", err)
	}

	now := time.Now().UnixMilli()
	var items []map[string]any
	appendTask := func(task cron.Task, sessionOnly bool) {
		typeLabel := "one-shot"
		if task.Recurring {
			typeLabel = "recurring"
		}
		if cron.IsExpired(task, now) {
			typeLabel += " [expired]"
		}
		if sessionOnly {
			typeLabel += " [session-only]"
		}
		items = append(items, map[string]any{
			"id":                 task.ID,
			"schedule":           task.Cron,
			"type":               typeLabel,
			"kind":               taskKind(task),
			"prompt":             task.Prompt,
			"workflow_name":      task.Metadata["workflow_name"],
			"workflow_arguments": task.Metadata["workflow_arguments"],
		})
	}
	for _, task := range fileTasks {
		appendTask(task, false)
	}
	for _, task := range sessionTasks {
		appendTask(task, true)
	}

	return mustJSON(map[string]any{"action": "list_cron", "tasks": items, "count": len(items)})
}
