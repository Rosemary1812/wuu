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

type ManageParticipantTool struct {
	env *Env
}

func NewManageParticipantTool(env *Env) *ManageParticipantTool {
	return &ManageParticipantTool{env: env}
}

func (t *ManageParticipantTool) Name() string { return "manage_participant" }

func (t *ManageParticipantTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name:        "manage_participant",
		Description: "Manage the named agents roster and the group threads they meet in. A named agent is a long-term identity (a person with a name, a history, and an accumulating memory), NOT a disposable worker type — build people, not tools. action=list returns active named agents (no memory). action=save creates or updates a named agent by name (the identity key); memory_seed is written only when the participant is new. action=retire retires one named agent; you cannot retire yourself. action=fork makes a temporary分身 (copy) of an existing named agent that starts with the母体's memory snapshot — use it only when an experienced member is needed in two places at once or is locked/busy; the fork's memory merges back into the母体 when you retire it. action=create_group opens a group conversation thread (title) and joins it as the first member; create a group only for an ongoing purpose, never a one-off, and prefer reusing an existing group. action=add_member adds a named teammate (participant_id) to a group thread (thread_id) you belong to; do not add members just in case — an invitation is a request for their time. When you are short-handed, prefer in this order: reuse an existing named agent, then spawn anonymous workers for throwaway parallel grunt work, and only fork or create a named agent when a genuine extra long-term hand is required.",
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"action": map[string]any{
					"type":        "string",
					"enum":        []string{"list", "save", "retire", "fork", "create_group", "add_member"},
					"description": "list returns the active named agents; save upserts by name; retire retires a named agent (a分身's memory merges back into its母体 first); fork makes a temporary分身 of a named agent with its memory snapshot; create_group opens a group thread and joins it; add_member adds a named agent to a group thread you belong to.",
				},
				"name": map[string]any{
					"type":        "string",
					"description": "Participant display name — the identity key. Required for save, retire, and fork. For fork it names the母体 to copy. Users always see the agent by this name.",
				},
				"role": map[string]any{
					"type":        "string",
					"description": "Optional free-form persona/职责说明 note (e.g. \"我们的部署守护者\"). Purely descriptive: it flavors the agent's self-description and does NOT change its tools or capabilities — every named agent shares the same tool surface. Do not treat it as a worker type.",
				},
				"tagline": map[string]any{
					"type":        "string",
					"description": "One-line description. Optional on save.",
				},
				"model": map[string]any{
					"type":        "string",
					"description": "Pinned model name. Optional on save.",
				},
				"memory_seed": map[string]any{
					"type":        "string",
					"description": "Initial MEMORY.md body. Optional on save. Written only when the participant is new — never overwrites an existing memory.",
				},
				"title": map[string]any{
					"type":        "string",
					"description": "Human-readable group title. Required for create_group. The reserved channel title \"all\" cannot be created.",
				},
				"thread_id": map[string]any{
					"type":        "string",
					"description": "Group thread id to add a member to. Required for add_member; you must already be a member of it.",
				},
				"participant_id": map[string]any{
					"type":        "string",
					"description": "Participant id of the named agent to add. Required for add_member.",
				},
			},
			"required": []string{"action"},
		},
	}
}

func (t *ManageParticipantTool) Execute(ctx context.Context, args string) (string, error) {
	if t == nil || t.env == nil {
		return "", errors.New("manage_participant: not configured")
	}
	// Peek at the action so group actions route through the resident-only
	// GroupManager instead of the roster backend. Group management is gated at
	// execute time (nil GroupManager) rather than at surface time, so the enum
	// stays static for all participant-speech agents.
	var head struct {
		Action        string `json:"action"`
		Title         string `json:"title"`
		ThreadID      string `json:"thread_id"`
		ParticipantID string `json:"participant_id"`
	}
	if err := json.Unmarshal([]byte(args), &head); err != nil {
		return "", fmt.Errorf("manage_participant: parse args: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(head.Action)) {
	case "create_group":
		return t.executeCreateGroup(ctx, head.Title)
	case "add_member":
		return t.executeAddMember(ctx, head.ThreadID, head.ParticipantID)
	}

	if t.env.AgentControl == nil {
		return "", errors.New("manage_participant: agent control not configured")
	}
	agentID := strings.TrimSpace(t.env.ParticipantID)
	if agentID == "" {
		agentID = strings.TrimSpace(t.env.AgentID)
	}
	result, err := t.env.AgentControl.ManageParticipant(ctx, agentID, args)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("manage_participant: marshal result: %w", err)
	}
	return string(data), nil
}

func (t *ManageParticipantTool) executeCreateGroup(ctx context.Context, title string) (string, error) {
	manager, err := groupManager(t.env, "create_group")
	if err != nil {
		return "", err
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return "", errors.New("create_group: title is required")
	}
	threadID, err := manager.CreateGroup(ctx, title)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(map[string]any{
		"action":    "create_group",
		"status":    "created",
		"thread_id": threadID,
		"title":     title,
	})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (t *ManageParticipantTool) executeAddMember(ctx context.Context, threadID, participantID string) (string, error) {
	manager, err := groupManager(t.env, "add_member")
	if err != nil {
		return "", err
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return "", errors.New("add_member: thread_id is required")
	}
	participantID = strings.TrimSpace(participantID)
	if participantID == "" {
		return "", errors.New("add_member: participant_id is required")
	}
	if err := manager.AddGroupMember(ctx, threadID, participantID); err != nil {
		return "", err
	}
	data, err := json.Marshal(map[string]any{
		"action":         "add_member",
		"status":         "added",
		"thread_id":      threadID,
		"participant_id": participantID,
	})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// groupManager returns the resident group-management backend or an
// execute-time error when the caller is not a resident participant (group
// actions are gated here rather than on the tool surface).
func groupManager(env *Env, action string) (GroupManager, error) {
	if env == nil || env.GroupManager == nil {
		return nil, fmt.Errorf("%s: group management not configured", action)
	}
	if strings.TrimSpace(env.ParticipantID) == "" {
		return nil, fmt.Errorf("%s: participant_id is required", action)
	}
	return env.GroupManager, nil
}

func (t *ManageParticipantTool) IsReadOnly() bool { return false }

func (t *ManageParticipantTool) IsConcurrencySafe() bool { return false }

func (t *ManageParticipantTool) Classify(string) ToolClassification {
	return ToolClassification{
		ReadOnly:        false,
		ConcurrencySafe: false,
		Risk:            ToolRiskMedium,
		Reason:          "manages durable named participants and memory",
	}
}

func (t *ManageParticipantTool) DeclaredCapability() (capability.Capability, bool) {
	return capability.CapabilityTaskManage, true
}
