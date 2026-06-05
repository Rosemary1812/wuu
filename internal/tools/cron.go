package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/cron"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/statepath"
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
			"Use workflow_name for scheduled, repeatable, multi-agent work so the scheduler can start a Workflow Run instead of an ordinary chat loop. " +
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
		if args.Prompt == "" {
			args.Prompt = scheduledWorkflowPrompt(wf.Name, args.WorkflowArguments)
		}
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

	task := cron.Task{
		ID:                cron.GenerateTaskID(),
		Cron:              args.Cron,
		Prompt:            args.Prompt,
		WorkflowName:      args.WorkflowName,
		WorkflowArguments: args.WorkflowArguments,
		CreatedAt:         time.Now().UnixMilli(),
		Recurring:         args.Recurring,
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
		"id":                 task.ID,
		"schedule":           args.Cron,
		"prompt":             args.Prompt,
		"kind":               map[bool]string{true: "workflow", false: "prompt"}[task.IsWorkflow()],
		"workflow_name":      task.WorkflowName,
		"workflow_arguments": task.WorkflowArguments,
		"type":               map[bool]string{true: "recurring", false: "one-shot"}[args.Recurring],
		"durability":         storeLabel,
	}
	return mustJSON(result)
}

func scheduledWorkflowPrompt(name, arguments string) string {
	if strings.TrimSpace(arguments) == "" {
		return fmt.Sprintf("Run workflow %s.", name)
	}
	return fmt.Sprintf("Run workflow %s with arguments: %s", name, strings.TrimSpace(arguments))
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

	result := map[string]any{"cancelled": args.ID}
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
			"kind":               map[bool]string{true: "workflow", false: "prompt"}[task.IsWorkflow()],
			"prompt":             task.Prompt,
			"workflow_name":      task.WorkflowName,
			"workflow_arguments": task.WorkflowArguments,
		})
	}
	for _, task := range fileTasks {
		appendTask(task, false)
	}
	for _, task := range sessionTasks {
		appendTask(task, true)
	}

	return mustJSON(map[string]any{"tasks": items, "count": len(items)})
}
