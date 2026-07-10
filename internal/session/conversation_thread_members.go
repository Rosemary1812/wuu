package session

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/participant"
)

// conversation_thread_members backs the weak-isolation participant subset of a
// reply subthread (cth-*): only these participants are pushed the subthread's
// traffic into their context. It is deliberately separate from thread_members —
// that table keys on session_id (a top-level thread), and a cth is not a
// session but a row in conversation_threads. The FK therefore points at
// conversation_threads(id) so deleting a subthread (or retiring a participant)
// cascades the membership away.

func conversationThreadExists(q sqlRowQuerier, subthreadID string) (bool, error) {
	var count int
	if err := q.QueryRow(`SELECT COUNT(*) FROM conversation_threads WHERE id = ?`, strings.TrimSpace(subthreadID)).Scan(&count); err != nil {
		return false, fmt.Errorf("check conversation thread exists: %w", err)
	}
	return count > 0, nil
}

// AddConversationThreadMember adds one active named participant to a reply
// subthread's member subset. It is idempotent (ON CONFLICT DO NOTHING) so
// seeding the opener and auto-joining a poster can both run without guarding
// for an existing row.
func AddConversationThreadMember(sessDir, subthreadID, participantID string) error {
	subthreadID = strings.TrimSpace(subthreadID)
	participantID = strings.TrimSpace(participantID)
	if subthreadID == "" {
		return errors.New("subthread_id is required")
	}
	if participantID == "" {
		return errors.New("participant_id is required")
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
		return fmt.Errorf("begin conversation thread member add: %w", err)
	}
	defer tx.Rollback()
	var parentSessionID string
	if err := tx.QueryRow(`SELECT session_id FROM conversation_threads WHERE id = ?`, subthreadID).Scan(&parentSessionID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: %q", ErrConversationThreadNotFound, subthreadID)
		}
		return fmt.Errorf("load conversation thread parent: %w", err)
	}
	var isGroup int
	if err := tx.QueryRow(`SELECT is_group FROM sessions WHERE id = ?`, parentSessionID).Scan(&isGroup); err != nil {
		return fmt.Errorf("load conversation thread parent session: %w", err)
	}
	if isGroup != 0 {
		if err := requireActiveNamedThreadMember(tx, parentSessionID, participantID); err != nil {
			return fmt.Errorf("conversation thread member: %w", err)
		}
	} else if err := requireActiveNamedParticipant(tx, participantID); err != nil {
		return err
	}
	if _, err := tx.Exec(`
INSERT INTO conversation_thread_members (conversation_thread_id, participant_id, joined_at)
VALUES (?, ?, ?)
ON CONFLICT(conversation_thread_id, participant_id) DO NOTHING`,
		subthreadID, participantID, time.Now().UTC().UnixMilli(),
	); err != nil {
		return fmt.Errorf("add conversation thread member: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit conversation thread member add: %w", err)
	}
	return nil
}

// RemoveConversationThreadMember drops a participant from a reply subthread's
// member subset.
func RemoveConversationThreadMember(sessDir, subthreadID, participantID string) error {
	subthreadID = strings.TrimSpace(subthreadID)
	participantID = strings.TrimSpace(participantID)
	if subthreadID == "" {
		return errors.New("subthread_id is required")
	}
	if participantID == "" {
		return errors.New("participant_id is required")
	}

	db, err := openStore(sessDir)
	if err != nil {
		return err
	}
	defer db.Close()

	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()

	var status ConversationThreadStatus
	var ownerID, leadID string
	err = db.QueryRow(`
SELECT status, thread_owner_participant_id, lead_participant_id
FROM conversation_threads
WHERE id = ?`, subthreadID).Scan(&status, &ownerID, &leadID)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %q", ErrConversationThreadNotFound, subthreadID)
	}
	if err != nil {
		return fmt.Errorf("load conversation thread member authority: %w", err)
	}
	if status == ConversationThreadOpen && participantID == ownerID {
		return fmt.Errorf("participant %q owns open Thread %q and must remain a member", participantID, subthreadID)
	}
	if status == ConversationThreadTask && participantID == leadID {
		return fmt.Errorf("participant %q leads active Task %q and must remain a member", participantID, subthreadID)
	}
	if _, err := db.Exec(`DELETE FROM conversation_thread_members WHERE conversation_thread_id = ? AND participant_id = ?`, subthreadID, participantID); err != nil {
		return fmt.Errorf("remove conversation thread member: %w", err)
	}
	return nil
}

// ListConversationThreadMembers returns the active named participants that make
// up a reply subthread's weak-isolation subset, ordered by join time. Retired
// and non-named participants are excluded (mirrors ListThreadMembers), so the
// routing fan-out never wakes a worker or a retired agent.
func ListConversationThreadMembers(sessDir, subthreadID string) ([]string, error) {
	subthreadID = strings.TrimSpace(subthreadID)
	if subthreadID == "" {
		return nil, errors.New("subthread_id is required")
	}
	db, err := openStore(sessDir)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if ok, err := conversationThreadExists(db, subthreadID); err != nil {
		return nil, err
	} else if !ok {
		return nil, fmt.Errorf("%w: %q", ErrConversationThreadNotFound, subthreadID)
	}

	rows, err := db.Query(`
SELECT ctm.participant_id
FROM conversation_thread_members ctm
JOIN conversation_threads ct ON ct.id = ctm.conversation_thread_id
JOIN sessions s ON s.id = ct.session_id
JOIN participants p ON p.id = ctm.participant_id
LEFT JOIN thread_members tm
  ON tm.session_id = ct.session_id AND tm.participant_id = ctm.participant_id
WHERE ctm.conversation_thread_id = ?
  AND p.kind = ?
  AND p.retired_at IS NULL
  AND (s.is_group = 0 OR tm.participant_id IS NOT NULL)
ORDER BY ctm.joined_at ASC, ctm.participant_id ASC`, subthreadID, string(participant.KindNamed))
	if err != nil {
		return nil, fmt.Errorf("list conversation thread members: %w", err)
	}
	defer rows.Close()

	var members []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan conversation thread members: %w", err)
		}
		members = append(members, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan conversation thread members: %w", err)
	}
	return members, nil
}
