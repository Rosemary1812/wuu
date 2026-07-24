package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/channels"
	"github.com/blueberrycongee/wuu/internal/providers"
)

type ChatCheckTool struct{ env *Env }

func NewChatCheckTool(env *Env) *ChatCheckTool   { return &ChatCheckTool{env: env} }
func (t *ChatCheckTool) Name() string            { return "chat_check" }
func (t *ChatCheckTool) IsReadOnly() bool        { return false }
func (t *ChatCheckTool) IsConcurrencySafe() bool { return false }
func (t *ChatCheckTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "chat_check",
		Description: "Check this named agent's group-chat inbox. Returns compact unread indexes and current room/thread sequences. " +
			"This advances chat cursors, marks returned indexes pulled, and clears the current wake; call chat_read for message bodies.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
	}
}
func (t *ChatCheckTool) Execute(ctx context.Context, _ string) (string, error) {
	if t == nil || t.env == nil || t.env.ChatAgent == nil {
		return "", errors.New("chat_check is available only in a named-agent session")
	}
	result, err := t.env.ChatAgent.Check(ctx)
	if err != nil {
		return "", err
	}
	return mustJSON(result)
}

type ChatReadTool struct{ env *Env }

func NewChatReadTool(env *Env) *ChatReadTool    { return &ChatReadTool{env: env} }
func (t *ChatReadTool) Name() string            { return "chat_read" }
func (t *ChatReadTool) IsReadOnly() bool        { return true }
func (t *ChatReadTool) IsConcurrencySafe() bool { return true }
func (t *ChatReadTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name:        "chat_read",
		Description: "Read group-chat message bodies either by chat_check item IDs or by a member room's sequence range. Choose exactly one mode.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"item_ids":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "minItems": 1, "maxItems": 50},
				"room_id":   map[string]any{"type": "string"},
				"after_seq": map[string]any{"type": "integer", "minimum": 0},
				"limit":     map[string]any{"type": "integer", "minimum": 1, "maximum": 500},
			},
		},
	}
}
func (t *ChatReadTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	if t == nil || t.env == nil || t.env.ChatAgent == nil {
		return "", errors.New("chat_read is available only in a named-agent session")
	}
	var args struct {
		ItemIDs  []string `json:"item_ids"`
		RoomID   string   `json:"room_id"`
		AfterSeq int64    `json:"after_seq"`
		Limit    int      `json:"limit"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", err
	}
	itemMode := len(args.ItemIDs) > 0
	roomMode := strings.TrimSpace(args.RoomID) != ""
	if itemMode == roomMode {
		return "", errors.New("chat_read requires exactly one of item_ids or room_id")
	}
	var (
		messages []channels.Message
		err      error
	)
	if itemMode {
		messages, err = t.env.ChatAgent.ReadInbox(ctx, args.ItemIDs)
	} else {
		messages, err = t.env.ChatAgent.ReadRoom(ctx, args.RoomID, args.AfterSeq, args.Limit)
	}
	if err != nil {
		return "", err
	}
	return mustJSON(map[string]any{"messages": messages})
}

type ChatSendTool struct{ env *Env }

func NewChatSendTool(env *Env) *ChatSendTool    { return &ChatSendTool{env: env} }
func (t *ChatSendTool) Name() string            { return "chat_send" }
func (t *ChatSendTool) IsReadOnly() bool        { return false }
func (t *ChatSendTool) IsConcurrencySafe() bool { return false }
func (t *ChatSendTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "chat_send",
		Description: "Send one short text message as this named agent. basis_seq is required and records the room version used to compose the reply. " +
			"Use thread_id for a thread reply and reply_to when answering a specific message. A stale basis is preserved as a held draft instead of being posted.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"room_id":   map[string]any{"type": "string"},
				"thread_id": map[string]any{"type": "string"},
				"reply_to":  map[string]any{"type": "string"},
				"kind":      map[string]any{"type": "string", "enum": []string{"text"}},
				"body":      map[string]any{"type": "string", "maxLength": channels.MaxMessageRunes},
				"basis_seq": map[string]any{"type": "integer", "minimum": 0},
			},
			"required": []string{"room_id", "kind", "body", "basis_seq"},
		},
	}
}
func (t *ChatSendTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	if t == nil || t.env == nil || t.env.ChatAgent == nil {
		return "", errors.New("chat_send is available only in a named-agent session")
	}
	var args struct {
		RoomID   string `json:"room_id"`
		ThreadID string `json:"thread_id"`
		ReplyTo  string `json:"reply_to"`
		Kind     string `json:"kind"`
		Body     string `json:"body"`
		BasisSeq *int64 `json:"basis_seq"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", err
	}
	if strings.TrimSpace(args.Kind) != string(channels.MessageText) {
		return "", errors.New("chat_send kind must be text")
	}
	if args.BasisSeq == nil {
		return "", errors.New("chat_send basis_seq is required")
	}
	result, err := t.env.ChatAgent.Send(ctx, channels.AgentSendParams{
		RoomID: args.RoomID, ThreadID: args.ThreadID, ReplyTo: args.ReplyTo,
		Body: args.Body, BasisSeq: *args.BasisSeq,
	})
	if err != nil {
		return "", err
	}
	if result.Status == channels.SendHeld {
		return mustJSON(map[string]any{"status": result.Status, "draft": result.Draft, "delta": result.Delta})
	}
	return mustJSON(map[string]any{"status": result.Status, "message": result.Message})
}

