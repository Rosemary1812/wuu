package session

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Task event kinds — the vocabulary of the durable task execution trace
// (2026-07-07 orchestration mechanism). Success-path events prove liveness and
// progress; failure-path events give the lead/supervisor enough to replay and
// decide retry / fallback / replan / ask-user / abort.
const (
	TaskEventTaskCreated     = "task_created"
	TaskEventWorkflowPlanned = "workflow_planned"
	TaskEventNodeStarted     = "node_started"
	TaskEventCommentary      = "commentary"
	TaskEventToolCall        = "tool_call"
	TaskEventToolResult      = "tool_result"
	TaskEventNodeProgress    = "node_progress"
	TaskEventHandoffCreated  = "handoff_created"
	TaskEventNodeSucceeded   = "node_succeeded"
	TaskEventNodeFailed      = "node_failed"
	TaskEventRetrying        = "retrying"
	TaskEventBlocked         = "blocked"
	TaskEventLeadInvoked     = "lead_invoked"
	TaskEventTaskCompleted   = "task_completed"
)

// TaskEvent is one row of the append-only task trace.
type TaskEvent struct {
	ID        string
	SessionID string // parent group thread id
	TaskID    string // the cth id upgraded to a task
	NodeID    string // plan piece id; empty for task-level events
	Seq       int    // per-task monotonic order (output-only; assigned on append)
	Kind      string
	Actor     string // participant id that produced the event, when applicable
	Summary   string // short human-readable line
	Payload   string // optional structured JSON (handoff payload, error, etc.)
	At        time.Time
}

// AppendTaskEvent appends one event to a task's durable trace, assigning the
// next per-task seq under the store write lock. Append-only: events are never
// updated or deleted, so the trace is a faithful replay of what happened.
func AppendTaskEvent(sessDir string, ev TaskEvent) (TaskEvent, error) {
	ev.SessionID = strings.TrimSpace(ev.SessionID)
	ev.TaskID = strings.TrimSpace(ev.TaskID)
	ev.NodeID = strings.TrimSpace(ev.NodeID)
	ev.Kind = strings.TrimSpace(ev.Kind)
	ev.Actor = strings.TrimSpace(ev.Actor)
	if ev.TaskID == "" {
		return TaskEvent{}, errors.New("task_id is required")
	}
	if ev.Kind == "" {
		return TaskEvent{}, errors.New("kind is required")
	}
	if ev.At.IsZero() {
		ev.At = time.Now().UTC()
	} else {
		ev.At = ev.At.UTC()
	}
	if strings.TrimSpace(ev.ID) == "" {
		ev.ID = "tev-" + NewID()
	}

	db, err := openStore(sessDir)
	if err != nil {
		return TaskEvent{}, err
	}
	defer db.Close()

	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()

	tx, err := db.Begin()
	if err != nil {
		return TaskEvent{}, fmt.Errorf("begin task event append: %w", err)
	}
	defer tx.Rollback()

	var maxSeq sql.NullInt64
	if err := tx.QueryRow(`SELECT MAX(seq) FROM task_events WHERE task_id = ?`, ev.TaskID).Scan(&maxSeq); err != nil {
		return TaskEvent{}, fmt.Errorf("next task event seq: %w", err)
	}
	ev.Seq = int(maxSeq.Int64) + 1

	if _, err := tx.Exec(`
INSERT INTO task_events (id, session_id, task_id, node_id, seq, kind, actor, summary, payload, at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ev.ID, ev.SessionID, ev.TaskID, ev.NodeID, ev.Seq, ev.Kind, ev.Actor, ev.Summary, ev.Payload, ev.At.UnixMilli(),
	); err != nil {
		return TaskEvent{}, fmt.Errorf("insert task event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return TaskEvent{}, fmt.Errorf("commit task event: %w", err)
	}
	return ev, nil
}

// TaskEvents returns a task's full trace in order. This is what the lead reads
// on failure to understand what happened before deciding how to recover.
func TaskEvents(sessDir, taskID string) ([]TaskEvent, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, errors.New("task_id is required")
	}
	db, err := openStore(sessDir)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`
SELECT id, session_id, task_id, node_id, seq, kind, actor, summary, payload, at
FROM task_events WHERE task_id = ? ORDER BY seq ASC`, taskID)
	if err != nil {
		return nil, fmt.Errorf("load task events: %w", err)
	}
	defer rows.Close()

	var out []TaskEvent
	for rows.Next() {
		var ev TaskEvent
		var at int64
		if err := rows.Scan(&ev.ID, &ev.SessionID, &ev.TaskID, &ev.NodeID, &ev.Seq, &ev.Kind, &ev.Actor, &ev.Summary, &ev.Payload, &at); err != nil {
			return nil, fmt.Errorf("scan task event: %w", err)
		}
		ev.At = time.UnixMilli(at).UTC()
		out = append(out, ev)
	}
	return out, rows.Err()
}
