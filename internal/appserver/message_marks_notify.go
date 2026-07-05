package appserver

import (
	"errors"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/tools"
)

// humanReactionParticipantID is the stable identity a reaction is attributed to
// when a human stamps an emoji from the chat UI (message/react). Agents react
// as their own named participant via the react tool; the human has no roster
// row, so its reactions all fold under this one id (one reaction per message,
// like every other participant).
const humanReactionParticipantID = "human"

// ensureHumanParticipant idempotently upserts the stable KindHuman roster row
// the human's chat actions (reactions, reply-thread posts) are attributed to.
// message_marks / participant messages both carry a FK on participant_id ->
// participants(id), and the human has no named roster row, so callers must seed
// this before attributing anything to humanReactionParticipantID. KindHuman is
// filtered out of every named-participant listing, so this never pollutes the
// group roster or the weak-isolation fan-out set.
func (s *Server) ensureHumanParticipant() error {
	return session.UpsertParticipant(s.rt.SessionDir, participant.Participant{
		ID:   humanReactionParticipantID,
		Kind: participant.KindHuman,
		Name: "你",
	})
}

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

// MessageReactParams is the human-side counterpart to the agent react tool:
// the chat UI stamps one reaction (a key from the shared vocabulary) on the
// message addressed by (ThreadID, Seq). ParticipantID is optional and defaults
// to the human identity; agents never reach this RPC (they only have tools), so
// this is a human-only affordance like escalate/bubble.
type MessageReactParams struct {
	ThreadID      string `json:"thread_id"`
	Seq           int    `json:"seq"`
	Reaction      string `json:"reaction"`
	ParticipantID string `json:"participant_id,omitempty"`
}

type MessageReactResult struct {
	OK bool `json:"ok"`
}

// handleMessageReact (message/react) records a human reaction on a message and
// notifies clients. Like the react tool it is terminal: it never routes, wakes
// an agent, or generates an envelope — reactions are a lightweight UI event
// (2026-07-04-read-receipts-and-reactions.md §3 invariant 1). The reaction key
// is validated against the same shared vocabulary the agent tool uses.
func (s *Server) handleMessageReact(req Request) error {
	var params MessageReactParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	threadID := strings.TrimSpace(params.ThreadID)
	if threadID == "" {
		return s.writeResponse(req.ID, nil, errors.New("thread_id is required"))
	}
	if params.Seq <= 0 {
		return s.writeResponse(req.ID, nil, errors.New("seq is required"))
	}
	reaction := strings.TrimSpace(params.Reaction)
	if !tools.IsReactionKey(reaction) {
		return s.writeResponse(req.ID, nil, errors.New("unknown reaction key"))
	}
	participantID := strings.TrimSpace(params.ParticipantID)
	if participantID == "" {
		participantID = humanReactionParticipantID
	}
	// message_marks has a FK on participant_id -> participants(id). The human has
	// no roster row (rosters list only KindNamed), so ensure a stable KindHuman
	// participant exists before attributing a reaction to it. Idempotent upsert;
	// KindHuman is filtered out of every named-participant listing so this never
	// pollutes the group roster.
	if participantID == humanReactionParticipantID {
		if err := s.ensureHumanParticipant(); err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
	}
	if err := s.recordParticipantReaction(threadID, params.Seq, participantID, reaction); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, MessageReactResult{OK: true}, nil)
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
