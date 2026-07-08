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
		Description: "Run the task board of a group thread or of your own DM. A task is trackable work with one owner at a time; claiming it is how work is divided — never coordinate by chat message. In your DM the board is yours alone: tasks are born owned by you (claim is implied) and unclaim is refused. action=create opens a task in the thread (anchor_seq anchors it on a main-stream message; omit for a standalone task, e.g. when splitting work; claim=true takes ownership in the same call). action=escalate converts an open discussion reply (subthread_id) you belong to into a board task once the discussion has converged — e.g. the user says it is ready; claim=true self-owns it. action=claim takes ownership of an unclaimed task — if someone else already owns it, you lost the race: move on silently, never duplicate their work. action=unclaim releases ownership. action=update_status files the task's conclusion with a summary (the one-line conclusion; result + how it was verified) and COMPLETES the task — that filing is the completion report, no review step follows. action=need_human flags the task for a decision that genuinely belongs to the human (subthread_id + reason); it halts nothing mechanically and wakes nobody — use it ONLY for decisions you truly cannot make. action=unfollow stops receiving this task thread's traffic once your part is done. action=list shows the group's board. Ownership is work responsibility only — it grants no authority over other agents.",
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"action": map[string]any{
					"type":        "string",
					"enum":        []string{"create", "escalate", "claim", "unclaim", "update_status", "need_human", "need_upstream", "unfollow", "list", "set_plan", "piece_done"},
					"description": "create opens a task (group members only); escalate converts a converged open discussion reply into a task; claim/unclaim take/release work ownership; update_status files the conclusion and completes the task; need_human flags the task for a decision only the human can make; unfollow leaves the task's push subset; list shows the board. As a task LEAD: set_plan declares a team plan (pieces with assignees + dependencies + per-piece prompt) on a task and the engine dispatches the ready pieces by @-waking their assignees; as an ASSIGNEE: piece_done reports your piece done and hands the structured result to the downstream node(s) — that handoff is what wakes the next agent — then the engine advances the plan (dispatches the next dependents, or wakes the lead when all pieces are done). If your piece requires a visible post_message, call post_message alone first and wait for status=\"posted\" before piece_done; if it is held, re-reason instead of reporting done. Never call post_message and piece_done in the same assistant tool-call batch. need_upstream bounces your piece (piece_id) back to its upstream node(s) when the handoff you were given is insufficient (reason names what is missing) — the engine re-runs the upstream and re-dispatches you with its fresh handoff; use it instead of silently working around a bad handoff.",
				},
				"thread_id": map[string]any{
					"type":        "string",
					"description": "Group thread id. Required for create and list.",
				},
				"subthread_id": map[string]any{
					"type":        "string",
					"description": "Task id (cth-…). Required for claim, unclaim, update_status, need_human, and unfollow.",
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
					"description": "With update_status: the one-line conclusion — the result and how it was verified. Filing it completes the task. Required.",
				},
				"reason": map[string]any{
					"type":        "string",
					"description": "Required for need_human (why this decision belongs to the human) and need_upstream (what is missing from the upstream handoff).",
				},
				"ack_collision_id": map[string]any{
					"type":        "string",
					"description": "With create on a standalone task (anchor_seq omitted): STRICT id-match escape hatch for the same-group same-title collision check (issue #4 v3). When the same title already exists in the thread as an unfinished task, pass the exact existing task id (from the conflict error message) to persist a same-titled duplicate (work-splitting case). Any other value — empty string, wrong id, made-up value — keeps the dedup hard-block. Usually you should claim the existing task (manage_task action=claim) or pick a distinguishing title instead.",
				},
				"plan": map[string]any{
					"type":        "array",
					"description": "With set_plan: the team work breakdown. Each item is a piece {id, title, assignee, prompt, depends_on}. id is a short label unique within the plan (e.g. \"p1\"); assignee is a teammate's participant id (prefer existing members); prompt is the briefing the assignee is woken with — write it so they can start without asking; depends_on lists the piece ids that must finish before this one starts (omit or [] for a piece that can start immediately). Declare the whole plan at once; the engine dispatches every piece with no unmet dependency and @-wakes the rest as their dependencies complete.",
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
					"description": "The id of your plan piece (as declared in set_plan). Required for piece_done (the piece you finished) and need_upstream (the piece whose upstream handoff was insufficient). If the piece required a visible post_message, only file piece_done after that post_message returned status=\"posted\".",
				},
				"handoff": map[string]any{
					"type":        "object",
					"description": "With piece_done: the structured result you hand to the downstream node(s) — the ONLY thing that wakes the next agent. Put the next node's real input here, never in a public post_message update (that update is progress for the human and wakes no teammate). Omit entirely if you produced nothing for a next node.",
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
		return "", errors.New("manage_task: task rail not configured in this environment")
	}
	if strings.TrimSpace(t.env.ParticipantID) == "" {
		return "", errors.New("manage_task: participant identity is required")
	}
	var params struct {
		Action         string       `json:"action"`
		ThreadID       string       `json:"thread_id"`
		SubthreadID    string       `json:"subthread_id"`
		Title          string       `json:"title"`
		AnchorSeq      int          `json:"anchor_seq"`
		Claim          bool         `json:"claim"`
		AckCollisionID string       `json:"ack_collision_id"`
		Summary        string       `json:"summary"`
		Reason         string       `json:"reason"`
		Plan           []TaskPiece  `json:"plan"`
		PieceID        string       `json:"piece_id"`
		Handoff        *TaskHandoff `json:"handoff"`
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
		view, err := manager.CreateTask(ctx, params.ThreadID, params.AnchorSeq, params.Title, params.Claim, params.AckCollisionID)
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
			return "", errors.New("update_status: summary is required (the one-line conclusion)")
		}
		view, err := manager.ConcludeTask(ctx, params.SubthreadID, params.Summary)
		if err != nil {
			return "", err
		}
		return marshalTaskResult("update_status", map[string]any{"task": view})
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
		views, err := manager.ListTasks(ctx, params.ThreadID)
		if err != nil {
			return "", err
		}
		return marshalTaskResult("list", map[string]any{"tasks": views})
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
		Reason:          "manages task-board coordination state (owner, status) inside the session store",
	}
}

func (t *ManageTaskTool) DeclaredCapability() (capability.Capability, bool) {
	return capability.CapabilityTaskManage, true
}
