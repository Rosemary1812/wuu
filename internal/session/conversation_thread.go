package session

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

type ConversationThreadStatus string

const (
	ConversationThreadOpen     ConversationThreadStatus = "open"
	ConversationThreadResolved ConversationThreadStatus = "resolved"
)

var ErrConversationThreadNotFound = errors.New("conversation thread not found")

type ConversationThread struct {
	ID           string                   `json:"id"`
	SessionID    string                   `json:"session_id"`
	AnchorItemID string                   `json:"anchor_item_id"`
	Title        string                   `json:"title,omitempty"`
	Status       ConversationThreadStatus `json:"status"`
	CreatedBy    string                   `json:"created_by,omitempty"`
	CreatedAt    time.Time                `json:"created_at"`
}

func NewConversationThreadID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "cth-" + hex.EncodeToString(b)
}

func CreateConversationThread(sessDir string, thread ConversationThread) (ConversationThread, error) {
	db, err := openStore(sessDir)
	if err != nil {
		return ConversationThread{}, err
	}
	defer db.Close()

	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()

	tx, err := db.Begin()
	if err != nil {
		return ConversationThread{}, fmt.Errorf("begin conversation thread create: %w", err)
	}
	defer tx.Rollback()

	thread = normalizeConversationThread(thread)
	if thread.SessionID == "" {
		return ConversationThread{}, fmt.Errorf("%w: %q", ErrSessionNotFound, thread.SessionID)
	}
	if thread.AnchorItemID == "" {
		return ConversationThread{}, fmt.Errorf("conversation thread anchor item is required")
	}
	if err := validateConversationThreadStatus(thread.Status); err != nil {
		return ConversationThread{}, err
	}
	exists, err := sessionExistsTx(tx, thread.SessionID)
	if err != nil {
		return ConversationThread{}, err
	}
	if !exists {
		return ConversationThread{}, fmt.Errorf("%w: %q", ErrSessionNotFound, thread.SessionID)
	}
	if thread.ID == "" {
		thread.ID = NewConversationThreadID()
	}
	if thread.CreatedAt.IsZero() {
		thread.CreatedAt = time.Now().UTC()
	}

	if _, err := tx.Exec(`
INSERT INTO conversation_threads (
	id, session_id, anchor_item_id, title, status, created_by, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		thread.ID, thread.SessionID, thread.AnchorItemID, thread.Title, string(thread.Status), thread.CreatedBy, timeText(thread.CreatedAt),
	); err != nil {
		return ConversationThread{}, fmt.Errorf("create conversation thread: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return ConversationThread{}, fmt.Errorf("commit conversation thread create: %w", err)
	}
	return thread, nil
}

func ListConversationThreads(sessDir, sessionID string) ([]ConversationThread, error) {
	sessionID = strings.TrimSpace(sessionID)
	db, err := openStore(sessDir)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	exists, err := sessionExistsDB(db, sessionID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("%w: %q", ErrSessionNotFound, sessionID)
	}

	rows, err := db.Query(`
SELECT id, session_id, anchor_item_id, title, status, created_by, created_at
FROM conversation_threads
WHERE session_id = ?
ORDER BY created_at ASC, id ASC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list conversation threads: %w", err)
	}
	defer rows.Close()

	var threads []ConversationThread
	for rows.Next() {
		thread, err := scanConversationThread(rows)
		if err != nil {
			return nil, fmt.Errorf("scan conversation threads: %w", err)
		}
		threads = append(threads, thread)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan conversation threads: %w", err)
	}
	return threads, nil
}

func UpdateConversationThreadStatus(sessDir, id string, status ConversationThreadStatus) error {
	id = strings.TrimSpace(id)
	status = normalizeConversationThreadStatus(status)
	if err := validateConversationThreadStatus(status); err != nil {
		return err
	}

	db, err := openStore(sessDir)
	if err != nil {
		return err
	}
	defer db.Close()

	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()

	res, err := db.Exec(`
UPDATE conversation_threads
SET status = ?
WHERE id = ?`, string(status), id)
	if err != nil {
		return fmt.Errorf("update conversation thread status: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update conversation thread status: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: %q", ErrConversationThreadNotFound, id)
	}
	return nil
}

func normalizeConversationThread(thread ConversationThread) ConversationThread {
	thread.ID = strings.TrimSpace(thread.ID)
	thread.SessionID = strings.TrimSpace(thread.SessionID)
	thread.AnchorItemID = strings.TrimSpace(thread.AnchorItemID)
	thread.Title = strings.TrimSpace(thread.Title)
	thread.CreatedBy = strings.TrimSpace(thread.CreatedBy)
	thread.Status = normalizeConversationThreadStatus(thread.Status)
	return thread
}

func normalizeConversationThreadStatus(status ConversationThreadStatus) ConversationThreadStatus {
	switch ConversationThreadStatus(strings.ToLower(strings.TrimSpace(string(status)))) {
	case "":
		return ConversationThreadOpen
	case ConversationThreadOpen:
		return ConversationThreadOpen
	case ConversationThreadResolved:
		return ConversationThreadResolved
	default:
		return ConversationThreadStatus(strings.ToLower(strings.TrimSpace(string(status))))
	}
}

func validateConversationThreadStatus(status ConversationThreadStatus) error {
	switch status {
	case ConversationThreadOpen, ConversationThreadResolved:
		return nil
	default:
		return fmt.Errorf("invalid conversation thread status %q", status)
	}
}

func scanConversationThread(scanner interface {
	Scan(dest ...any) error
}) (ConversationThread, error) {
	var thread ConversationThread
	var status, createdAt string
	if err := scanner.Scan(
		&thread.ID, &thread.SessionID, &thread.AnchorItemID, &thread.Title, &status, &thread.CreatedBy, &createdAt,
	); err != nil {
		return ConversationThread{}, err
	}
	thread.Status = ConversationThreadStatus(status)
	thread.CreatedAt = parseTime(createdAt)
	return thread, nil
}
