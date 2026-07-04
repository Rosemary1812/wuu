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
SELECT id, participant_id, envelope_json, created_at, consumed_at
FROM resident_inbox
WHERE participant_id = ? AND consumed_at IS NULL
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
	var consumedAt sql.NullInt64
	if err := scanner.Scan(&env.ID, &env.ParticipantID, &envelopeJSON, &createdAt, &consumedAt); err != nil {
		return ResidentEnvelope{}, err
	}
	env.EnvelopeJSON = rawMessage(envelopeJSON)
	env.CreatedAt = time.UnixMilli(createdAt).UTC()
	if consumedAt.Valid {
		t := time.UnixMilli(consumedAt.Int64).UTC()
		env.ConsumedAt = &t
	}
	return env, nil
}
