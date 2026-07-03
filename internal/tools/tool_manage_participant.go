package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/blueberrycongee/wuu/internal/capability"
	"github.com/blueberrycongee/wuu/internal/providers"
)

type ManageParticipantTool struct {
	env *Env
}

func NewManageParticipantTool(env *Env) *ManageParticipantTool { return &ManageParticipantTool{env: env} }

func (t *ManageParticipantTool) Name() string { return "manage_participant" }

func (t *ManageParticipantTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name:        "manage_participant",
		Description: "Manage the named agents roster. action=list returns active named agents (no memory). action=save creates or updates a named agent by name; memory_seed is written only when the participant is new. action=retire retires one named agent; you cannot retire yourself.",
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"action": map[string]any{
					"type":        "string",
					"enum":        []string{"list", "save", "retire"},
					"description": "list returns the active named agents; save upserts by name; retire retires a named agent.",
				},
				"name": map[string]any{
					"type":        "string",
					"description": "Participant display name. Required for save and retire.",
				},
				"role": map[string]any{
					"type":        "string",
					"description": "Worker type role (e.g. reviewer, planner, qa). Optional on save.",
				},
				"avatar": map[string]any{
					"type":        "string",
					"description": "Emoji glyph. Optional on save.",
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
			},
			"required": []string{"action"},
		},
	}
}

func (t *ManageParticipantTool) Execute(ctx context.Context, args string) (string, error) {
	if t == nil || t.env == nil || t.env.AgentControl == nil {
		return "", errors.New("manage_participant: agent control not configured")
	}
	result, err := t.env.AgentControl.ManageParticipant(ctx, t.env.AgentID, args)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("manage_participant: marshal result: %w", err)
	}
	return string(data), nil
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
