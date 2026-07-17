package session

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

const (
	HeldUserWorkOriginQueue = "queue"
	HeldUserWorkOriginSteer = "steer"
)

type HeldUserWork struct {
	SessionID   string
	Position    int
	ID          string
	Origin      string
	MessageJSON []byte
	RuntimeJSON []byte
}

func ReplaceHeldUserWork(sessDir, sessionID string, entries []HeldUserWork) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("session_id is required")
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
		return fmt.Errorf("begin held user work replacement: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM held_user_work WHERE session_id = ?`, sessionID); err != nil {
		return fmt.Errorf("clear held user work: %w", err)
	}
	for position, entry := range entries {
		id := strings.TrimSpace(entry.ID)
		origin := strings.TrimSpace(entry.Origin)
		if id == "" {
			return fmt.Errorf("held user work %d: id is required", position)
		}
		if origin != HeldUserWorkOriginQueue && origin != HeldUserWorkOriginSteer {
			return fmt.Errorf("held user work %q: invalid origin %q", id, origin)
		}
		if len(entry.MessageJSON) == 0 || len(entry.RuntimeJSON) == 0 {
			return fmt.Errorf("held user work %q: message and runtime are required", id)
		}
		if _, err := tx.Exec(`
INSERT INTO held_user_work(session_id, position, id, origin, message_json, runtime_json)
VALUES (?, ?, ?, ?, ?, ?)`, sessionID, position, id, origin, string(entry.MessageJSON), string(entry.RuntimeJSON)); err != nil {
			return fmt.Errorf("insert held user work %q: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit held user work replacement: %w", err)
	}
	return nil
}

func LoadHeldUserWork(sessDir, sessionID string) ([]HeldUserWork, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, errors.New("session_id is required")
	}
	db, err := openStore(sessDir)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`
SELECT position, id, origin, message_json, runtime_json
FROM held_user_work
WHERE session_id = ?
ORDER BY position`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("load held user work: %w", err)
	}
	defer rows.Close()
	var entries []HeldUserWork
	for rows.Next() {
		var entry HeldUserWork
		var messageJSON, runtimeJSON string
		entry.SessionID = sessionID
		if err := rows.Scan(&entry.Position, &entry.ID, &entry.Origin, &messageJSON, &runtimeJSON); err != nil {
			return nil, fmt.Errorf("scan held user work: %w", err)
		}
		entry.MessageJSON = []byte(messageJSON)
		entry.RuntimeJSON = []byte(runtimeJSON)
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate held user work: %w", err)
	}
	return entries, nil
}

func DeleteHeldUserWork(sessDir, sessionID, id string) (bool, error) {
	sessionID = strings.TrimSpace(sessionID)
	id = strings.TrimSpace(id)
	if sessionID == "" || id == "" {
		return false, errors.New("session_id and id are required")
	}
	db, err := openStore(sessDir)
	if err != nil {
		return false, err
	}
	defer db.Close()
	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()
	tx, err := db.Begin()
	if err != nil {
		return false, fmt.Errorf("begin held user work deletion: %w", err)
	}
	defer tx.Rollback()
	var position int
	if err := tx.QueryRow(`SELECT position FROM held_user_work WHERE session_id = ? AND id = ?`, sessionID, id).Scan(&position); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("find held user work: %w", err)
	}
	res, err := tx.Exec(`DELETE FROM held_user_work WHERE session_id = ? AND id = ?`, sessionID, id)
	if err != nil {
		return false, fmt.Errorf("delete held user work: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("count deleted held user work: %w", err)
	}
	if affected > 0 {
		// Negate first so the UNIQUE(session_id, position) constraint cannot
		// collide while adjacent positions shift down by one.
		if _, err := tx.Exec(`UPDATE held_user_work SET position = -position - 1 WHERE session_id = ? AND position > ?`, sessionID, position); err != nil {
			return false, fmt.Errorf("stage held user work position compaction: %w", err)
		}
		if _, err := tx.Exec(`UPDATE held_user_work SET position = -position - 2 WHERE session_id = ? AND position < 0`, sessionID); err != nil {
			return false, fmt.Errorf("compact held user work positions: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit held user work deletion: %w", err)
	}
	return affected > 0, nil
}
