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

const (
	createGroupToolName    = "create_group"
	addGroupMemberToolName = "add_group_member"
)

// CreateGroupTool creates a group thread and makes the calling resident
// participant its first member. Resident-only: gated on resident
// participant capability alongside fetch_thread_messages.
type CreateGroupTool struct {
	env *Env
}

func NewCreateGroupTool(env *Env) *CreateGroupTool { return &CreateGroupTool{env: env} }

func (t *CreateGroupTool) Name() string { return createGroupToolName }

func (t *CreateGroupTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name:        createGroupToolName,
		Description: "Create a group conversation thread and join it as the first member. Create a group only for an ongoing purpose — a project, a standing topic — never for a one-off question; prefer reusing an existing group. Limited to one new group per turn.",
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"title"},
			"properties": map[string]any{
				"title": map[string]any{
					"type":        "string",
					"description": "Human-readable group title. The reserved channel title \"all\" cannot be created.",
				},
			},
		},
	}
}

func (t *CreateGroupTool) Execute(ctx context.Context, args string) (string, error) {
	manager, err := groupManager(t.env, createGroupToolName)
	if err != nil {
		return "", err
	}
	var params struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal([]byte(args), &params); err != nil {
		return "", fmt.Errorf("create_group: parse args: %w", err)
	}
	title := strings.TrimSpace(params.Title)
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

func (t *CreateGroupTool) IsReadOnly() bool { return false }

func (t *CreateGroupTool) IsConcurrencySafe() bool { return false }

func (t *CreateGroupTool) Classify(string) ToolClassification {
	return ToolClassification{
		ReadOnly:        false,
		ConcurrencySafe: false,
		Risk:            ToolRiskLow,
		Reason:          "creates a visible group conversation thread",
	}
}

func (t *CreateGroupTool) DeclaredCapability() (capability.Capability, bool) {
	return capability.CapabilityTaskManage, true
}

// AddGroupMemberTool adds a named participant to a group thread the calling
// resident participant belongs to.
type AddGroupMemberTool struct {
	env *Env
}

func NewAddGroupMemberTool(env *Env) *AddGroupMemberTool { return &AddGroupMemberTool{env: env} }

func (t *AddGroupMemberTool) Name() string { return addGroupMemberToolName }

func (t *AddGroupMemberTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name:        addGroupMemberToolName,
		Description: "Add a named teammate to a group thread you belong to. The target must be a group thread; the participant must be an active named agent. Do not add members just in case — an invitation is a request for their time.",
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"thread_id", "participant_id"},
			"properties": map[string]any{
				"thread_id": map[string]any{
					"type":        "string",
					"description": "Group thread id to add the member to. You must already be a member of it.",
				},
				"participant_id": map[string]any{
					"type":        "string",
					"description": "Participant id of the named agent to add (as listed by manage_participant).",
				},
			},
		},
	}
}

func (t *AddGroupMemberTool) Execute(ctx context.Context, args string) (string, error) {
	manager, err := groupManager(t.env, addGroupMemberToolName)
	if err != nil {
		return "", err
	}
	var params struct {
		ThreadID      string `json:"thread_id"`
		ParticipantID string `json:"participant_id"`
	}
	if err := json.Unmarshal([]byte(args), &params); err != nil {
		return "", fmt.Errorf("add_group_member: parse args: %w", err)
	}
	threadID := strings.TrimSpace(params.ThreadID)
	if threadID == "" {
		return "", errors.New("add_group_member: thread_id is required")
	}
	participantID := strings.TrimSpace(params.ParticipantID)
	if participantID == "" {
		return "", errors.New("add_group_member: participant_id is required")
	}
	if err := manager.AddGroupMember(ctx, threadID, participantID); err != nil {
		return "", err
	}
	data, err := json.Marshal(map[string]any{
		"action":         "add_group_member",
		"status":         "added",
		"thread_id":      threadID,
		"participant_id": participantID,
	})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (t *AddGroupMemberTool) IsReadOnly() bool { return false }

func (t *AddGroupMemberTool) IsConcurrencySafe() bool { return false }

func (t *AddGroupMemberTool) Classify(string) ToolClassification {
	return ToolClassification{
		ReadOnly:        false,
		ConcurrencySafe: false,
		Risk:            ToolRiskLow,
		Reason:          "adds a named participant to a group thread",
	}
}

func (t *AddGroupMemberTool) DeclaredCapability() (capability.Capability, bool) {
	return capability.CapabilityTaskManage, true
}

func groupManager(env *Env, toolName string) (GroupManager, error) {
	if env == nil || env.GroupManager == nil {
		return nil, fmt.Errorf("%s: group management not configured", toolName)
	}
	if strings.TrimSpace(env.ParticipantID) == "" {
		return nil, fmt.Errorf("%s: participant_id is required", toolName)
	}
	return env.GroupManager, nil
}
