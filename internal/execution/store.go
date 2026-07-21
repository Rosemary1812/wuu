package execution

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/stringutil"
)

var (
	ErrNotFound = errors.New("execution run not found")
	ErrConflict = errors.New("execution run state conflict")
)

type Store struct {
	sessDir string
	mu      sync.Mutex
	memory  map[string]Run
}

func NewStore(sessDir string) (*Store, error) {
	sessDir = strings.TrimSpace(sessDir)
	if sessDir == "" {
		return nil, errors.New("execution store session directory is required")
	}
	db, err := session.OpenStore(sessDir)
	if err != nil {
		return nil, fmt.Errorf("open execution store: %w", err)
	}
	if err := db.Close(); err != nil {
		return nil, fmt.Errorf("close execution store: %w", err)
	}
	return &Store{sessDir: sessDir, memory: make(map[string]Run)}, nil
}

func (s *Store) Create(ctx context.Context, params CreateParams) (Run, error) {
	if s == nil {
		return Run{}, errors.New("execution store is required")
	}
	params.RuntimeID = strings.TrimSpace(params.RuntimeID)
	params.ThreadID = strings.TrimSpace(params.ThreadID)
	params.Request = normalizeRequest(params.Request)
	params.Runtime = normalizeRuntime(params.Runtime)
	params.Workspace = normalizeWorkspace(params.Workspace)
	if err := validateManifest(params); err != nil {
		return Run{}, err
	}
	now := time.Now().UTC()
	run := Run{
		ID:        "run-" + session.NewID(),
		RuntimeID: params.RuntimeID,
		Status:    StatusAccepted,
		Request:   params.Request,
		Runtime:   params.Runtime,
		Workspace: params.Workspace,
		ThreadID:  params.ThreadID,
		Turns:     []TurnRef{},
		Ephemeral: params.Ephemeral,
		CreatedAt: now,
		UpdatedAt: now,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if params.Ephemeral {
		for _, existing := range s.memory {
			if existing.ThreadID == run.ThreadID && !existing.Status.Terminal() {
				return Run{}, fmt.Errorf("%w: thread %q already has active run %q", ErrConflict, run.ThreadID, existing.ID)
			}
		}
		s.memory[run.ID] = cloneRun(run)
		return run, nil
	}
	db, err := session.OpenStore(s.sessDir)
	if err != nil {
		return Run{}, err
	}
	defer db.Close()
	requestJSON, err := json.Marshal(run.Request)
	if err != nil {
		return Run{}, fmt.Errorf("encode execution request: %w", err)
	}
	runtimeJSON, err := json.Marshal(run.Runtime)
	if err != nil {
		return Run{}, fmt.Errorf("encode execution runtime: %w", err)
	}
	_, err = db.ExecContext(ctx, `
INSERT INTO execution_runs (
    id, runtime_id, status, request_json, runtime_json, thread_id, workspace_id, workspace_root,
    created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID, run.RuntimeID, run.Status, string(requestJSON), string(runtimeJSON), run.ThreadID,
		run.Workspace.ID, run.Workspace.Root,
		toMillis(run.CreatedAt), toMillis(run.UpdatedAt),
	)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique constraint") {
			return Run{}, fmt.Errorf("%w: thread %q already has an active run", ErrConflict, run.ThreadID)
		}
		return Run{}, fmt.Errorf("create execution run: %w", err)
	}
	return run, nil
}

func (s *Store) Get(ctx context.Context, id string) (Run, error) {
	if s == nil {
		return Run{}, errors.New("execution store is required")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return Run{}, errors.New("execution run id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if run, ok := s.memory[id]; ok {
		return cloneRun(run), nil
	}
	db, err := session.OpenStore(s.sessDir)
	if err != nil {
		return Run{}, err
	}
	defer db.Close()
	var run Run
	err = withReadSnapshot(ctx, db, func(q queryer) error {
		var loadErr error
		run, loadErr = loadRun(ctx, q, id)
		return loadErr
	})
	return run, err
}

func (s *Store) List(ctx context.Context, opts ListOptions) ([]Run, error) {
	if s == nil {
		return nil, errors.New("execution store is required")
	}
	opts.WorkspaceID = strings.TrimSpace(opts.WorkspaceID)
	opts.WorkspaceRoot = strings.TrimSpace(opts.WorkspaceRoot)
	opts.ThreadID = strings.TrimSpace(opts.ThreadID)
	if opts.Limit <= 0 {
		opts.Limit = 50
	}
	if opts.Limit > 500 {
		opts.Limit = 500
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	db, err := session.OpenStore(s.sessDir)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var durable []Run
	err = withReadSnapshot(ctx, db, func(q queryer) error {
		var listErr error
		durable, listErr = listDurableRuns(ctx, q, opts)
		return listErr
	})
	if err != nil {
		return nil, err
	}
	all := append([]Run(nil), durable...)
	for _, run := range s.memory {
		if matchesList(run, opts) {
			all = append(all, cloneRun(run))
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].UpdatedAt.Equal(all[j].UpdatedAt) {
			return all[i].ID > all[j].ID
		}
		return all[i].UpdatedAt.After(all[j].UpdatedAt)
	})
	if len(all) > opts.Limit {
		all = all[:opts.Limit]
	}
	return all, nil
}

func (s *Store) AttachTurn(ctx context.Context, runID, threadID, turnID string, at time.Time) (Run, error) {
	runID = strings.TrimSpace(runID)
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	if runID == "" || threadID == "" || turnID == "" {
		return Run{}, errors.New("execution run, thread, and turn ids are required")
	}
	at = normalizedTime(at)
	return s.update(ctx, runID, func(run *Run) error {
		if run.Status.Terminal() {
			return stateConflict(run.ID, run.Status, "attach turn")
		}
		if run.ThreadID == "" {
			run.ThreadID = threadID
		} else if run.ThreadID != threadID {
			return fmt.Errorf("%w: run %q belongs to thread %q", ErrConflict, run.ID, run.ThreadID)
		}
		for _, ref := range run.Turns {
			if ref.TurnID == turnID {
				return nil
			}
		}
		run.Turns = append(run.Turns, TurnRef{
			TurnID:     turnID,
			ThreadID:   threadID,
			Ordinal:    len(run.Turns) + 1,
			AttachedAt: at,
		})
		run.Status = StatusRunning
		if run.StartedAt == nil {
			run.StartedAt = timePtr(at)
		}
		run.UpdatedAt = at
		return nil
	})
}

// Resolve records the effective runtime and workspace selected under Turn
// admission. It is allowed only before the first canonical Turn is attached.
func (s *Store) Resolve(ctx context.Context, runID string, runtime RuntimeManifest, workspace WorkspaceRef, at time.Time) (Run, error) {
	runtime = normalizeRuntime(runtime)
	workspace = normalizeWorkspace(workspace)
	if runtime.Resolved.Provider == "" || runtime.Resolved.Model == "" || runtime.ProtocolVersion == "" {
		return Run{}, errors.New("resolved execution runtime is incomplete")
	}
	if workspace.Root == "" {
		return Run{}, errors.New("resolved execution workspace root is required")
	}
	at = normalizedTime(at)
	return s.update(ctx, runID, func(run *Run) error {
		if run.Status != StatusAccepted || len(run.Turns) != 0 {
			return stateConflict(run.ID, run.Status, "resolve runtime")
		}
		run.Runtime = runtime
		run.Workspace = workspace
		run.UpdatedAt = at
		return nil
	})
}

func (s *Store) FinishTurn(ctx context.Context, runID, turnID string, terminal TurnTerminal) (Run, error) {
	runID = strings.TrimSpace(runID)
	turnID = strings.TrimSpace(turnID)
	if runID == "" || turnID == "" {
		return Run{}, errors.New("execution run and turn ids are required")
	}
	terminal.TracePath = strings.TrimSpace(terminal.TracePath)
	terminal.At = normalizedTime(terminal.At)
	return s.update(ctx, runID, func(run *Run) error {
		for i := range run.Turns {
			if run.Turns[i].TurnID != turnID {
				continue
			}
			if run.Status.Terminal() {
				if run.Turns[i].TracePath == terminal.TracePath {
					return nil
				}
				return stateConflict(run.ID, run.Status, "change turn trace")
			}
			run.Turns[i].TracePath = terminal.TracePath
			run.UpdatedAt = terminal.At
			return nil
		}
		return fmt.Errorf("%w: turn %q is not attached to run %q", ErrConflict, turnID, run.ID)
	})
}

func (s *Store) Complete(ctx context.Context, runID string, result Result, at time.Time) (Run, error) {
	return s.finish(ctx, runID, StatusCompleted, result, nil, at)
}

func (s *Store) Fail(ctx context.Context, runID string, status Status, result Result, runErr Error, at time.Time) (Run, error) {
	if status == StatusCompleted || !status.Terminal() {
		return Run{}, fmt.Errorf("invalid failure status %q", status)
	}
	runErr = sanitizeError(runErr)
	if runErr.Message == "" {
		return Run{}, errors.New("execution failure message is required")
	}
	return s.finish(ctx, runID, status, result, &runErr, at)
}

// ReconcileOrphans marks durable work from dead app-server processes as
// interrupted. Detached reattachment is intentionally outside the first Run
// contract, so leaving orphaned rows active would claim work is still live.
func (s *Store) ReconcileOrphans(ctx context.Context, currentRuntimeID string, at time.Time) ([]Run, error) {
	if s == nil {
		return nil, errors.New("execution store is required")
	}
	currentRuntimeID = strings.TrimSpace(currentRuntimeID)
	if currentRuntimeID == "" {
		return nil, errors.New("current execution runtime id is required")
	}
	at = normalizedTime(at)
	s.mu.Lock()
	defer s.mu.Unlock()
	db, err := session.OpenStore(s.sessDir)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	cutoff := at.Add(-session.InferenceJournalRecoveryInterval()).UnixMilli()
	rows, err := db.QueryContext(ctx, `
SELECT r.id, rt.pid, rt.closed_at
FROM execution_runs r
JOIN inference_journal_runtimes rt ON rt.id = r.runtime_id
WHERE r.runtime_id <> ? AND r.status IN (?, ?)
  AND (rt.closed_at <> 0 OR rt.heartbeat_at < ?)
ORDER BY r.created_at, r.id`, currentRuntimeID, StatusAccepted, StatusRunning, cutoff)
	if err != nil {
		return nil, fmt.Errorf("list orphaned execution runs: %w", err)
	}
	type candidate struct {
		id       string
		pid      int
		closedAt int64
	}
	var candidates []candidate
	for rows.Next() {
		var item candidate
		if err := rows.Scan(&item.id, &item.pid, &item.closedAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan orphaned execution run: %w", err)
		}
		candidates = append(candidates, item)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close orphaned execution rows: %w", err)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate orphaned execution runs: %w", err)
	}
	runErr := Error{Code: "process_restarted", Category: "cancelled", Message: "app-server stopped before the run reached a terminal state"}
	errorJSON, _ := json.Marshal(runErr)
	var recovered []Run
	for _, item := range candidates {
		if item.closedAt == 0 && session.InferenceRuntimeProcessAlive(item.pid) {
			continue
		}
		result, err := db.ExecContext(ctx, `
UPDATE execution_runs
SET status = ?, error_json = ?, updated_at = ?, completed_at = ?
WHERE id = ? AND status IN (?, ?)`,
			StatusInterrupted, string(errorJSON), toMillis(at), toMillis(at), item.id, StatusAccepted, StatusRunning,
		)
		if err != nil {
			return nil, fmt.Errorf("reconcile execution run %q: %w", item.id, err)
		}
		if changed, _ := result.RowsAffected(); changed == 0 {
			continue
		}
		run, err := loadRun(ctx, db, item.id)
		if err != nil {
			return nil, err
		}
		recovered = append(recovered, run)
	}
	return recovered, nil
}

func (s *Store) finish(ctx context.Context, runID string, status Status, result Result, runErr *Error, at time.Time) (Run, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return Run{}, errors.New("execution run id is required")
	}
	at = normalizedTime(at)
	return s.update(ctx, runID, func(run *Run) error {
		if run.Status.Terminal() {
			resultCopy := cloneResult(result)
			if run.Status == status && reflect.DeepEqual(run.Result, &resultCopy) && reflect.DeepEqual(run.Error, runErr) {
				return nil
			}
			return stateConflict(run.ID, run.Status, "finish")
		}
		run.Status = status
		resultCopy := cloneResult(result)
		run.Result = &resultCopy
		if runErr != nil {
			errorCopy := *runErr
			run.Error = &errorCopy
		}
		run.UpdatedAt = at
		run.CompletedAt = timePtr(at)
		return nil
	})
}

func (s *Store) update(ctx context.Context, runID string, mutate func(*Run) error) (Run, error) {
	if s == nil {
		return Run{}, errors.New("execution store is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if run, ok := s.memory[runID]; ok {
		copy := cloneRun(run)
		if err := mutate(&copy); err != nil {
			return Run{}, err
		}
		s.memory[runID] = cloneRun(copy)
		return copy, nil
	}
	db, err := session.OpenStore(s.sessDir)
	if err != nil {
		return Run{}, err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Run{}, fmt.Errorf("begin execution update: %w", err)
	}
	defer tx.Rollback()
	run, err := loadRun(ctx, tx, runID)
	if err != nil {
		return Run{}, err
	}
	beforeTurns := append([]TurnRef(nil), run.Turns...)
	if err := mutate(&run); err != nil {
		return Run{}, err
	}
	if err := persistRun(ctx, tx, run); err != nil {
		return Run{}, err
	}
	if err := persistTurnChanges(ctx, tx, run.ID, beforeTurns, run.Turns); err != nil {
		return Run{}, err
	}
	if err := tx.Commit(); err != nil {
		return Run{}, fmt.Errorf("commit execution update: %w", err)
	}
	return run, nil
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func withReadSnapshot(ctx context.Context, db *sql.DB, read func(queryer) error) error {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("begin execution read snapshot: %w", err)
	}
	defer tx.Rollback()
	if err := read(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit execution read snapshot: %w", err)
	}
	return nil
}

func loadRun(ctx context.Context, q queryer, id string) (Run, error) {
	var run Run
	var requestJSON, runtimeJSON, resultJSON, errorJSON string
	var createdMS, startedMS, updatedMS, completedMS int64
	err := q.QueryRowContext(ctx, `
SELECT id, runtime_id, status, request_json, runtime_json,
       thread_id, workspace_id, workspace_root, result_json, error_json,
       created_at, started_at, updated_at, completed_at
FROM execution_runs WHERE id = ?`, id).Scan(
		&run.ID, &run.RuntimeID, &run.Status, &requestJSON, &runtimeJSON,
		&run.ThreadID, &run.Workspace.ID, &run.Workspace.Root, &resultJSON, &errorJSON,
		&createdMS, &startedMS, &updatedMS, &completedMS,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, fmt.Errorf("%w: %q", ErrNotFound, id)
	}
	if err != nil {
		return Run{}, fmt.Errorf("load execution run: %w", err)
	}
	if err := json.Unmarshal([]byte(requestJSON), &run.Request); err != nil {
		return Run{}, fmt.Errorf("decode execution request for %q: %w", id, err)
	}
	if err := json.Unmarshal([]byte(runtimeJSON), &run.Runtime); err != nil {
		return Run{}, fmt.Errorf("decode execution runtime for %q: %w", id, err)
	}
	if resultJSON != "" {
		var result Result
		if err := json.Unmarshal([]byte(resultJSON), &result); err != nil {
			return Run{}, fmt.Errorf("decode execution result for %q: %w", id, err)
		}
		run.Result = &result
	}
	if errorJSON != "" {
		var runErr Error
		if err := json.Unmarshal([]byte(errorJSON), &runErr); err != nil {
			return Run{}, fmt.Errorf("decode execution error for %q: %w", id, err)
		}
		run.Error = &runErr
	}
	run.CreatedAt = fromMillis(createdMS)
	run.StartedAt = optionalTime(startedMS)
	run.UpdatedAt = fromMillis(updatedMS)
	run.CompletedAt = optionalTime(completedMS)
	turns, err := loadTurns(ctx, q, id)
	if err != nil {
		return Run{}, err
	}
	run.Turns = turns
	return run, nil
}

func loadTurns(ctx context.Context, q queryer, runID string) ([]TurnRef, error) {
	rows, err := q.QueryContext(ctx, `
SELECT thread_id, turn_id, ordinal, trace_path, attached_at
FROM execution_run_turns WHERE run_id = ? ORDER BY ordinal`, runID)
	if err != nil {
		return nil, fmt.Errorf("load execution turn refs: %w", err)
	}
	defer rows.Close()
	turns := make([]TurnRef, 0)
	for rows.Next() {
		var ref TurnRef
		var attachedMS int64
		if err := rows.Scan(&ref.ThreadID, &ref.TurnID, &ref.Ordinal, &ref.TracePath, &attachedMS); err != nil {
			return nil, fmt.Errorf("scan execution turn ref: %w", err)
		}
		ref.AttachedAt = fromMillis(attachedMS)
		turns = append(turns, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate execution turn refs: %w", err)
	}
	return turns, nil
}

func listDurableRuns(ctx context.Context, q queryer, opts ListOptions) ([]Run, error) {
	clauses := []string{"1 = 1"}
	args := make([]any, 0, 5)
	if opts.WorkspaceID != "" {
		clauses = append(clauses, "workspace_id = ?")
		args = append(args, opts.WorkspaceID)
	}
	if opts.WorkspaceRoot != "" {
		clauses = append(clauses, "workspace_root = ?")
		args = append(args, opts.WorkspaceRoot)
	}
	if opts.ThreadID != "" {
		clauses = append(clauses, "thread_id = ?")
		args = append(args, opts.ThreadID)
	}
	if opts.Status != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, opts.Status)
	}
	args = append(args, opts.Limit)
	rows, err := q.QueryContext(ctx, `
SELECT id FROM execution_runs
WHERE `+strings.Join(clauses, " AND ")+`
ORDER BY updated_at DESC, id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("list execution runs: %w", err)
	}
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan execution run id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close execution run rows: %w", err)
	}
	runs := make([]Run, 0, len(ids))
	for _, id := range ids {
		run, err := loadRun(ctx, q, id)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, nil
}

func persistRun(ctx context.Context, tx *sql.Tx, run Run) error {
	requestJSON, err := json.Marshal(run.Request)
	if err != nil {
		return fmt.Errorf("encode execution request: %w", err)
	}
	runtimeJSON, err := json.Marshal(run.Runtime)
	if err != nil {
		return fmt.Errorf("encode execution runtime: %w", err)
	}
	resultJSON := ""
	if run.Result != nil {
		data, marshalErr := json.Marshal(run.Result)
		if marshalErr != nil {
			return fmt.Errorf("encode execution result: %w", marshalErr)
		}
		resultJSON = string(data)
	}
	errorJSON := ""
	if run.Error != nil {
		data, marshalErr := json.Marshal(run.Error)
		if marshalErr != nil {
			return fmt.Errorf("encode execution error: %w", marshalErr)
		}
		errorJSON = string(data)
	}
	result, err := tx.ExecContext(ctx, `
UPDATE execution_runs
SET status = ?, request_json = ?, runtime_json = ?, thread_id = ?, workspace_id = ?, workspace_root = ?,
    result_json = ?, error_json = ?, started_at = ?, updated_at = ?, completed_at = ?
WHERE id = ?`,
		run.Status, string(requestJSON), string(runtimeJSON), run.ThreadID, run.Workspace.ID, run.Workspace.Root,
		resultJSON, errorJSON, timeMillis(run.StartedAt), toMillis(run.UpdatedAt), timeMillis(run.CompletedAt), run.ID,
	)
	if err != nil {
		return fmt.Errorf("update execution run: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return fmt.Errorf("%w: %q", ErrNotFound, run.ID)
	}
	return nil
}

func persistTurnChanges(ctx context.Context, tx *sql.Tx, runID string, before, after []TurnRef) error {
	if len(after) < len(before) {
		return errors.New("execution turn refs cannot be removed")
	}
	for _, ref := range after {
		_, err := tx.ExecContext(ctx, `
INSERT INTO execution_run_turns (
    run_id, thread_id, turn_id, ordinal, trace_path, attached_at
) VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(run_id, turn_id) DO UPDATE SET
    trace_path = excluded.trace_path`,
			runID, ref.ThreadID, ref.TurnID, ref.Ordinal, ref.TracePath, toMillis(ref.AttachedAt),
		)
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "unique constraint") {
				return fmt.Errorf("%w: turn %q is already attached to another run", ErrConflict, ref.TurnID)
			}
			return fmt.Errorf("persist execution turn ref: %w", err)
		}
	}
	return nil
}

