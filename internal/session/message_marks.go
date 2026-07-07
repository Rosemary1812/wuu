package session

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Message mark kinds and seen-lifecycle statuses
// (2026-07-04-read-receipts-and-reactions.md §3/§4).
const (
	MessageMarkKindSeen     = "seen"
	MessageMarkKindReaction = "reaction"
	// MessageMarkKindMention records that a message @-addressed a participant.
	// Routing writes one per mentioned member at delivery time; the pull inbox
	// reads them to rebuild each envelope's addressed flag (which member a
	// message must be answered by). Under push this flag rode inside each
	// per-member enqueued envelope; under pull the message lives once in
	// history, so "who was addressed" is a durable side record.
	MessageMarkKindMention = "mention"

	// SeenStatus* are the lifecycle of a (message, agent) read receipt.
	// "seen" in the product sense means SeenStatusCompleted — the turn that
	// consumed the message finished. SeenStatusInProgress marks that the
	// turn started (envelope consumed) but has not finished; SeenStatusFailed
	// marks a turn that started and then errored/was interrupted. There is no
	// stored "queued" row: the absence of a seen row is "unread".
	//
	// SeenStatusExpiredUnprocessed is the boot-settle terminal state for
	// issue #3 pivot (replay → settle/expire): a row left in_progress by
	// a turn that started but did not finish before the previous process
	// died gets flipped to this status by settleOnBoot at startup, so the
	// front-end can distinguish "system error" (failed) from "we just
	// didn't get a turn before the user closed the laptop"
	// (expired_unprocessed). It is a terminal state — no further
	// transition out of it. Note: MarkMessageSeen (regular-session
	// writes) does NOT accept this value — boot uses a dedicated helper
	// that does the in_progress → expired_unprocessed flip in one
	// direct UPDATE, not via UpsertMessageMark.
	SeenStatusInProgress         = "in_progress"
	SeenStatusCompleted          = "completed"
	SeenStatusFailed             = "failed"
	SeenStatusExpiredUnprocessed = "expired_unprocessed"
)

// MessageMark is one row of message_marks: a per-(message, participant, kind)
// annotation. A message is addressed by (SessionID, Seq).
type MessageMark struct {
	SessionID     string
	Seq           int
	ParticipantID string
	Kind          string
	Status        string // seen: in_progress|completed|failed; reaction: ""
	Payload       string // reaction: reaction key; seen: ""
	At            time.Time
}

// UpsertMessageMark writes (or replaces) one mark. The primary key is
// (session_id, seq, participant_id, kind), so re-marking the same message by
// the same participant updates status/payload/at in place — a seen record
// advancing in_progress → completed, or a reaction being swapped, is a single
// evolving row, never a duplicate.
func UpsertMessageMark(sessDir string, mark MessageMark) error {
	mark.SessionID = strings.TrimSpace(mark.SessionID)
	mark.ParticipantID = strings.TrimSpace(mark.ParticipantID)
	mark.Kind = strings.TrimSpace(mark.Kind)
	if mark.SessionID == "" {
		return errors.New("session_id is required")
	}
	if mark.Seq <= 0 {
		return errors.New("seq is required")
	}
	if mark.ParticipantID == "" {
		return errors.New("participant_id is required")
	}
	switch mark.Kind {
	case MessageMarkKindSeen, MessageMarkKindReaction, MessageMarkKindMention:
	default:
		return fmt.Errorf("unsupported message mark kind %q", mark.Kind)
	}
	if mark.At.IsZero() {
		mark.At = time.Now().UTC()
	} else {
		mark.At = mark.At.UTC()
	}

	db, err := openStore(sessDir)
	if err != nil {
		return err
	}
	defer db.Close()

	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin message mark upsert: %w", err)
	}
	defer tx.Rollback()
	if ok, err := sessionExistsTx(tx, mark.SessionID); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("%w: %q", ErrSessionNotFound, mark.SessionID)
	}
	if _, err := tx.Exec(`
INSERT INTO message_marks (session_id, seq, participant_id, kind, status, payload, at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(session_id, seq, participant_id, kind)
DO UPDATE SET status = excluded.status, payload = excluded.payload, at = excluded.at`,
		mark.SessionID, mark.Seq, mark.ParticipantID, mark.Kind, mark.Status, mark.Payload, mark.At.UnixMilli(),
	); err != nil {
		return fmt.Errorf("upsert message mark: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit message mark upsert: %w", err)
	}
	return nil
}

