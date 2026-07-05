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
		Description: "Manage the named agents roster. A named agent is a long-term identity (a person with a name, a history, and an accumulating memory), NOT a disposable worker type — build people, not tools. action=list returns active named agents (no memory). action=save creates or updates a named agent by name (the identity key); memory_seed is written only when the participant is new. action=retire retires one named agent; you cannot retire yourself. action=fork makes a temporary分身 (copy) of an existing named agent that starts with the母体's memory snapshot — use it only when an experienced member is needed in two places at once or is locked/busy; the fork's memory merges back into the母体 when you retire it. When you are short-handed, prefer in this order: reuse an existing named agent, then spawn anonymous workers for throwaway parallel grunt work, and only fork or create a named agent when a genuine extra long-term hand is required.",
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"action": map[string]any{
					"type":        "string",
					"enum":        []string{"list", "save", "retire", "fork"},
					"description": "list returns the active named agents; save upserts by name; retire retires a named agent (a分身's memory merges back into its母体 first); fork makes a temporary分身 of a named agent with its memory snapshot.",
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
			},
			"required": []string{"action"},
		},
	}
}

func (t *ManageParticipantTool) Execute(ctx context.Context, args string) (string, error) {
	if t == nil || t.env == nil || t.env.AgentControl == nil {
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
