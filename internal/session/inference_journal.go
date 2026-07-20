package session

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
)

const inferenceJournalRetention = 30 * 24 * time.Hour

const (
	inferenceJournalHeartbeatInterval = 3 * time.Second
	inferenceJournalStaleAfter        = 12 * time.Second
	// inferenceJournalProgressFlushInterval bounds how often coalesced streaming
	// submission estimates reach disk. It only keeps the durable cost estimate
	// roughly current; terminal accuracy is guaranteed by the synchronous flush
	// barrier at CompleteAttempt / CompleteOperation, not by this cadence.
	inferenceJournalProgressFlushInterval = 250 * time.Millisecond
)

func InferenceJournalRecoveryInterval() time.Duration {
	return inferenceJournalStaleAfter
}

// InferenceJournalRuntime binds all journals created by one runtime process to
// a workspace scope. A fresh runtime id lets startup recovery distinguish
// records left by the previous process from work created during this boot.
type InferenceJournalRuntime struct {
	sessDir        string
	workspaceScope string
	runtimeID      string
	heartbeatOnce  sync.Once
	heartbeatStop  chan struct{}
	heartbeatDone  chan struct{}
	closeOnce      sync.Once

	// Streaming submission progress is coalesced by submission id and flushed
	// off the caller's goroutine by a single background writer, so a fast token
	// stream never pays a per-delta open+lock+transaction cost. progressErr
	// records the last flush failure for diagnostics; it never propagates to a
	// stream.
	progressOnce sync.Once
	progressStop chan struct{}
	progressDone chan struct{}
	progressMu   sync.Mutex
	progress     map[string]pendingSubmissionProgress
	progressErr  error
}

// pendingSubmissionProgress carries a coalesced streaming update together with
// the owner-scoped journal that must apply it (ownership is asserted per row).
type pendingSubmissionProgress struct {
	journal *inferenceJournal
	record  providers.InferenceSubmissionJournalRecord
}

func NewInferenceJournalRuntime(sessDir, workspaceScope string) (*InferenceJournalRuntime, error) {
	sessDir = strings.TrimSpace(sessDir)
	workspaceScope = journalText(workspaceScope, 512)
	if sessDir == "" {
		return nil, errors.New("inference journal session directory is required")
	}
	if workspaceScope == "" {
		return nil, errors.New("inference journal workspace scope is required")
	}
	db, err := openStore(sessDir)
	if err != nil {
		return nil, fmt.Errorf("open inference journal: %w", err)
	}
	runtimeID := newInferenceRuntimeID()
	now := time.Now().UTC().UnixMilli()
	storeWriteMu.Lock()
	_, registerErr := db.Exec(`
INSERT INTO inference_journal_runtimes (
    id, workspace_scope, pid, started_at, heartbeat_at, closed_at
) VALUES (?, ?, ?, ?, ?, 0)`, runtimeID, workspaceScope, os.Getpid(), now, now)
	storeWriteMu.Unlock()
	if registerErr != nil {
		db.Close()
		return nil, fmt.Errorf("register inference journal runtime: %w", registerErr)
	}
	if err := db.Close(); err != nil {
		return nil, fmt.Errorf("close inference journal: %w", err)
	}
	runtime := &InferenceJournalRuntime{
		sessDir:        sessDir,
		workspaceScope: workspaceScope,
		runtimeID:      runtimeID,
		heartbeatStop:  make(chan struct{}),
		heartbeatDone:  make(chan struct{}),
		progressStop:   make(chan struct{}),
		progressDone:   make(chan struct{}),
	}
	runtime.startHeartbeat()
	runtime.startProgressFlusher()
	return runtime, nil
}

func (r *InferenceJournalRuntime) RuntimeID() string {
	if r == nil {
		return ""
	}
	return r.runtimeID
}

func (r *InferenceJournalRuntime) startHeartbeat() {
	if r == nil {
		return
	}
	r.heartbeatOnce.Do(func() {
		go func() {
			defer close(r.heartbeatDone)
			db, err := openStore(r.sessDir)
			if err != nil {
				providers.DebugLogf("inference journal heartbeat open: %v", err)
				return
			}
			defer db.Close()
			ticker := time.NewTicker(inferenceJournalHeartbeatInterval)
			defer ticker.Stop()
			for {
				select {
				case at := <-ticker.C:
					storeWriteMu.Lock()
					_, heartbeatErr := db.Exec(`
UPDATE inference_journal_runtimes SET heartbeat_at = ?
WHERE id = ? AND closed_at = 0`, at.UTC().UnixMilli(), r.runtimeID)
					storeWriteMu.Unlock()
					if heartbeatErr != nil {
						providers.DebugLogf("inference journal heartbeat update: %v", heartbeatErr)
					}
				case <-r.heartbeatStop:
					return
				}
			}
		}()
	})
}

// Close releases the runtime lease. Process crashes leave closed_at unset and
// are detected by the heartbeat deadline instead.
func (r *InferenceJournalRuntime) Close() error {
	if r == nil {
		return nil
	}
	var closeErr error
	r.closeOnce.Do(func() {
		close(r.heartbeatStop)
		<-r.heartbeatDone
		// Drain any coalesced streaming progress while the runtime lease is
		// still open, before we stamp closed_at below.
		if r.progressStop != nil {
			close(r.progressStop)
			<-r.progressDone
		}
		db, err := openStore(r.sessDir)
		if err != nil {
			closeErr = fmt.Errorf("close inference journal runtime: %w", err)
			return
		}
		defer db.Close()
		storeWriteMu.Lock()
		_, err = db.Exec(`
UPDATE inference_journal_runtimes
SET heartbeat_at = ?, closed_at = ?
WHERE id = ?`, time.Now().UTC().UnixMilli(), time.Now().UTC().UnixMilli(), r.runtimeID)
		storeWriteMu.Unlock()
		if err != nil {
			closeErr = fmt.Errorf("close inference journal runtime: %w", err)
		}
	})
	return closeErr
}

func (r *InferenceJournalRuntime) ForOwner(ownerID string) providers.InferenceJournal {
	if r == nil {
		return nil
	}
	ownerID = journalText(ownerID, 512)
	if ownerID == "" {
		ownerID = "workspace-runtime"
	}
	return &inferenceJournal{
		runtime: r,
		ownerID: ownerID,
	}
}

func (r *InferenceJournalRuntime) startProgressFlusher() {
	if r == nil {
		return
	}
	r.progressOnce.Do(func() {
		go func() {
			defer close(r.progressDone)
			ticker := time.NewTicker(inferenceJournalProgressFlushInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					r.flushSubmissionProgress()
				case <-r.progressStop:
					r.flushSubmissionProgress()
					return
				}
			}
		}()
	})
}

// enqueueSubmissionProgress coalesces a streaming submission update by id. Only
// the latest state per submission survives until the next flush, so a burst of
// deltas collapses to one write regardless of token rate.
func (r *InferenceJournalRuntime) enqueueSubmissionProgress(j *inferenceJournal, record providers.InferenceSubmissionJournalRecord) {
	if r == nil || j == nil {
		return
	}
	id := strings.TrimSpace(record.ID)
	if id == "" {
		return
	}
	r.progressMu.Lock()
	if r.progress == nil {
		r.progress = make(map[string]pendingSubmissionProgress)
	}
	r.progress[id] = pendingSubmissionProgress{journal: j, record: record}
	r.progressMu.Unlock()
}

// flushSubmissionProgress writes all coalesced updates in one transaction. It is
// safe to call from the background flusher and, as a durability barrier, from a
// synchronous terminal write concurrently; each caller claims a disjoint batch
// under progressMu. A failure is recorded and the batch dropped: these are
// best-effort in-flight estimates, and terminal cost is anchored by the
// synchronous CompleteAttempt write that follows the barrier.
func (r *InferenceJournalRuntime) flushSubmissionProgress() {
	if r == nil {
		return
	}
	r.progressMu.Lock()
	if len(r.progress) == 0 {
		r.progressMu.Unlock()
		return
	}
	batch := r.progress
	r.progress = make(map[string]pendingSubmissionProgress)
	r.progressMu.Unlock()

	if err := r.writeSubmissionProgressBatch(batch); err != nil {
		r.progressMu.Lock()
		r.progressErr = err
		r.progressMu.Unlock()
		providers.DebugLogf("inference journal: submission progress flush degraded (%d records): %v", len(batch), err)
	}
}