func normalizeRequest(request Request) Request {
	request.Mode = Mode(strings.TrimSpace(string(request.Mode)))
	request.SourceThreadID = strings.TrimSpace(request.SourceThreadID)
	request.Requested = normalizeSelection(request.Requested)
	request.AgentProfile = strings.TrimSpace(request.AgentProfile)
	return request
}

func normalizeRuntime(runtime RuntimeManifest) RuntimeManifest {
	runtime.Resolved = normalizeSelection(runtime.Resolved)
	runtime.ProtocolVersion = strings.TrimSpace(runtime.ProtocolVersion)
	runtime.CoreVersion = strings.TrimSpace(runtime.CoreVersion)
	runtime.CoreCommit = strings.TrimSpace(runtime.CoreCommit)
	return runtime
}

func normalizeSelection(selection Selection) Selection {
	selection.Provider = strings.TrimSpace(selection.Provider)
	selection.Model = strings.TrimSpace(selection.Model)
	selection.Variant = strings.TrimSpace(selection.Variant)
	selection.Effort = strings.TrimSpace(selection.Effort)
	selection.PermissionMode = strings.TrimSpace(selection.PermissionMode)
	return selection
}

func normalizeWorkspace(workspace WorkspaceRef) WorkspaceRef {
	workspace.ID = strings.TrimSpace(workspace.ID)
	workspace.Root = strings.TrimSpace(workspace.Root)
	return workspace
}

