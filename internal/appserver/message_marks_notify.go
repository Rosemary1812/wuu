package appserver

import (
	"errors"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/session"
)

// MessageMarkWire is one message_marks row on the wire (read receipts +
// reactions). A message is addressed by (thread, Seq); Kind is "seen"
// (Status carries the lifecycle) or "reaction" (Reaction carries the key).
type MessageMarkWire struct {
	Seq           int    `json:"seq"`
	ParticipantID string `json:"participant_id"`
	Kind          string `json:"kind"`
	Status        string `json:"status,omitempty"`
	Reaction      string `json:"reaction,omitempty"`
	AtMS          int64  `json:"at_ms"`
}

type ThreadMarksParams struct {
	ThreadID string `json:"thread_id"`
}

type ThreadMarksResult struct {
	Marks []MessageMarkWire `json:"marks"`
}

// handleThreadMarks (thread/marks) returns every read-receipt and reaction row
// for a thread so the chat view can render seen rings and reaction chips on
// load. Live changes arrive incrementally via NotificationMessageMark.
func (s *Server) handleThreadMarks(req Request) error {
	var params ThreadMarksParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	threadID := strings.TrimSpace(params.ThreadID)
	if threadID == "" {
		return s.writeResponse(req.ID, nil, errors.New("thread_id is required"))
	}
	marks, err := session.ListMessageMarks(s.rt.SessionDir, threadID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	wire := make([]MessageMarkWire, 0, len(marks))
	for _, m := range marks {
		w := MessageMarkWire{
			Seq:           m.Seq,
			ParticipantID: m.ParticipantID,
			Kind:          m.Kind,
			AtMS:          m.At.UnixMilli(),
		}
		if m.Kind == session.MessageMarkKindReaction {
			w.Reaction = m.Payload
		} else {
			w.Status = m.Status
		}
		wire = append(wire, w)
	}
	return s.writeResponse(req.ID, ThreadMarksResult{Marks: wire}, nil)
}

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
