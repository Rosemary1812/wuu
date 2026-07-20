package session

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/kanban"
)

// Kanban store: persistence for the kanban-OS domain (agent-neutral tasks,
// dispatch runs, produced artifacts). Timestamps use Unix milliseconds.

func newKanbanID(prefix string) string { return prefix + NewID() }

// CreateKanbanTask inserts a task. Initial status must be draft or ready.
func CreateKanbanTask(sessDir string, task kanban.Task) (kanban.Task, error) {
	task.SessionID = strings.TrimSpace(task.SessionID)
	task.Title = strings.TrimSpace(task.Title)
	if task.SessionID == "" || task.Title == "" {
		return kanban.Task{}, errors.New("session_id and title are required")
	}
	if task.Status == "" {
		task.Status = kanban.TaskStatusDraft
	}
	if task.Status != kanban.TaskStatusDraft && task.Status != kanban.TaskStatusReady {
		return kanban.Task{}, fmt.Errorf("create kanban task: initial status must be draft or ready, got %q", task.Status)
	}
	db, err := openStore(sessDir)
	if err != nil {
		return kanban.Task{}, err
	}
	defer db.Close()
	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()

	now := time.Now().UTC()
	if task.ID == "" {
		task.ID = newKanbanID("kt-")
	}
	task.LatestRunID = ""
	task.CreatedAt = now
	task.UpdatedAt = now
	_, err = db.Exec(`
INSERT INTO kanban_tasks (
 id, session_id, parent_id, title, brief, status, source_thread_id,
 created_by, sort_index, latest_run_id, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?, ?)`,
		task.ID, task.SessionID, task.ParentID, task.Title, task.Brief, task.Status,
		task.SourceThreadID, task.CreatedBy, task.SortIndex, now.UnixMilli(), now.UnixMilli())
	if err != nil {
		return kanban.Task{}, fmt.Errorf("insert kanban task: %w", err)
	}
	return task, nil
}

func scanKanbanTask(row interface{ Scan(...any) error }) (kanban.Task, error) {
	var t kanban.Task
	var createdMS, updatedMS int64
	err := row.Scan(&t.ID, &t.SessionID, &t.ParentID, &t.Title, &t.Brief, &t.Status,
		&t.SourceThreadID, &t.CreatedBy, &t.SortIndex, &t.LatestRunID, &createdMS, &updatedMS)
	if err != nil {
		return kanban.Task{}, err
	}
	t.CreatedAt = time.UnixMilli(createdMS).UTC()
	t.UpdatedAt = time.UnixMilli(updatedMS).UTC()
	return t, nil
}

const kanbanTaskColumns = `id, session_id, parent_id, title, brief, status, source_thread_id, created_by, sort_index, latest_run_id, created_at, updated_at`

// GetKanbanTask loads one task by id.
func GetKanbanTask(sessDir, taskID string) (kanban.Task, error) {
	db, err := openStore(sessDir)
	if err != nil {
		return kanban.Task{}, err
	}
	defer db.Close()
	t, err := scanKanbanTask(db.QueryRow(`SELECT `+kanbanTaskColumns+` FROM kanban_tasks WHERE id = ?`, taskID))
	if errors.Is(err, sql.ErrNoRows) {
		return kanban.Task{}, kanban.ErrTaskNotFound
	}
	return t, err
}

