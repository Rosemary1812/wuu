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
		Description: "Post one signed message from this participant into a visible conversation thread. For group chat replies, set thread_id to the source thread_id from the incoming_message. Plain assistant text is private and is not posted to the group. Use result only for a concise final result worth the user's attention. Use question only when blocked on the user. Use update only for important progress. Use decline to explicitly choose silence when the right outcome is no visible answer. Silence is valid; do not post routine status.",
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"kind": map[string]any{
					"type":        "string",
					"enum":        []string{"result", "question", "update", "decline"},
					"description": "result appears as a signed result card; question asks the user for blocking input; update belongs in a task thread and requires thread_id; decline records one short reason shown as muted text and is allowed even after you have completed.",
				},
				"text": map[string]any{
					"type":        "string",
					"description": "Markdown text to show in the conversation under this participant's identity. For decline it is the one short reason shown as muted text.",
				},
				"thread_id": map[string]any{
					"type":        "string",
					"description": "Target conversation thread id. For group replies, use the incoming_message thread_id. Leaving thread_id empty posts to your own DM. Resident named agents may post to threads they belong to or their own DM thread. Pass a reply subthread id (cth-…) to fold the message into that reply thread instead of the main group stream.",
				},
				"basis_seq": map[string]any{
					"type":        "integer",
					"description": "The message number (seq) you are generating this post against — your view of the latest message you have accounted for in this thread. On submit the system mechanically checks it: if the thread has NOT moved past this basis, the post lands; if someone else posted after it, the post is HELD and returned with what arrived so you can re-reason against the new state. Omit (0) to use your read cursor as the basis.",
				},
				"force": map[string]any{
					"type":        "boolean",
					"description": "Set true to publish even if the thread moved past your basis. Leave unset first: if new messages arrived while you were composing, the post is HELD (not published) and returned to you with what arrived, so you can revise, resend with force=true, or stay silent instead of posting something now redundant or stale.",
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
		BasisSeq int    `json:"basis_seq"`
		Force    bool   `json:"force"`
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
	msg, err := speech.PostMessage(ctx, kind, params.Text, params.ThreadID, params.BasisSeq, params.Force)
	if err != nil {
		return "", err
	}
	if msg.Held {
		result := map[string]any{
			"action":                   "post_message",
			"status":                   "held",
			"reason":                   "The thread moved past your basis — these messages arrived while you were composing. Your draft was NOT posted. If the arrivals change your view of the room or would otherwise bloat this resident context, consider inception before deciding whether to revise, resend with force=true, or stay silent. Post again with an updated basis_seq only if a visible reply is still needed.",
			"arrived_since_your_basis": msg.HeldNote,
		}
		data, err := json.Marshal(result)
		if err != nil {
			return "", err
		}
		return string(data), nil
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

func (s agentControlParticipantSpeech) PostMessage(ctx context.Context, kind, text, targetThreadID string, _ int, _ bool) (PostedMessage, error) {
	// The agent-control fallback (worker speech) has no room-freshness state, so
	// basis_seq/force are not meaningful here — it always publishes.
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
