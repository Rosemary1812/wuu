package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/blueberrycongee/wuu/internal/capability"
	"github.com/blueberrycongee/wuu/internal/providers"
)

type DeclineTool struct {
	env *Env
}

func NewDeclineTool(env *Env) *DeclineTool { return &DeclineTool{env: env} }

func (t *DeclineTool) Name() string { return "decline" }

func (t *DeclineTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name:        "decline",
		Description: "Explicitly choose not to post a visible answer after being mentioned or asked to respond. Use this when the right outcome is silence, another participant already covered it, or there is no useful update.",
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"reason": map[string]any{
					"type":        "string",
					"description": "One short reason shown as muted text in the conversation.",
				},
			},
			"required": []string{"reason"},
		},
	}
}

func (t *DeclineTool) Execute(ctx context.Context, args string) (string, error) {
	if t == nil || t.env == nil || t.env.AgentControl == nil {
		return "", errors.New("decline: agent control not configured")
	}
	var params struct {
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(args), &params); err != nil {
		return "", err
	}
	reason := strings.TrimSpace(params.Reason)
	if reason == "" {
		return "", errors.New("decline: reason is required")
	}
	msg, err := t.env.AgentControl.PostParticipantMessage(ctx, t.env.AgentID, "decline", reason, "")
	if err != nil {
		return "", err
	}
	result := map[string]any{
		"action":         "decline",
		"status":         "declined",
		"agent_id":       msg.AgentID,
		"participant_id": msg.ParticipantID,
	}
	data, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (t *DeclineTool) IsReadOnly() bool { return false }

func (t *DeclineTool) IsConcurrencySafe() bool { return false }

func (t *DeclineTool) Classify(string) ToolClassification {
	return ToolClassification{
		ReadOnly:        false,
		ConcurrencySafe: false,
		Risk:            ToolRiskLow,
		Reason:          "visible participant decline",
	}
}

func (t *DeclineTool) DeclaredCapability() (capability.Capability, bool) {
	return capability.CapabilityTaskCommunicate, true
}
