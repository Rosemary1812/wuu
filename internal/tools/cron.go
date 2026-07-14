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

// CronTool is the single read/write surface for scheduled prompt tasks.
// action=list reads all scheduled tasks, action=add creates one prompt task,
// action=remove deletes one by id.
type CronTool struct{ env *Env }

func NewCronTool(env *Env) *CronTool        { return &CronTool{env: env} }
func (t *CronTool) Name() string            { return "cron" }
func (t *CronTool) IsReadOnly() bool        { return false }
func (t *CronTool) IsConcurrencySafe() bool { return false }

func (t *CronTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "cron",
		Description: "Manage scheduled tasks that run a prompt at cron intervals. " +
			"action=list returns all scheduled tasks with their IDs, schedules, and prompts. " +
			"action=add creates a task and requires cron plus prompt. A task can be recurring (runs repeatedly until removed or expired) or one-shot (runs once). " +
			"action=remove deletes a task and requires id.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action": map[string]any{
					"type":        "string",
					"enum":        []string{"list", "add", "remove"},
					"description": "Required. list reads all scheduled tasks; add creates one (requires cron and prompt); remove deletes one (requires id).",
				},
				"cron": map[string]any{
					"type":        "string",
					"description": "Used by action=add. 5-field cron expression in local time (min hour dom month dow). Example: */5 * * * *",
				},
				"prompt": map[string]any{
					"type":        "string",
					"description": "Used by action=add. The prompt to execute each time the task fires.",
				},
				"recurring": map[string]any{
					"type":        "boolean",
					"description": "Used by action=add. If true, the task repeats until removed or it expires (7 days). If false, it runs once.",
				},
				"durable": map[string]any{
					"type":        "boolean",
					"description": "Used by action=add. If true, persist to disk and survive restarts. If false (default), session-only.",
				},
				"id": map[string]any{
					"type":        "string",
					"description": "Used by action=remove. The task ID to remove.",
				},
			},
			"required": []string{"action"},
		},
	}
}

func (t *CronTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		Action    string `json:"action"`
		Cron      string `json:"cron"`
		Prompt    string `json:"prompt"`
		Recurring bool   `json:"recurring"`
		Durable   bool   `json:"durable"`
		ID        string `json:"id"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	switch strings.TrimSpace(args.Action) {
	case "list":
		return t.executeList()
	case "add":
		return t.executeAdd(args.Cron, args.Prompt, args.Recurring, args.Durable)
	case "remove":
		return t.executeRemove(args.ID)
	default:
		return "", fmt.Errorf("cron requires action=list, add, or remove")
	}
}

func (t *CronTool) executeAdd(cronExpr, prompt string, recurring, durable bool) (string, error) {
	var args struct {
		Cron      string
		Prompt    string
		Recurring bool
		Durable   bool
	}
	args.Cron = strings.TrimSpace(cronExpr)
	args.Prompt = strings.TrimSpace(prompt)
	args.Recurring = recurring
	args.Durable = durable
	if args.Cron == "" {
		return "", fmt.Errorf("cron action=add requires cron")
	}
	if args.Prompt == "" {
		return "", fmt.Errorf("cron action=add requires prompt")
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
		ID:        cron.GenerateTaskID(),
		Cron:      args.Cron,
		Prompt:    args.Prompt,
		Metadata:  map[string]string{"kind": "prompt"},
		CreatedAt: time.Now().UnixMilli(),
		Recurring: args.Recurring,
	}

	storeLabel := "session-only"
	var storeErr error
	if args.Durable {
		storeLabel = "durable"
		storeErr = fileStore.Add(task)
	} else {
		storeErr = sessionStore.Add(task)
	}
	if storeErr != nil {
		return "", fmt.Errorf("failed to save task: %w", storeErr)
	}

	result := map[string]any{
		"action":     "cron",
		"id":         task.ID,
		"schedule":   args.Cron,
		"prompt":     args.Prompt,
		"kind":       taskKind(task),
		"type":       map[bool]string{true: "recurring", false: "one-shot"}[args.Recurring],
		"durability": storeLabel,
	}
	return mustJSON(result)
}

func taskKind(task cron.Task) string {
	return "prompt"
}

func (t *CronTool) executeRemove(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("cron action=remove requires id")
	}

	stateDir, err := t.env.WorkspaceStateDir()
	if err != nil {
		return "", err
	}
	fileStore := cron.NewTaskStore(taskStorePath(stateDir))
	sessionStore := cron.NewSessionTaskStore(stateDir)
	if err := fileStore.Remove(id); err != nil {
		return "", fmt.Errorf("failed to remove task: %w", err)
	}
	if err := sessionStore.Remove(id); err != nil {
		return "", fmt.Errorf("failed to remove task: %w", err)
	}

	result := map[string]any{"action": "cron", "removed": id}
	return mustJSON(result)
}

func (t *CronTool) executeList() (string, error) {
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
			"id":       task.ID,
			"schedule": task.Cron,
			"type":     typeLabel,
			"kind":     taskKind(task),
			"prompt":   task.Prompt,
		})
	}
	for _, task := range fileTasks {
		appendTask(task, false)
	}
	for _, task := range sessionTasks {
		appendTask(task, true)
	}

	return mustJSON(map[string]any{"action": "cron", "tasks": items, "count": len(items)})
}