func validateManifest(params CreateParams) error {
	if params.RuntimeID == "" {
		return errors.New("execution runtime id is required")
	}
	if params.ThreadID == "" {
		return errors.New("execution thread id is required")
	}
	switch params.Request.Mode {
	case ModeStart, ModeReview:
		if params.Request.SourceThreadID != "" {
			return fmt.Errorf("%s execution request cannot have a source thread", params.Request.Mode)
		}
	case ModeResume, ModeFork:
		if params.Request.SourceThreadID == "" {
			return fmt.Errorf("%s execution request requires a source thread", params.Request.Mode)
		}
	default:
		return fmt.Errorf("invalid execution mode %q", params.Request.Mode)
	}
	if params.Workspace.Root == "" {
		return errors.New("execution workspace root is required")
	}
	if params.Runtime.Resolved.Provider == "" || params.Runtime.Resolved.Model == "" {
		return errors.New("resolved execution provider and model are required")
	}
	if params.Runtime.ProtocolVersion == "" {
		return errors.New("execution protocol version is required")
	}
	if params.Request.MaxTurns < 0 {
		return errors.New("execution max turns cannot be negative")
	}
	if params.Request.TimeoutMS < 0 {
		return errors.New("execution timeout cannot be negative")
	}
	if params.Request.ImageCount < 0 || params.Request.FileCount < 0 {
		return errors.New("execution attachment counts cannot be negative")
	}
	return nil
}

