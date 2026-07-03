package appserver

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// MessageEnvelope is the compact unit that enters a resident agent's
// context. One envelope carries exactly one new message -- never room
// history. Agents pull surrounding context with fetch_thread_messages.
type MessageEnvelope struct {
	ID                  string    `json:"id"`
	SourceThreadID      string    `json:"source_thread_id"`
	SourceTitle         string    `json:"source_title"`
	SenderKind          string    `json:"sender_kind"`
	SenderName          string    `json:"sender_name"`
	SenderParticipantID string    `json:"sender_participant_id,omitempty"`
	Addressed           bool      `json:"addressed"`
	Hop                 int       `json:"hop"`
	Text                string    `json:"text"`
	CreatedAt           time.Time `json:"created_at"`
}

// Prompt renders the envelope into the user-role message injected into
// the resident thread. Format is load-bearing: the resident system prompt
// teaches the agent to read these attributes.
func (e MessageEnvelope) Prompt() string {
	return fmt.Sprintf(
		"<incoming_message thread=%q thread_id=%q from=%q sender=%q addressed=%q hop=%q>\n%s\n</incoming_message>",
		e.SourceTitle, e.SourceThreadID, e.SenderKind, e.SenderName,
		strconv.FormatBool(e.Addressed), strconv.Itoa(e.Hop),
		strings.TrimSpace(e.Text),
	)
}

func coalesceEnvelopes(envs []MessageEnvelope) string {
	prompts := make([]string, 0, len(envs))
	for _, env := range envs {
		prompt := strings.TrimSpace(env.Prompt())
		if prompt != "" {
			prompts = append(prompts, prompt)
		}
	}
	if len(prompts) == 0 {
		return ""
	}
	joined := strings.Join(prompts, "\n\n")
	if len(prompts) == 1 {
		return joined
	}
	return fmt.Sprintf("You received %d messages while busy:\n\n%s", len(prompts), joined)
}
