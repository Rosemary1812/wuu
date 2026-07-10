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

// ManageTaskTool is the group Thread -> Task workflow surface for named agents.
// A discussion starts from a real group message, its immutable Thread owner may
// promote it, and that same named agent becomes Task lead. DMs, standalone
// tasks, and work-claim races are intentionally not representable.
type ManageTaskTool struct {
	env *Env
}

func NewManageTaskTool(env *Env) *ManageTaskTool { return &ManageTaskTool{env: env} }

func (t *ManageTaskTool) Name() string { return "manage_task" }

func (t *ManageTaskTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name:        "manage_task",
		Description: "Manage the Group -> Thread -> Task workflow. action=open_thread starts a focused discussion on a real group-chat message (thread_id + anchor_seq); it never creates a Task directly. A named parent author owns that Thread; for a human parent, the calling named agent becomes owner. action=promote is owner-only and converts that same Thread identity into a Task; the owner becomes immutable Task lead. action=conclude is lead-only and publishes the verified conclusion. DMs, standalone Tasks, claim/unclaim, and lead reassignment do not exist. action=list shows the group's open Threads and active Tasks. Leads use set_plan to orchestrate other named agents; assignees use piece_done for an early or structured handoff. need_human and need_upstream are explicit exception paths; unfollow leaves a Task after your assigned work is over.",
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"action": map[string]any{
					"type":        "string",
					"enum":        []string{"open_thread", "promote", "conclude", "need_human", "need_upstream", "unfollow", "list", "set_plan", "piece_done"},
					"description": "open_thread starts convergence on an anchored group message; promote is Thread-owner-only and makes that owner Task lead; conclude is Task-lead-only and completes the Task with a verified summary; list shows open Threads and active Tasks. set_plan, piece_done, need_human, need_upstream, and unfollow operate only inside the promoted Task.",
				},
				"thread_id": map[string]any{
					"type":        "string",
					"description": "Parent group-chat id. Required for open_thread and list. DM ids are rejected.",
				},
				"subthread_id": map[string]any{
					"type":        "string",
					"description": "Thread/Task id (cth-…). Required for promote and Task actions.",
				},
				"title": map[string]any{
					"type":        "string",
					"description": "Optional focused title for open_thread or promote.",
				},
				"anchor_seq": map[string]any{
					"type":        "integer",
					"description": "Main-stream human or named-agent message seq shown on incoming_message. Required for open_thread. One message hosts exactly one durable Thread; repeated opens return it without changing owner.",
				},
				"summary": map[string]any{
					"type":        "string",
					"description": "With conclude: the result and how it was verified. Filing it completes the Task. Required.",
				},
				"reason": map[string]any{
					"type":        "string",
					"description": "Required for need_human (why this decision belongs to the human) and need_upstream (what is missing from the upstream handoff).",
				},
				"plan": map[string]any{
					"type":        "array",
					"description": "With set_plan: the team work breakdown. Each item is a piece {id, title, assignee, prompt, depends_on}. id is a short label unique within the plan (e.g. \"p1\"); assignee is a teammate's participant id (prefer existing members); prompt is the briefing the assignee is woken with — write it so they can start without asking; depends_on lists the piece ids that must finish before this one starts — each must be a piece listed EARLIER in the plan (omit or [] for a piece that can start immediately), which keeps dependency cycles impossible by construction. Declare the whole plan at once; the engine dispatches every piece with no unmet dependency and @-wakes the rest as their dependencies complete.",
					"items": map[string]any{
						"type":                 "object",
						"additionalProperties": false,
						"properties": map[string]any{
							"id":       map[string]any{"type": "string"},
							"title":    map[string]any{"type": "string"},
							"assignee": map[string]any{"type": "string"},
							"prompt": map[string]any{
								"type":        "string",
								"description": "The briefing the assignee is woken with.",
							},
							"depends_on": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						},
						"required": []string{"id", "title", "assignee"},
					},
				},
				"piece_id": map[string]any{
					"type":        "string",
					"description": "The id of your plan piece (as declared in set_plan). Required for piece_done (the piece you finished) and need_upstream (the piece whose upstream handoff was insufficient).",
				},
				"handoff": map[string]any{
					"type":        "object",
					"description": "With piece_done: the structured result you hand to the downstream node(s) — the next node's real input, carried into its wake. Put that input here, never in a public post_message update (that update is progress for the human and wakes no teammate). Omit entirely if you produced nothing for a next node.",
					"properties": map[string]any{
						"done":       map[string]any{"type": "string", "description": "What you completed."},
						"findings":   map[string]any{"type": "string", "description": "What you learned that the next node needs."},
						"artifacts":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Paths / ids of concrete outputs (files, patches, docs)."},
						"limits":     map[string]any{"type": "string", "description": "What you did NOT do, and known gaps or caveats."},
						"next_goal":  map[string]any{"type": "string", "description": "The goal you are handing to the next node."},
						"acceptance": map[string]any{"type": "string", "description": "How the next node should judge its own result done."},
						"notes":      map[string]any{"type": "string", "description": "Anything else the next node should know."},
					},
					"additionalProperties": false,
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
		return "", errors.New("manage_task: group workflow not configured in this environment")
	}
	if strings.TrimSpace(t.env.ParticipantID) == "" {
		return "", errors.New("manage_task: participant identity is required")
	}
	var params struct {
		Action      string       `json:"action"`
		ThreadID    string       `json:"thread_id"`
		SubthreadID string       `json:"subthread_id"`
		Title       string       `json:"title"`
		AnchorSeq   int          `json:"anchor_seq"`
		Summary     string       `json:"summary"`
		Reason      string       `json:"reason"`
		Plan        []TaskPiece  `json:"plan"`
		PieceID     string       `json:"piece_id"`
		Handoff     *TaskHandoff `json:"handoff"`
	}
	if err := json.Unmarshal([]byte(args), &params); err != nil {
		return "", fmt.Errorf("manage_task: parse args: %w", err)
	}

	switch strings.ToLower(strings.TrimSpace(params.Action)) {
	case "open_thread":
		if strings.TrimSpace(params.ThreadID) == "" {
			return "", errors.New("open_thread: thread_id is required")
		}
		if params.AnchorSeq <= 0 {
			return "", errors.New("open_thread: anchor_seq is required")
		}
		view, err := manager.OpenThread(ctx, params.ThreadID, params.AnchorSeq, params.Title)
		if err != nil {
			return "", err
		}
		return marshalTaskResult("open_thread", map[string]any{"thread": view})
	case "promote":
		if strings.TrimSpace(params.SubthreadID) == "" {
			return "", errors.New("promote: subthread_id is required")
		}
		view, err := manager.PromoteThread(ctx, params.SubthreadID, params.Title)
		if err != nil {
			return "", err
		}
		return marshalTaskResult("promote", map[string]any{"task": view})
	case "conclude":
		if strings.TrimSpace(params.SubthreadID) == "" {
			return "", errors.New("conclude: subthread_id is required")
		}
		if strings.TrimSpace(params.Summary) == "" {
			return "", errors.New("conclude: summary is required (result and verification)")
		}
		view, err := manager.ConcludeTask(ctx, params.SubthreadID, params.Summary)
		if err != nil {
			return "", err
		}
		return marshalTaskResult("conclude", map[string]any{"task": view})
	case "need_human":
		if strings.TrimSpace(params.SubthreadID) == "" {
			return "", errors.New("need_human: subthread_id is required")
		}
		if strings.TrimSpace(params.Reason) == "" {
			return "", errors.New("need_human: reason is required (why this decision belongs to the human)")
		}
		view, err := manager.NeedHuman(ctx, params.SubthreadID, params.Reason)
		if err != nil {
			return "", err
		}
		return marshalTaskResult("need_human", map[string]any{"task": view})
	case "need_upstream":
		if strings.TrimSpace(params.SubthreadID) == "" {
			return "", errors.New("need_upstream: subthread_id is required")
		}
		if strings.TrimSpace(params.PieceID) == "" {
			return "", errors.New("need_upstream: piece_id is required")
		}
		if strings.TrimSpace(params.Reason) == "" {
			return "", errors.New("need_upstream: reason is required (what the upstream handoff is missing)")
		}
		view, err := manager.NeedUpstream(ctx, params.SubthreadID, params.PieceID, params.Reason)
		if err != nil {
			return "", err
		}
		return marshalTaskResult("need_upstream", map[string]any{"task": view})
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
		views, err := manager.ListWorkflowThreads(ctx, params.ThreadID)
		if err != nil {
			return "", err
		}
		return marshalTaskResult("list", map[string]any{"threads": views})
	case "set_plan":
		if strings.TrimSpace(params.SubthreadID) == "" {
			return "", errors.New("set_plan: subthread_id is required")
		}
		if len(params.Plan) == 0 {
			return "", errors.New("set_plan: plan is required (one or more pieces)")
		}
		view, err := manager.SetPlan(ctx, params.SubthreadID, params.Plan)
		if err != nil {
			return "", err
		}
		return marshalTaskResult("set_plan", map[string]any{"task": view})
	case "piece_done":
		if strings.TrimSpace(params.SubthreadID) == "" {
			return "", errors.New("piece_done: subthread_id is required")
		}
		if strings.TrimSpace(params.PieceID) == "" {
			return "", errors.New("piece_done: piece_id is required")
		}
		view, err := manager.PieceDone(ctx, params.SubthreadID, params.PieceID, params.Handoff)
		if err != nil {
			return "", err
		}
		return marshalTaskResult("piece_done", map[string]any{"task": view})
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
		Reason:          "manages group Thread and Task workflow state inside the session store",
	}
}

func (t *ManageTaskTool) DeclaredCapability() (capability.Capability, bool) {
	return capability.CapabilityTaskManage, true
}
