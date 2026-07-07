package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/blueberrycongee/wuu/internal/capability"
	"github.com/blueberrycongee/wuu/internal/providers"
)

// ManageTaskTool is the agent task rail (2026-07-06 agent-task-rail design):
// coordination state lives on task cards — owner, status, thread — so chat
// never has to carry it. The schema is static for every participant-speech
// agent (prompt-cache stable); availability is gated at execute time on the
// injected TaskManager backend, mirroring manage_participant's group actions.
type ManageTaskTool struct {
	env *Env
}

func NewManageTaskTool(env *Env) *ManageTaskTool { return &ManageTaskTool{env: env} }

func (t *ManageTaskTool) Name() string { return "manage_task" }

func (t *ManageTaskTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name:        "manage_task",
		Description: "Run the group task board. A task is trackable work with one owner at a time; claiming it is how work is divided — never coordinate by chat message. action=create opens a task in a group thread (anchor_seq anchors it on a main-stream message; omit for a standalone task, e.g. when splitting work; claim=true takes ownership in the same call). action=escalate converts an open discussion reply (subthread_id) you belong to into a board task once the discussion has converged — e.g. the user says it is ready; claim=true self-owns it. action=claim takes ownership of an unclaimed task — if someone else already owns it, you lost the race: move on silently, never duplicate their work. action=unclaim releases ownership. action=update_status files an owned task for review with a summary (the one-line conclusion; result + how it was verified). action=unfollow stops receiving this task thread's traffic once your part is done. action=list shows the group's board. Ownership is work responsibility only — it grants no authority over other agents.",
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"action": map[string]any{
					"type":        "string",
					"enum":        []string{"create", "escalate", "claim", "unclaim", "update_status", "unfollow", "list"},
					"description": "create opens a task (group members only); escalate converts a converged open discussion reply into a task; claim/unclaim take/release work ownership; update_status files an owned task for review; unfollow leaves the task's push subset; list shows the board.",
				},
				"thread_id": map[string]any{
					"type":        "string",
					"description": "Group thread id. Required for create and list.",
				},
				"subthread_id": map[string]any{
					"type":        "string",
					"description": "Task id (cth-…). Required for claim, unclaim, update_status, and unfollow.",
				},
				"title": map[string]any{
					"type":        "string",
					"description": "Task title. Required for create; write it for humans scanning the board.",
				},
				"anchor_seq": map[string]any{
					"type":        "integer",
					"description": "Main-stream message seq to anchor the task on (the seq shown on the incoming_message). One message hosts at most one task/reply. Omit for a standalone task.",
				},
				"claim": map[string]any{
					"type":        "boolean",
					"description": "With create: take ownership of the new task in the same call. Use when you will do the work yourself; leave unset when dispatching for teammates to claim.",
				},
				"summary": map[string]any{
					"type":        "string",
					"description": "With update_status: the one-line conclusion draft — the result and how it was verified. Required.",
				},
			},
			"required": []string{"action"},
		},
	}
}

