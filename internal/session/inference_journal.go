package session

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
)

const inferenceJournalRetention = 30 * 24 * time.Hour

// InferenceJournalRuntime binds all journals created by one runtime process to
// a workspace scope. A fresh runtime id lets startup recovery distinguish
// records left by the previous process from work created during this boot.
type InferenceJournalRuntime struct {
	sessDir        string
	workspaceScope string
	runtimeID      string
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
	if err := db.Close(); err != nil {
		return nil, fmt.Errorf("close inference journal: %w", err)
	}
	return &InferenceJournalRuntime{
		sessDir:        sessDir,
		workspaceScope: workspaceScope,
		runtimeID:      newInferenceRuntimeID(),
	}, nil
}

func (r *InferenceJournalRuntime) RuntimeID() string {
	if r == nil {
		return ""
	}
	return r.runtimeID
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
		sessDir:        r.sessDir,
		workspaceScope: r.workspaceScope,
		runtimeID:      r.runtimeID,
		ownerID:        ownerID,
	}
}

type inferenceJournal struct {
	sessDir        string
	workspaceScope string
	runtimeID      string
	ownerID        string
}

func (j *inferenceJournal) PrepareOperation(record providers.InferenceOperationJournalRecord) error {
	op := record.Operation
	op.ID = strings.TrimSpace(op.ID)
	record.RequestHash = strings.TrimSpace(record.RequestHash)
	if op.ID == "" || record.RequestHash == "" {
		return errors.New("prepare inference operation: operation id and request hash are required")
	}
	if op.PayloadVersion < 1 {
		return errors.New("prepare inference operation: payload version must be positive")
	}
	at := journalTime(record.At)
	return j.write("prepare inference operation", func(tx *sql.Tx) error {
		var runtimeID, scope, owner, kind, profile, requestHash, status string
		var payloadVersion int
		err := tx.QueryRow(`
SELECT runtime_id, workspace_scope, owner_id, kind, workload_profile,
       payload_version, request_hash, status
FROM inference_operations WHERE id = ?`, op.ID).Scan(
			&runtimeID, &scope, &owner, &kind, &profile, &payloadVersion, &requestHash, &status,
		)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			_, err = tx.Exec(`
INSERT INTO inference_operations (
    id, runtime_id, workspace_scope, owner_id, kind, workload_profile,
    payload_version, request_hash, status, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'active', ?, ?)`,
				op.ID, j.runtimeID, j.workspaceScope, j.ownerID,
				string(op.Kind), string(op.WorkloadProfile), op.PayloadVersion,
				record.RequestHash, at, at,
			)
			return err
		case err != nil:
			return err
		case runtimeID != j.runtimeID || scope != j.workspaceScope || owner != j.ownerID ||
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
	record.OperationID = strings.TrimSpace(record.OperationID)
	record.AttemptID = strings.TrimSpace(record.AttemptID)
	record.RequestHash = strings.TrimSpace(record.RequestHash)
	if record.OperationID == "" || record.AttemptID == "" || record.RequestHash == "" || record.Ordinal < 1 {
		return errors.New("prepare inference attempt: operation, attempt, ordinal, and request hash are required")
	}
	at := journalTime(record.At)
	return j.write("prepare inference attempt", func(tx *sql.Tx) error {
		var runtimeID, requestHash, status string
		if err := tx.QueryRow(`
SELECT runtime_id, request_hash, status FROM inference_operations WHERE id = ?`, record.OperationID).
			Scan(&runtimeID, &requestHash, &status); err != nil {
			return err
		}
		if runtimeID != j.runtimeID || requestHash != record.RequestHash || status != "active" {
			return fmt.Errorf("operation %q is not the active prepared operation", record.OperationID)
		}
		result, err := tx.Exec(`
INSERT OR IGNORE INTO inference_attempts (
    id, operation_id, ordinal, request_hash, phase, prepared_at
) VALUES (?, ?, ?, ?, 'prepared', ?)`,
			record.AttemptID, record.OperationID, record.Ordinal, record.RequestHash, at,
		)
		if err != nil {
			return err
		}
		if n, _ := result.RowsAffected(); n == 0 {
			var operationID, attemptHash string
			var ordinal int
			if err := tx.QueryRow(`
SELECT operation_id, ordinal, request_hash FROM inference_attempts WHERE id = ?`, record.AttemptID).
				Scan(&operationID, &ordinal, &attemptHash); err != nil {
				return err
			}
			if operationID != record.OperationID || ordinal != record.Ordinal || attemptHash != record.RequestHash {
				return fmt.Errorf("attempt %q metadata changed after preparation", record.AttemptID)
			}
		}
		_, err = tx.Exec(`UPDATE inference_operations SET updated_at = ? WHERE id = ?`, at, record.OperationID)
		return err
	})
}

func (j *inferenceJournal) MarkAttemptDispatching(operationID, attemptID string, at time.Time) error {
	operationID = strings.TrimSpace(operationID)
	attemptID = strings.TrimSpace(attemptID)
	if operationID == "" || attemptID == "" {
		return errors.New("mark inference attempt dispatching: ids are required")
	}
	stamp := journalTime(at)
	return j.write("mark inference attempt dispatching", func(tx *sql.Tx) error {
		result, err := tx.Exec(`
UPDATE inference_attempts
SET phase = CASE WHEN phase = 'prepared' THEN 'dispatching' ELSE phase END,
    dispatching_at = CASE WHEN dispatching_at = 0 THEN ? ELSE dispatching_at END
WHERE id = ? AND operation_id = ? AND phase IN ('prepared', 'dispatching', 'sent', 'streaming')`,
			stamp, attemptID, operationID,
		)
		if err != nil {
			return err
		}
		if n, _ := result.RowsAffected(); n != 1 {
			return fmt.Errorf("attempt %q is not dispatchable", attemptID)
		}
		_, err = tx.Exec(`UPDATE inference_operations SET updated_at = ? WHERE id = ? AND status = 'active'`, stamp, operationID)
		return err
	})
}

func (j *inferenceJournal) UpsertSubmission(record providers.InferenceSubmissionJournalRecord) error {
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
	return j.write("upsert inference submission", func(tx *sql.Tx) error {
		var operationID, attemptID string
		var ordinal, attemptOrdinal int
		err := tx.QueryRow(`
SELECT operation_id, attempt_id, ordinal, attempt_ordinal
FROM inference_submissions WHERE id = ?`, record.ID).
			Scan(&operationID, &attemptID, &ordinal, &attemptOrdinal)
		switch {
		case errors.Is(err, sql.ErrNoRows):
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
			_, err = tx.Exec(`
UPDATE inference_submissions SET
    outcome = ?, failure_category = ?, cost_state = ?,
    reported_input_tokens = ?, reported_output_tokens = ?, reported_cache_creation = ?,
    reported_cache_read = ?, reported_cache_unknown = ?, has_reported_usage = ?,
    estimated_input_tokens = ?, estimated_output_tokens = ?, estimated_cache_creation = ?,
    estimated_cache_read = ?, estimated_cache_unknown = ?, has_estimated_usage = ?,
    output_bytes = ?, completed_at = ?
WHERE id = ?`,
				string(record.Outcome), string(record.FailureCategory), string(record.CostState),
				reported.input, reported.output, reported.cacheCreation, reported.cacheRead, reported.cacheUnknown, reported.present,
				estimated.input, estimated.output, estimated.cacheCreation, estimated.cacheRead, estimated.cacheUnknown, estimated.present,
				record.OutputBytes, completedAt, record.ID,
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
	})
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
	stamp := journalTime(record.At)
	return j.write("complete inference attempt", func(tx *sql.Tx) error {
		return completeInferenceAttemptTx(tx, record.OperationID, record.AttemptID, record.Outcome, record.Failure, stamp)
	})
}

func (j *inferenceJournal) RecordRecovery(record providers.InferenceRecoveryJournalRecord) error {
	record.OperationID = strings.TrimSpace(record.OperationID)
	record.AttemptID = strings.TrimSpace(record.AttemptID)
	if record.OperationID == "" || record.AttemptID == "" || record.Action == "" {
		return errors.New("record inference recovery: ids and action are required")
	}
	stamp := journalTime(record.At)
	retryAt := optionalJournalTime(record.RetryAt)
	return j.write("record inference recovery", func(tx *sql.Tx) error {
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
	})
}

func (j *inferenceJournal) CompleteOperation(record providers.InferenceOperationTerminalRecord) error {
	record.OperationID = strings.TrimSpace(record.OperationID)
	if record.OperationID == "" || record.Outcome == "" {
		return errors.New("complete inference operation: id and outcome are required")
	}
	stamp := journalTime(record.At)
	return j.write("complete inference operation", func(tx *sql.Tx) error {
		return completeInferenceOperationTx(tx, record.OperationID, record.Outcome, "", record.Failure, stamp)
	})
}

func (j *inferenceJournal) write(action string, fn func(*sql.Tx) error) error {
	if j == nil || strings.TrimSpace(j.sessDir) == "" || strings.TrimSpace(j.runtimeID) == "" {
		return fmt.Errorf("%s: inference journal is not initialized", action)
	}
	db, err := openStore(j.sessDir)
	if err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	defer db.Close()
	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("%s: begin: %w", action, err)
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%s: commit: %w", action, err)
	}
	return nil
}

type InferenceCrashRecovery struct {
	OperationID string
	AttemptID   string
	Profile     providers.InferenceWorkloadProfile
	PriorPhase  string
	Outcome     providers.InferenceTerminalOutcome
	Action      providers.RecoveryActionKind
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

	rows, err := tx.Query(`
SELECT o.id, o.workload_profile,
       COALESCE(a.id, ''), COALESCE(a.phase, '')
FROM inference_operations o
LEFT JOIN inference_attempts a
  ON a.operation_id = o.id
 AND a.ordinal = (SELECT MAX(last.ordinal) FROM inference_attempts last WHERE last.operation_id = o.id)
WHERE o.workspace_scope = ? AND o.runtime_id <> ? AND o.status = 'active'
ORDER BY o.created_at, o.id`, r.workspaceScope, r.runtimeID)
	if err != nil {
		return nil, fmt.Errorf("reconcile inference journal: list: %w", err)
	}
	var recoveries []InferenceCrashRecovery
	for rows.Next() {
		var item InferenceCrashRecovery
		var profile string
		if err := rows.Scan(&item.OperationID, &profile, &item.AttemptID, &item.PriorPhase); err != nil {
			rows.Close()
			return nil, fmt.Errorf("reconcile inference journal: scan: %w", err)
		}
		item.Profile = providers.InferenceWorkloadProfile(profile)
		switch {
		case item.Profile == providers.InferenceProfileBestEffort:
			item.Outcome = providers.InferenceOutcomeAbandoned
			item.Action = providers.RecoveryStop
		case item.PriorPhase == "" || item.PriorPhase == "prepared":
			item.Outcome = providers.InferenceOutcomeInterrupted
			item.Action = providers.RecoveryRescheduleSafe
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
		if item.AttemptID != "" {
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
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("reconcile inference journal: commit: %w", err)
	}
	return recoveries, nil
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
	return result.RowsAffected()
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
    provider_code = ?, http_status = ?, confidence = ?
WHERE id = ? AND operation_id = ?`,
		string(outcome), stamp, string(failure.Origin), string(failure.Category),
		journalText(failure.ProviderFamily, 128), journalText(failure.ProviderCode, 128),
		failure.HTTPStatus, string(failure.Confidence), attemptID, operationID,
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
    provider_code = ?, http_status = ?, confidence = ?,
    updated_at = ?, terminal_at = ?
WHERE id = ?`,
		string(outcome), string(outcome), string(action),
		string(failure.Origin), string(failure.Category), journalText(failure.ProviderFamily, 128),
		journalText(failure.ProviderCode, 128), failure.HTTPStatus, string(failure.Confidence),
		stamp, stamp, operationID,
	)
	return err
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

func newInferenceRuntimeID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic(fmt.Sprintf("session: generate inference runtime id: %v", err))
	}
	return "irt-" + hex.EncodeToString(raw[:])
}
