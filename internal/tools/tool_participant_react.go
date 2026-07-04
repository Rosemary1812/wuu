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

// ParticipantReactor is the optional half of participant speech that stamps a
// lightweight reaction on a specific message. It is separate from
// ParticipantSpeech (post_message/decline) because reactions are a resident /
// group-chat affordance: worker agents that speak through agent control do not
// react, so the react tool resolves this by type assertion and reports
// "unsupported" when the active speech does not implement it.
type ParticipantReactor interface {
	// React stamps reaction (a key from reactionKeys) on the message
	// addressed by (targetThreadID, seq). An empty targetThreadID means the
	// participant's own DM thread. It must not generate an envelope or wake
	// any other agent — a reaction is a terminal UI event.
	React(ctx context.Context, targetThreadID string, seq int, reaction string) error
}

// reactionKeys are the semantic reaction keys a participant may stamp. They are
// stable, model-facing identifiers; the frontend maps each to a visual — an
// emoji placeholder now, custom art later
// (2026-07-04-read-receipts-and-reactions.md §6).
var reactionKeys = []string{"eyes", "nice", "shrug", "sideeye", "smug", "whoa"}

// IsReactionKey reports whether s is a known reaction key.
func IsReactionKey(s string) bool {
	s = strings.TrimSpace(s)
	for _, k := range reactionKeys {
		if k == s {
			return true
		}
	}
	return false
}

func reactionKeyEnum() []any {
	out := make([]any, len(reactionKeys))
	for i, k := range reactionKeys {
		out[i] = k
	}
	return out
}

type ReactTool struct {
	env *Env
}

func NewReactTool(env *Env) *ReactTool { return &ReactTool{env: env} }

func (t *ReactTool) Name() string { return "react" }

func (t *ReactTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "react",
		Description: "Stamp a lightweight reaction on a specific message instead of a full reply. " +
			"Use it to acknowledge or add a bit of attitude to a message you are not replying to — " +
			"it does not count as answering an addressed message, and it does not notify anyone. " +
			"Target the message by its thread_id and seq (both shown on the incoming_message you received).",
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"seq": map[string]any{
					"type":        "integer",
					"description": "The seq of the message to react to (shown as seq=\"…\" on the incoming_message).",
				},
				"reaction": map[string]any{
					"type":        "string",
					"enum":        reactionKeyEnum(),
					"description": "Which reaction: eyes (seen/noticed), nice (approve), shrug (whatever), sideeye (skeptical), smug, whoa (surprised).",
				},
				"thread_id": map[string]any{
					"type":        "string",
					"description": "Thread the message lives in (the incoming_message's thread_id). Omit only to react in your own DM.",
				},
			},
			"required": []string{"seq", "reaction"},
		},
	}
}

func (t *ReactTool) Execute(ctx context.Context, args string) (string, error) {
	if t == nil || t.env == nil {
		return "", errors.New("react: participant speech not configured")
	}
	var params struct {
		Seq      int    `json:"seq"`
		Reaction string `json:"reaction"`
		ThreadID string `json:"thread_id"`
	}
	if err := json.Unmarshal([]byte(args), &params); err != nil {
		return "", err
	}
	if params.Seq <= 0 {
		return "", errors.New("react: seq is required")
	}
	reaction := strings.TrimSpace(params.Reaction)
	if !IsReactionKey(reaction) {
		return "", fmt.Errorf("react: unknown reaction %q", reaction)
	}
	speech := participantSpeech(t.env)
	reactor, ok := speech.(ParticipantReactor)
	if !ok || reactor == nil {
		return "", errors.New("react: not supported in this context")
	}
	if err := reactor.React(ctx, params.ThreadID, params.Seq, reaction); err != nil {
		return "", err
	}
	result := map[string]any{
		"action":    "react",
		"status":    "reacted",
		"seq":       params.Seq,
		"reaction":  reaction,
		"thread_id": strings.TrimSpace(params.ThreadID),
	}
	data, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (t *ReactTool) IsReadOnly() bool { return false }

func (t *ReactTool) IsConcurrencySafe() bool { return false }

func (t *ReactTool) Classify(string) ToolClassification {
	return ToolClassification{
		ReadOnly:        false,
		ConcurrencySafe: false,
		Risk:            ToolRiskLow,
		Reason:          "message reaction",
	}
}

func (t *ReactTool) DeclaredCapability() (capability.Capability, bool) {
	return capability.CapabilityTaskCommunicate, true
}
