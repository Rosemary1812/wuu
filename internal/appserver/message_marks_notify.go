package appserver

import (
	"time"

	"github.com/blueberrycongee/wuu/internal/session"
)

// MessageMarkNotification is one read-receipt or reaction change for a single
// message, pushed so a live chat view can patch that bubble in place
// (2026-07-04-read-receipts-and-reactions.md §5). A message is addressed by
// (ThreadID, Seq). Kind is "seen" (Status carries the lifecycle) or "reaction"
// (Reaction carries the key).
type MessageMarkNotification struct {
	ThreadID      string `json:"thread_id"`
	Seq           int    `json:"seq"`
	ParticipantID string `json:"participant_id"`
	Kind          string `json:"kind"`
	Status        string `json:"status,omitempty"`
	Reaction      string `json:"reaction,omitempty"`
	AtMS          int64  `json:"at_ms"`
}

func (s *Server) notifyMessageReaction(threadID string, seq int, participantID, reaction string) {
	if s == nil {
		return
	}
	_ = s.writeNotification(NotificationMessageMark, MessageMarkNotification{
		ThreadID:      threadID,
		Seq:           seq,
		ParticipantID: participantID,
		Kind:          session.MessageMarkKindReaction,
		Reaction:      reaction,
		AtMS:          time.Now().UTC().UnixMilli(),
	})
}

func (s *Server) notifyMessageSeen(threadID string, seq int, participantID, status string) {
	if s == nil {
		return
	}
	_ = s.writeNotification(NotificationMessageMark, MessageMarkNotification{
		ThreadID:      threadID,
		Seq:           seq,
		ParticipantID: participantID,
		Kind:          session.MessageMarkKindSeen,
		Status:        status,
		AtMS:          time.Now().UTC().UnixMilli(),
	})
}