func (r *InferenceJournalRuntime) writeSubmissionProgressBatch(batch map[string]pendingSubmissionProgress) error {
	db, err := openStore(r.sessDir)
	if err != nil {
		return err
	}
	defer db.Close()
	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("begin submission progress: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`
UPDATE inference_journal_runtimes SET heartbeat_at = ?
WHERE id = ? AND closed_at = 0`, time.Now().UTC().UnixMilli(), r.runtimeID); err != nil {
		return fmt.Errorf("refresh runtime lease: %w", err)
	}
	for _, pending := range batch {
		if err := pending.journal.upsertSubmissionRecordTx(tx, pending.record); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// pendingProgressErr returns the last coalesced-flush failure, if any. It exists
// for diagnostics and tests; a degraded flush never affects control flow.
func (r *InferenceJournalRuntime) pendingProgressErr() error {
	if r == nil {
		return nil
	}
	r.progressMu.Lock()
	defer r.progressMu.Unlock()
	return r.progressErr
}

type inferenceJournal struct {
	runtime *InferenceJournalRuntime
	ownerID string
}

func (j *inferenceJournal) PrepareOperation(record providers.InferenceOperationJournalRecord) error {
	op := record.Operation
	op.ID = strings.TrimSpace(op.ID)
	record.RequestHash = strings.TrimSpace(record.RequestHash)
	if op.ID == "" || !validInferenceRequestHash(record.RequestHash) {
		return errors.New("prepare inference operation: operation id and request hash are required")
	}
	if op.PayloadVersion < 1 {
		return errors.New("prepare inference operation: payload version must be positive")
	}
	if op.AttemptLimit < 1 {
		return errors.New("prepare inference operation: attempt limit must be positive")
	}
	workflow := normalizeInferenceWorkflowRecord(record.Workflow, op, record.At)
	op.WorkflowID = strings.TrimSpace(op.WorkflowID)
	if op.WorkflowID == "" {
		op.WorkflowID = workflow.ID
	}
	if workflow.ID != op.WorkflowID {
		return fmt.Errorf("prepare inference operation: operation workflow %q does not match record workflow %q", op.WorkflowID, workflow.ID)
	}
	at := journalTime(record.At)
	return j.write("prepare inference operation", func(tx *sql.Tx) error {
		if err := j.prepareWorkflowTx(tx, workflow, at); err != nil {
			return err
		}
		var workflowID, parentID, runtimeID, scope, owner, kind, profile, requestHash, status string
		var attemptLimit, payloadVersion int
		err := tx.QueryRow(`
SELECT workflow_id, parent_operation_id, attempt_limit,
       runtime_id, workspace_scope, owner_id, kind, workload_profile,
       payload_version, request_hash, status
FROM inference_operations WHERE id = ?`, op.ID).Scan(
			&workflowID, &parentID, &attemptLimit,
			&runtimeID, &scope, &owner, &kind, &profile, &payloadVersion, &requestHash, &status,
		)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			if err := validateInferenceParentTx(tx, op); err != nil {
				return err
			}
			if err := assertWorkflowCostAdmissibleTx(tx, workflow.ID); err != nil {
				return err
			}
			if err := reserveWorkflowCounterTx(tx, workflow.ID, providers.WorkflowBudgetOperations, "used_operations", "max_operations", 1, at); err != nil {
				return err
			}
			if op.ParentOperationID != "" {
				if err := reserveWorkflowCounterTx(tx, workflow.ID, providers.WorkflowBudgetChildOperations, "used_child_operations", "max_child_operations", 1, at); err != nil {
					return err
				}
			}
			_, err = tx.Exec(`
INSERT INTO inference_operations (
    id, workflow_id, parent_operation_id, attempt_limit,
    runtime_id, workspace_scope, owner_id, kind, workload_profile,
    payload_version, request_hash, status, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'active', ?, ?)`,
				op.ID, workflow.ID, strings.TrimSpace(op.ParentOperationID), op.AttemptLimit,
				j.runtime.runtimeID, j.runtime.workspaceScope, j.ownerID,
				string(op.Kind), string(op.WorkloadProfile), op.PayloadVersion,
				record.RequestHash, at, at,
			)
			return err
		case err != nil:
			return err
		case workflowID != workflow.ID || parentID != strings.TrimSpace(op.ParentOperationID) || attemptLimit != op.AttemptLimit ||
			runtimeID != j.runtime.runtimeID || scope != j.runtime.workspaceScope || owner != j.ownerID ||
			kind != string(op.Kind) || profile != string(op.WorkloadProfile) ||
			payloadVersion != op.PayloadVersion || requestHash != record.RequestHash:
			return fmt.Errorf("operation %q metadata changed after preparation", op.ID)
		case status != "active":
			return fmt.Errorf("operation %q is already terminal (%s)", op.ID, status)
		default:
			return nil
		}
	})
}

func (j *inferenceJournal) PrepareAttempt(record providers.InferenceAttemptJournalRecord) error {
	if err := normalizeInferenceAttemptJournalRecord(&record); err != nil {
		return err
	}
	at := journalTime(record.At)
	return j.write("prepare inference attempt", func(tx *sql.Tx) error {
		return j.prepareAttemptTx(tx, record, at)
	})
}

func normalizeInferenceAttemptJournalRecord(record *providers.InferenceAttemptJournalRecord) error {
	record.OperationID = strings.TrimSpace(record.OperationID)
	record.WorkflowID = strings.TrimSpace(record.WorkflowID)
	record.AttemptID = strings.TrimSpace(record.AttemptID)
	record.RequestHash = strings.TrimSpace(record.RequestHash)
	if record.OperationID == "" || record.AttemptID == "" || !validInferenceRequestHash(record.RequestHash) || record.Ordinal < 1 {
		return errors.New("prepare inference attempt: operation, attempt, ordinal, and request hash are required")
	}
	return nil
}

func (j *inferenceJournal) prepareAttemptTx(tx *sql.Tx, record providers.InferenceAttemptJournalRecord, at int64) error {
	var existingOperationID, existingHash string
	var existingOrdinal int
	err := tx.QueryRow(`
SELECT operation_id, ordinal, request_hash FROM inference_attempts WHERE id = ?`, record.AttemptID).
		Scan(&existingOperationID, &existingOrdinal, &existingHash)
	if err == nil {
		if existingOperationID != record.OperationID || existingOrdinal != record.Ordinal || existingHash != record.RequestHash {
			return fmt.Errorf("attempt %q metadata changed after preparation", record.AttemptID)
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	var workflowID, runtimeID, scope, owner, requestHash, status string
	var attemptLimit int
	if err := tx.QueryRow(`
SELECT workflow_id, attempt_limit, runtime_id, workspace_scope, owner_id, request_hash, status
FROM inference_operations WHERE id = ?`, record.OperationID).
		Scan(&workflowID, &attemptLimit, &runtimeID, &scope, &owner, &requestHash, &status); err != nil {
		return err
	}
	if runtimeID != j.runtime.runtimeID || scope != j.runtime.workspaceScope || owner != j.ownerID ||
		requestHash != record.RequestHash || status != "active" {
		return fmt.Errorf("operation %q is not the active prepared operation", record.OperationID)
	}
	if record.WorkflowID != "" && record.WorkflowID != workflowID {
		return fmt.Errorf("attempt %q workflow %q does not match operation workflow %q", record.AttemptID, record.WorkflowID, workflowID)
	}
	if record.Ordinal > attemptLimit {
		return &providers.WorkflowBudgetExceededError{
			WorkflowID: workflowID,
			Dimension:  providers.WorkflowBudgetAttempts,
			Limit:      uint64(attemptLimit),
			Used:       uint64(attemptLimit),
			Requested:  1,
		}
	}
	if err := assertWorkflowCostAdmissibleTx(tx, workflowID); err != nil {
		return err
	}
	if err := reserveWorkflowCounterTx(tx, workflowID, providers.WorkflowBudgetAttempts, "used_attempts", "max_attempts", 1, at); err != nil {
		return err
	}
	if record.Ordinal > 1 {
		answered, err := priorAttemptAnsweredTx(tx, record.OperationID, record.Ordinal-1)
		if err != nil {
			return err
		}
		if answered {
			if err := reserveWorkflowCounterTx(tx, workflowID, providers.WorkflowBudgetSamePayloadReplays, "used_replays", "max_replays", 1, at); err != nil {
				return err
			}
		}
	}
	_, err = tx.Exec(`
INSERT INTO inference_attempts (
    id, operation_id, ordinal, request_hash, phase, prepared_at
) VALUES (?, ?, ?, ?, 'prepared', ?)`,
		record.AttemptID, record.OperationID, record.Ordinal, record.RequestHash, at,
	)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE inference_operations SET updated_at = ? WHERE id = ?`, at, record.OperationID)
	return err
}

