package channels

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

func currentScopeSequenceTx(ctx context.Context, tx *sql.Tx, roomID, threadID string) (int64, error) {
	query := `SELECT COALESCE(MAX(seq), 0) FROM room_messages WHERE room_id = ? AND thread_id IS NULL`
	args := []any{roomID}
	if threadID != "" {
		query = `SELECT COALESCE(MAX(seq), 0) FROM room_messages WHERE room_id = ? AND (id = ? OR thread_id = ?)`
		args = []any{roomID, threadID, threadID}
	}
	var seq int64
	if err := tx.QueryRowContext(ctx, query, args...).Scan(&seq); err != nil {
		return 0, fmt.Errorf("query chat scope sequence: %w", err)
	}
	return seq, nil
}

func holdNewDraftTx(ctx context.Context, tx *sql.Tx, agentID, roomID, threadID, body string, basisSeq int64, now time.Time) (Draft, DraftDelta, error) {
	id, err := randomID("draft", 12)
	if err != nil {
		return Draft{}, DraftDelta{}, err
	}
	draft := Draft{
		ID: id, AgentID: agentID, RoomID: roomID, ThreadID: threadID, Body: body,
		BasisSeq: basisSeq, HoldCount: 1, State: DraftHeld, CreatedAt: now, UpdatedAt: now,
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO drafts (id, agent_id, room_id, thread_id, body, basis_seq, hold_count, state, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'held', ?, ?)`,
		draft.ID, draft.AgentID, draft.RoomID, nullableString(draft.ThreadID), draft.Body,
		draft.BasisSeq, draft.HoldCount, toMillis(draft.CreatedAt), toMillis(draft.UpdatedAt)); err != nil {
		return Draft{}, DraftDelta{}, fmt.Errorf("insert held draft: %w", err)
	}
	delta, err := draftDeltaTx(ctx, tx, roomID, threadID, basisSeq)
	return draft, delta, err
}

func reholdDraftTx(ctx context.Context, tx *sql.Tx, draft Draft, basisSeq int64, now time.Time) (Draft, DraftDelta, error) {
	draft.BasisSeq = basisSeq
	draft.HoldCount++
	draft.UpdatedAt = now
	result, err := tx.ExecContext(ctx, `
		UPDATE drafts SET basis_seq = ?, hold_count = ?, updated_at = ? WHERE id = ? AND state = 'held'`,
		draft.BasisSeq, draft.HoldCount, toMillis(draft.UpdatedAt), draft.ID)
	if err != nil {
		return Draft{}, DraftDelta{}, fmt.Errorf("update held draft: %w", err)
	}
	if changed, err := result.RowsAffected(); err != nil {
		return Draft{}, DraftDelta{}, fmt.Errorf("count held draft update: %w", err)
	} else if changed != 1 {
		return Draft{}, DraftDelta{}, fmt.Errorf("%w: draft %q is no longer held", ErrConflict, draft.ID)
	}
	delta, err := draftDeltaTx(ctx, tx, draft.RoomID, draft.ThreadID, basisSeq)
	return draft, delta, err
}

func draftDeltaTx(ctx context.Context, tx *sql.Tx, roomID, threadID string, basisSeq int64) (DraftDelta, error) {
	where := `room_id = ? AND thread_id IS NULL AND seq > ?`
	args := []any{roomID, basisSeq}
	if threadID != "" {
		where = `room_id = ? AND (id = ? OR thread_id = ?) AND seq > ?`
		args = []any{roomID, threadID, threadID, basisSeq}
	}
	var delta DraftDelta
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM room_messages WHERE `+where, args...).Scan(&delta.Count); err != nil {
		return DraftDelta{}, fmt.Errorf("count held draft delta: %w", err)
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id, seq, author_type, author_id, body FROM room_messages
		WHERE `+where+` ORDER BY seq LIMIT 50`, args...)
	if err != nil {
		return DraftDelta{}, fmt.Errorf("query held draft delta: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item DraftDeltaItem
		var body string
		if err := rows.Scan(&item.MessageID, &item.Seq, &item.AuthorType, &item.AuthorID, &body); err != nil {
			return DraftDelta{}, fmt.Errorf("scan held draft delta: %w", err)
		}
		item.Preview = preview(body, checkPreviewRunes)
		delta.Items = append(delta.Items, item)
	}
	if err := rows.Err(); err != nil {
		return DraftDelta{}, fmt.Errorf("iterate held draft delta: %w", err)
	}
	return delta, nil
}

func loadDraftTx(ctx context.Context, tx *sql.Tx, draftID string) (Draft, error) {
	return scanDraft(tx.QueryRowContext(ctx, `
		SELECT id, agent_id, room_id, COALESCE(thread_id, ''), body, basis_seq,
			hold_count, state, created_at, updated_at FROM drafts WHERE id = ?`, draftID))
}

func scanDraft(row scanner) (Draft, error) {
	var draft Draft
	var createdAt, updatedAt int64
	if err := row.Scan(
		&draft.ID, &draft.AgentID, &draft.RoomID, &draft.ThreadID, &draft.Body,
		&draft.BasisSeq, &draft.HoldCount, &draft.State, &createdAt, &updatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Draft{}, ErrNotFound
		}
		return Draft{}, fmt.Errorf("scan chat draft: %w", err)
	}
	draft.CreatedAt = fromMillis(createdAt)
	draft.UpdatedAt = fromMillis(updatedAt)
	return draft, nil
}

func (s *Service) ListDrafts(ctx context.Context, agentID, token string) ([]Draft, error) {
	if _, err := s.AuthenticateAgent(ctx, agentID, token); err != nil {
		return nil, err
	}
	agentID = strings.TrimSpace(agentID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.expireDraftsLocked(ctx); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, agent_id, room_id, COALESCE(thread_id, ''), body, basis_seq,
			hold_count, state, created_at, updated_at
		FROM drafts WHERE agent_id = ? AND state = 'held' ORDER BY created_at, id`, agentID)
	if err != nil {
		return nil, fmt.Errorf("list held drafts: %w", err)
	}
	defer rows.Close()
	drafts := make([]Draft, 0)
	for rows.Next() {
		draft, err := scanDraft(rows)
		if err != nil {
			return nil, err
		}
		drafts = append(drafts, draft)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate held drafts: %w", err)
	}
	return drafts, nil
}