// MarkMessageSeen records/advances a read receipt for one (message, agent).
func MarkMessageSeen(sessDir, sessionID string, seq int, participantID, status string, at time.Time) error {
	switch status {
	case SeenStatusInProgress, SeenStatusCompleted, SeenStatusFailed:
	default:
		return fmt.Errorf("unsupported seen status %q", status)
	}
	return UpsertMessageMark(sessDir, MessageMark{
		SessionID:     sessionID,
		Seq:           seq,
		ParticipantID: participantID,
		Kind:          MessageMarkKindSeen,
		Status:        status,
		At:            at,
	})
}

// SetMessageReaction records/replaces one participant's reaction on a message.
func SetMessageReaction(sessDir, sessionID string, seq int, participantID, reaction string, at time.Time) error {
	if strings.TrimSpace(reaction) == "" {
		return errors.New("reaction is required")
	}
	return UpsertMessageMark(sessDir, MessageMark{
		SessionID:     sessionID,
		Seq:           seq,
		ParticipantID: participantID,
		Kind:          MessageMarkKindReaction,
		Payload:       strings.TrimSpace(reaction),
		At:            at,
	})
}

// ListMessageMarks returns every mark in a thread (both seen and reaction
// rows), ordered by message then time. The caller aggregates for display.
// ThreadReadWatermark is the durable read cursor for one participant in one
// thread: the highest message seq they have a seen receipt for. It is the
// single source of truth for "how far has this agent read this thread",
// shared by the held-draft freshness check (has the thread moved past what I
// read) and, going forward, by pull-style inbox reads (give me messages past
// my watermark). Returns 0 when the participant has read nothing in the thread.
func ThreadReadWatermark(sessDir, sessionID, participantID string) (int, error) {
	sessionID = strings.TrimSpace(sessionID)
	participantID = strings.TrimSpace(participantID)
	if sessionID == "" || participantID == "" {
		return 0, nil
	}
	db, err := openStore(sessDir)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var seq sql.NullInt64
	if err := db.QueryRow(`
SELECT MAX(seq) FROM message_marks
WHERE session_id = ? AND participant_id = ? AND kind = ?`,
		sessionID, participantID, MessageMarkKindSeen).Scan(&seq); err != nil {
		return 0, fmt.Errorf("thread read watermark: %w", err)
	}
	if !seq.Valid {
		return 0, nil
	}
	return int(seq.Int64), nil
}

// MarkMessageMention records that message `seq` in a thread @-addressed
// `participantID`. Written by routing at delivery time so the pull inbox can
// rebuild the addressed flag. Idempotent (upsert).
func MarkMessageMention(sessDir, sessionID string, seq int, participantID string, at time.Time) error {
	return UpsertMessageMark(sessDir, MessageMark{
		SessionID: sessionID, Seq: seq, ParticipantID: participantID,
		Kind: MessageMarkKindMention, At: at,
	})
}

// MentionedSeqs returns the set of message seqs in a thread that @-addressed
// the given participant — the pull inbox reads it once per thread to stamp
// each pulled message's addressed flag.
func MentionedSeqs(sessDir, sessionID, participantID string) (map[int]bool, error) {
	sessionID = strings.TrimSpace(sessionID)
	participantID = strings.TrimSpace(participantID)
	out := map[int]bool{}
	if sessionID == "" || participantID == "" {
		return out, nil
	}
	db, err := openStore(sessDir)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`
SELECT seq FROM message_marks
WHERE session_id = ? AND participant_id = ? AND kind = ?`,
		sessionID, participantID, MessageMarkKindMention)
	if err != nil {
		return nil, fmt.Errorf("mentioned seqs: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var seq int
		if err := rows.Scan(&seq); err != nil {
			return nil, fmt.Errorf("scan mentioned seq: %w", err)
		}
		out[seq] = true
	}
	return out, rows.Err()
}

func ListMessageMarks(sessDir, sessionID string) ([]MessageMark, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, errors.New("session_id is required")
	}
	db, err := openStore(sessDir)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if ok, err := sessionExistsDB(db, sessionID); err != nil {
		return nil, err
	} else if !ok {
		return nil, fmt.Errorf("%w: %q", ErrSessionNotFound, sessionID)
	}

	rows, err := db.Query(`
SELECT session_id, seq, participant_id, kind, status, payload, at
FROM message_marks
WHERE session_id = ?
ORDER BY seq ASC, at ASC, participant_id ASC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list message marks: %w", err)
	}
	defer rows.Close()

	var marks []MessageMark
	for rows.Next() {
		var mark MessageMark
		var at int64
		if err := rows.Scan(&mark.SessionID, &mark.Seq, &mark.ParticipantID, &mark.Kind, &mark.Status, &mark.Payload, &at); err != nil {
			return nil, fmt.Errorf("scan message marks: %w", err)
		}
		mark.At = time.UnixMilli(at).UTC()
		marks = append(marks, mark)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan message marks: %w", err)
	}
	return marks, nil
}