func (j *inferenceJournal) UpsertSubmission(record providers.InferenceSubmissionJournalRecord) error {
	return j.write("upsert inference submission", func(tx *sql.Tx) error {
		return j.upsertSubmissionRecordTx(tx, record)
	})
}

// RecordSubmissionProgress implements providers.InferenceProgressJournal. It
// hands a streaming cost estimate to the runtime's coalescing writer and
// returns immediately, so token delivery never blocks on a durability write and
// a flush failure only degrades bookkeeping.
func (j *inferenceJournal) RecordSubmissionProgress(record providers.InferenceSubmissionJournalRecord) {
	if j == nil || j.runtime == nil {
		return
	}
	j.runtime.enqueueSubmissionProgress(j, record)
}

// upsertSubmissionRecordTx applies one submission record inside an existing
// transaction. The synchronous UpsertSubmission checkpoint and the asynchronous
// progress flusher share it, so a coalesced batch write and a single write take
// exactly the same monotonic merge path (an in-flight estimate can never
// downgrade a recorded terminal outcome, regardless of flush ordering).
func (j *inferenceJournal) upsertSubmissionRecordTx(tx *sql.Tx, record providers.InferenceSubmissionJournalRecord) error {
	record.OperationID = strings.TrimSpace(record.OperationID)
	record.AttemptID = strings.TrimSpace(record.AttemptID)
	record.ID = strings.TrimSpace(record.ID)
	if record.OperationID == "" || record.AttemptID == "" || record.ID == "" || record.Ordinal < 1 || record.AttemptOrdinal < 1 {
		return errors.New("upsert inference submission: ids and ordinals are required")
	}
	startedAt := journalTime(record.StartedAt)
	completedAt := optionalJournalTime(record.CompletedAt)
	reported := journalUsage(record.ReportedUsage)
	estimated := journalUsage(record.EstimatedUsage)
	if err := j.assertOperationTx(tx, record.OperationID, false); err != nil {
		return err
	}
	var linkedOperation, workflowID string
	var linkedOrdinal int
	if err := tx.QueryRow(`
SELECT a.operation_id, a.ordinal, o.workflow_id
FROM inference_attempts a
JOIN inference_operations o ON o.id = a.operation_id
WHERE a.id = ?`, record.AttemptID).
		Scan(&linkedOperation, &linkedOrdinal, &workflowID); err != nil {
		return err
	}
	if linkedOperation != record.OperationID || linkedOrdinal != record.AttemptOrdinal {
		return fmt.Errorf("attempt %q does not belong to operation %q", record.AttemptID, record.OperationID)
	}
	var operationID, attemptID, existingOutcome, existingFailure, existingCost string
	var ordinal, attemptOrdinal, existingOutputBytes int
	var existingReported, existingEstimated inferenceJournalUsage
	var existingCompletedAt int64
	err := tx.QueryRow(`
SELECT operation_id, attempt_id, ordinal, attempt_ordinal,
       outcome, failure_category, cost_state,
       reported_input_tokens, reported_output_tokens, reported_cache_creation,
       reported_cache_read, reported_cache_unknown, has_reported_usage,
       estimated_input_tokens, estimated_output_tokens, estimated_cache_creation,
       estimated_cache_read, estimated_cache_unknown, has_estimated_usage,
       output_bytes, completed_at
FROM inference_submissions WHERE id = ?`, record.ID).
		Scan(
			&operationID, &attemptID, &ordinal, &attemptOrdinal,
			&existingOutcome, &existingFailure, &existingCost,
			&existingReported.input, &existingReported.output, &existingReported.cacheCreation,
			&existingReported.cacheRead, &existingReported.cacheUnknown, &existingReported.present,
			&existingEstimated.input, &existingEstimated.output, &existingEstimated.cacheCreation,
			&existingEstimated.cacheRead, &existingEstimated.cacheUnknown, &existingEstimated.present,
			&existingOutputBytes, &existingCompletedAt,
		)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if err := assertWorkflowCostAdmissibleTx(tx, workflowID); err != nil {
			return err
		}
		if err := reserveWorkflowCounterTx(tx, workflowID, providers.WorkflowBudgetSubmissions, "used_submissions", "max_submissions", 1, startedAt); err != nil {
			return err
		}
		if err := applyWorkflowCostDeltaTx(
			tx, workflowID,
			"", inferenceJournalUsage{}, inferenceJournalUsage{},
			string(record.CostState), reported, estimated,
			startedAt,
		); err != nil {
			return err
		}
		_, err = tx.Exec(`
INSERT INTO inference_submissions (
    id, operation_id, attempt_id, ordinal, attempt_ordinal,
    provider, protocol, transport, mode, reason, outcome, failure_category,
    cost_state,
    reported_input_tokens, reported_output_tokens, reported_cache_creation,
    reported_cache_read, reported_cache_unknown, has_reported_usage,
    estimated_input_tokens, estimated_output_tokens, estimated_cache_creation,
    estimated_cache_read, estimated_cache_unknown, has_estimated_usage,
    output_bytes, started_at, completed_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			record.ID, record.OperationID, record.AttemptID, record.Ordinal, record.AttemptOrdinal,
			journalText(record.Provider, 128), journalText(record.Protocol, 128),
			journalText(record.Transport, 128), journalText(record.Mode, 128), journalText(record.Reason, 256),
			string(record.Outcome), string(record.FailureCategory), string(record.CostState),
			reported.input, reported.output, reported.cacheCreation, reported.cacheRead, reported.cacheUnknown, reported.present,
			estimated.input, estimated.output, estimated.cacheCreation, estimated.cacheRead, estimated.cacheUnknown, estimated.present,
			record.OutputBytes, startedAt, completedAt,
		)
		if err != nil {
			return err
		}
	case err != nil:
		return err
	case operationID != record.OperationID || attemptID != record.AttemptID ||
		ordinal != record.Ordinal || attemptOrdinal != record.AttemptOrdinal:
		return fmt.Errorf("submission %q metadata changed after preparation", record.ID)
	default:
		mergedOutcome := string(record.Outcome)
		if existingOutcome != string(providers.InferenceSubmissionInFlight) {
			if mergedOutcome != string(providers.InferenceSubmissionInFlight) && mergedOutcome != existingOutcome {
				return fmt.Errorf("submission %q already completed as %s", record.ID, existingOutcome)
			}
			mergedOutcome = existingOutcome
		}
		mergedFailure := string(record.FailureCategory)
		if existingFailure != "" {
			mergedFailure = existingFailure
		}
		mergedCost := string(record.CostState)
		mergedReported, mergedEstimated := reported, estimated
		switch {
		case inferenceCostRank(existingCost) > inferenceCostRank(mergedCost):
			mergedCost = existingCost
			mergedReported = existingReported
			mergedEstimated = existingEstimated
		case inferenceCostRank(existingCost) == inferenceCostRank(mergedCost):
			mergedReported = mergeJournalUsage(existingReported, reported)
			mergedEstimated = mergeJournalUsage(existingEstimated, estimated)
		case existingEstimated.present != 0 && mergedEstimated.present == 0:
			mergedEstimated = existingEstimated
		}
		mergedOutputBytes := record.OutputBytes
		if existingOutputBytes > mergedOutputBytes {
			mergedOutputBytes = existingOutputBytes
		}
		mergedCompletedAt := completedAt
		if existingCompletedAt != 0 {
			mergedCompletedAt = existingCompletedAt
		}
		if err := applyWorkflowCostDeltaTx(
			tx, workflowID,
			existingCost, existingReported, existingEstimated,
			mergedCost, mergedReported, mergedEstimated,
			startedAt,
		); err != nil {
			return err
		}
		_, err = tx.Exec(`
UPDATE inference_submissions SET
    outcome = ?, failure_category = ?, cost_state = ?,
    reported_input_tokens = ?, reported_output_tokens = ?, reported_cache_creation = ?,
    reported_cache_read = ?, reported_cache_unknown = ?, has_reported_usage = ?,
    estimated_input_tokens = ?, estimated_output_tokens = ?, estimated_cache_creation = ?,
    estimated_cache_read = ?, estimated_cache_unknown = ?, has_estimated_usage = ?,
    output_bytes = ?, completed_at = ?
WHERE id = ?`,
			mergedOutcome, mergedFailure, mergedCost,
			mergedReported.input, mergedReported.output, mergedReported.cacheCreation, mergedReported.cacheRead, mergedReported.cacheUnknown, mergedReported.present,
			mergedEstimated.input, mergedEstimated.output, mergedEstimated.cacheCreation, mergedEstimated.cacheRead, mergedEstimated.cacheUnknown, mergedEstimated.present,
			mergedOutputBytes, mergedCompletedAt, record.ID,
		)
		if err != nil {
			return err
		}
	}
	_, err = tx.Exec(`
UPDATE inference_attempts
SET phase = CASE WHEN phase IN ('prepared', 'dispatching') THEN 'sent' ELSE phase END,
    sent_at = CASE WHEN sent_at = 0 THEN ? ELSE sent_at END
WHERE id = ? AND operation_id = ? AND phase <> 'terminal'`, startedAt, record.AttemptID, record.OperationID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE inference_operations SET updated_at = ? WHERE id = ? AND status = 'active'`, startedAt, record.OperationID)
	return err
}