func matchesList(run Run, opts ListOptions) bool {
	return (opts.WorkspaceID == "" || run.Workspace.ID == opts.WorkspaceID) &&
		(opts.WorkspaceRoot == "" || run.Workspace.Root == opts.WorkspaceRoot) &&
		(opts.ThreadID == "" || run.ThreadID == opts.ThreadID) &&
		(opts.Status == "" || run.Status == opts.Status)
}

func stateConflict(id string, status Status, operation string) error {
	return fmt.Errorf("%w: cannot %s run %q in status %q", ErrConflict, operation, id, status)
}

func sanitizeError(runErr Error) Error {
	runErr.Code = strings.TrimSpace(runErr.Code)
	runErr.Category = strings.TrimSpace(runErr.Category)
	runErr.Provider = strings.TrimSpace(runErr.Provider)
	runErr.Message = strings.Join(strings.Fields(runErr.Message), " ")
	const maxErrorBytes = 2000
	runErr.Message = stringutil.Truncate(runErr.Message, maxErrorBytes, "")
	return runErr
}

func normalizedTime(value time.Time) time.Time {
	if value.IsZero() {
		return time.Now().UTC()
	}
	return value.UTC()
}

func toMillis(value time.Time) int64 {
	return value.UTC().UnixMilli()
}

func timeMillis(value *time.Time) int64 {
	if value == nil || value.IsZero() {
		return 0
	}
	return toMillis(*value)
}

func fromMillis(value int64) time.Time {
	return time.UnixMilli(value).UTC()
}

func optionalTime(value int64) *time.Time {
	if value == 0 {
		return nil
	}
	return timePtr(fromMillis(value))
}

func timePtr(value time.Time) *time.Time {
	copy := value.UTC()
	return &copy
}

func cloneResult(result Result) Result {
	return result
}

func cloneRun(run Run) Run {
	copy := run
	copy.Request = normalizeRequest(run.Request)
	copy.Runtime = normalizeRuntime(run.Runtime)
	copy.Workspace = normalizeWorkspace(run.Workspace)
	copy.Turns = append([]TurnRef(nil), run.Turns...)
	if run.Result != nil {
		result := cloneResult(*run.Result)
		copy.Result = &result
	}
	if run.Error != nil {
		runErr := *run.Error
		copy.Error = &runErr
	}
	if run.StartedAt != nil {
		copy.StartedAt = timePtr(*run.StartedAt)
	}
	if run.CompletedAt != nil {
		copy.CompletedAt = timePtr(*run.CompletedAt)
	}
	return copy
}
