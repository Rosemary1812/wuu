package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/blueberrycongee/wuu/internal/capability"
	"github.com/blueberrycongee/wuu/internal/providers"
)

type PostMessageTool struct {
	env *Env
}

func NewPostMessageTool(env *Env) *PostMessageTool { return &PostMessageTool{env: env} }

func (t *PostMessageTool) Name() string { return "post_message" }

func (t *PostMessageTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name:        "post_message",
		Description: "Post one signed result message from this worker into the visible conversation. Use only when the task has a concise result worth the user's attention. Silence is valid; do not use this for progress logs or routine status.",
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"kind": map[string]any{
					"type":        "string",
					"enum":        []string{"result"},
					"description": "Phase 2 only supports result messages.",
				},
				"text": map[string]any{
					"type":        "string",
					"description": "Markdown result text to show in the conversation under this participant's identity.",
				},
			},
			"required": []string{"kind", "text"},
		},
	}
}

func (t *PostMessageTool) Execute(ctx context.Context, args string) (string, error) {
	if t == nil || t.env == nil || t.env.AgentControl == nil {
		return "", errors.New("post_message: agent control not configured")
	}
	var params struct {
		Kind string `json:"kind"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(args), &params); err != nil {
		return "", err
	}
	kind := strings.ToLower(strings.TrimSpace(params.Kind))
	if kind == "" {
		kind = "result"
	}
	msg, err := t.env.AgentControl.PostParticipantMessage(ctx, t.env.AgentID, kind, params.Text)
	if err != nil {
		return "", err
	}
	result := map[string]any{
		"action":         "post_message",
		"status":         "posted",
		"kind":           msg.Kind,
		"agent_id":       msg.AgentID,
		"participant_id": msg.ParticipantID,
	}
	data, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (t *PostMessageTool) IsReadOnly() bool { return false }

func (t *PostMessageTool) IsConcurrencySafe() bool { return false }

func (t *PostMessageTool) Classify(string) ToolClassification {
	return ToolClassification{
		ReadOnly:        false,
		ConcurrencySafe: false,
		Risk:            ToolRiskLow,
		Reason:          "visible participant message",
	}
}

func (t *PostMessageTool) DeclaredCapability() (capability.Capability, bool) {
	return capability.CapabilityTaskCommunicate, true
}