func (j *inferenceJournal) MarkAttemptFirstEvent(operationID, attemptID, submissionID string, at time.Time) error {
	operationID = strings.TrimSpace(operationID)
	attemptID = strings.TrimSpace(attemptID)
	submissionID = strings.TrimSpace(submissionID)
	if operationID == "" || attemptID == "" || submissionID == "" {
		return errors.New("mark inference first event: ids are required")
	}
	stamp := journalTime(at)
	return j.write("mark inference first event", func(tx *sql.Tx) error {
		if err := j.assertOperationTx(tx, operationID, true); err != nil {
			return err
		}
		var count int
		if err := tx.QueryRow(`
SELECT COUNT(1) FROM inference_submissions
WHERE id = ? AND attempt_id = ? AND operation_id = ?`, submissionID, attemptID, operationID).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("submission %q does not belong to attempt %q", submissionID, attemptID)
		}
		result, err := tx.Exec(`
UPDATE inference_attempts
SET phase = CASE WHEN phase IN ('prepared', 'dispatching', 'sent') THEN 'streaming' ELSE phase END,
    first_event_at = CASE WHEN first_event_at = 0 THEN ? ELSE first_event_at END
WHERE id = ? AND operation_id = ? AND phase <> 'terminal'`, stamp, attemptID, operationID)
		if err != nil {
			return err
		}
		if n, _ := result.RowsAffected(); n != 1 {
			return fmt.Errorf("attempt %q is not active", attemptID)
		}
		_, err = tx.Exec(`UPDATE inference_operations SET updated_at = ? WHERE id = ? AND status = 'active'`, stamp, operationID)
		return err
	})
}

func (j *inferenceJournal) CompleteAttempt(record providers.InferenceAttemptTerminalRecord) error {
	record.OperationID = strings.TrimSpace(record.OperationID)
	record.AttemptID = strings.TrimSpace(record.AttemptID)
	if record.OperationID == "" || record.AttemptID == "" || record.Outcome == "" {
		return errors.New("complete inference attempt: ids and outcome are required")
	}
	// Durability barrier: land this attempt's coalesced streaming estimates
	// before we record the attempt terminal, so the two never disagree on crash.
	j.runtime.flushSubmissionProgress()
	stamp := journalTime(record.At)
	return j.write("complete inference attempt", func(tx *sql.Tx) error {
		if err := j.assertOperationTx(tx, record.OperationID, false); err != nil {
			return err
		}
		return completeInferenceAttemptTx(tx, record.OperationID, record.AttemptID, record.Outcome, record.Failure, stamp)
	})
}

func (j *inferenceJournal) PrepareRecoveryAttempt(ctx context.Context, record providers.InferenceRecoveryAttemptJournalRecord) error {
	recovery := record.Recovery
	recovery.OperationID = strings.TrimSpace(recovery.OperationID)
	recovery.AttemptID = strings.TrimSpace(recovery.AttemptID)
	if recovery.OperationID == "" || recovery.AttemptID == "" || recovery.Action == "" {
		return errors.New("record inference recovery: ids and action are required")
	}
	if err := normalizeInferenceAttemptJournalRecord(&record.NextAttempt); err != nil {
		return err
	}
	if recovery.OperationID != record.NextAttempt.OperationID {
		return errors.New("recovery and next attempt operations do not match")
	}
	stamp := journalTime(recovery.At)
	retryAt := optionalJournalTime(recovery.RetryAt)
	return j.writeContext(ctx, "prepare inference recovery attempt", func(tx *sql.Tx) error {
		if err := inferenceJournalContextError(ctx); err != nil {
			return err
		}
		if err := j.recordRecoveryTx(tx, recovery, stamp, retryAt); err != nil {
			return err
		}
		if err := j.prepareAttemptTx(tx, record.NextAttempt, journalTime(record.NextAttempt.At)); err != nil {
			return err
		}
		return inferenceJournalContextError(ctx)
	})
}

func inferenceJournalContextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func (j *inferenceJournal) recordRecoveryTx(tx *sql.Tx, record providers.InferenceRecoveryJournalRecord, stamp, retryAt int64) error {
	if err := j.assertOperationTx(tx, record.OperationID, true); err != nil {
		return err
	}
	var workflowID, phase, existingAction string
	if err := tx.QueryRow(`
SELECT o.workflow_id, a.phase, a.recovery_action
FROM inference_attempts a
JOIN inference_operations o ON o.id = a.operation_id
WHERE a.id = ? AND a.operation_id = ?`, record.AttemptID, record.OperationID).
		Scan(&workflowID, &phase, &existingAction); err != nil {
		return err
	}
	if phase != "terminal" {
		return fmt.Errorf("attempt %q is not terminal for recovery", record.AttemptID)
	}
	if existingAction != "" {
		if existingAction == string(record.Action) {
			return nil
		}
		return fmt.Errorf("attempt %q already recorded recovery %q", record.AttemptID, existingAction)
	}
	if err := assertWorkflowCostAdmissibleTx(tx, workflowID); err != nil {
		return err
	}
	if dimension, usedColumn, maxColumn := recoveryWorkflowColumns(record.Action); dimension != "" {
		if err := reserveWorkflowCounterTx(tx, workflowID, dimension, usedColumn, maxColumn, 1, stamp); err != nil {
			return err
		}
	}
	waitMillis := int64(0)
	if retryAt > stamp {
		waitMillis = retryAt - stamp
	}
	if waitMillis > 0 {
		if err := reserveWorkflowCounterTx(tx, workflowID, providers.WorkflowBudgetRecoveryWait, "used_recovery_wait_ms", "max_recovery_wait_ms", waitMillis, stamp); err != nil {
			return err
		}
	}
	result, err := tx.Exec(`
UPDATE inference_attempts SET recovery_action = ?, retry_at = ?
WHERE id = ? AND operation_id = ? AND phase = 'terminal'`,
		string(record.Action), retryAt, record.AttemptID, record.OperationID,
	)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return fmt.Errorf("attempt %q is not terminal for recovery", record.AttemptID)
	}
	result, err = tx.Exec(`
UPDATE inference_operations SET recovery_action = ?, updated_at = ?
WHERE id = ? AND status = 'active'`, string(record.Action), stamp, record.OperationID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return fmt.Errorf("operation %q is not active for recovery", record.OperationID)
	}
	return nil
}

func (j *inferenceJournal) CompleteOperation(record providers.InferenceOperationTerminalRecord) error {
	record.OperationID = strings.TrimSpace(record.OperationID)
	if record.OperationID == "" || record.Outcome == "" {
		return errors.New("complete inference operation: id and outcome are required")
	}
	// Durability barrier: land coalesced streaming estimates before the
	// operation terminal so cost accounting is complete at the boundary.
	j.runtime.flushSubmissionProgress()
	stamp := journalTime(record.At)
	return j.write("complete inference operation", func(tx *sql.Tx) error {
		if err := j.assertOperationTx(tx, record.OperationID, false); err != nil {
			return err
		}
		return completeInferenceOperationTx(tx, record.OperationID, record.Outcome, "", record.Failure, stamp)
	})
}

