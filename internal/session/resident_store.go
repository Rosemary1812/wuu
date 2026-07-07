package session

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/participant"
)

type ResidentEnvelope struct {
	ID            string
	ParticipantID string
	EnvelopeJSON  json.RawMessage
	CreatedAt     time.Time
	ConsumedAt    *time.Time
	// ExpiredAt is the issue #3 pivot's boot-settle timestamp: set by
	// MarkPendingResidentEnvelopesExpired when a previous process died
	// with this row still unprocessed. Pending = NULL; processed =
	// non-NULL ConsumedAt; expired = NULL ConsumedAt AND non-NULL
	// ExpiredAt. The pivot invariant "boot can't burn tokens" relies on
	// the partial index idx_resident_inbox_pending excluding expired
	// rows (see session.go for the migration).
	ExpiredAt *time.Time
}

type sqlRowQuerier interface {
	QueryRow(query string, args ...any) *sql.Row
}

func AddThreadMember(sessDir, sessionID, participantID string) error {
	sessionID = strings.TrimSpace(sessionID)
	participantID = strings.TrimSpace(participantID)
	if sessionID == "" {
		return errors.New("session_id is required")
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
		return fmt.Errorf("begin thread member add: %w", err)
	}
	defer tx.Rollback()
	if ok, err := sessionExistsTx(tx, sessionID); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("%w: %q", ErrSessionNotFound, sessionID)
	}
	if err := requireActiveNamedParticipant(tx, participantID); err != nil {
		return err
	}
	if _, err := tx.Exec(`
INSERT INTO thread_members (session_id, participant_id, joined_at)
VALUES (?, ?, ?)
ON CONFLICT(session_id, participant_id) DO NOTHING`,
		sessionID, participantID, time.Now().UTC().UnixMilli(),
	); err != nil {
		return fmt.Errorf("add thread member: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit thread member add: %w", err)
	}
	return nil
}

func RemoveThreadMember(sessDir, sessionID, participantID string) error {
	sessionID = strings.TrimSpace(sessionID)
	participantID = strings.TrimSpace(participantID)
	if sessionID == "" {
		return errors.New("session_id is required")
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
		return fmt.Errorf("begin thread member remove: %w", err)
	}
	defer tx.Rollback()
	if ok, err := sessionExistsTx(tx, sessionID); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("%w: %q", ErrSessionNotFound, sessionID)
	}
	if _, err := tx.Exec(`DELETE FROM thread_members WHERE session_id = ? AND participant_id = ?`, sessionID, participantID); err != nil {
		return fmt.Errorf("remove thread member: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit thread member remove: %w", err)
	}
	return nil
}

