package channels

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

func (s *Service) MarkWakePending(ctx context.Context, agentID string) error {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return errors.New("wake agent is required")
	}
	now := toMillis(s.now())
	result, err := s.db.ExecContext(ctx, `
		UPDATE agent_wake_state SET pending = 1, updated_at = ?
		WHERE agent_id = ? AND outstanding = 1`, now, agentID)
	if err != nil {
		return fmt.Errorf("mark agent wake pending: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count pending agent wake: %w", err)
	}
	if rows == 0 {
		if _, err := s.GetNamedAgent(ctx, agentID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) TakePendingWake(ctx context.Context, agentID string) (bool, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return false, errors.New("wake agent is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin pending wake take: %w", err)
	}
	defer tx.Rollback()
	var pending, outstanding int
	err = tx.QueryRowContext(ctx, `
		SELECT pending, outstanding FROM agent_wake_state WHERE agent_id = ?`, agentID,
	).Scan(&pending, &outstanding)
	if errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("%w: named agent %q", ErrNotFound, agentID)
	}
	if err != nil {
		return false, fmt.Errorf("read pending agent wake: %w", err)
	}
	if pending != 0 {
		if _, err := tx.ExecContext(ctx, `
			UPDATE agent_wake_state SET pending = 0, updated_at = ? WHERE agent_id = ?`,
			toMillis(s.now()), agentID); err != nil {
			return false, fmt.Errorf("clear pending agent wake: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit pending wake take: %w", err)
	}
	return pending != 0 && outstanding != 0, nil
}

func (s *Service) ClearWakeOnCheck(ctx context.Context, agentID string) error {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return errors.New("wake agent is required")
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE agent_wake_state SET outstanding = 0, pending = 0, updated_at = ? WHERE agent_id = ?`,
		toMillis(s.now()), agentID)
	if err != nil {
		return fmt.Errorf("clear checked agent wake: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count checked agent wake: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("%w: named agent %q", ErrNotFound, agentID)
	}
	return nil
}