func (j *inferenceJournal) CompleteWorkflow(record providers.InferenceWorkflowTerminalRecord) error {
	record.WorkflowID = strings.TrimSpace(record.WorkflowID)
	if record.WorkflowID == "" || record.Outcome == "" {
		return errors.New("complete inference workflow: id and outcome are required")
	}
	stamp := journalTime(record.At)
	return j.write("complete inference workflow", func(tx *sql.Tx) error {
		var runtimeID, scope, owner string
		if err := tx.QueryRow(`
SELECT runtime_id, workspace_scope, owner_id
FROM inference_workflows WHERE id = ?`, record.WorkflowID).Scan(&runtimeID, &scope, &owner); err != nil {
			return err
		}
		if runtimeID != j.runtime.runtimeID || scope != j.runtime.workspaceScope || owner != j.ownerID {
			return fmt.Errorf("workflow %q is owned by another inference journal", record.WorkflowID)
		}
		return completeInferenceWorkflowTx(tx, record.WorkflowID, record.Outcome, stamp)
	})
}

func (j *inferenceJournal) assertOperationTx(tx *sql.Tx, operationID string, requireActive bool) error {
	var runtimeID, scope, owner, status string
	if err := tx.QueryRow(`
SELECT runtime_id, workspace_scope, owner_id, status
FROM inference_operations WHERE id = ?`, operationID).
		Scan(&runtimeID, &scope, &owner, &status); err != nil {
		return err
	}
	if runtimeID != j.runtime.runtimeID || scope != j.runtime.workspaceScope || owner != j.ownerID {
		return fmt.Errorf("operation %q is owned by another inference journal", operationID)
	}
	if requireActive && status != "active" {
		return fmt.Errorf("operation %q is already terminal (%s)", operationID, status)
	}
	return nil
}

func normalizeInferenceWorkflowRecord(record providers.InferenceWorkflowJournalRecord, operation providers.InferenceOperation, at time.Time) providers.InferenceWorkflowJournalRecord {
	emptyRecord := strings.TrimSpace(record.ID) == ""
	if emptyRecord {
		record.ID = strings.TrimSpace(operation.WorkflowID)
		if record.ID == "" {
			record.ID = "iwf-" + operation.ID
		}
	}
	if record.Profile == "" {
		record.Profile = operation.WorkloadProfile
	}
	if record.Profile == "" {
		record.Profile = providers.InferenceProfileInteractive
	}
	if emptyRecord {
		record.Budget = providers.WorkflowBudgetSpecForProfile(record.Profile)
	}
	if record.StartedAt.IsZero() {
		record.StartedAt = at
	}
	return record
}

type inferenceWorkflowLimitColumns struct {
	operations, attempts, submissions, replays int64
	transportSwitches, credentialRefreshes     int64
	payloadTransforms, childOperations         int64
	recoveryWaitMillis, unknownBillable        int64
	usageTokens                                int64
}

func inferenceWorkflowLimits(spec providers.WorkflowBudgetSpec) (inferenceWorkflowLimitColumns, error) {
	convert := func(name string, limit providers.BudgetLimit) (int64, error) {
		if !limit.Set {
			return -1, nil
		}
		if limit.Value > uint64(1<<63-1) {
			return 0, fmt.Errorf("workflow budget %s exceeds SQLite integer range", name)
		}
		return int64(limit.Value), nil
	}
	var out inferenceWorkflowLimitColumns
	var err error
	fields := []struct {
		name  string
		limit providers.BudgetLimit
		dest  *int64
	}{
		{"operations", spec.MaxOperations, &out.operations},
		{"attempts", spec.MaxAttempts, &out.attempts},
		{"submissions", spec.MaxSubmissions, &out.submissions},
		{"replays", spec.MaxSamePayloadReplays, &out.replays},
		{"transport switches", spec.MaxTransportSwitches, &out.transportSwitches},
		{"credential refreshes", spec.MaxCredentialRefreshes, &out.credentialRefreshes},
		{"payload transforms", spec.MaxPayloadTransforms, &out.payloadTransforms},
		{"child operations", spec.MaxChildOperations, &out.childOperations},
		{"recovery wait", spec.MaxRecoveryWaitMillis, &out.recoveryWaitMillis},
		{"unknown billable", spec.MaxUnknownBillableSubmissions, &out.unknownBillable},
		{"usage tokens", spec.MaxUsageTokens, &out.usageTokens},
	}
	for _, field := range fields {
		*field.dest, err = convert(field.name, field.limit)
		if err != nil {
			return inferenceWorkflowLimitColumns{}, err
		}
	}
	return out, nil
}

func (j *inferenceJournal) prepareWorkflowTx(tx *sql.Tx, record providers.InferenceWorkflowJournalRecord, fallbackAt int64) error {
	record.ID = strings.TrimSpace(record.ID)
	if record.ID == "" || record.Profile == "" {
		return errors.New("prepare inference workflow: id and profile are required")
	}
	limits, err := inferenceWorkflowLimits(record.Budget)
	if err != nil {
		return err
	}
	startedAt := optionalJournalTime(record.StartedAt)
	if startedAt == 0 {
		startedAt = fallbackAt
	}
	var runtimeID, scope, owner, profile, status string
	var existing inferenceWorkflowLimitColumns
	err = tx.QueryRow(`
SELECT runtime_id, workspace_scope, owner_id, workload_profile,
       max_operations, max_attempts, max_submissions, max_replays,
       max_transport_switches, max_credential_refreshes, max_payload_transforms,
       max_child_operations, max_recovery_wait_ms, max_unknown_billable,
       max_usage_tokens, status
FROM inference_workflows WHERE id = ?`, record.ID).Scan(
		&runtimeID, &scope, &owner, &profile,
		&existing.operations, &existing.attempts, &existing.submissions, &existing.replays,
		&existing.transportSwitches, &existing.credentialRefreshes, &existing.payloadTransforms,
		&existing.childOperations, &existing.recoveryWaitMillis, &existing.unknownBillable,
		&existing.usageTokens, &status,
	)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		_, err = tx.Exec(`
INSERT INTO inference_workflows (
    id, runtime_id, workspace_scope, owner_id, workload_profile,
    max_operations, max_attempts, max_submissions, max_replays,
    max_transport_switches, max_credential_refreshes, max_payload_transforms,
    max_child_operations, max_recovery_wait_ms, max_unknown_billable,
    max_usage_tokens, status, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'active', ?, ?)`,
			record.ID, j.runtime.runtimeID, j.runtime.workspaceScope, j.ownerID, string(record.Profile),
			limits.operations, limits.attempts, limits.submissions, limits.replays,
			limits.transportSwitches, limits.credentialRefreshes, limits.payloadTransforms,
			limits.childOperations, limits.recoveryWaitMillis, limits.unknownBillable,
			limits.usageTokens, startedAt, fallbackAt,
		)
		return err
	case err != nil:
		return err
	case runtimeID != j.runtime.runtimeID || scope != j.runtime.workspaceScope || owner != j.ownerID ||
		profile != string(record.Profile) || existing != limits:
		return fmt.Errorf("workflow %q metadata changed after preparation", record.ID)
	case status != "active":
		return fmt.Errorf("workflow %q is already terminal (%s)", record.ID, status)
	default:
		return nil
	}
}

func validateInferenceParentTx(tx *sql.Tx, operation providers.InferenceOperation) error {
	parentID := strings.TrimSpace(operation.ParentOperationID)
	if parentID == "" {
		return nil
	}
	if parentID == operation.ID {
		return errors.New("inference operation cannot be its own parent")
	}
	var workflowID string
	if err := tx.QueryRow(`SELECT workflow_id FROM inference_operations WHERE id = ?`, parentID).Scan(&workflowID); err != nil {
		return fmt.Errorf("load parent inference operation %q: %w", parentID, err)
	}
	if workflowID != operation.WorkflowID {
		return fmt.Errorf("parent inference operation %q belongs to workflow %q, not %q", parentID, workflowID, operation.WorkflowID)
	}
	return nil
}