func (s *Service) ResolveDraft(ctx context.Context, params ResolveDraftParams) (DraftResult, error) {
	if _, err := s.AuthenticateAgent(ctx, params.AgentID, params.Token); err != nil {
		return DraftResult{}, err
	}
	if err := s.ExpireDrafts(ctx); err != nil {
		return DraftResult{}, err
	}
	params.AgentID = strings.TrimSpace(params.AgentID)
	params.DraftID = strings.TrimSpace(params.DraftID)
	if params.DraftID == "" {
		return DraftResult{}, errors.New("chat draft id is required")
	}
	if params.Resolution == DraftSilent {
		return s.dropDraft(ctx, params.AgentID, params.DraftID)
	}
	if params.Resolution != DraftAsIs && params.Resolution != DraftAnyway {
		return DraftResult{}, errors.New("chat draft resolution must be as_is, silent, or anyway")
	}
	if params.Resolution == DraftAsIs && params.BasisSeq == nil {
		return DraftResult{}, errors.New("chat draft as_is requires basis_seq")
	}
	if params.BasisSeq != nil && *params.BasisSeq < 0 {
		return DraftResult{}, errors.New("chat draft basis sequence cannot be negative")
	}
	draft, err := scanDraft(s.db.QueryRowContext(ctx, `
		SELECT id, agent_id, room_id, COALESCE(thread_id, ''), body, basis_seq,
			hold_count, state, created_at, updated_at FROM drafts WHERE id = ?`, params.DraftID))
	if err != nil {
		return DraftResult{}, err
	}
	if draft.AgentID != params.AgentID {
		return DraftResult{}, ErrUnauthorized
	}
	basisSeq := draft.BasisSeq
	if params.BasisSeq != nil {
		basisSeq = *params.BasisSeq
	}
	result, err := s.send(ctx, sendParams{
		RoomID: draft.RoomID, AuthorType: MemberAgent, AuthorID: draft.AgentID,
		ThreadID: draft.ThreadID, Body: draft.Body, BasisSeq: basisSeq,
		DraftID: draft.ID, Force: params.Resolution == DraftAnyway,
	})
	if err != nil {
		return DraftResult{}, err
	}
	if result.Status == SendHeld {
		return DraftResult{Status: SendHeld, Draft: *result.Draft, Delta: result.Delta}, nil
	}
	draft.State = DraftCommitted
	draft.UpdatedAt = fromMillis(toMillis(s.now()))
	return DraftResult{Status: SendCommitted, Draft: draft, Message: &result.Message}, nil
}

func (s *Service) dropDraft(ctx context.Context, agentID, draftID string) (DraftResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return DraftResult{}, fmt.Errorf("begin drop chat draft: %w", err)
	}
	defer tx.Rollback()
	draft, err := loadDraftTx(ctx, tx, draftID)
	if err != nil {
		return DraftResult{}, err
	}
	if draft.AgentID != agentID {
		return DraftResult{}, ErrUnauthorized
	}
	if draft.State != DraftHeld {
		return DraftResult{}, fmt.Errorf("%w: draft %q is %s", ErrConflict, draft.ID, draft.State)
	}
	draft.State = DraftDropped
	draft.UpdatedAt = fromMillis(toMillis(s.now()))
	if _, err := tx.ExecContext(ctx, `UPDATE drafts SET state = 'dropped', updated_at = ? WHERE id = ?`, toMillis(draft.UpdatedAt), draft.ID); err != nil {
		return DraftResult{}, fmt.Errorf("drop chat draft: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return DraftResult{}, fmt.Errorf("commit dropped chat draft: %w", err)
	}
	return DraftResult{Draft: draft}, nil
}

func (s *Service) ExpireDrafts(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.expireDraftsLocked(ctx)
}

func (s *Service) expireDraftsLocked(ctx context.Context) error {
	cutoff := toMillis(s.now().Add(-DraftExpiry))
	if _, err := s.db.ExecContext(ctx, `UPDATE drafts SET state = 'expired', updated_at = ? WHERE state = 'held' AND updated_at < ?`, toMillis(s.now()), cutoff); err != nil {
		return fmt.Errorf("expire held drafts: %w", err)
	}
	return nil
}