type ChatDraftTool struct{ env *Env }

func NewChatDraftTool(env *Env) *ChatDraftTool   { return &ChatDraftTool{env: env} }
func (t *ChatDraftTool) Name() string            { return "chat_draft" }
func (t *ChatDraftTool) IsReadOnly() bool        { return false }
func (t *ChatDraftTool) IsConcurrencySafe() bool { return false }
func (t *ChatDraftTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "chat_draft",
		Description: "List or explicitly resolve this named agent's held chat drafts. Resolve with as_is to post against a fresh basis, " +
			"silent to drop, or anyway to force-post after at least two holds. To revise, read the delta and call chat_send with new text and a fresh basis.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action":     map[string]any{"type": "string", "enum": []string{"list", "resolve"}},
				"draft_id":   map[string]any{"type": "string"},
				"resolution": map[string]any{"type": "string", "enum": []string{"as_is", "silent", "anyway"}},
				"basis_seq":  map[string]any{"type": "integer", "minimum": 0},
			},
			"required": []string{"action"},
		},
	}
}
func (t *ChatDraftTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	if t == nil || t.env == nil || t.env.ChatAgent == nil {
		return "", errors.New("chat_draft is available only in a named-agent session")
	}
	var args struct {
		Action     string `json:"action"`
		DraftID    string `json:"draft_id"`
		Resolution string `json:"resolution"`
		BasisSeq   *int64 `json:"basis_seq"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", err
	}
	switch strings.TrimSpace(args.Action) {
	case "list":
		if args.DraftID != "" || args.Resolution != "" || args.BasisSeq != nil {
			return "", errors.New("chat_draft list does not accept resolution fields")
		}
		drafts, err := t.env.ChatAgent.ListDrafts(ctx)
		if err != nil {
			return "", err
		}
		return mustJSON(map[string]any{"drafts": drafts})
	case "resolve":
		if strings.TrimSpace(args.DraftID) == "" {
			return "", errors.New("chat_draft resolve requires draft_id")
		}
		result, err := t.env.ChatAgent.ResolveDraft(ctx, channels.ResolveDraftParams{
			DraftID: args.DraftID, Resolution: channels.DraftResolution(args.Resolution), BasisSeq: args.BasisSeq,
		})
		if err != nil {
			return "", err
		}
		return mustJSON(result)
	default:
		return "", errors.New("chat_draft action must be list or resolve")
	}
}

type ChatTaskTool struct{ env *Env }

func NewChatTaskTool(env *Env) *ChatTaskTool    { return &ChatTaskTool{env: env} }
func (t *ChatTaskTool) Name() string            { return "chat_task" }
func (t *ChatTaskTool) IsReadOnly() bool        { return false }
func (t *ChatTaskTool) IsConcurrencySafe() bool { return false }
func (t *ChatTaskTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name:        "chat_task",
		Description: "Create, update, or list lightweight tasks in a group-chat room. Only the task owner may update it; task progress belongs in the task message's thread.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action":    map[string]any{"type": "string", "enum": []string{"create", "update", "list"}},
				"room_id":   map[string]any{"type": "string"},
				"thread_id": map[string]any{"type": "string"},
				"task_id":   map[string]any{"type": "string"},
				"title":     map[string]any{"type": "string"},
				"body":      map[string]any{"type": "string"},
				"owner_id":  map[string]any{"type": "string"},
				"state":     map[string]any{"type": "string", "enum": []string{"open", "doing", "done"}},
			},
			"required": []string{"action"},
		},
	}
}
func (t *ChatTaskTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	if t == nil || t.env == nil || t.env.ChatAgent == nil {
		return "", errors.New("chat_task is available only in a named-agent session")
	}
	var args struct {
		Action   string `json:"action"`
		RoomID   string `json:"room_id"`
		ThreadID string `json:"thread_id"`
		TaskID   string `json:"task_id"`
		Title    string `json:"title"`
		Body     string `json:"body"`
		OwnerID  string `json:"owner_id"`
		State    string `json:"state"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", err
	}
	switch strings.TrimSpace(args.Action) {
	case "create":
		message, err := t.env.ChatAgent.CreateTask(ctx, channels.TaskCreateParams{RoomID: args.RoomID, ThreadID: args.ThreadID, Title: args.Title, Body: args.Body, OwnerID: args.OwnerID})
		if err != nil {
			return "", err
		}
		return mustJSON(map[string]any{"task": message})
	case "update":
		message, err := t.env.ChatAgent.UpdateTask(ctx, channels.TaskUpdateParams{TaskID: args.TaskID, RoomID: args.RoomID, State: channels.TaskState(args.State), OwnerID: args.OwnerID})
		if err != nil {
			return "", err
		}
		return mustJSON(map[string]any{"task": message})
	case "list":
		tasks, err := t.env.ChatAgent.ListTasks(ctx, args.RoomID)
		if err != nil {
			return "", err
		}
		return mustJSON(map[string]any{"tasks": tasks})
	default:
		return "", errors.New("chat_task action must be create, update, or list")
	}
}