func reserveWorkflowCounterTx(
	tx *sql.Tx,
	workflowID string,
	dimension providers.WorkflowBudgetDimension,
	usedColumn, maxColumn string,
	delta int64,
	stamp int64,
) error {
	if delta < 0 {
		panic("session: negative workflow budget reservation")
	}
	allowed := map[string]string{
		"used_operations":           "max_operations",
		"used_attempts":             "max_attempts",
		"used_submissions":          "max_submissions",
		"used_replays":              "max_replays",
		"used_transport_switches":   "max_transport_switches",
		"used_credential_refreshes": "max_credential_refreshes",
		"used_payload_transforms":   "max_payload_transforms",
		"used_child_operations":     "max_child_operations",
		"used_recovery_wait_ms":     "max_recovery_wait_ms",
	}
	if allowed[usedColumn] != maxColumn {
		panic("session: invalid workflow budget columns")
	}
	query := fmt.Sprintf(`
UPDATE inference_workflows
SET %s = %s + ?, updated_at = ?
WHERE id = ? AND status = 'active'
  AND (%s < 0 OR %s + ? <= %s)`, usedColumn, usedColumn, maxColumn, usedColumn, maxColumn)
	result, err := tx.Exec(query, delta, stamp, workflowID, delta)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 1 {
		return nil
	}
	var used, limit int64
	var status string
	read := fmt.Sprintf(`SELECT %s, %s, status FROM inference_workflows WHERE id = ?`, usedColumn, maxColumn)
	if err := tx.QueryRow(read, workflowID).Scan(&used, &limit, &status); err != nil {
		return err
	}
	if status != "active" {
		return fmt.Errorf("workflow %q is already terminal (%s)", workflowID, status)
	}
	if limit < 0 {
		return fmt.Errorf("workflow %q counter %s could not be reserved", workflowID, dimension)
	}
	return &providers.WorkflowBudgetExceededError{
		WorkflowID: workflowID,
		Dimension:  dimension,
		Limit:      uint64(limit),
		Used:       uint64(used),
		Requested:  uint64(delta),
	}
}

func assertWorkflowCostAdmissibleTx(tx *sql.Tx, workflowID string) error {
	var maxUsage, knownUsage, estimatedUsage, unknown int64
	var status string
	if err := tx.QueryRow(`
SELECT max_usage_tokens, known_usage_tokens, estimated_usage_tokens, unknown_billable, status
FROM inference_workflows WHERE id = ?`, workflowID).
		Scan(&maxUsage, &knownUsage, &estimatedUsage, &unknown, &status); err != nil {
		return err
	}
	if status != "active" {
		return fmt.Errorf("workflow %q is already terminal (%s)", workflowID, status)
	}
	if maxUsage < 0 {
		return nil
	}
	if unknown > 0 {
		return &providers.WorkflowCostIndeterminateError{
			WorkflowID: workflowID, UnknownBillableSubmissions: uint64(unknown),
		}
	}
	used := knownUsage + estimatedUsage
	if used <= maxUsage {
		return nil
	}
	return &providers.WorkflowBudgetExceededError{
		WorkflowID: workflowID,
		Dimension:  providers.WorkflowBudgetUsageTokens,
		Limit:      uint64(maxUsage),
		Used:       uint64(used),
	}
}

func applyWorkflowCostDeltaTx(
	tx *sql.Tx,
	workflowID string,
	oldState string,
	oldReported, oldEstimated inferenceJournalUsage,
	newState string,
	newReported, newEstimated inferenceJournalUsage,
	stamp int64,
) error {
	type contribution struct {
		known, estimated, unknown    int64
		knownTokens, estimatedTokens int64
	}
	contribute := func(state string, reported, estimated inferenceJournalUsage) contribution {
		switch state {
		case string(providers.InferenceCostKnown):
			return contribution{known: 1, knownTokens: journalUsageTokens(reported)}
		case string(providers.InferenceCostEstimated):
			return contribution{estimated: 1, estimatedTokens: journalUsageTokens(estimated)}
		case string(providers.InferenceCostUnknownBillable):
			return contribution{unknown: 1}
		default:
			return contribution{}
		}
	}
	oldCost := contribute(oldState, oldReported, oldEstimated)
	newCost := contribute(newState, newReported, newEstimated)
	knownDelta := newCost.known - oldCost.known
	estimatedDelta := newCost.estimated - oldCost.estimated
	unknownDelta := newCost.unknown - oldCost.unknown
	knownTokensDelta := newCost.knownTokens - oldCost.knownTokens
	estimatedTokensDelta := newCost.estimatedTokens - oldCost.estimatedTokens
	if knownDelta == 0 && estimatedDelta == 0 && unknownDelta == 0 && knownTokensDelta == 0 && estimatedTokensDelta == 0 {
		return nil
	}
	var known, estimated, unknown, knownTokens, estimatedTokens, maxUnknown int64
	if err := tx.QueryRow(`
SELECT known_submissions, estimated_submissions, unknown_billable,
       known_usage_tokens, estimated_usage_tokens, max_unknown_billable
FROM inference_workflows WHERE id = ?`, workflowID).
		Scan(&known, &estimated, &unknown, &knownTokens, &estimatedTokens, &maxUnknown); err != nil {
		return err
	}
	known += knownDelta
	estimated += estimatedDelta
	unknown += unknownDelta
	knownTokens += knownTokensDelta
	estimatedTokens += estimatedTokensDelta
	if known < 0 || estimated < 0 || unknown < 0 || knownTokens < 0 || estimatedTokens < 0 {
		return errors.New("workflow cost accounting underflow")
	}
	if maxUnknown >= 0 && unknown > maxUnknown {
		return &providers.WorkflowBudgetExceededError{
			WorkflowID: workflowID,
			Dimension:  providers.WorkflowBudgetUnknownBillable,
			Limit:      uint64(maxUnknown),
			Used:       uint64(unknown - unknownDelta),
			Requested:  uint64(unknownDelta),
		}
	}
	_, err := tx.Exec(`
UPDATE inference_workflows SET
    known_submissions = ?, estimated_submissions = ?, unknown_billable = ?,
    known_usage_tokens = ?, estimated_usage_tokens = ?, updated_at = ?
WHERE id = ?`,
		known, estimated, unknown, knownTokens, estimatedTokens, stamp, workflowID,
	)
	return err
}

func journalUsageTokens(usage inferenceJournalUsage) int64 {
	if usage.present == 0 {
		return 0
	}
	return int64(usage.input + usage.output + usage.cacheCreation + usage.cacheRead)
}

// priorAttemptAnsweredTx reports whether the attempt immediately before
// ordinal produced any provider signal — an HTTP status or at least one
// response event. Replaying an unanswered attempt cannot be billed because
// the request died in transit, so those replays are admitted without
// consuming the same-payload replay budget; only answered-attempt replays
// (partial output, provider error responses) carry billing or semantic risk
// and consume it. Missing evidence stays conservative: the replay is charged.
func priorAttemptAnsweredTx(tx *sql.Tx, operationID string, priorOrdinal int) (bool, error) {
	var origin string
	var firstEventAt int64
	err := tx.QueryRow(`
SELECT failure_origin, first_event_at FROM inference_attempts
WHERE operation_id = ? AND ordinal = ?`, operationID, priorOrdinal).Scan(&origin, &firstEventAt)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if origin == string(providers.FailureOriginNetwork) && firstEventAt == 0 {
		return false, nil
	}
	return true, nil
}

func recoveryWorkflowColumns(action providers.RecoveryActionKind) (providers.WorkflowBudgetDimension, string, string) {
	switch action {
	case providers.RecoverySwitchTransport:
		return providers.WorkflowBudgetTransportSwitches, "used_transport_switches", "max_transport_switches"
	case providers.RecoveryRefreshAuth:
		return providers.WorkflowBudgetCredentialRefresh, "used_credential_refreshes", "max_credential_refreshes"
	case providers.RecoveryTransformPayload:
		return providers.WorkflowBudgetPayloadTransforms, "used_payload_transforms", "max_payload_transforms"
	default:
		return "", "", ""
	}
}

func (j *inferenceJournal) write(action string, fn func(*sql.Tx) error) error {
	return j.writeContext(context.Background(), action, fn)
}