func (t *ManageTaskTool) Execute(ctx context.Context, args string) (string, error) {
	if t == nil || t.env == nil {
		return "", errors.New("manage_task: not configured")
	}
	manager := t.env.TaskManager
	if manager == nil {
		return "", errors.New("manage_task: task rail not configured in this environment")
	}
	if strings.TrimSpace(t.env.ParticipantID) == "" {
		return "", errors.New("manage_task: participant identity is required")
	}
	var params struct {
		Action      string `json:"action"`
		ThreadID    string `json:"thread_id"`
		SubthreadID string `json:"subthread_id"`
		Title       string `json:"title"`
		AnchorSeq   int    `json:"anchor_seq"`
		Claim       bool   `json:"claim"`
		Summary     string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(args), &params); err != nil {
		return "", fmt.Errorf("manage_task: parse args: %w", err)
	}

	switch strings.ToLower(strings.TrimSpace(params.Action)) {
	case "create":
		if strings.TrimSpace(params.ThreadID) == "" {
			return "", errors.New("create: thread_id is required")
		}
		if strings.TrimSpace(params.Title) == "" {
			return "", errors.New("create: title is required")
		}
		view, err := manager.CreateTask(ctx, params.ThreadID, params.AnchorSeq, params.Title, params.Claim)
		if err != nil {
			return "", err
		}
		return marshalTaskResult("create", map[string]any{"task": view})
	case "escalate":
		if strings.TrimSpace(params.SubthreadID) == "" {
			return "", errors.New("escalate: subthread_id is required")
		}
		view, err := manager.EscalateTask(ctx, params.SubthreadID, params.Title, params.Claim)
		if err != nil {
			return "", err
		}
		return marshalTaskResult("escalate", map[string]any{"task": view})
	case "claim":
		if strings.TrimSpace(params.SubthreadID) == "" {
			return "", errors.New("claim: subthread_id is required")
		}
		view, claimed, err := manager.ClaimTask(ctx, params.SubthreadID)
		if err != nil {
			return "", err
		}
		if !claimed {
			return marshalTaskResult("claim", map[string]any{
				"claimed": false,
				"task":    view,
				"note":    "already owned by " + firstNonEmpty(view.OwnerName, view.Owner) + " — move on, do not duplicate their work and do not reply about it",
			})
		}
		return marshalTaskResult("claim", map[string]any{"claimed": true, "task": view})
	case "unclaim":
		if strings.TrimSpace(params.SubthreadID) == "" {
			return "", errors.New("unclaim: subthread_id is required")
		}
		view, err := manager.UnclaimTask(ctx, params.SubthreadID)
		if err != nil {
			return "", err
		}
		return marshalTaskResult("unclaim", map[string]any{"task": view})
	case "update_status":
		if strings.TrimSpace(params.SubthreadID) == "" {
			return "", errors.New("update_status: subthread_id is required")
		}
		if strings.TrimSpace(params.Summary) == "" {
			return "", errors.New("update_status: summary is required (the one-line conclusion draft)")
		}
		view, err := manager.FileTaskReview(ctx, params.SubthreadID, params.Summary)
		if err != nil {
			return "", err
		}
		return marshalTaskResult("update_status", map[string]any{"task": view})
	case "unfollow":
		if strings.TrimSpace(params.SubthreadID) == "" {
			return "", errors.New("unfollow: subthread_id is required")
		}
		if err := manager.UnfollowTask(ctx, params.SubthreadID); err != nil {
			return "", err
		}
		return marshalTaskResult("unfollow", map[string]any{"subthread_id": strings.TrimSpace(params.SubthreadID)})
	case "list":
		if strings.TrimSpace(params.ThreadID) == "" {
			return "", errors.New("list: thread_id is required")
		}
		views, err := manager.ListTasks(ctx, params.ThreadID)
		if err != nil {
			return "", err
		}
		return marshalTaskResult("list", map[string]any{"tasks": views})
	default:
		return "", fmt.Errorf("manage_task: unknown action %q", params.Action)
	}
}

func marshalTaskResult(action string, fields map[string]any) (string, error) {
	payload := map[string]any{"action": action}
	for k, v := range fields {
		payload[k] = v
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("manage_task: marshal result: %w", err)
	}
	return string(data), nil
}

func (t *ManageTaskTool) IsReadOnly() bool { return false }

func (t *ManageTaskTool) IsConcurrencySafe() bool { return false }

func (t *ManageTaskTool) Classify(string) ToolClassification {
	return ToolClassification{
		ReadOnly:        false,
		ConcurrencySafe: false,
		Risk:            ToolRiskLow,
		Reason:          "manages task-board coordination state (owner, status) inside the session store",
	}
}

func (t *ManageTaskTool) DeclaredCapability() (capability.Capability, bool) {
	return capability.CapabilityTaskManage, true
}