type ChatRemindTool struct{ env *Env }

func NewChatRemindTool(env *Env) *ChatRemindTool  { return &ChatRemindTool{env: env} }
func (t *ChatRemindTool) Name() string            { return "chat_remind" }
func (t *ChatRemindTool) IsReadOnly() bool        { return false }
func (t *ChatRemindTool) IsConcurrencySafe() bool { return false }
func (t *ChatRemindTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name:        "chat_remind",
		Description: "Set, list, or cancel this named agent's reminders. Set with exactly one RFC3339 fire_at or relative duration such as 1m; the minimum delay is one minute.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action": map[string]any{"type": "string", "enum": []string{"set", "list", "cancel"}}, "fire_at": map[string]any{"type": "string"},
				"after": map[string]any{"type": "string"}, "note": map[string]any{"type": "string"}, "room_id": map[string]any{"type": "string"},
				"thread_id": map[string]any{"type": "string"}, "state": map[string]any{"type": "string", "enum": []string{"pending", "fired", "cancelled"}},
				"reminder_id": map[string]any{"type": "string"},
			},
			"required": []string{"action"},
		},
	}
}
func (t *ChatRemindTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	if t == nil || t.env == nil || t.env.ChatAgent == nil {
		return "", errors.New("chat_remind is available only in a named-agent session")
	}
	var args struct {
		Action     string `json:"action"`
		FireAt     string `json:"fire_at"`
		After      string `json:"after"`
		Note       string `json:"note"`
		RoomID     string `json:"room_id"`
		ThreadID   string `json:"thread_id"`
		State      string `json:"state"`
		ReminderID string `json:"reminder_id"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", err
	}
	switch strings.TrimSpace(args.Action) {
	case "set":
		if (strings.TrimSpace(args.FireAt) == "") == (strings.TrimSpace(args.After) == "") {
			return "", errors.New("chat_remind set requires exactly one of fire_at or after")
		}
		params := channels.ReminderSetParams{Note: args.Note, RoomID: args.RoomID, ThreadID: args.ThreadID}
		if strings.TrimSpace(args.FireAt) != "" {
			fireAt, err := time.Parse(time.RFC3339, strings.TrimSpace(args.FireAt))
			if err != nil {
				return "", err
			}
			params.FireAt = fireAt
			reminder, err := t.env.ChatAgent.SetReminder(ctx, params)
			if err != nil {
				return "", err
			}
			return mustJSON(map[string]any{"reminder": reminder})
		}
		delay, err := time.ParseDuration(strings.TrimSpace(args.After))
		if err != nil {
			return "", err
		}
		reminder, err := t.env.ChatAgent.SetReminderAfter(ctx, delay, params)
		if err != nil {
			return "", err
		}
		return mustJSON(map[string]any{"reminder": reminder})
	case "list":
		reminders, err := t.env.ChatAgent.ListReminders(ctx, channels.ReminderState(args.State))
		if err != nil {
			return "", err
		}
		return mustJSON(map[string]any{"reminders": reminders})
	case "cancel":
		reminder, err := t.env.ChatAgent.CancelReminder(ctx, args.ReminderID)
		if err != nil {
			return "", err
		}
		return mustJSON(map[string]any{"reminder": reminder})
	default:
		return "", errors.New("chat_remind action must be set, list, or cancel")
	}
}