func (j *inferenceJournal) writeContext(ctx context.Context, action string, fn func(*sql.Tx) error) error {
	if j == nil || j.runtime == nil || strings.TrimSpace(j.runtime.sessDir) == "" || strings.TrimSpace(j.runtime.runtimeID) == "" {
		return fmt.Errorf("%s: inference journal is not initialized", action)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	j.runtime.startHeartbeat()
	db, err := openStore(j.runtime.sessDir)
	if err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	defer db.Close()
	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%s: begin: %w", action, err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`
UPDATE inference_journal_runtimes SET heartbeat_at = ?
WHERE id = ? AND closed_at = 0`, time.Now().UTC().UnixMilli(), j.runtime.runtimeID); err != nil {
		return fmt.Errorf("%s: refresh runtime lease: %w", action, err)
	}
	if err := fn(tx); err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%s: commit: %w", action, err)
	}
	return nil
}

type InferenceCrashRecovery struct {
	OperationID   string
	AttemptID     string
	Profile       providers.InferenceWorkloadProfile
	PriorPhase    string
	PriorOutcome  string
	PriorRecovery providers.RecoveryActionKind
	Outcome       providers.InferenceTerminalOutcome
	Action        providers.RecoveryActionKind
}

// ReconcileOrphans terminalizes active operations from an older runtime in
// this workspace. It never sends or reconstructs a provider request.
func (r *InferenceJournalRuntime) ReconcileOrphans(now time.Time) ([]InferenceCrashRecovery, error) {
	if r == nil {
		return nil, nil
	}
	db, err := openStore(r.sessDir)
	if err != nil {
		return nil, fmt.Errorf("reconcile inference journal: %w", err)
	}
	defer db.Close()
	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()
	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("reconcile inference journal: begin: %w", err)
	}
	defer tx.Rollback()

	cutoff := journalTime(now.Add(-inferenceJournalStaleAfter))
	rows, err := tx.Query(`
SELECT o.id, o.workload_profile,
       COALESCE(a.id, ''), COALESCE(a.phase, ''),
       COALESCE(a.terminal_outcome, ''), COALESCE(a.recovery_action, ''),
       rt.pid, rt.heartbeat_at, rt.closed_at
FROM inference_operations o
JOIN inference_journal_runtimes rt ON rt.id = o.runtime_id
LEFT JOIN inference_attempts a
  ON a.operation_id = o.id
 AND a.ordinal = (SELECT MAX(last.ordinal) FROM inference_attempts last WHERE last.operation_id = o.id)
WHERE o.workspace_scope = ? AND o.runtime_id <> ? AND o.status = 'active'
  AND (rt.closed_at <> 0 OR rt.heartbeat_at < ?)
ORDER BY o.created_at, o.id`, r.workspaceScope, r.runtimeID, cutoff)
	if err != nil {
		return nil, fmt.Errorf("reconcile inference journal: list: %w", err)
	}
	var recoveries []InferenceCrashRecovery
	for rows.Next() {
		var item InferenceCrashRecovery
		var profile, priorRecovery string
		var pid int
		var heartbeatAt, closedAt int64
		if err := rows.Scan(
			&item.OperationID, &profile, &item.AttemptID, &item.PriorPhase,
			&item.PriorOutcome, &priorRecovery, &pid, &heartbeatAt, &closedAt,
		); err != nil {
			rows.Close()
			return nil, fmt.Errorf("reconcile inference journal: scan: %w", err)
		}
		if closedAt == 0 && inferenceRuntimeProcessAlive(pid) {
			continue
		}
		item.Profile = providers.InferenceWorkloadProfile(profile)
		item.PriorRecovery = providers.RecoveryActionKind(priorRecovery)
		switch {
		case item.Profile == providers.InferenceProfileBestEffort:
			item.Outcome = providers.InferenceOutcomeAbandoned
			item.Action = providers.RecoveryStop
		case item.PriorPhase == "" || item.PriorPhase == "prepared":
			item.Outcome = providers.InferenceOutcomeInterrupted
			item.Action = providers.RecoveryRescheduleSafe
		case item.PriorPhase == "terminal" && recoveryCanCreateAnotherAttempt(item.PriorRecovery):
			item.Outcome = providers.InferenceOutcomeInterrupted
			item.Action = providers.RecoveryRescheduleSafe
		case item.PriorPhase == "terminal":
			item.Outcome = providers.InferenceOutcomeInterrupted
			item.Action = providers.RecoveryStop
		default:
			item.Outcome = providers.InferenceOutcomeBlocked
			item.Action = providers.RecoveryBlockAmbiguous
		}
		recoveries = append(recoveries, item)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("reconcile inference journal: close rows: %w", err)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reconcile inference journal: rows: %w", err)
	}

	stamp := journalTime(now)
	for _, item := range recoveries {
		submissionOutcome := providers.InferenceSubmissionInterrupted
		if item.Outcome == providers.InferenceOutcomeAbandoned {
			submissionOutcome = providers.InferenceSubmissionAbandoned
		}
		if _, err := tx.Exec(`
UPDATE inference_submissions
SET outcome = ?, completed_at = CASE WHEN completed_at = 0 THEN ? ELSE completed_at END
WHERE operation_id = ? AND outcome = ?`,
			string(submissionOutcome), stamp, item.OperationID, string(providers.InferenceSubmissionInFlight)); err != nil {
			return nil, fmt.Errorf("reconcile inference journal submissions %q: %w", item.OperationID, err)
		}
		if item.AttemptID != "" && item.PriorPhase != "terminal" {
			if err := completeInferenceAttemptTx(tx, item.OperationID, item.AttemptID, item.Outcome, providers.InferenceJournalFailure{}, stamp); err != nil {
				return nil, fmt.Errorf("reconcile inference journal attempt %q: %w", item.AttemptID, err)
			}
			if _, err := tx.Exec(`
UPDATE inference_attempts SET recovery_action = ? WHERE id = ? AND operation_id = ?`,
				string(item.Action), item.AttemptID, item.OperationID); err != nil {
				return nil, fmt.Errorf("reconcile inference journal recovery %q: %w", item.AttemptID, err)
			}
		}
		if err := completeInferenceOperationTx(tx, item.OperationID, item.Outcome, item.Action, providers.InferenceJournalFailure{}, stamp); err != nil {
			return nil, fmt.Errorf("reconcile inference journal operation %q: %w", item.OperationID, err)
		}
	}
	if err := reconcileOrphanWorkflowsTx(tx, r.workspaceScope, r.runtimeID, cutoff, stamp); err != nil {
		return nil, fmt.Errorf("reconcile inference journal workflows: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("reconcile inference journal: commit: %w", err)
	}
	return recoveries, nil
}

func recoveryCanCreateAnotherAttempt(action providers.RecoveryActionKind) bool {
	switch action {
	case providers.RecoveryReplaySame, providers.RecoveryWaitThenReplay,
		providers.RecoveryTransformPayload, providers.RecoveryRefreshAuth,
		providers.RecoverySwitchTransport, providers.RecoveryRescheduleSafe:
		return true
	default:
		return false
	}
}

func (r *InferenceJournalRuntime) Prune(now time.Time) (int64, error) {
	if r == nil {
		return 0, nil
	}
	db, err := openStore(r.sessDir)
	if err != nil {
		return 0, fmt.Errorf("prune inference journal: %w", err)
	}
	defer db.Close()
	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()
	cutoff := journalTime(now.Add(-inferenceJournalRetention))
	result, err := db.Exec(`
DELETE FROM inference_operations
WHERE workspace_scope = ? AND status <> 'active' AND updated_at < ?`, r.workspaceScope, cutoff)
	if err != nil {
		return 0, fmt.Errorf("prune inference journal: %w", err)
	}
	deletedOperations, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("prune inference journal operations: %w", err)
	}
	workflowResult, err := db.Exec(`
DELETE FROM inference_workflows
WHERE workspace_scope = ? AND status <> 'active' AND updated_at < ?
  AND NOT EXISTS (
      SELECT 1 FROM inference_operations o WHERE o.workflow_id = inference_workflows.id
  )`, r.workspaceScope, cutoff)
	if err != nil {
		return 0, fmt.Errorf("prune inference journal workflows: %w", err)
	}
	deletedWorkflows, err := workflowResult.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("prune inference journal workflows: %w", err)
	}
	return deletedOperations + deletedWorkflows, nil
}

func reconcileOrphanWorkflowsTx(tx *sql.Tx, workspaceScope, currentRuntimeID string, cutoff, stamp int64) error {
	rows, err := tx.Query(`
SELECT w.id, w.workload_profile, rt.pid, rt.closed_at
FROM inference_workflows w
JOIN inference_journal_runtimes rt ON rt.id = w.runtime_id
WHERE w.workspace_scope = ? AND w.runtime_id <> ? AND w.status = 'active'
  AND (rt.closed_at <> 0 OR rt.heartbeat_at < ?)
  AND NOT EXISTS (
      SELECT 1 FROM inference_operations o
      WHERE o.workflow_id = w.id AND o.status = 'active'
  )
ORDER BY w.created_at, w.id`, workspaceScope, currentRuntimeID, cutoff)
	if err != nil {
		return err
	}
	type candidate struct {
		id       string
		profile  providers.InferenceWorkloadProfile
		pid      int
		closedAt int64
	}
	var candidates []candidate
	for rows.Next() {
		var item candidate
		var profile string
		if err := rows.Scan(&item.id, &profile, &item.pid, &item.closedAt); err != nil {
			rows.Close()
			return err
		}
		item.profile = providers.InferenceWorkloadProfile(profile)
		candidates = append(candidates, item)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, item := range candidates {
		if item.closedAt == 0 && inferenceRuntimeProcessAlive(item.pid) {
			continue
		}
		outcome, err := orphanWorkflowOutcomeTx(tx, item.id, item.profile)
		if err != nil {
			return err
		}
		if err := completeInferenceWorkflowTx(tx, item.id, outcome, stamp); err != nil {
			return err
		}
	}
	return nil
}

func orphanWorkflowOutcomeTx(tx *sql.Tx, workflowID string, profile providers.InferenceWorkloadProfile) (providers.InferenceTerminalOutcome, error) {
	var blocked int
	if err := tx.QueryRow(`
SELECT SUM(CASE WHEN status = 'blocked' THEN 1 ELSE 0 END)
FROM inference_operations WHERE workflow_id = ?`, workflowID).
		Scan(&blocked); err != nil {
		return "", err
	}
	switch {
	case blocked > 0:
		return providers.InferenceOutcomeBlocked, nil
	case profile == providers.InferenceProfileBestEffort:
		return providers.InferenceOutcomeAbandoned, nil
	default:
		// Historical failures may already have been recovered by a later
		// compaction or agent operation, and all-success history may simply be
		// a tool loop between rounds. Without a durable outer completion record,
		// the workflow outcome is unproven and therefore interrupted.
		return providers.InferenceOutcomeInterrupted, nil
	}
}

func completeInferenceAttemptTx(
	tx *sql.Tx,
	operationID, attemptID string,
	outcome providers.InferenceTerminalOutcome,
	failure providers.InferenceJournalFailure,
	stamp int64,
) error {
	var phase, existingOutcome string
	if err := tx.QueryRow(`
SELECT phase, terminal_outcome FROM inference_attempts
WHERE id = ? AND operation_id = ?`, attemptID, operationID).Scan(&phase, &existingOutcome); err != nil {
		return err
	}
	if phase == "terminal" {
		if existingOutcome == string(outcome) {
			return nil
		}
		return fmt.Errorf("attempt %q already completed as %s", attemptID, existingOutcome)
	}
	_, err := tx.Exec(`
UPDATE inference_attempts SET
    phase = 'terminal', terminal_outcome = ?, terminal_at = ?,
    failure_origin = ?, failure_category = ?, provider_family = ?,
    provider_code = ?, http_status = ?, confidence = ?, failure_message = ?
WHERE id = ? AND operation_id = ?`,
		string(outcome), stamp, string(failure.Origin), string(failure.Category),
		journalText(failure.ProviderFamily, 128), journalText(failure.ProviderCode, 128),
		failure.HTTPStatus, string(failure.Confidence), journalText(failure.Message, 256), attemptID, operationID,
	)
	return err
}

func completeInferenceOperationTx(
	tx *sql.Tx,
	operationID string,
	outcome providers.InferenceTerminalOutcome,
	action providers.RecoveryActionKind,
	failure providers.InferenceJournalFailure,
	stamp int64,
) error {
	var status, existingOutcome string
	if err := tx.QueryRow(`
SELECT status, terminal_outcome FROM inference_operations WHERE id = ?`, operationID).
		Scan(&status, &existingOutcome); err != nil {
		return err
	}
	if status != "active" {
		if existingOutcome == string(outcome) {
			return nil
		}
		return fmt.Errorf("operation %q already completed as %s", operationID, existingOutcome)
	}
	_, err := tx.Exec(`
UPDATE inference_operations SET
    status = ?, terminal_outcome = ?, recovery_action = ?,
    failure_origin = ?, failure_category = ?, provider_family = ?,
    provider_code = ?, http_status = ?, confidence = ?, failure_message = ?,
    updated_at = ?, terminal_at = ?
WHERE id = ?`,
		string(outcome), string(outcome), string(action),
		string(failure.Origin), string(failure.Category), journalText(failure.ProviderFamily, 128),
		journalText(failure.ProviderCode, 128), failure.HTTPStatus, string(failure.Confidence),
		journalText(failure.Message, 256), stamp, stamp, operationID,
	)
	return err
}

func completeInferenceWorkflowTx(
	tx *sql.Tx,
	workflowID string,
	outcome providers.InferenceTerminalOutcome,
	stamp int64,
) error {
	var status string
	if err := tx.QueryRow(`SELECT status FROM inference_workflows WHERE id = ?`, workflowID).Scan(&status); err != nil {
		return err
	}
	if status != "active" {
		if status == string(outcome) {
			return nil
		}
		return fmt.Errorf("workflow %q already completed as %s", workflowID, status)
	}
	var activeOperations int
	if err := tx.QueryRow(`
SELECT COUNT(*) FROM inference_operations
WHERE workflow_id = ? AND status = 'active'`, workflowID).Scan(&activeOperations); err != nil {
		return err
	}
	if activeOperations != 0 {
		return fmt.Errorf("workflow %q still has %d active operation(s)", workflowID, activeOperations)
	}
	result, err := tx.Exec(`
UPDATE inference_workflows
SET status = ?, updated_at = ?, terminal_at = ?
WHERE id = ? AND status = 'active'`, string(outcome), stamp, stamp, workflowID)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return fmt.Errorf("workflow %q was not active for completion", workflowID)
	}
	return nil
}

type inferenceJournalUsage struct {
	input, output, cacheCreation, cacheRead int
	cacheUnknown, present                   int
}

func journalUsage(usage *providers.TokenUsage) inferenceJournalUsage {
	if usage == nil {
		return inferenceJournalUsage{}
	}
	unknown := 0
	if usage.CacheCreationUnknown {
		unknown = 1
	}
	return inferenceJournalUsage{
		input:         usage.InputTokens,
		output:        usage.OutputTokens,
		cacheCreation: usage.CacheCreationTokens,
		cacheRead:     usage.CacheReadTokens,
		cacheUnknown:  unknown,
		present:       1,
	}
}

func inferenceCostRank(state string) int {
	switch providers.InferenceCostState(state) {
	case providers.InferenceCostKnown:
		return 3
	case providers.InferenceCostEstimated:
		return 2
	default:
		return 1
	}
}

func mergeJournalUsage(left, right inferenceJournalUsage) inferenceJournalUsage {
	if left.present == 0 {
		return right
	}
	if right.present == 0 {
		return left
	}
	return inferenceJournalUsage{
		input:         maxInt(left.input, right.input),
		output:        maxInt(left.output, right.output),
		cacheCreation: maxInt(left.cacheCreation, right.cacheCreation),
		cacheRead:     maxInt(left.cacheRead, right.cacheRead),
		cacheUnknown:  maxInt(left.cacheUnknown, right.cacheUnknown),
		present:       1,
	}
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func journalTime(at time.Time) int64 {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	return at.UTC().UnixMilli()
}

func optionalJournalTime(at time.Time) int64 {
	if at.IsZero() {
		return 0
	}
	return at.UTC().UnixMilli()
}

func journalText(value string, maxBytes int) string {
	value = strings.TrimSpace(value)
	if maxBytes > 0 && len(value) > maxBytes {
		value = value[:maxBytes]
	}
	return value
}

func validInferenceRequestHash(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 64 {
		return false
	}
	for _, ch := range value {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return true
}

func newInferenceRuntimeID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic(fmt.Sprintf("session: generate inference runtime id: %v", err))
	}
	return "irt-" + hex.EncodeToString(raw[:])
}
