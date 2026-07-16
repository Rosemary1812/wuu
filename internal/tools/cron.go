package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/automation"
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
			"Use new_thread when each run should be a separate visible conversation. Use thread_heartbeat when continuity in the current conversation matters. " +
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
				"title": map[string]any{
					"type":        "string",
					"description": "Used by action=add. Optional name shown for the automation.",
				},
				"timezone": map[string]any{
					"type":        "string",
					"description": "Used by action=add. IANA timezone for the schedule. Defaults to local time.",
				},
				"mode": map[string]any{
					"type":        "string",
					"enum":        []string{"new_thread", "thread_heartbeat"},
					"description": "Used by action=add. Defaults to new_thread. Choose new_thread for an independent visible thread per run; choose thread_heartbeat when each run must continue with the current thread's latest context.",
				},
				"heartbeat_thread_id": map[string]any{
					"type":        "string",
					"description": "Used with thread_heartbeat. Defaults to the current thread.",
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
		Action            string `json:"action"`
		Cron              string `json:"cron"`
		Prompt            string `json:"prompt"`
		Recurring         bool   `json:"recurring"`
		Durable           bool   `json:"durable"`
		ID                string `json:"id"`
		Mode              string `json:"mode"`
		HeartbeatThreadID string `json:"heartbeat_thread_id"`
		Title             string `json:"title"`
		Timezone          string `json:"timezone"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	switch strings.TrimSpace(args.Action) {
	case "list":
		return t.executeList()
	case "add":
		return t.executeAdd(args.Title, args.Cron, args.Timezone, args.Prompt, args.Mode, args.HeartbeatThreadID, args.Recurring, args.Durable)
	case "remove":
		return t.executeRemove(args.ID)
	default:
		return "", fmt.Errorf("cron requires action=list, add, or remove")
	}
}

func (t *CronTool) executeAdd(title, cronExpr, timezone, prompt, mode, heartbeatThreadID string, recurring, durable bool) (string, error) {
	manager, err := t.automationManager()
	if err != nil {
		return "", err
	}
	task, err := manager.AddTask(automation.AddTaskParams{
		Title:             title,
		Prompt:            prompt,
		Schedule:          cronExpr,
		Timezone:          timezone,
		Mode:              automation.Mode(strings.TrimSpace(mode)),
		CreatorThreadID:   strings.TrimSpace(t.env.SessionID),
		HeartbeatThreadID: heartbeatThreadID,
		Recurring:         recurring,
		Durable:           durable,
	})
	if err != nil {
		return "", fmt.Errorf("failed to save task: %w", err)
	}
	result := map[string]any{
		"action":     "cron",
		"id":         task.ID,
		"title":      task.Title,
		"schedule":   task.Cron,
		"timezone":   task.Timezone,
		"prompt":     task.Prompt,
		"mode":       task.Mode,
		"kind":       taskKind(task),
		"type":       map[bool]string{true: "recurring", false: "one-shot"}[task.Recurring],
		"durability": task.Metadata["durability"],
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

	manager, err := t.automationManager()
	if err != nil {
		return "", err
	}
	if err := manager.RemoveTask(id); err != nil {
		return "", fmt.Errorf("failed to remove task: %w", err)
	}

	result := map[string]any{"action": "cron", "removed": id}
	return mustJSON(result)
}

func (t *CronTool) executeList() (string, error) {
	manager, err := t.automationManager()
	if err != nil {
		return "", err
	}
	tasks, err := manager.ListTasks()
	if err != nil {
		return "", fmt.Errorf("failed to list tasks: %w", err)
	}

	now := time.Now().UnixMilli()
	var items []map[string]any
	for _, task := range tasks {
		typeLabel := "one-shot"
		if task.Recurring {
			typeLabel = "recurring"
		}
		if cron.IsExpired(task, now) {
			typeLabel += " [expired]"
		}
		if task.Metadata["durability"] != "durable" {
			typeLabel += " [session-only]"
		}
		items = append(items, map[string]any{
			"id":                  task.ID,
			"title":               task.Title,
			"schedule":            task.Cron,
			"timezone":            task.Timezone,
			"type":                typeLabel,
			"kind":                taskKind(task),
			"prompt":              task.Prompt,
			"mode":                task.Mode,
			"creator_thread_id":   task.CreatorThreadID,
			"heartbeat_thread_id": task.HeartbeatThreadID,
			"paused":              task.Paused,
		})
	}

	return mustJSON(map[string]any{"action": "cron", "tasks": items, "count": len(items)})
}

func (t *CronTool) automationManager() (*automation.Manager, error) {
	if t == nil || t.env == nil || t.env.AutomationManager == nil {
		return nil, fmt.Errorf("automation manager is unavailable")
	}
	return t.env.AutomationManager, nil
}
