package agentcontrol

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/subagent"
)

// ParticipantMessage is a visible conversation post authored by a worker
// participant. AgentControl owns the rate limit; app-server owns persistence
// and UI notification.
type ParticipantMessage struct {
	AgentID       string
	ParticipantID string
	AgentPath     string
	ParentID      string
	TaskName      string
	AgentType     string
	Kind          string
	Text          string
	CreatedAt     time.Time
}

// SubscribeParticipantMessages registers a listener for visible participant
// posts. Receivers must drain promptly; PostParticipantMessage blocks until
// every active listener accepts the post or the caller's context is canceled.
func (c *AgentControl) SubscribeParticipantMessages(ch chan<- ParticipantMessage) {
	if c == nil || ch == nil {
		return
	}
	c.participantMessagesMu.Lock()
	defer c.participantMessagesMu.Unlock()
	c.participantMessages = append(c.participantMessages, ch)
}

func (c *AgentControl) UnsubscribeParticipantMessages(ch chan<- ParticipantMessage) {
	if c == nil || ch == nil {
		return
	}
	c.participantMessagesMu.Lock()
	defer c.participantMessagesMu.Unlock()
	for i, listener := range c.participantMessages {
		if listener == ch {
			c.participantMessages = append(c.participantMessages[:i], c.participantMessages[i+1:]...)
			return
		}
	}
}

// EnableParticipantSpeech grants one agent run permission to call
// conversation-native participant speech tools. Ordinary subagents never call
// this path.
func (c *AgentControl) EnableParticipantSpeech(agentID string) {
	if c == nil {
		return
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return
	}
	c.participantMessagesMu.Lock()
	defer c.participantMessagesMu.Unlock()
	if c.participantSpeech == nil {
		c.participantSpeech = make(map[string]struct{})
	}
	c.participantSpeech[agentID] = struct{}{}
}

func (c *AgentControl) ParticipantSpeechEnabled(agentID string) bool {
	if c == nil {
		return false
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return false
	}
	c.participantMessagesMu.Lock()
	defer c.participantMessagesMu.Unlock()
	_, ok := c.participantSpeech[agentID]
	return ok
}

// PostParticipantMessage records that a worker explicitly chose to occupy the
// visible conversation stream. Phase 2 only supports result posts.
func (c *AgentControl) PostParticipantMessage(ctx context.Context, agentID, kind, text string) (ParticipantMessage, error) {
	if c == nil || c.manager == nil {
		return ParticipantMessage{}, errors.New("post_message: agent control not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return ParticipantMessage{}, errors.New("post_message: agent_id is required")
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" {
		kind = "result"
	}
	if kind != "result" {
		return ParticipantMessage{}, fmt.Errorf("post_message: kind %q is not supported yet", kind)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return ParticipantMessage{}, errors.New("post_message: text is required")
	}

	worker := c.manager.Get(agentID)
	if worker == nil {
		return ParticipantMessage{}, fmt.Errorf("post_message: unknown agent %q", agentID)
	}
	snap := worker.Snapshot()
	if !canParticipantPost(snap.Status) {
		return ParticipantMessage{}, fmt.Errorf("post_message: agent %q is %s", agentID, snap.Status)
	}
	if !c.ParticipantSpeechEnabled(agentID) {
		return ParticipantMessage{}, fmt.Errorf("post_message: agent %q does not have participant speech capability", agentID)
	}
	msg := ParticipantMessage{
		AgentID:       snap.ID,
		ParticipantID: snap.ParticipantID,
		AgentPath:     snap.AgentPath,
		ParentID:      snap.ParentID,
		TaskName:      snap.TaskName,
		AgentType:     snap.Type,
		Kind:          kind,
		Text:          text,
		CreatedAt:     time.Now().UTC(),
	}

	c.participantMessagesMu.Lock()
	if c.participantResultPosts == nil {
		c.participantResultPosts = make(map[string]struct{})
	}
	if _, exists := c.participantResultPosts[agentID]; exists {
		c.participantMessagesMu.Unlock()
		return ParticipantMessage{}, fmt.Errorf("post_message: agent %q already posted a result", agentID)
	}
	listeners := append([]chan<- ParticipantMessage(nil), c.participantMessages...)
	if len(listeners) == 0 {
		c.participantMessagesMu.Unlock()
		return ParticipantMessage{}, errors.New("post_message: no conversation message sink is attached")
	}
	c.participantResultPosts[agentID] = struct{}{}
	c.participantMessagesMu.Unlock()

	for _, listener := range listeners {
		select {
		case listener <- msg:
		case <-ctx.Done():
			return ParticipantMessage{}, ctx.Err()
		}
	}
	return msg, nil
}

func canParticipantPost(status subagent.Status) bool {
	switch status {
	case subagent.StatusPending, subagent.StatusQueued, subagent.StatusRunning:
		return true
	default:
		return false
	}
}