func ListThreadMembers(sessDir, sessionID string) ([]string, error) {
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
SELECT tm.participant_id
FROM thread_members tm
JOIN participants p ON p.id = tm.participant_id
WHERE tm.session_id = ?
  AND p.kind = ?
  AND p.retired_at IS NULL
ORDER BY tm.joined_at ASC, tm.participant_id ASC`, sessionID, string(participant.KindNamed))
	if err != nil {
		return nil, fmt.Errorf("list thread members: %w", err)
	}
	defer rows.Close()

	var members []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan thread members: %w", err)
		}
		members = append(members, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan thread members: %w", err)
	}
	return members, nil
}

// ThreadsForParticipant returns the group threads this participant follows
// (thread_members rows) — the reverse of ListThreadMembers. It is the follow
// set the pull-inbox scans: "which threads might hold messages past my read
// cursor". DM and reply-subthread (cth) follows are tracked separately; this
// covers the top-level group/DM sessions the participant is a member of.
func ThreadsForParticipant(sessDir, participantID string) ([]string, error) {
	participantID = strings.TrimSpace(participantID)
	if participantID == "" {
		return nil, nil
	}
	db, err := openStore(sessDir)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`
SELECT session_id FROM thread_members
WHERE participant_id = ?
ORDER BY joined_at ASC, session_id ASC`, participantID)
	if err != nil {
		return nil, fmt.Errorf("threads for participant: %w", err)
	}
	defer rows.Close()
	var threads []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan threads for participant: %w", err)
		}
		threads = append(threads, id)
	}
	return threads, rows.Err()
}

func EnqueueResidentEnvelope(sessDir string, env ResidentEnvelope) (ResidentEnvelope, error) {
	env.ID = strings.TrimSpace(env.ID)
	env.ParticipantID = strings.TrimSpace(env.ParticipantID)
	env.EnvelopeJSON = json.RawMessage(bytesTrimSpace(env.EnvelopeJSON))
	if env.ID == "" {
		env.ID = "env-" + NewID()
	}
	if env.ParticipantID == "" {
		return ResidentEnvelope{}, errors.New("participant_id is required")
	}
	if len(env.EnvelopeJSON) == 0 || string(env.EnvelopeJSON) == "null" {
		return ResidentEnvelope{}, errors.New("envelope_json is required")
	}
	if env.CreatedAt.IsZero() {
		env.CreatedAt = time.Now().UTC()
	} else {
		env.CreatedAt = env.CreatedAt.UTC()
	}
	env.ConsumedAt = nil

	db, err := openStore(sessDir)
	if err != nil {
		return ResidentEnvelope{}, err
	}
	defer db.Close()

	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()

	tx, err := db.Begin()
	if err != nil {
		return ResidentEnvelope{}, fmt.Errorf("begin resident envelope enqueue: %w", err)
	}
	defer tx.Rollback()
	if err := requireActiveNamedParticipant(tx, env.ParticipantID); err != nil {
		return ResidentEnvelope{}, err
	}
	if _, err := tx.Exec(`
INSERT INTO resident_inbox (id, participant_id, envelope_json, created_at, consumed_at)
VALUES (?, ?, ?, ?, NULL)`,
		env.ID, env.ParticipantID, string(env.EnvelopeJSON), env.CreatedAt.UnixMilli(),
	); err != nil {
		return ResidentEnvelope{}, fmt.Errorf("enqueue resident envelope: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return ResidentEnvelope{}, fmt.Errorf("commit resident envelope enqueue: %w", err)
	}
	return env, nil
}

func PendingResidentEnvelopes(sessDir, participantID string, limit int) ([]ResidentEnvelope, error) {
	participantID = strings.TrimSpace(participantID)
	if participantID == "" {
		return nil, errors.New("participant_id is required")
	}
	db, err := openStore(sessDir)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := requireActiveNamedParticipant(db, participantID); err != nil {
		return nil, err
	}

	query := `
SELECT id, participant_id, envelope_json, created_at, consumed_at, expired_at
FROM resident_inbox
WHERE participant_id = ? AND consumed_at IS NULL AND expired_at IS NULL
ORDER BY created_at ASC, id ASC`
	args := []any{participantID}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list pending resident envelopes: %w", err)
	}
	defer rows.Close()

	var envs []ResidentEnvelope
	for rows.Next() {
		env, err := scanResidentEnvelope(rows)
		if err != nil {
			return nil, fmt.Errorf("scan resident envelopes: %w", err)
		}
		envs = append(envs, env)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan resident envelopes: %w", err)
	}
	return envs, nil
}

func MarkResidentEnvelopesConsumed(sessDir string, ids []string, consumedAt time.Time) error {
	cleanIDs := make([]string, 0, len(ids))
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" {
			cleanIDs = append(cleanIDs, id)
		}
	}
	if len(cleanIDs) == 0 {
		return nil
	}
	if consumedAt.IsZero() {
		consumedAt = time.Now().UTC()
	} else {
		consumedAt = consumedAt.UTC()
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
		return fmt.Errorf("begin resident envelope consume: %w", err)
	}
	defer tx.Rollback()
	for _, id := range cleanIDs {
		if _, err := tx.Exec(`
UPDATE resident_inbox
SET consumed_at = COALESCE(consumed_at, ?)
WHERE id = ?`, consumedAt.UnixMilli(), id); err != nil {
			return fmt.Errorf("mark resident envelope consumed: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit resident envelope consume: %w", err)
	}
	return nil
}

func AppendHistoryRecordAndConsumeResidentEnvelopes(sessDir, sessionID string, rec HistoryRecord, ids []string, consumedAt time.Time) (int, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return 0, errors.New("session_id is required")
	}
	cleanIDs := make([]string, 0, len(ids))
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" {
			cleanIDs = append(cleanIDs, id)
		}
	}
	if consumedAt.IsZero() {
		consumedAt = time.Now().UTC()
	} else {
		consumedAt = consumedAt.UTC()
	}

	db, err := openStore(sessDir)
	if err != nil {
		return 0, err
	}
	defer db.Close()

	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()

	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin history append resident consume: %w", err)
	}
	defer tx.Rollback()
	if ok, err := sessionExistsTx(tx, sessionID); err != nil {
		return 0, err
	} else if !ok {
		return 0, fmt.Errorf("%w: %q", ErrSessionNotFound, sessionID)
	}
	seq, err := appendHistoryRecordTx(tx, sessionID, rec)
	if err != nil {
		return 0, err
	}
	for _, id := range cleanIDs {
		if _, err := tx.Exec(`
UPDATE resident_inbox
SET consumed_at = COALESCE(consumed_at, ?)
WHERE id = ?`, consumedAt.UnixMilli(), id); err != nil {
			return 0, fmt.Errorf("mark resident envelope consumed: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit history append resident consume: %w", err)
	}
	return seq, nil
}

// ParticipantsWithPendingResidentEnvelopes returns the participant IDs of
// every active named agent whose resident inbox still has at least one
// un-consumed envelope. Used by the app-server boot path to drain
// envelopes left over from a previous process (issue #3: without this,
// pending envelopes sit until the next new message happens to land in the
// same inbox — for low-traffic participants that effectively means
// "forever"). The retired / non-named filter runs in SQL so callers don't
// pay for participants that drainResidentAgent would skip anyway.
func ParticipantsWithPendingResidentEnvelopes(sessDir string) ([]string, error) {
	db, err := openStore(sessDir)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`
SELECT DISTINCT ri.participant_id
FROM resident_inbox ri
JOIN participants p ON p.id = ri.participant_id
WHERE ri.consumed_at IS NULL
  AND ri.expired_at IS NULL
  AND p.kind = ?
  AND p.retired_at IS NULL`, string(participant.KindNamed))
	if err != nil {
		return nil, fmt.Errorf("list pending inbox participants: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan pending inbox participants: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows pending inbox participants: %w", err)
	}
	return ids, nil
}

// MarkPendingResidentEnvelopesExpired is the issue #3 pivot's boot-side
// pass 1: scan all resident_inbox rows where consumed_at IS NULL AND
// expired_at IS NULL (i.e. the previous process died with these envelopes
// unprocessed), set expired_at to the given timestamp, and return the
// count of rows affected. The user spec calls these envelopes
// "expired" — front-end distinguishes this from FAILED (system error)
// and from a missing row (never enqueued). Expired is a terminal state:
// no expired row is re-kicked, even when a fresh new message arrives
// for the same participant, and the partial index
// idx_resident_inbox_pending excludes expired rows so subsequent
// PendingResidentEnvelopes queries do not surface them.
func MarkPendingResidentEnvelopesExpired(sessDir string, at time.Time) (int64, error) {
	if at.IsZero() {
		at = time.Now().UTC()
	} else {
		at = at.UTC()
	}
	db, err := openStore(sessDir)
	if err != nil {
		return 0, err
	}
	defer db.Close()

	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()

	res, err := db.Exec(`
UPDATE resident_inbox
SET expired_at = ?
WHERE consumed_at IS NULL
  AND expired_at IS NULL`, at.UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("mark pending resident envelopes expired: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected (mark pending expired): %w", err)
	}
	return n, nil
}

// MarkStuckInProgressReadReceiptsExpired is the issue #3 pivot's boot-side
// pass 2: scan all message_marks rows where kind='seen' AND
// status='in_progress' (a turn started consuming a message but did not
// finish before the process died), set status to SeenStatusExpiredUnprocessed
// and at to the given timestamp, and return the count of rows affected.
// The front-end sees this distinct from FAILED (system error) so the
// user knows the message was not at fault — the system just didn't get a
// turn before the user closed the laptop. expired_unprocessed is a
// terminal state — no further transition out of it.
//
// This deliberately bypasses MarkMessageSeen's whitelist (which keeps
// the 3 regular-session lifecycle values tight): boot is the only
// legitimate writer of SeenStatusExpiredUnprocessed, and the whitelist
// is a guard against regular-session writes, not boot writes.
func MarkStuckInProgressReadReceiptsExpired(sessDir string, at time.Time) (int64, error) {
	if at.IsZero() {
		at = time.Now().UTC()
	} else {
		at = at.UTC()
	}
	db, err := openStore(sessDir)
	if err != nil {
		return 0, err
	}
	defer db.Close()

	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()

	res, err := db.Exec(`
UPDATE message_marks
SET status = ?, at = ?
WHERE kind = ?
  AND status = ?`,
		SeenStatusExpiredUnprocessed, at.UnixMilli(), MessageMarkKindSeen, SeenStatusInProgress)
	if err != nil {
		return 0, fmt.Errorf("mark stuck in-progress read receipts expired: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected (mark stuck in-progress expired): %w", err)
	}
	return n, nil
}

// MigrateResidentInboxExpiredAt is the production-schema migration
// helper for issue #3 pivot (replay → settle/expire): the new
// `expired_at` column is added to the CREATE TABLE IF NOT EXISTS
// statement, but pre-existing databases created before the pivot
// don't have the column — and the CREATE statement is a no-op for
// existing tables. Calling this helper once at startup closes that
// gap: it ALTERs the existing table to add the column, idempotently
// (the "duplicate column name" error is swallowed as success, since
// the only way to reach that error is the column already existing).
// Settle then runs against the now-up-to-date schema. NewDBs do not
// need this; session.go's CREATE statement already includes the
// column from the start.
func MigrateResidentInboxExpiredAt(sessDir string) error {
	db, err := openStore(sessDir)
	if err != nil {
		return err
	}
	defer db.Close()

	if _, err := db.Exec(`ALTER TABLE resident_inbox ADD COLUMN expired_at INTEGER`); err != nil {
		if strings.Contains(err.Error(), "duplicate column name") {
			return nil // idempotent — column already exists
		}
		return fmt.Errorf("migrate resident_inbox expired_at: %w", err)
	}
	return nil
}

func requireActiveNamedParticipant(q sqlRowQuerier, participantID string) error {
	var kind string
	var retiredAt sql.NullString
	err := q.QueryRow(`SELECT kind, retired_at FROM participants WHERE id = ?`, participantID).Scan(&kind, &retiredAt)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %q", ErrParticipantNotFound, participantID)
	}
	if err != nil {
		return fmt.Errorf("load participant: %w", err)
	}
	if participant.Kind(kind) != participant.KindNamed || retiredAt.Valid {
		return fmt.Errorf("participant %q must be an active named participant", participantID)
	}
	return nil
}

func scanResidentEnvelope(scanner interface {
	Scan(dest ...any) error
}) (ResidentEnvelope, error) {
	var env ResidentEnvelope
	var envelopeJSON string
	var createdAt int64
	var consumedAt, expiredAt sql.NullInt64
	if err := scanner.Scan(&env.ID, &env.ParticipantID, &envelopeJSON, &createdAt, &consumedAt, &expiredAt); err != nil {
		return ResidentEnvelope{}, err
	}
	env.EnvelopeJSON = rawMessage(envelopeJSON)
	env.CreatedAt = time.UnixMilli(createdAt).UTC()
	if consumedAt.Valid {
		t := time.UnixMilli(consumedAt.Int64).UTC()
		env.ConsumedAt = &t
	}
	if expiredAt.Valid {
		t := time.UnixMilli(expiredAt.Int64).UTC()
		env.ExpiredAt = &t
	}
	return env, nil
}
