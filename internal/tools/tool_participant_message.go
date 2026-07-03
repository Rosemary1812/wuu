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

type PostMessageTool struct {
	env *Env
}

func NewPostMessageTool(env *Env) *PostMessageTool { return &PostMessageTool{env: env} }

func (t *PostMessageTool) Name() string { return "post_message" }

func (t *PostMessageTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name:        "post_message",
		Description: "Post one signed message from this participant into a visible conversation thread. Use result only for a concise final result worth the user's attention. Use question only when blocked on the user. Use update only for important progress. Silence is valid; do not post routine status.",
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"kind": map[string]any{
					"type":        "string",
					"enum":        []string{"result", "question", "update"},
					"description": "result appears as a signed result card; question asks the user for blocking input; update belongs in a task thread and requires thread_id.",
				},
				"text": map[string]any{
					"type":        "string",
					"description": "Markdown result text to show in the conversation under this participant's identity.",
				},
				"thread_id": map[string]any{
					"type":        "string",
					"description": "Target conversation thread id. Resident named agents may post to threads they belong to or their own DM thread.",
				},
			},
			"required": []string{"kind", "text"},
		},
	}
}

func (t *PostMessageTool) Execute(ctx context.Context, args string) (string, error) {
	if t == nil || t.env == nil {
		return "", errors.New("post_message: participant speech not configured")
	}
	var params struct {
		Kind     string `json:"kind"`
		Text     string `json:"text"`
		ThreadID string `json:"thread_id"`
	}
	if err := json.Unmarshal([]byte(args), &params); err != nil {
		return "", err
	}
	kind := strings.ToLower(strings.TrimSpace(params.Kind))
	if kind == "" {
		kind = "result"
	}
	speech := participantSpeech(t.env)
	if speech == nil {
		return "", errors.New("post_message: participant speech not configured")
	}
	msg, err := speech.PostMessage(ctx, kind, params.Text, params.ThreadID)
	if err != nil {
		return "", err
	}
	result := map[string]any{
		"action":         "post_message",
		"status":         "posted",
		"kind":           msg.Kind,
		"thread_id":      msg.ThreadID,
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

func participantSpeech(env *Env) ParticipantSpeech {
	if env == nil {
		return nil
	}
	if env.ParticipantSpeech != nil {
		return env.ParticipantSpeech
	}
	if env.AgentControl == nil {
		return nil
	}
	return agentControlParticipantSpeech{env: env}
}

type agentControlParticipantSpeech struct {
	env *Env
}

func (s agentControlParticipantSpeech) PostMessage(ctx context.Context, kind, text, targetThreadID string) (PostedMessage, error) {
	if s.env == nil || s.env.AgentControl == nil {
		return PostedMessage{}, errors.New("post_message: agent control not configured")
	}
	msg, err := s.env.AgentControl.PostParticipantMessage(ctx, s.env.AgentID, kind, text, targetThreadID)
	if err != nil {
		return PostedMessage{}, err
	}
	return PostedMessage{
		AgentID:       msg.AgentID,
		ParticipantID: msg.ParticipantID,
		Kind:          msg.Kind,
		ThreadID:      msg.ThreadID,
		Text:          msg.Text,
		CreatedAt:     msg.CreatedAt,
	}, nil
}

func (s agentControlParticipantSpeech) Decline(ctx context.Context, reason, targetThreadID string) error {
	if strings.TrimSpace(reason) == "" {
		return errors.New("decline: reason is required")
	}
	_, err := s.PostMessage(ctx, "decline", reason, targetThreadID)
	if err != nil {
		return fmt.Errorf("decline: %w", err)
	}
	return nil
}
