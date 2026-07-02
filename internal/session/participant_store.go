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

// RetireParticipant marks a participant as retired.
func RetireParticipant(sessDir, id string) error {
	db, err := openStore(sessDir)
	if err != nil {
		return err
	}
	defer db.Close()

	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()

	now := time.Now().UTC()
	res, err := db.Exec(`
UPDATE participants
SET retired_at = ?, updated_at = ?
WHERE id = ?`, timeText(now), timeText(now), strings.TrimSpace(id))
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
	return nil
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
