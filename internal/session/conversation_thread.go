package session

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

type ConversationThreadStatus string

const (
	ConversationThreadOpen     ConversationThreadStatus = "open"
	ConversationThreadTask     ConversationThreadStatus = "task"
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
	// EscalatedAt / EscalatedBy mark that this reply subthread was promoted to a
	// task by a human (open -> task). EscalatedAt stays set even after the task
	// resolves (status -> resolved), so a resolved cth still carries "this was a
	// task" — a plain reply resolve leaves EscalatedAt zero. Summary is the
	// one-line conclusion bubbled back to the main stream when the task wraps up.
	EscalatedAt time.Time `json:"escalated_at,omitempty"`
	EscalatedBy string    `json:"escalated_by,omitempty"`
	Summary     string    `json:"summary,omitempty"`
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
SELECT id, session_id, anchor_item_id, title, status, created_by, created_at, escalated_at, escalated_by, summary
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

// FindConversationThreadByID loads a single conversation subthread by its id
// (cth-*). It returns ErrConversationThreadNotFound when no row matches. Unlike
// findConversationSubthread (which scans a parent session's threads by anchor),
// this resolves a subthread id directly to its record — including SessionID, the
// parent (group) thread the subthread hangs off of.
func FindConversationThreadByID(sessDir, id string) (ConversationThread, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return ConversationThread{}, fmt.Errorf("%w: %q", ErrConversationThreadNotFound, id)
	}
	db, err := openStore(sessDir)
	if err != nil {
		return ConversationThread{}, err
	}
	defer db.Close()

	row := db.QueryRow(`
SELECT id, session_id, anchor_item_id, title, status, created_by, created_at, escalated_at, escalated_by, summary
FROM conversation_threads
WHERE id = ?`, id)
	thread, err := scanConversationThread(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ConversationThread{}, fmt.Errorf("%w: %q", ErrConversationThreadNotFound, id)
		}
		return ConversationThread{}, fmt.Errorf("find conversation thread: %w", err)
	}
	return thread, nil
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
	case ConversationThreadTask:
		return ConversationThreadTask
	case ConversationThreadResolved:
		return ConversationThreadResolved
	default:
		return ConversationThreadStatus(strings.ToLower(strings.TrimSpace(string(status))))
	}
}

func validateConversationThreadStatus(status ConversationThreadStatus) error {
	switch status {
	case ConversationThreadOpen, ConversationThreadTask, ConversationThreadResolved:
		return nil
	default:
		return fmt.Errorf("invalid conversation thread status %q", status)
	}
}

// EscalateConversationThread promotes a reply subthread to a task: it advances
// the status from the discussion state (open) to the execution state (task) and
// records who escalated it plus the escalation time. It is idempotent — calling
// it on an already-escalated (task) thread just refreshes escalated_by/title and
// leaves escalated_at pinned to the first escalation. A resolved thread is
// re-opened into the task state so the human can re-run it. The escalation entry
// is a client RPC (human click); agents have no tool that reaches this.
func EscalateConversationThread(sessDir, id, escalatedBy, title string) (ConversationThread, error) {
	id = strings.TrimSpace(id)
	escalatedBy = strings.TrimSpace(escalatedBy)
	title = strings.TrimSpace(title)
	if id == "" {
		return ConversationThread{}, fmt.Errorf("%w: %q", ErrConversationThreadNotFound, id)
	}

	db, err := openStore(sessDir)
	if err != nil {
		return ConversationThread{}, err
	}
	defer db.Close()

	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()

	tx, err := db.Begin()
	if err != nil {
		return ConversationThread{}, fmt.Errorf("begin conversation thread escalate: %w", err)
	}
	defer tx.Rollback()

	row := tx.QueryRow(`
SELECT id, session_id, anchor_item_id, title, status, created_by, created_at, escalated_at, escalated_by, summary
FROM conversation_threads
WHERE id = ?`, id)
	thread, err := scanConversationThread(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ConversationThread{}, fmt.Errorf("%w: %q", ErrConversationThreadNotFound, id)
		}
		return ConversationThread{}, fmt.Errorf("find conversation thread: %w", err)
	}

	thread.Status = ConversationThreadTask
	if thread.EscalatedAt.IsZero() {
		thread.EscalatedAt = time.Now().UTC()
	}
	if escalatedBy != "" {
		thread.EscalatedBy = escalatedBy
	}
	if title != "" {
		thread.Title = title
	}

	if _, err := tx.Exec(`
UPDATE conversation_threads
SET status = ?, title = ?, escalated_at = ?, escalated_by = ?
WHERE id = ?`,
		string(thread.Status), thread.Title, timeText(thread.EscalatedAt), thread.EscalatedBy, id,
	); err != nil {
		return ConversationThread{}, fmt.Errorf("escalate conversation thread: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return ConversationThread{}, fmt.Errorf("commit conversation thread escalate: %w", err)
	}
	return thread, nil
}

// SetConversationThreadSummary stores the one-line conclusion for a subthread
// (the same text bubbled back to the main stream). It only writes the summary
// column; status transitions go through UpdateConversationThreadStatus so the
// two concerns stay composable (resolve = set summary + set status resolved).
func SetConversationThreadSummary(sessDir, id, summary string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("%w: %q", ErrConversationThreadNotFound, id)
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
SET summary = ?
WHERE id = ?`, strings.TrimSpace(summary), id)
	if err != nil {
		return fmt.Errorf("update conversation thread summary: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update conversation thread summary: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: %q", ErrConversationThreadNotFound, id)
	}
	return nil
}

func scanConversationThread(scanner interface {
	Scan(dest ...any) error
}) (ConversationThread, error) {
	var thread ConversationThread
	var status, createdAt, escalatedAt string
	if err := scanner.Scan(
		&thread.ID, &thread.SessionID, &thread.AnchorItemID, &thread.Title, &status, &thread.CreatedBy, &createdAt,
		&escalatedAt, &thread.EscalatedBy, &thread.Summary,
	); err != nil {
		return ConversationThread{}, err
	}
	thread.Status = ConversationThreadStatus(status)
	thread.CreatedAt = parseTime(createdAt)
	thread.EscalatedAt = parseTime(escalatedAt)
	return thread, nil
}
