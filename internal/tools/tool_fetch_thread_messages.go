package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/capability"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/statepath"
)

const (
	fetchThreadMessagesName       = "fetch_thread_messages"
	fetchThreadMessagesDefault    = 20
	fetchThreadMessagesMax        = 30
	fetchThreadMessageTextMaxRune = 500
)

type FetchThreadMessagesTool struct {
	env *Env
}

func NewFetchThreadMessagesTool(env *Env) *FetchThreadMessagesTool {
	return &FetchThreadMessagesTool{env: env}
}

func (t *FetchThreadMessagesTool) Name() string { return fetchThreadMessagesName }

func (t *FetchThreadMessagesTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name:        fetchThreadMessagesName,
		Description: "Fetch the recent visible messages from a conversation thread this resident named agent belongs to. Use this when an incoming envelope is too thin and you need surrounding context. Returns only user, assistant, and participant posts; no tool calls or reasoning.",
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"thread_id"},
			"properties": map[string]any{
				"thread_id": map[string]any{
					"type":        "string",
					"description": "Conversation thread id to read. The resident named agent must be a member of this thread or the bound participant of its DM thread.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum number of recent messages to return. Defaults to 20 and is capped at 30.",
					"minimum":     1,
					"maximum":     fetchThreadMessagesMax,
				},
			},
		},
	}
}

func (t *FetchThreadMessagesTool) Execute(ctx context.Context, args string) (string, error) {
	_ = ctx
	if t == nil || t.env == nil {
		return "", errors.New("fetch_thread_messages: env is not configured")
	}
	participantID := strings.TrimSpace(t.env.ParticipantID)
	if participantID == "" {
		return "", errors.New("fetch_thread_messages: participant_id is required")
	}
	var params struct {
		ThreadID string `json:"thread_id"`
		Limit    int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(args), &params); err != nil {
		return "", fmt.Errorf("fetch_thread_messages: parse args: %w", err)
	}
	threadID := strings.TrimSpace(params.ThreadID)
	if threadID == "" {
		return "", errors.New("fetch_thread_messages: thread_id is required")
	}
	limit := normalizeFetchThreadMessagesLimit(params.Limit)
	sessDir, err := conversationSessionDir(t.env)
	if err != nil {
		return "", err
	}
	if err := requireFetchThreadMembership(sessDir, threadID, participantID); err != nil {
		return "", err
	}
	records, err := session.LoadHistoryRecords(sessDir, threadID, false)
	if err != nil {
		return "", fmt.Errorf("fetch_thread_messages: load history: %w", err)
	}
	messages := visibleFetchedThreadMessages(records)
	truncated := false
	if len(messages) > limit {
		truncated = true
		messages = messages[len(messages)-limit:]
	}
	text := fetchedThreadMessagesText(messages)
	data, err := json.Marshal(map[string]any{
		"thread_id": threadID,
		"limit":     limit,
		"truncated": truncated,
		"messages":  messages,
		"text":      text,
	})
	if err != nil {
		return "", fmt.Errorf("fetch_thread_messages: marshal result: %w", err)
	}
	return string(data), nil
}

func (t *FetchThreadMessagesTool) IsReadOnly() bool { return true }

func (t *FetchThreadMessagesTool) IsConcurrencySafe() bool { return true }

func (t *FetchThreadMessagesTool) Classify(string) ToolClassification {
	return ToolClassification{
		ReadOnly:        true,
		ConcurrencySafe: true,
		Risk:            ToolRiskLow,
		Reason:          "read-only recent conversation messages",
	}
}

func (t *FetchThreadMessagesTool) DeclaredCapability() (capability.Capability, bool) {
	return capability.CapabilityTaskCommunicate, true
}

type fetchedThreadMessage struct {
	Role          string    `json:"role"`
	Sender        string    `json:"sender"`
	Text          string    `json:"text"`
	At            time.Time `json:"at,omitempty"`
	ParticipantID string    `json:"participant_id,omitempty"`
	PostKind      string    `json:"post_kind,omitempty"`
}

func normalizeFetchThreadMessagesLimit(limit int) int {
	if limit <= 0 {
		return fetchThreadMessagesDefault
	}
	if limit > fetchThreadMessagesMax {
		return fetchThreadMessagesMax
	}
	return limit
}

func conversationSessionDir(env *Env) (string, error) {
	if env == nil {
		return "", errors.New("fetch_thread_messages: env is not configured")
	}
	if dir := strings.TrimSpace(env.ConversationSessionDir); dir != "" {
		return dir, nil
	}
	home, err := statepath.Home("")
	if err != nil {
		return "", fmt.Errorf("fetch_thread_messages: resolve wuu home: %w", err)
	}
	dir := statepath.SessionsDir(home)
	if strings.TrimSpace(dir) == "" {
		return "", errors.New("fetch_thread_messages: sessions dir is empty")
	}
	return dir, nil
}

func requireFetchThreadMembership(sessDir, threadID, participantID string) error {
	meta, ok, err := session.Find(sessDir, threadID)
	if err != nil {
		return fmt.Errorf("fetch_thread_messages: lookup thread: %w", err)
	}
	if !ok {
		return fmt.Errorf("%w: %q", session.ErrSessionNotFound, threadID)
	}
	if strings.TrimSpace(meta.DMParticipantID) == participantID {
		return nil
	}
	members, err := session.ListThreadMembers(sessDir, threadID)
	if err != nil {
		return fmt.Errorf("fetch_thread_messages: list thread members: %w", err)
	}
	for _, memberID := range members {
		if strings.TrimSpace(memberID) == participantID {
			return nil
		}
	}
	return fmt.Errorf("fetch_thread_messages: participant %q is not a member of thread %q", participantID, threadID)
}

func visibleFetchedThreadMessages(records []session.HistoryRecord) []fetchedThreadMessage {
	out := make([]fetchedThreadMessage, 0, len(records))
	for _, rec := range records {
		if rec.Hidden {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(rec.Role))
		text := strings.TrimSpace(rec.DisplayContent)
		if text == "" {
			text = strings.TrimSpace(rec.Content)
		}
		if text == "" {
			continue
		}
		msg := fetchedThreadMessage{
			Role: strings.TrimSpace(role),
			Text: truncateRunes(text, fetchThreadMessageTextMaxRune),
			At:   rec.At,
		}
		switch role {
		case "user":
			msg.Sender = firstNonEmpty(strings.TrimSpace(rec.Name), "User")
		case "assistant":
			msg.Sender = firstNonEmpty(strings.TrimSpace(rec.Name), "Assistant")
		case "participant":
			msg.Sender = firstNonEmpty(strings.TrimSpace(rec.Name), strings.TrimSpace(rec.ParticipantID), "Participant")
			msg.ParticipantID = strings.TrimSpace(rec.ParticipantID)
			msg.PostKind = strings.TrimSpace(rec.PostKind)
		default:
			continue
		}
		out = append(out, msg)
	}
	return out
}

func fetchedThreadMessagesText(messages []fetchedThreadMessage) string {
	lines := make([]string, 0, len(messages))
	for _, msg := range messages {
		lines = append(lines, fmt.Sprintf("[%s] %s", msg.Sender, msg.Text))
	}
	return strings.Join(lines, "\n")
}

func truncateRunes(text string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= maxRunes {
		return string(runes)
	}
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-3]) + "..."
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
