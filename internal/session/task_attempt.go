package session

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	TaskAttemptQueued      = "queued"
	TaskAttemptRunning     = "running"
	TaskAttemptSucceeded   = "succeeded"
	TaskAttemptFailed      = "failed"
	TaskAttemptInterrupted = "interrupted"
)

var ErrTaskAssigneeBusy = errors.New("task assignee already has an active attempt")

// TaskAttempt is one durable execution of one plan node. The attempt identity
// is carried in the dispatch envelope and remains stable from reservation
// through the resident turn's terminal outcome.
type TaskAttempt struct {
	ID              string
	SessionID       string
	TaskID          string
	NodeID          string
	AssigneeID      string
	Ordinal         int
	Status          string
	FailureCategory string
	FailureMessage  string
	CreatedAt       time.Time
	StartedAt       time.Time
	FinishedAt      time.Time
}

// ReserveTaskAttempt atomically reserves an assignee, creates a queued attempt,
// and binds it to the plan node. A named agent can have only one queued/running
// attempt across every Task in the store.
func ReserveTaskAttempt(sessDir, taskID, nodeID, assigneeID string) (TaskAttempt, ConversationThread, error) {
	taskID = strings.TrimSpace(taskID)
	nodeID = strings.TrimSpace(nodeID)
	assigneeID = strings.TrimSpace(assigneeID)
	if taskID == "" || nodeID == "" || assigneeID == "" {
		return TaskAttempt{}, ConversationThread{}, errors.New("task_id, node_id, and assignee_id are required")
	}
	db, err := openStore(sessDir)
	if err != nil {
		return TaskAttempt{}, ConversationThread{}, err
	}
	defer db.Close()
	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()
	tx, err := db.Begin()
	if err != nil {
		return TaskAttempt{}, ConversationThread{}, fmt.Errorf("begin task attempt reservation: %w", err)
	}
	defer tx.Rollback()

	thread, err := findConversationThreadByIDQuery(tx, taskID)
	if err != nil {
		return TaskAttempt{}, ConversationThread{}, err
	}
	if thread.Status != ConversationThreadTask || thread.ExecState != ExecStateExecuting {
		return TaskAttempt{}, thread, fmt.Errorf("reserve task attempt: task %q is %q/%q, not task/executing", taskID, thread.Status, thread.ExecState)
	}
	piece := taskPieceByID(thread.Plan, nodeID)
	if piece == nil {
		return TaskAttempt{}, thread, fmt.Errorf("reserve task attempt: task %q has no piece %q", taskID, nodeID)
	}
	if piece.Assignee != assigneeID {
		return TaskAttempt{}, thread, fmt.Errorf("reserve task attempt: piece %q is assigned to %q, not %q", nodeID, piece.Assignee, assigneeID)
	}
	if piece.Status != TaskPiecePending && piece.Status != TaskPieceRetrying {
		return TaskAttempt{}, thread, fmt.Errorf("reserve task attempt: piece %q is %q, not pending or retrying", nodeID, piece.Status)
	}
	if strings.TrimSpace(piece.CurrentAttemptID) != "" {
		return TaskAttempt{}, thread, fmt.Errorf("reserve task attempt: piece %q already has attempt %q", nodeID, piece.CurrentAttemptID)
	}
	var activeID string
	err = tx.QueryRow(`
SELECT id FROM task_attempts
WHERE assignee_id = ? AND status IN (?, ?)
LIMIT 1`, assigneeID, TaskAttemptQueued, TaskAttemptRunning).Scan(&activeID)
	if err == nil {
		return TaskAttempt{}, thread, fmt.Errorf("%w: %q (%s)", ErrTaskAssigneeBusy, assigneeID, activeID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return TaskAttempt{}, thread, fmt.Errorf("check active task attempt: %w", err)
	}

	now := time.Now().UTC()
	attempt := TaskAttempt{
		ID: "tat-" + NewID(), SessionID: thread.SessionID, TaskID: taskID,
		NodeID: nodeID, AssigneeID: assigneeID, Ordinal: piece.Attempts + 1,
		Status: TaskAttemptQueued, CreatedAt: now,
	}
	if attempt.Ordinal > piece.RetryBudget {
		return TaskAttempt{}, thread, fmt.Errorf("reserve task attempt: piece %q exhausted its %d-attempt budget", nodeID, piece.RetryBudget)
	}
	if _, err := tx.Exec(`
INSERT INTO task_attempts (
 id, session_id, task_id, node_id, assignee_id, ordinal, status,
 failure_category, failure_message, created_at, started_at, finished_at
) VALUES (?, ?, ?, ?, ?, ?, ?, '', '', ?, 0, 0)`,
		attempt.ID, attempt.SessionID, attempt.TaskID, attempt.NodeID,
		attempt.AssigneeID, attempt.Ordinal, attempt.Status, now.UnixMilli()); err != nil {
		return TaskAttempt{}, thread, fmt.Errorf("insert task attempt: %w", err)
	}
	piece.Status = TaskPieceActive
	piece.Attempts = attempt.Ordinal
	piece.CurrentAttemptID = attempt.ID
	piece.LastActivityAt = now
	if err := writeConversationThreadPlan(tx, thread); err != nil {
		return TaskAttempt{}, thread, err
	}
	if err := tx.Commit(); err != nil {
		return TaskAttempt{}, thread, fmt.Errorf("commit task attempt reservation: %w", err)
	}
	return attempt, thread, nil
}

// StartTaskAttempt marks the exact queued attempt as running when its resident
// turn actually starts. Repeating the same transition is idempotent.
func StartTaskAttempt(sessDir, attemptID string, at time.Time) (TaskAttempt, error) {
	attemptID = strings.TrimSpace(attemptID)
	if attemptID == "" {
		return TaskAttempt{}, errors.New("attempt_id is required")
	}
	if at.IsZero() {
		at = time.Now().UTC()
	} else {
		at = at.UTC()
	}
	db, err := openStore(sessDir)
	if err != nil {
		return TaskAttempt{}, err
	}
	defer db.Close()
	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()
	res, err := db.Exec(`
UPDATE task_attempts SET status = ?, started_at = ?
WHERE id = ? AND status = ?`, TaskAttemptRunning, at.UnixMilli(), attemptID, TaskAttemptQueued)
	if err != nil {
		return TaskAttempt{}, fmt.Errorf("start task attempt: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return TaskAttempt{}, fmt.Errorf("start task attempt: %w", err)
	}
	attempt, err := findTaskAttemptByIDQuery(db, attemptID)
	if err != nil {
		return TaskAttempt{}, err
	}
	if affected == 0 && attempt.Status != TaskAttemptRunning {
		return TaskAttempt{}, fmt.Errorf("start task attempt %q: status is %q", attemptID, attempt.Status)
	}
	return attempt, nil
}

// FinishTaskAttempt atomically terminates one exact attempt and applies the
// corresponding node state. Stale or duplicate turn completions cannot mutate a
// newer attempt because CurrentAttemptID must still match.
func FinishTaskAttempt(sessDir, attemptID, attemptStatus, pieceStatus, category, message string, at time.Time) (TaskAttempt, ConversationThread, error) {
	attemptID = strings.TrimSpace(attemptID)
	attemptStatus = strings.TrimSpace(attemptStatus)
	pieceStatus = strings.TrimSpace(pieceStatus)
	category = strings.TrimSpace(category)
	message = strings.TrimSpace(message)
	if attemptID == "" {
		return TaskAttempt{}, ConversationThread{}, errors.New("attempt_id is required")
	}
	if !validTerminalTaskAttemptStatus(attemptStatus) {
		return TaskAttempt{}, ConversationThread{}, fmt.Errorf("invalid terminal task attempt status %q", attemptStatus)
	}
	if !validFinishedPieceStatus(pieceStatus) {
		return TaskAttempt{}, ConversationThread{}, fmt.Errorf("invalid finished task piece status %q", pieceStatus)
	}
	if at.IsZero() {
		at = time.Now().UTC()
	} else {
		at = at.UTC()
	}
	db, err := openStore(sessDir)
	if err != nil {
		return TaskAttempt{}, ConversationThread{}, err
	}
	defer db.Close()
	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()
	tx, err := db.Begin()
	if err != nil {
		return TaskAttempt{}, ConversationThread{}, fmt.Errorf("begin task attempt finish: %w", err)
	}
	defer tx.Rollback()
	attempt, err := findTaskAttemptByIDQuery(tx, attemptID)
	if err != nil {
		return TaskAttempt{}, ConversationThread{}, err
	}
	thread, err := findConversationThreadByIDQuery(tx, attempt.TaskID)
	if err != nil {
		return TaskAttempt{}, ConversationThread{}, err
	}
	piece := taskPieceByID(thread.Plan, attempt.NodeID)
	if piece == nil {
		return TaskAttempt{}, thread, fmt.Errorf("finish task attempt: task %q has no piece %q", attempt.TaskID, attempt.NodeID)
	}
	if attempt.Status != TaskAttemptQueued && attempt.Status != TaskAttemptRunning {
		return TaskAttempt{}, thread, fmt.Errorf("finish task attempt %q: status is already %q", attemptID, attempt.Status)
	}
	if piece.CurrentAttemptID != attemptID {
		return TaskAttempt{}, thread, fmt.Errorf("finish task attempt %q: piece %q is bound to %q", attemptID, piece.ID, piece.CurrentAttemptID)
	}
	if _, err := tx.Exec(`
UPDATE task_attempts
SET status = ?, failure_category = ?, failure_message = ?, finished_at = ?
WHERE id = ? AND status IN (?, ?)`,
		attemptStatus, category, message, at.UnixMilli(), attemptID,
		TaskAttemptQueued, TaskAttemptRunning); err != nil {
		return TaskAttempt{}, thread, fmt.Errorf("finish task attempt: %w", err)
	}
	piece.Status = pieceStatus
	piece.CurrentAttemptID = ""
	piece.FailureReason = message
	piece.LastActivityAt = at
	if err := writeConversationThreadPlan(tx, thread); err != nil {
		return TaskAttempt{}, thread, err
	}
	if err := tx.Commit(); err != nil {
		return TaskAttempt{}, thread, fmt.Errorf("commit task attempt finish: %w", err)
	}
	attempt.Status = attemptStatus
	attempt.FailureCategory = category
	attempt.FailureMessage = message
	attempt.FinishedAt = at
	return attempt, thread, nil
}

func TaskAttemptByID(sessDir, attemptID string) (TaskAttempt, error) {
	db, err := openStore(sessDir)
	if err != nil {
		return TaskAttempt{}, err
	}
	defer db.Close()
	return findTaskAttemptByIDQuery(db, strings.TrimSpace(attemptID))
}

func TaskAttempts(sessDir, taskID string) ([]TaskAttempt, error) {
	db, err := openStore(sessDir)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`
SELECT id, session_id, task_id, node_id, assignee_id, ordinal, status,
       failure_category, failure_message, created_at, started_at, finished_at
FROM task_attempts WHERE task_id = ? ORDER BY created_at, id`, strings.TrimSpace(taskID))
	if err != nil {
		return nil, fmt.Errorf("list task attempts: %w", err)
	}
	defer rows.Close()
	var out []TaskAttempt
	for rows.Next() {
		attempt, err := scanTaskAttempt(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, attempt)
	}
	return out, rows.Err()
}

// SettleActiveTaskAttempts interrupts every queued/running attempt left by a
// dead process, clears its node binding, and pauses the Task without starting a
// model turn. The returned attempts are the durable recovery facts for tracing.
func SettleActiveTaskAttempts(sessDir string, at time.Time) ([]TaskAttempt, error) {
	if at.IsZero() {
		at = time.Now().UTC()
	} else {
		at = at.UTC()
	}
	db, err := openStore(sessDir)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()
	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin task attempt settle: %w", err)
	}
	defer tx.Rollback()
	rows, err := tx.Query(`
SELECT id, session_id, task_id, node_id, assignee_id, ordinal, status,
       failure_category, failure_message, created_at, started_at, finished_at
FROM task_attempts WHERE status IN (?, ?) ORDER BY created_at, id`, TaskAttemptQueued, TaskAttemptRunning)
	if err != nil {
		return nil, fmt.Errorf("list active task attempts: %w", err)
	}
	var attempts []TaskAttempt
	for rows.Next() {
		attempt, err := scanTaskAttempt(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		attempts = append(attempts, attempt)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range attempts {
		attempt := &attempts[i]
		thread, err := findConversationThreadByIDQuery(tx, attempt.TaskID)
		if err != nil {
			return nil, err
		}
		if piece := taskPieceByID(thread.Plan, attempt.NodeID); piece != nil && piece.CurrentAttemptID == attempt.ID {
			piece.CurrentAttemptID = ""
			piece.Status = TaskPieceBlocked
			piece.FailureReason = "interrupted by app restart"
			piece.LastActivityAt = at
			if err := writeConversationThreadPlan(tx, thread); err != nil {
				return nil, err
			}
			if _, err := tx.Exec(`
UPDATE conversation_threads SET exec_state = ? WHERE id = ? AND status = ?`,
				ExecStateBlocked, thread.ID, string(ConversationThreadTask)); err != nil {
				return nil, fmt.Errorf("pause interrupted task: %w", err)
			}
		}
		if _, err := tx.Exec(`
UPDATE task_attempts
SET status = ?, failure_category = 'restart', failure_message = ?, finished_at = ?
WHERE id = ?`, TaskAttemptInterrupted, "interrupted by app restart", at.UnixMilli(), attempt.ID); err != nil {
			return nil, fmt.Errorf("interrupt task attempt: %w", err)
		}
		attempt.Status = TaskAttemptInterrupted
		attempt.FailureCategory = "restart"
		attempt.FailureMessage = "interrupted by app restart"
		attempt.FinishedAt = at
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit task attempt settle: %w", err)
	}
	return attempts, nil
}

// ExecutingTaskThreads returns every live Task the scheduler may reconcile.
func ExecutingTaskThreads(sessDir string) ([]ConversationThread, error) {
	db, err := openStore(sessDir)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`
SELECT id, session_id, anchor_item_id, title, status, created_by, created_at,
       thread_owner_participant_id, escalated_at, escalated_by, summary,
       lead_participant_id, plan, exec_state, parent_seq, parent_author_participant_id
FROM conversation_threads WHERE status = ? AND exec_state = ?
ORDER BY escalated_at, id`, string(ConversationThreadTask), ExecStateExecuting)
	if err != nil {
		return nil, fmt.Errorf("list executing task threads: %w", err)
	}
	defer rows.Close()
	var out []ConversationThread
	for rows.Next() {
		thread, err := scanConversationThread(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, thread)
	}
	return out, rows.Err()
}

func findConversationThreadByIDQuery(q interface {
	QueryRow(query string, args ...any) *sql.Row
}, id string) (ConversationThread, error) {
	row := q.QueryRow(`
SELECT id, session_id, anchor_item_id, title, status, created_by, created_at,
       thread_owner_participant_id, escalated_at, escalated_by, summary,
       lead_participant_id, plan, exec_state, parent_seq, parent_author_participant_id
FROM conversation_threads WHERE id = ?`, id)
	thread, err := scanConversationThread(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ConversationThread{}, fmt.Errorf("%w: %q", ErrConversationThreadNotFound, id)
	}
	if err != nil {
		return ConversationThread{}, fmt.Errorf("find conversation thread: %w", err)
	}
	return thread, nil
}

func writeConversationThreadPlan(exec interface {
	Exec(query string, args ...any) (sql.Result, error)
}, thread ConversationThread) error {
	data, err := json.Marshal(thread.Plan)
	if err != nil {
		return fmt.Errorf("encode task plan: %w", err)
	}
	if _, err := exec.Exec(`UPDATE conversation_threads SET plan = ? WHERE id = ?`, string(data), thread.ID); err != nil {
		return fmt.Errorf("write task plan: %w", err)
	}
	return nil
}

func taskPieceByID(plan []TaskPiece, nodeID string) *TaskPiece {
	for i := range plan {
		if plan[i].ID == nodeID {
			return &plan[i]
		}
	}
	return nil
}

func findTaskAttemptByIDQuery(q interface {
	QueryRow(query string, args ...any) *sql.Row
}, attemptID string) (TaskAttempt, error) {
	row := q.QueryRow(`
SELECT id, session_id, task_id, node_id, assignee_id, ordinal, status,
       failure_category, failure_message, created_at, started_at, finished_at
FROM task_attempts WHERE id = ?`, attemptID)
	attempt, err := scanTaskAttempt(row)
	if errors.Is(err, sql.ErrNoRows) {
		return TaskAttempt{}, fmt.Errorf("task attempt not found: %q", attemptID)
	}
	return attempt, err
}

func scanTaskAttempt(scanner interface{ Scan(dest ...any) error }) (TaskAttempt, error) {
	var attempt TaskAttempt
	var createdAt, startedAt, finishedAt int64
	if err := scanner.Scan(
		&attempt.ID, &attempt.SessionID, &attempt.TaskID, &attempt.NodeID,
		&attempt.AssigneeID, &attempt.Ordinal, &attempt.Status,
		&attempt.FailureCategory, &attempt.FailureMessage,
		&createdAt, &startedAt, &finishedAt,
	); err != nil {
		return TaskAttempt{}, err
	}
	attempt.CreatedAt = unixMilliTime(createdAt)
	attempt.StartedAt = unixMilliTime(startedAt)
	attempt.FinishedAt = unixMilliTime(finishedAt)
	return attempt, nil
}

func unixMilliTime(value int64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(value).UTC()
}

func validTerminalTaskAttemptStatus(status string) bool {
	switch status {
	case TaskAttemptSucceeded, TaskAttemptFailed, TaskAttemptInterrupted:
		return true
	default:
		return false
	}
}

func validFinishedPieceStatus(status string) bool {
	switch status {
	case TaskPieceDone, TaskPieceRetrying, TaskPieceFailed, TaskPieceBlocked, TaskPieceCancelled:
		return true
	default:
		return false
	}
}
