package session

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/participant"
)

var ErrParticipantNotFound = errors.New("participant not found")

type ParticipantRun struct {
	ID            string
	ParticipantID string
	AgentID       string
	TaskID        string
	SessionID     string
	Summary       string
	Outcome       string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// UpsertParticipant inserts or updates a participant record.
func UpsertParticipant(sessDir string, p participant.Participant) error {
	db, err := openStore(sessDir)
	if err != nil {
		return err
	}
	defer db.Close()

	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()

	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	if p.UpdatedAt.IsZero() {
		p.UpdatedAt = now
	}
	_, err = db.Exec(`
INSERT INTO participants (
	id, kind, name, role, avatar, tagline, workspace, model,
	created_at, updated_at, retired_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
	kind = excluded.kind,
	name = excluded.name,
	role = excluded.role,
	avatar = excluded.avatar,
	tagline = excluded.tagline,
	workspace = excluded.workspace,
	model = excluded.model,
	updated_at = excluded.updated_at,
	retired_at = excluded.retired_at`,
		p.ID, string(p.Kind), p.Name, p.Role, p.Avatar, p.Tagline, p.Workspace, p.Model,
		timeText(p.CreatedAt), timeText(p.UpdatedAt), nullableTimeText(p.RetiredAt),
	)
	if err != nil {
		return fmt.Errorf("upsert participant: %w", err)
	}
	return nil
}

func UpsertParticipantRun(sessDir string, run ParticipantRun) error {
	run.ParticipantID = strings.TrimSpace(run.ParticipantID)
	if run.ParticipantID == "" {
		return errors.New("participant_id is required")
	}
	run.ID = strings.TrimSpace(run.ID)
	if run.ID == "" {
		run.ID = strings.TrimSpace(run.AgentID)
	}
	if run.ID == "" {
		run.ID = NewID()
	}
	now := time.Now().UTC()
	if run.CreatedAt.IsZero() {
		run.CreatedAt = now
	}
	if run.UpdatedAt.IsZero() {
		run.UpdatedAt = now
	}

	db, err := openStore(sessDir)
	if err != nil {
		return err
	}
	defer db.Close()

	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()

	_, err = db.Exec(`
INSERT INTO participant_runs (
	id, participant_id, agent_id, task_id, session_id, summary, outcome,
	created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
	participant_id = excluded.participant_id,
	agent_id = excluded.agent_id,
	task_id = excluded.task_id,
	session_id = excluded.session_id,
	summary = excluded.summary,
	outcome = excluded.outcome,
	updated_at = excluded.updated_at`,
		run.ID, run.ParticipantID, strings.TrimSpace(run.AgentID),
		strings.TrimSpace(run.TaskID), strings.TrimSpace(run.SessionID),
		strings.TrimSpace(run.Summary), strings.TrimSpace(run.Outcome),
		timeText(run.CreatedAt), timeText(run.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("upsert participant run: %w", err)
	}
	return nil
}

func CompleteParticipantRun(sessDir, participantID, agentID, outcome, summary string) error {
	participantID = strings.TrimSpace(participantID)
	agentID = strings.TrimSpace(agentID)
	if participantID == "" || agentID == "" {
		return nil
	}
	db, err := openStore(sessDir)
	if err != nil {
		return err
	}
	defer db.Close()

	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()

	now := time.Now().UTC()
	_, err = db.Exec(`
UPDATE participant_runs
SET outcome = ?, summary = CASE WHEN ? <> '' THEN ? ELSE summary END, updated_at = ?
WHERE participant_id = ? AND agent_id = ?`,
		strings.TrimSpace(outcome), strings.TrimSpace(summary), strings.TrimSpace(summary),
		timeText(now), participantID, agentID,
	)
	if err != nil {
		return fmt.Errorf("complete participant run: %w", err)
	}
	return nil
}

func ListParticipantRuns(sessDir, participantID string, limit int) ([]ParticipantRun, error) {
	participantID = strings.TrimSpace(participantID)
	if participantID == "" {
		return nil, errors.New("participant_id is required")
	}
	db, err := openStore(sessDir)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	query := `
SELECT id, participant_id, agent_id, task_id, session_id, summary, outcome,
       created_at, updated_at
FROM participant_runs
WHERE participant_id = ?
ORDER BY created_at DESC, id DESC`
	args := []any{participantID}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list participant runs: %w", err)
	}
	defer rows.Close()

	var runs []ParticipantRun
	for rows.Next() {
		run, err := scanParticipantRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan participant runs: %w", err)
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan participant runs: %w", err)
	}
	return runs, nil
}

// GetParticipant returns one participant by ID.
func GetParticipant(sessDir, id string) (participant.Participant, error) {
	db, err := openStore(sessDir)
	if err != nil {
		return participant.Participant{}, err
	}
	defer db.Close()

	row := db.QueryRow(`
SELECT id, kind, name, role, avatar, tagline, workspace, model,
       created_at, updated_at, retired_at
FROM participants
WHERE id = ?`, strings.TrimSpace(id))
	p, err := scanParticipant(row)
	if errors.Is(err, sql.ErrNoRows) {
		return participant.Participant{}, fmt.Errorf("%w: %q", ErrParticipantNotFound, id)
	}
	if err != nil {
		return participant.Participant{}, fmt.Errorf("get participant: %w", err)
	}
	return p, nil
}

// ListParticipants returns non-retired participants ordered by creation time.
// Empty kind matches all kinds.
func ListParticipants(sessDir string, kind participant.Kind) ([]participant.Participant, error) {
	db, err := openStore(sessDir)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	query := `
SELECT id, kind, name, role, avatar, tagline, workspace, model,
       created_at, updated_at, retired_at
FROM participants
WHERE retired_at IS NULL`
	args := []any{}
	if kind != "" {
		query += ` AND kind = ?`
		args = append(args, string(kind))
	}
	query += ` ORDER BY created_at`

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list participants: %w", err)
	}
	defer rows.Close()

	var participants []participant.Participant
	for rows.Next() {
		p, err := scanParticipant(rows)
		if err != nil {
			return nil, fmt.Errorf("scan participants: %w", err)
		}
		participants = append(participants, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan participants: %w", err)
	}
	return participants, nil
}

// CountParticipantsByKind returns the number of participants with the given
// kind, including retired rows. Useful for "have we ever created one of these?"
// checks where retiring must not allow the seed to re-fire.
func CountParticipantsByKind(sessDir string, kind participant.Kind) (int, error) {
	if kind == "" {
		return 0, errors.New("kind is required")
	}
	db, err := openStore(sessDir)
	if err != nil {
		return 0, err
	}
	defer db.Close()

	var count int
	if err := db.QueryRow(`SELECT COUNT(1) FROM participants WHERE kind = ?`, string(kind)).Scan(&count); err != nil {
		return 0, fmt.Errorf("count participants: %w", err)
	}
	return count, nil
}

// RetireParticipant marks a participant as retired and, in the same
// transaction, removes the rows derived from its active identity:
//
//   - thread_members: the participant leaves every group/conversation thread
//     it was a member of. The CASCADE FK on thread_members only fires on
//     participant DELETE; retire is an UPDATE, so the rows are removed here
//     explicitly.
//   - resident_inbox: unconsumed envelopes are dropped — a retired resident
//     never drains its inbox again, so pending envelopes would sit forever.
//     Consumed envelopes are kept as delivery history.
//
// The retired_at stamp uses COALESCE so retiring an already-retired
// participant is idempotent (the original retirement time is preserved),
// which lets callers safely re-run the retire cleanup protocol after a
// partial failure.
func RetireParticipant(sessDir, id string) error {
	id = strings.TrimSpace(id)
	db, err := openStore(sessDir)
	if err != nil {
		return err
	}
	defer db.Close()

	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin retire participant: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	res, err := tx.Exec(`
UPDATE participants
SET retired_at = COALESCE(retired_at, ?), updated_at = ?
WHERE id = ?`, timeText(now), timeText(now), id)
	if err != nil {
		return fmt.Errorf("retire participant: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("retire participant: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: %q", ErrParticipantNotFound, id)
	}
	if _, err := tx.Exec(`DELETE FROM thread_members WHERE participant_id = ?`, id); err != nil {
		return fmt.Errorf("retire participant: remove thread members: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM resident_inbox WHERE participant_id = ? AND consumed_at IS NULL`, id); err != nil {
		return fmt.Errorf("retire participant: drop pending envelopes: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit retire participant: %w", err)
	}
	return nil
}

// FindRetiredParticipantByName returns the most recently retired participant
// of the given kind whose name matches case-insensitively (the same matching
// the active roster uses). This backs the same-name rebuild guard: creating
// a named participant checks for an archived predecessor so callers can
// surface its archived state (participants/.archived/<id>/) alongside the
// fresh identity. Empty kind matches all kinds.
func FindRetiredParticipantByName(sessDir string, kind participant.Kind, name string) (participant.Participant, bool, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return participant.Participant{}, false, errors.New("name is required")
	}
	db, err := openStore(sessDir)
	if err != nil {
		return participant.Participant{}, false, err
	}
	defer db.Close()

	query := `
SELECT id, kind, name, role, avatar, tagline, workspace, model,
       created_at, updated_at, retired_at
FROM participants
WHERE retired_at IS NOT NULL`
	args := []any{}
	if kind != "" {
		query += ` AND kind = ?`
		args = append(args, string(kind))
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return participant.Participant{}, false, fmt.Errorf("find retired participant: %w", err)
	}
	defer rows.Close()

	var best participant.Participant
	found := false
	for rows.Next() {
		p, err := scanParticipant(rows)
		if err != nil {
			return participant.Participant{}, false, fmt.Errorf("scan retired participants: %w", err)
		}
		if !strings.EqualFold(strings.TrimSpace(p.Name), name) {
			continue
		}
		// Latest retirement wins; ordering is resolved on parsed times in
		// Go because RFC3339Nano text does not sort reliably across
		// differing fractional precision.
		if !found || retiredAfter(p, best) {
			best = p
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		return participant.Participant{}, false, fmt.Errorf("scan retired participants: %w", err)
	}
	return best, found, nil
}

// retiredAfter reports whether a was retired after b, tie-breaking on
// updated_at so re-stamped rows resolve deterministically.
func retiredAfter(a, b participant.Participant) bool {
	var at, bt time.Time
	if a.RetiredAt != nil {
		at = *a.RetiredAt
	}
	if b.RetiredAt != nil {
		bt = *b.RetiredAt
	}
	if !at.Equal(bt) {
		return at.After(bt)
	}
	return a.UpdatedAt.After(b.UpdatedAt)
}

func scanParticipant(scanner interface {
	Scan(dest ...any) error
}) (participant.Participant, error) {
	var p participant.Participant
	var kind, createdAt, updatedAt string
	var retiredAt sql.NullString
	if err := scanner.Scan(
		&p.ID, &kind, &p.Name, &p.Role, &p.Avatar, &p.Tagline, &p.Workspace, &p.Model,
		&createdAt, &updatedAt, &retiredAt,
	); err != nil {
		return participant.Participant{}, err
	}
	p.Kind = participant.Kind(kind)
	p.CreatedAt = parseTime(createdAt)
	p.UpdatedAt = parseTime(updatedAt)
	if retiredAt.Valid {
		if t := parseTime(retiredAt.String); !t.IsZero() {
			p.RetiredAt = &t
		}
	}
	return p, nil
}

func scanParticipantRun(scanner interface {
	Scan(dest ...any) error
}) (ParticipantRun, error) {
	var run ParticipantRun
	var createdAt, updatedAt string
	if err := scanner.Scan(
		&run.ID,
		&run.ParticipantID,
		&run.AgentID,
		&run.TaskID,
		&run.SessionID,
		&run.Summary,
		&run.Outcome,
		&createdAt,
		&updatedAt,
	); err != nil {
		return ParticipantRun{}, err
	}
	run.CreatedAt = parseTime(createdAt)
	run.UpdatedAt = parseTime(updatedAt)
	return run, nil
}
