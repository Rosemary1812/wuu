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

// ResidentAdmissionCompensation is the durable recovery intent for a resident
// admission that committed but whose model runner never launched. It is kept
// until the admission side effects are rolled back in the same transaction
// that deletes the intent.
type ResidentAdmissionCompensation struct {
	ID                 string               `json:"id"`
	ParticipantID      string               `json:"participant_id"`
	ThreadID           string               `json:"thread_id"`
	AttemptIDs         []string             `json:"attempt_ids,omitempty"`
	AttemptStartedAt   map[string]time.Time `json:"attempt_started_at,omitempty"`
	EnvelopeIDs        []string             `json:"envelope_ids,omitempty"`
	EnvelopeConsumedAt time.Time            `json:"envelope_consumed_at,omitempty"`
	Marks              []MessageMark        `json:"marks,omitempty"`
	CreatedAt          time.Time            `json:"created_at"`
}

// ResidentWakeIntent is the durable wake obligation published by a resolved
// resident admission compensation. It outlives the resolving process until a
// live resident drain has accepted the restored work.
type ResidentWakeIntent struct {
	ID            string
	ParticipantID string
	ThreadID      string
	CreatedAt     time.Time
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
	var authorityID, authorityStatus string
	err = tx.QueryRow(`
SELECT id, status
FROM conversation_threads
WHERE session_id = ?
  AND ((status = ? AND thread_owner_participant_id = ?)
    OR (status = ? AND lead_participant_id = ?))
ORDER BY created_at ASC, id ASC
LIMIT 1`, sessionID, string(ConversationThreadOpen), participantID,
		string(ConversationThreadTask), participantID).Scan(&authorityID, &authorityStatus)
	switch {
	case err == nil && ConversationThreadStatus(authorityStatus) == ConversationThreadOpen:
		return fmt.Errorf("participant %q owns open Thread %q and cannot leave the group", participantID, authorityID)
	case err == nil:
		return fmt.Errorf("participant %q leads active Task %q and cannot leave the group", participantID, authorityID)
	case !errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("check thread member authority: %w", err)
	}
	if _, err := tx.Exec(`
DELETE FROM conversation_thread_members
WHERE participant_id = ?
  AND conversation_thread_id IN (
    SELECT id FROM conversation_threads WHERE session_id = ?
  )`, participantID, sessionID); err != nil {
		return fmt.Errorf("remove conversation thread memberships: %w", err)
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

// deletePendingResidentEnvelopesFromSourceThreadTx removes unconsumed pushes
// that would otherwise outlive their source thread. It runs in the same
// transaction as session deletion so failure leaves both the thread and its
// inbox references intact. Malformed pending payloads fail closed because the
// store cannot prove that they belong to a different source thread.
func deletePendingResidentEnvelopesFromSourceThreadTx(tx *sql.Tx, sourceThreadID string) error {
	sourceThreadID = strings.TrimSpace(sourceThreadID)
	if sourceThreadID == "" {
		return errors.New("source thread id is required")
	}
	rows, err := tx.Query(`
SELECT id, envelope_json
FROM resident_inbox
WHERE consumed_at IS NULL
ORDER BY id ASC`)
	if err != nil {
		return fmt.Errorf("list pending resident envelopes for source cleanup: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id, payload string
		if err := rows.Scan(&id, &payload); err != nil {
			rows.Close()
			return fmt.Errorf("scan pending resident envelope for source cleanup: %w", err)
		}
		var source struct {
			ThreadID string `json:"source_thread_id"`
		}
		if err := json.Unmarshal([]byte(payload), &source); err != nil {
			rows.Close()
			return fmt.Errorf("decode pending resident envelope %q for source cleanup: %w", id, err)
		}
		if strings.TrimSpace(source.ThreadID) == sourceThreadID {
			ids = append(ids, id)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("scan pending resident envelopes for source cleanup: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close pending resident envelopes for source cleanup: %w", err)
	}
	for _, id := range ids {
		if _, err := tx.Exec(`DELETE FROM resident_inbox WHERE id = ? AND consumed_at IS NULL`, id); err != nil {
			return fmt.Errorf("delete pending resident envelope %q for source cleanup: %w", id, err)
		}
	}
	return nil
}

func AppendHistoryRecordAndConsumeResidentEnvelopes(sessDir, sessionID string, rec HistoryRecord, ids []string, consumedAt time.Time) (int, error) {
	return AppendHistoryRecordAndCommitResidentAdmission(sessDir, sessionID, rec, ids, nil, consumedAt)
}

// AppendHistoryRecordAndCommitResidentAdmission atomically persists the
// resident turn marker, consumes its push envelopes, and records in-progress
// receipts for every source message. A process crash can therefore leave all
// three facts committed (which boot settlement can expire) or none of them;
// it cannot silently consume an envelope without an observable receipt.
func AppendHistoryRecordAndCommitResidentAdmission(sessDir, sessionID string, rec HistoryRecord, ids []string, marks []MessageMark, consumedAt time.Time) (int, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return 0, errors.New("session_id is required")
	}
	return commitResidentAdmission(sessDir, sessionID, &rec, ids, marks, consumedAt)
}

// CommitExistingResidentAdmission re-claims envelopes for a stable resident
// turn marker that was already persisted before a local prelaunch failure.
// It shares the same atomic receipt invariant as the initial append path.
func CommitExistingResidentAdmission(sessDir string, ids []string, marks []MessageMark, consumedAt time.Time) error {
	_, err := commitResidentAdmission(sessDir, "", nil, ids, marks, consumedAt)
	return err
}

// SaveResidentAdmissionCompensation records a pending prelaunch rollback before
// it is attempted. Re-saving the same intent is idempotent; the same ID with a
// different payload fails closed instead of replacing recovery evidence.
func SaveResidentAdmissionCompensation(sessDir string, pending ResidentAdmissionCompensation) error {
	pending, payload, err := normalizeResidentAdmissionCompensation(pending)
	if err != nil {
		return err
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
		return fmt.Errorf("begin resident admission compensation journal: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`
INSERT INTO resident_admission_compensations (id, thread_id, payload_json, created_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
 thread_id = CASE
  WHEN resident_admission_compensations.thread_id = '' THEN excluded.thread_id
  ELSE resident_admission_compensations.thread_id
 END`, pending.ID, pending.ThreadID, string(payload), pending.CreatedAt.UnixMilli()); err != nil {
		return fmt.Errorf("save resident admission compensation %q: %w", pending.ID, err)
	}
	var storedThreadID, stored string
	if err := tx.QueryRow(`SELECT thread_id, payload_json FROM resident_admission_compensations WHERE id = ?`, pending.ID).Scan(&storedThreadID, &stored); err != nil {
		return fmt.Errorf("verify resident admission compensation %q: %w", pending.ID, err)
	}
	if storedThreadID != pending.ThreadID || stored != string(payload) {
		return fmt.Errorf("resident admission compensation %q conflicts with its durable payload", pending.ID)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit resident admission compensation journal: %w", err)
	}
	return nil
}

// ResolveResidentAdmissionCompensation atomically rolls back one admission and
// removes its recovery intent. Replaying after an ambiguous commit is safe: all
// rollback writes are idempotent and a missing intent is accepted by the owner
// that still holds the thread execution lease.
func ResolveResidentAdmissionCompensation(sessDir string, pending ResidentAdmissionCompensation) error {
	pending, payload, err := normalizeResidentAdmissionCompensation(pending)
	if err != nil {
		return err
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
		return fmt.Errorf("begin resident admission compensation resolution: %w", err)
	}
	defer tx.Rollback()
	var storedThreadID, stored string
	scanErr := tx.QueryRow(`SELECT thread_id, payload_json FROM resident_admission_compensations WHERE id = ?`, pending.ID).Scan(&storedThreadID, &stored)
	if errors.Is(scanErr, sql.ErrNoRows) {
		// Journal deletion is atomic with rollback. A missing row therefore means
		// another recovery already committed it; never replay the payload against
		// durable state that may have advanced since then.
		return nil
	}
	if scanErr != nil {
		return fmt.Errorf("load resident admission compensation %q: %w", pending.ID, scanErr)
	}
	if storedThreadID != pending.ThreadID || stored != string(payload) {
		return fmt.Errorf("resident admission compensation %q conflicts with its durable payload", pending.ID)
	}
	if err := compensateResidentAdmissionTx(
		tx, pending.AttemptIDs, pending.AttemptStartedAt,
		pending.EnvelopeIDs, pending.EnvelopeConsumedAt, pending.Marks,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(`
INSERT OR IGNORE INTO resident_wake_intents (id, participant_id, thread_id, created_at)
VALUES (?, ?, ?, ?)`, pending.ID, pending.ParticipantID, pending.ThreadID, pending.CreatedAt.UnixMilli()); err != nil {
		return fmt.Errorf("publish resident wake intent %q: %w", pending.ID, err)
	}
	var wakeParticipantID, wakeThreadID string
	if err := tx.QueryRow(`SELECT participant_id, thread_id FROM resident_wake_intents WHERE id = ?`, pending.ID).Scan(&wakeParticipantID, &wakeThreadID); err != nil {
		return fmt.Errorf("verify resident wake intent %q: %w", pending.ID, err)
	}
	if wakeParticipantID != pending.ParticipantID || wakeThreadID != pending.ThreadID {
		return fmt.Errorf("resident wake intent %q conflicts with participant or thread identity", pending.ID)
	}
	if _, err := tx.Exec(`DELETE FROM resident_admission_compensations WHERE id = ?`, pending.ID); err != nil {
		return fmt.Errorf("delete resident admission compensation %q: %w", pending.ID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit resident admission compensation resolution: %w", err)
	}
	return nil
}

// PendingResidentWakeIntents returns live-peer wake obligations in creation
// order. A cold-boot owner discards them before settlement; non-boot peers use
// them to recover a kick lost after compensation committed.
func PendingResidentWakeIntents(sessDir string) ([]ResidentWakeIntent, error) {
	db, err := openStore(sessDir)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`
SELECT id, participant_id, thread_id, created_at
FROM resident_wake_intents
ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list resident wake intents: %w", err)
	}
	defer rows.Close()
	var pending []ResidentWakeIntent
	for rows.Next() {
		var item ResidentWakeIntent
		var createdAt int64
		if err := rows.Scan(&item.ID, &item.ParticipantID, &item.ThreadID, &createdAt); err != nil {
			return nil, fmt.Errorf("scan resident wake intents: %w", err)
		}
		item.CreatedAt = time.UnixMilli(createdAt).UTC()
		pending = append(pending, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan resident wake intents: %w", err)
	}
	return pending, nil
}

// AcknowledgeResidentWakeIntents removes wake obligations accepted by a live
// resident drain. Replaying an acknowledgement is safe.
func AcknowledgeResidentWakeIntents(sessDir string, ids []string) (int, error) {
	clean := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		clean = append(clean, id)
	}
	if len(clean) == 0 {
		return 0, nil
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
		return 0, fmt.Errorf("begin resident wake acknowledgement: %w", err)
	}
	defer tx.Rollback()
	removed := 0
	for _, id := range clean {
		result, err := tx.Exec(`DELETE FROM resident_wake_intents WHERE id = ?`, id)
		if err != nil {
			return 0, fmt.Errorf("acknowledge resident wake intent %q: %w", id, err)
		}
		if count, err := result.RowsAffected(); err == nil {
			removed += int(count)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit resident wake acknowledgement: %w", err)
	}
	return removed, nil
}

// DiscardResidentWakeIntents settles live-only wake obligations on cold boot.
// The boot owner separately expires restored inbox work before serving traffic.
func DiscardResidentWakeIntents(sessDir string) (int, error) {
	db, err := openStore(sessDir)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()
	result, err := db.Exec(`DELETE FROM resident_wake_intents`)
	if err != nil {
		return 0, fmt.Errorf("discard resident wake intents: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count discarded resident wake intents: %w", err)
	}
	return int(count), nil
}

// PendingResidentAdmissionCompensations returns the durable recovery journal
// in creation order. It is exported for boot recovery and state assertions.
func PendingResidentAdmissionCompensations(sessDir string) ([]ResidentAdmissionCompensation, error) {
	db, err := openStore(sessDir)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`
SELECT payload_json
FROM resident_admission_compensations
ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list resident admission compensations: %w", err)
	}
	defer rows.Close()
	var pending []ResidentAdmissionCompensation
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("scan resident admission compensation: %w", err)
		}
		var item ResidentAdmissionCompensation
		if err := json.Unmarshal([]byte(payload), &item); err != nil {
			return nil, fmt.Errorf("decode resident admission compensation: %w", err)
		}
		item, _, err = normalizeResidentAdmissionCompensation(item)
		if err != nil {
			return nil, fmt.Errorf("validate resident admission compensation: %w", err)
		}
		pending = append(pending, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan resident admission compensations: %w", err)
	}
	return pending, nil
}

// RecoverResidentAdmissionCompensations resolves intents left by a process
// that exited after admission failed but before its retry committed.
func RecoverResidentAdmissionCompensations(sessDir string) (int, error) {
	pending, err := PendingResidentAdmissionCompensations(sessDir)
	if err != nil {
		return 0, err
	}
	for i, item := range pending {
		if err := ResolveResidentAdmissionCompensation(sessDir, item); err != nil {
			return i, fmt.Errorf("recover resident admission compensation %q: %w", item.ID, err)
		}
	}
	return len(pending), nil
}

// RecoverResidentAdmissionCompensationsForThread resolves only intents that
// block one thread. A non-boot app-server calls this after acquiring that
// thread's execution lease, so a shutdown handoff is also safe when another
// app-server process remains present.
func RecoverResidentAdmissionCompensationsForThread(sessDir, threadID string) (int, error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return 0, errors.New("thread_id is required")
	}
	pending, err := PendingResidentAdmissionCompensations(sessDir)
	if err != nil {
		return 0, err
	}
	recovered := 0
	for _, item := range pending {
		if item.ThreadID != threadID {
			continue
		}
		if err := ResolveResidentAdmissionCompensation(sessDir, item); err != nil {
			return recovered, fmt.Errorf("recover resident admission compensation %q for thread %q: %w", item.ID, threadID, err)
		}
		recovered++
	}
	return recovered, nil
}

func normalizeResidentAdmissionCompensation(pending ResidentAdmissionCompensation) (ResidentAdmissionCompensation, []byte, error) {
	pending.ID = strings.TrimSpace(pending.ID)
	pending.ParticipantID = strings.TrimSpace(pending.ParticipantID)
	pending.ThreadID = strings.TrimSpace(pending.ThreadID)
	if pending.ID == "" || pending.ParticipantID == "" || pending.ThreadID == "" {
		return ResidentAdmissionCompensation{}, nil, errors.New("resident admission compensation requires id, participant_id, and thread_id")
	}
	var err error
	pending.AttemptIDs, pending.EnvelopeIDs, pending.Marks, err = cleanResidentAdmissionCompensation(
		pending.AttemptIDs, pending.EnvelopeIDs, pending.Marks,
	)
	if err != nil {
		return ResidentAdmissionCompensation{}, nil, err
	}
	if len(pending.AttemptIDs) > 0 {
		cleanStartedAt := make(map[string]time.Time, len(pending.AttemptIDs))
		for _, id := range pending.AttemptIDs {
			startedAt := pending.AttemptStartedAt[id]
			if startedAt.IsZero() {
				return ResidentAdmissionCompensation{}, nil, fmt.Errorf("resident admission compensation attempt %q requires started_at", id)
			}
			cleanStartedAt[id] = startedAt.UTC()
		}
		pending.AttemptStartedAt = cleanStartedAt
	} else {
		pending.AttemptStartedAt = nil
	}
	if len(pending.EnvelopeIDs) > 0 {
		if pending.EnvelopeConsumedAt.IsZero() {
			return ResidentAdmissionCompensation{}, nil, errors.New("resident admission compensation envelopes require consumed_at")
		}
		pending.EnvelopeConsumedAt = pending.EnvelopeConsumedAt.UTC()
	} else {
		pending.EnvelopeConsumedAt = time.Time{}
	}
	if pending.CreatedAt.IsZero() {
		pending.CreatedAt = time.Now().UTC()
	} else {
		pending.CreatedAt = pending.CreatedAt.UTC()
	}
	payload, err := json.Marshal(pending)
	if err != nil {
		return ResidentAdmissionCompensation{}, nil, fmt.Errorf("encode resident admission compensation: %w", err)
	}
	return pending, payload, nil
}

func cleanResidentAdmissionCompensation(attemptIDs, envelopeIDs []string, marks []MessageMark) ([]string, []string, []MessageMark, error) {
	cleanAttemptIDs := cleanResidentAdmissionIDs(attemptIDs)
	cleanEnvelopeIDs := cleanResidentAdmissionIDs(envelopeIDs)
	cleanMarks := make([]MessageMark, 0, len(marks))
	for _, mark := range marks {
		mark.SessionID = strings.TrimSpace(mark.SessionID)
		mark.ParticipantID = strings.TrimSpace(mark.ParticipantID)
		mark.ThreadID = strings.TrimSpace(mark.ThreadID)
		if mark.SessionID == "" || mark.Seq <= 0 || mark.ParticipantID == "" {
			return nil, nil, nil, errors.New("resident admission compensation receipt requires session_id, seq, and participant_id")
		}
		if mark.Kind != MessageMarkKindSeen || mark.Status != SeenStatusInProgress || mark.At.IsZero() {
			return nil, nil, nil, fmt.Errorf("resident admission compensation requires a timestamped seen/in_progress receipt")
		}
		mark.At = mark.At.UTC()
		cleanMarks = append(cleanMarks, mark)
	}
	return cleanAttemptIDs, cleanEnvelopeIDs, cleanMarks, nil
}

func compensateResidentAdmissionTx(tx *sql.Tx, cleanAttemptIDs []string, attemptStartedAt map[string]time.Time, cleanEnvelopeIDs []string, envelopeConsumedAt time.Time, cleanMarks []MessageMark) error {
	for _, id := range cleanAttemptIDs {
		startedAt := attemptStartedAt[id]
		if _, err := tx.Exec(`
UPDATE task_attempts
SET status = ?, started_at = 0
WHERE id = ? AND status = ? AND started_at = ?`,
			TaskAttemptQueued, id, TaskAttemptRunning, startedAt.UTC().UnixMilli(),
		); err != nil {
			return fmt.Errorf("requeue exact resident task attempt %q: %w", id, err)
		}
	}
	for _, id := range cleanEnvelopeIDs {
		if _, err := tx.Exec(`
UPDATE resident_inbox
SET consumed_at = NULL
WHERE id = ? AND consumed_at = ? AND expired_at IS NULL`, id, envelopeConsumedAt.UTC().UnixMilli()); err != nil {
			return fmt.Errorf("requeue exact resident envelope %q: %w", id, err)
		}
	}
	for _, mark := range cleanMarks {
		if _, err := tx.Exec(`
DELETE FROM message_marks
WHERE session_id = ? AND seq = ? AND participant_id = ? AND kind = ?
  AND status = ? AND thread_id = ? AND at = ?`,
			mark.SessionID, mark.Seq, mark.ParticipantID, MessageMarkKindSeen,
			SeenStatusInProgress, mark.ThreadID, mark.At.UnixMilli(),
		); err != nil {
			return fmt.Errorf("restore resident receipt unread: %w", err)
		}
	}
	return nil
}

func cleanResidentAdmissionIDs(ids []string) []string {
	clean := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		clean = append(clean, id)
	}
	return clean
}

func commitResidentAdmission(sessDir, sessionID string, rec *HistoryRecord, ids []string, marks []MessageMark, consumedAt time.Time) (int, error) {
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
	cleanMarks := make([]MessageMark, 0, len(marks))
	for _, mark := range marks {
		mark.SessionID = strings.TrimSpace(mark.SessionID)
		mark.ParticipantID = strings.TrimSpace(mark.ParticipantID)
		mark.ThreadID = strings.TrimSpace(mark.ThreadID)
		if mark.SessionID == "" || mark.Seq <= 0 || mark.ParticipantID == "" {
			return 0, errors.New("resident admission receipt requires session_id, seq, and participant_id")
		}
		if mark.Kind != MessageMarkKindSeen || mark.Status != SeenStatusInProgress {
			return 0, fmt.Errorf("resident admission receipt must be seen/in_progress, got %q/%q", mark.Kind, mark.Status)
		}
		if mark.At.IsZero() {
			mark.At = consumedAt
		} else {
			mark.At = mark.At.UTC()
		}
		cleanMarks = append(cleanMarks, mark)
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
	seq := 0
	if rec != nil {
		if ok, err := sessionExistsTx(tx, sessionID); err != nil {
			return 0, err
		} else if !ok {
			return 0, fmt.Errorf("%w: %q", ErrSessionNotFound, sessionID)
		}
		seq, err = appendHistoryRecordTx(tx, sessionID, *rec)
		if err != nil {
			return 0, err
		}
	}
	for _, id := range cleanIDs {
		if _, err := tx.Exec(`
UPDATE resident_inbox
SET consumed_at = COALESCE(consumed_at, ?)
WHERE id = ?`, consumedAt.UnixMilli(), id); err != nil {
			return 0, fmt.Errorf("mark resident envelope consumed: %w", err)
		}
	}
	for _, mark := range cleanMarks {
		if _, err := tx.Exec(`
INSERT INTO message_marks (session_id, seq, participant_id, kind, status, payload, thread_id, at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(session_id, seq, participant_id, kind)
DO UPDATE SET status = excluded.status, payload = excluded.payload, thread_id = excluded.thread_id, at = excluded.at`,
			mark.SessionID, mark.Seq, mark.ParticipantID, mark.Kind, mark.Status, mark.Payload, mark.ThreadID, mark.At.UnixMilli(),
		); err != nil {
			return 0, fmt.Errorf("record resident admission receipt: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit resident admission: %w", err)
	}
	return seq, nil
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

func requireActiveNamedThreadMember(q sqlRowQuerier, sessionID, participantID string) error {
	if err := requireActiveNamedParticipant(q, participantID); err != nil {
		return err
	}
	var exists int
	if err := q.QueryRow(`
SELECT EXISTS(
	SELECT 1 FROM thread_members
	WHERE session_id = ? AND participant_id = ?
)`, sessionID, participantID).Scan(&exists); err != nil {
		return fmt.Errorf("check thread member: %w", err)
	}
	if exists == 0 {
		return fmt.Errorf("participant %q is not a member of parent thread %q", participantID, sessionID)
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