// ListKanbanTasks lists a session's tasks, ordered for board projection.
// parentID empty lists root tasks; pass a task id to list its subtasks.
func ListKanbanTasks(sessDir, sessionID, parentID string) ([]kanban.Task, error) {
	db, err := openStore(sessDir)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`SELECT `+kanbanTaskColumns+` FROM kanban_tasks
WHERE session_id = ? AND parent_id = ? ORDER BY sort_index, created_at`, sessionID, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []kanban.Task{}
	for rows.Next() {
		t, err := scanKanbanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListAllKanbanTasks lists every task in a session regardless of nesting,
// ordered by sort_index then creation — the auto-dispatch scan order.
func ListAllKanbanTasks(sessDir, sessionID string) ([]kanban.Task, error) {
	db, err := openStore(sessDir)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`SELECT `+kanbanTaskColumns+` FROM kanban_tasks
WHERE session_id = ? ORDER BY sort_index, created_at`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []kanban.Task{}
	for rows.Next() {
		t, err := scanKanbanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// TransitionKanbanTaskStatus moves a task through the legal status graph.
func TransitionKanbanTaskStatus(sessDir, taskID, to string) (kanban.Task, error) {
	db, err := openStore(sessDir)
	if err != nil {
		return kanban.Task{}, err
	}
	defer db.Close()
	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()
	tx, err := db.Begin()
	if err != nil {
		return kanban.Task{}, err
	}
	defer tx.Rollback()
	t, err := scanKanbanTask(tx.QueryRow(`SELECT `+kanbanTaskColumns+` FROM kanban_tasks WHERE id = ?`, taskID))
	if errors.Is(err, sql.ErrNoRows) {
		return kanban.Task{}, kanban.ErrTaskNotFound
	}
	if err != nil {
		return kanban.Task{}, err
	}
	if err := kanban.CheckTransition(t.Status, to); err != nil {
		return kanban.Task{}, err
	}
	now := time.Now().UTC()
	if _, err := tx.Exec(`UPDATE kanban_tasks SET status = ?, updated_at = ? WHERE id = ?`, to, now.UnixMilli(), taskID); err != nil {
		return kanban.Task{}, fmt.Errorf("transition kanban task: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return kanban.Task{}, err
	}
	t.Status = to
	t.UpdatedAt = now
	return t, nil
}

func scanKanbanRun(row interface{ Scan(...any) error }) (kanban.Run, error) {
	var r kanban.Run
	var createdMS, startedMS, finishedMS int64
	err := row.Scan(&r.ID, &r.TaskID, &r.SessionID, &r.Kind, &r.TargetID, &r.ThreadID,
		&r.HostThreadID, &r.Status, &r.Summary, &r.ErrorMessage, &r.CreatedBy, &createdMS, &startedMS, &finishedMS)
	if err != nil {
		return kanban.Run{}, err
	}
	r.CreatedAt = time.UnixMilli(createdMS).UTC()
	r.StartedAt = time.UnixMilli(startedMS).UTC()
	r.FinishedAt = time.UnixMilli(finishedMS).UTC()
	return r, nil
}

const kanbanRunColumns = `id, task_id, session_id, kind, target_id, thread_id, host_thread_id, status, summary, error_message, created_by, created_at, started_at, finished_at`

// CreateKanbanRun is the dispatch action: it binds a task to a target
// participant as a queued run and moves the task to running in one
// transaction. The target must be a live named participant with no active
// run; the busy reservation is pre-checked and backstopped by the partial
// unique index idx_kanban_runs_active_target.
func CreateKanbanRun(sessDir string, run kanban.Run) (kanban.Run, error) {
	run.TaskID = strings.TrimSpace(run.TaskID)
	run.TargetID = strings.TrimSpace(run.TargetID)
	if run.TaskID == "" || run.TargetID == "" {
		return kanban.Run{}, errors.New("task_id and target_id are required")
	}
	if run.Kind == "" {
		run.Kind = kanban.RunKindExecute
	}
	if run.Kind != kanban.RunKindExecute && run.Kind != kanban.RunKindReview {
		return kanban.Run{}, fmt.Errorf("create kanban run: unknown kind %q", run.Kind)
	}
	db, err := openStore(sessDir)
	if err != nil {
		return kanban.Run{}, err
	}
	defer db.Close()
	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()
	tx, err := db.Begin()
	if err != nil {
		return kanban.Run{}, err
	}
	defer tx.Rollback()

	task, err := scanKanbanTask(tx.QueryRow(`SELECT `+kanbanTaskColumns+` FROM kanban_tasks WHERE id = ?`, run.TaskID))
	if errors.Is(err, sql.ErrNoRows) {
		return kanban.Run{}, kanban.ErrTaskNotFound
	}
	if err != nil {
		return kanban.Run{}, err
	}
	if !kanban.Dispatchable(task.Status) {
		return kanban.Run{}, fmt.Errorf("%w: task %q is %q", kanban.ErrTaskNotReady, run.TaskID, task.Status)
	}
	var kind string
	var retiredAt any
	err = tx.QueryRow(`SELECT kind, retired_at FROM participants WHERE id = ?`, run.TargetID).Scan(&kind, &retiredAt)
	if errors.Is(err, sql.ErrNoRows) {
		return kanban.Run{}, fmt.Errorf("create kanban run: target %q: %w", run.TargetID, ErrParticipantNotFound)
	}
	if err != nil {
		return kanban.Run{}, err
	}
	if retiredAt != nil {
		return kanban.Run{}, fmt.Errorf("create kanban run: target %q is retired", run.TargetID)
	}
	var activeID string
	err = tx.QueryRow(`SELECT id FROM kanban_runs WHERE target_id = ? AND status IN (?, ?) LIMIT 1`,
		run.TargetID, kanban.RunStatusQueued, kanban.RunStatusRunning).Scan(&activeID)
	if err == nil {
		return kanban.Run{}, fmt.Errorf("%w: %q (%s)", kanban.ErrTargetBusy, run.TargetID, activeID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return kanban.Run{}, fmt.Errorf("check active kanban run: %w", err)
	}

	now := time.Now().UTC()
	run.ID = newKanbanID("kr-")
	run.SessionID = task.SessionID
	run.Status = kanban.RunStatusQueued
	run.ThreadID = ""
	run.CreatedAt = now
	_, err = tx.Exec(`
INSERT INTO kanban_runs (
 id, task_id, session_id, kind, target_id, thread_id, host_thread_id, status, summary,
 error_message, created_by, created_at, started_at, finished_at
) VALUES (?, ?, ?, ?, ?, '', ?, ?, '', '', ?, ?, 0, 0)`,
		run.ID, run.TaskID, run.SessionID, run.Kind, run.TargetID, run.HostThreadID, run.Status,
		run.CreatedBy, now.UnixMilli())
	if err != nil {
		if strings.Contains(err.Error(), "idx_kanban_runs_active_target") {
			return kanban.Run{}, fmt.Errorf("%w: %q", kanban.ErrTargetBusy, run.TargetID)
		}
		return kanban.Run{}, fmt.Errorf("insert kanban run: %w", err)
	}
	if _, err := tx.Exec(`UPDATE kanban_tasks SET status = ?, latest_run_id = ?, updated_at = ? WHERE id = ?`,
		kanban.TaskStatusRunning, run.ID, now.UnixMilli(), run.TaskID); err != nil {
		return kanban.Run{}, fmt.Errorf("mark task running: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return kanban.Run{}, err
	}
	return run, nil
}

// StartKanbanRun binds the execution thread and moves a queued run to running.
func StartKanbanRun(sessDir, runID, threadID string) (kanban.Run, error) {
	db, err := openStore(sessDir)
	if err != nil {
		return kanban.Run{}, err
	}
	defer db.Close()
	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()
	run, err := scanKanbanRun(db.QueryRow(`SELECT `+kanbanRunColumns+` FROM kanban_runs WHERE id = ?`, runID))
	if errors.Is(err, sql.ErrNoRows) {
		return kanban.Run{}, kanban.ErrRunNotFound
	}
	if err != nil {
		return kanban.Run{}, err
	}
	if run.Status != kanban.RunStatusQueued {
		return kanban.Run{}, fmt.Errorf("start kanban run: %q is %q, not queued", runID, run.Status)
	}
	now := time.Now().UTC()
	if _, err := db.Exec(`UPDATE kanban_runs SET status = ?, thread_id = ?, started_at = ? WHERE id = ?`,
		kanban.RunStatusRunning, threadID, now.UnixMilli(), runID); err != nil {
		return kanban.Run{}, fmt.Errorf("start kanban run: %w", err)
	}
	run.Status = kanban.RunStatusRunning
	run.ThreadID = threadID
	run.StartedAt = now
	return run, nil
}

// CompleteKanbanRun terminally resolves a run and folds the outcome back into
// the task: succeeded parks the task in human review, anything else returns
// it to ready for redispatch.
func CompleteKanbanRun(sessDir, runID, status, summary, errorMessage string) (kanban.Run, error) {
	if !kanban.RunTerminal(status) {
		return kanban.Run{}, fmt.Errorf("complete kanban run: %q is not terminal", status)
	}
	db, err := openStore(sessDir)
	if err != nil {
		return kanban.Run{}, err
	}
	defer db.Close()
	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()
	tx, err := db.Begin()
	if err != nil {
		return kanban.Run{}, err
	}
	defer tx.Rollback()
	run, err := scanKanbanRun(tx.QueryRow(`SELECT `+kanbanRunColumns+` FROM kanban_runs WHERE id = ?`, runID))
	if errors.Is(err, sql.ErrNoRows) {
		return kanban.Run{}, kanban.ErrRunNotFound
	}
	if err != nil {
		return kanban.Run{}, err
	}
	if kanban.RunTerminal(run.Status) {
		return kanban.Run{}, fmt.Errorf("%w: %q is %q", kanban.ErrRunAlreadyTerminal, runID, run.Status)
	}
	now := time.Now().UTC()
	if _, err := tx.Exec(`UPDATE kanban_runs SET status = ?, summary = ?, error_message = ?, finished_at = ? WHERE id = ?`,
		status, summary, errorMessage, now.UnixMilli(), runID); err != nil {
		return kanban.Run{}, fmt.Errorf("complete kanban run: %w", err)
	}
	taskStatus := kanban.TaskStatusAfterRun(status)
	if _, err := tx.Exec(`UPDATE kanban_tasks SET status = ?, updated_at = ? WHERE id = ?`,
		taskStatus, now.UnixMilli(), run.TaskID); err != nil {
		return kanban.Run{}, fmt.Errorf("fold run outcome into task: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return kanban.Run{}, err
	}
	run.Status = status
	run.Summary = summary
	run.ErrorMessage = errorMessage
	run.FinishedAt = now
	return run, nil
}

// GetKanbanRun loads one run by id.
func GetKanbanRun(sessDir, runID string) (kanban.Run, error) {
	db, err := openStore(sessDir)
	if err != nil {
		return kanban.Run{}, err
	}
	defer db.Close()
	r, err := scanKanbanRun(db.QueryRow(`SELECT `+kanbanRunColumns+` FROM kanban_runs WHERE id = ?`, runID))
	if errors.Is(err, sql.ErrNoRows) {
		return kanban.Run{}, kanban.ErrRunNotFound
	}
	return r, err
}

// GetActiveKanbanRunByThreadID finds the queued/running run bound to one
// execution site (spawned agent id). Returns kanban.ErrRunNotFound when the
// agent is not executing a kanban run, so terminal hooks can no-op on
// ordinary participant runs.
func GetActiveKanbanRunByThreadID(sessDir, threadID string) (kanban.Run, error) {
	db, err := openStore(sessDir)
	if err != nil {
		return kanban.Run{}, err
	}
	defer db.Close()
	r, err := scanKanbanRun(db.QueryRow(`SELECT `+kanbanRunColumns+` FROM kanban_runs
WHERE thread_id = ? AND status IN (?, ?) ORDER BY created_at DESC LIMIT 1`,
		threadID, kanban.RunStatusQueued, kanban.RunStatusRunning))
	if errors.Is(err, sql.ErrNoRows) {
		return kanban.Run{}, kanban.ErrRunNotFound
	}
	return r, err
}

// ListKanbanRuns lists a task's runs, newest first.
func ListKanbanRuns(sessDir, taskID string) ([]kanban.Run, error) {
	db, err := openStore(sessDir)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`SELECT `+kanbanRunColumns+` FROM kanban_runs WHERE task_id = ? ORDER BY created_at DESC`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []kanban.Run{}
	for rows.Next() {
		r, err := scanKanbanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AddKanbanArtifact attributes one produced file to a run and its task.
func AddKanbanArtifact(sessDir string, artifact kanban.Artifact) (kanban.Artifact, error) {
	artifact.Path = strings.TrimSpace(artifact.Path)
	if artifact.RunID == "" || artifact.TaskID == "" || artifact.Path == "" {
		return kanban.Artifact{}, errors.New("run_id, task_id, and path are required")
	}
	db, err := openStore(sessDir)
	if err != nil {
		return kanban.Artifact{}, err
	}
	defer db.Close()
	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()
	now := time.Now().UTC()
	artifact.ID = newKanbanID("ka-")
	artifact.CreatedAt = now
	_, err = db.Exec(`
INSERT INTO kanban_artifacts (
 id, run_id, task_id, session_id, path, display_name, media_type, size_bytes, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		artifact.ID, artifact.RunID, artifact.TaskID, artifact.SessionID, artifact.Path,
		artifact.DisplayName, artifact.MediaType, artifact.SizeBytes, now.UnixMilli())
	if err != nil {
		return kanban.Artifact{}, fmt.Errorf("insert kanban artifact: %w", err)
	}
	return artifact, nil
}

// ListKanbanArtifacts lists a task's artifacts across all its runs, oldest
// first, so reference resolution sees the production history in order.
func ListKanbanArtifacts(sessDir, taskID string) ([]kanban.Artifact, error) {
	db, err := openStore(sessDir)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`SELECT id, run_id, task_id, session_id, path, display_name, media_type, size_bytes, created_at
FROM kanban_artifacts WHERE task_id = ? ORDER BY created_at`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []kanban.Artifact{}
	for rows.Next() {
		var a kanban.Artifact
		var createdMS int64
		if err := rows.Scan(&a.ID, &a.RunID, &a.TaskID, &a.SessionID, &a.Path, &a.DisplayName, &a.MediaType, &a.SizeBytes, &createdMS); err != nil {
			return nil, err
		}
		a.CreatedAt = time.UnixMilli(createdMS).UTC()
		out = append(out, a)
	}
	return out, rows.Err()
}
