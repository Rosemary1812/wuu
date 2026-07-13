package session

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
)

func budgetedInferenceOperationRecord(
	op providers.InferenceOperation,
	workflowID string,
	spec providers.WorkflowBudgetSpec,
	hash string,
	at time.Time,
) providers.InferenceOperationJournalRecord {
	op.WorkflowID = workflowID
	return providers.InferenceOperationJournalRecord{
		Operation: op,
		Workflow: providers.InferenceWorkflowJournalRecord{
			ID: workflowID, Profile: op.WorkloadProfile, Budget: spec, StartedAt: at,
		},
		RequestHash: hash,
		At:          at,
	}
}

func requireWorkflowBudgetDimension(t *testing.T, err error, dimension providers.WorkflowBudgetDimension) {
	t.Helper()
	var exceeded *providers.WorkflowBudgetExceededError
	if !errors.As(err, &exceeded) || exceeded.Dimension != dimension {
		t.Fatalf("budget error = %v, want dimension %q", err, dimension)
	}
}

func TestInferenceJournalPersistsMetadataOnlyLifecycle(t *testing.T) {
	dir := t.TempDir()
	runtime, err := NewInferenceJournalRuntime(dir, "workspace-test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	journal := runtime.ForOwner("thread-test")
	now := time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)
	op := providers.NewInferenceOperation(providers.InferenceOperationAgentRound, providers.InferenceProfileInteractive)
	hash := "5f3c4c7d31302a01d05b4d8726f34f743f0e97c65c91af35dc0704fdb8e9f35d"
	attemptID := op.AttemptID(1)
	submissionID := op.ID + "-s1"

	if err := journal.PrepareOperation(providers.InferenceOperationJournalRecord{Operation: op, RequestHash: hash, At: now}); err != nil {
		t.Fatal(err)
	}
	if err := journal.PrepareAttempt(providers.InferenceAttemptJournalRecord{
		OperationID: op.ID, AttemptID: attemptID, Ordinal: 1, RequestHash: hash, At: now.Add(time.Millisecond),
	}); err != nil {
		t.Fatal(err)
	}
	submission := providers.InferenceSubmissionJournalRecord{
		OperationID: op.ID, AttemptID: attemptID, ID: submissionID, Ordinal: 1, AttemptOrdinal: 1,
		Provider: "openai", Protocol: "responses", Transport: "http", Mode: "stream",
		StartedAt: now.Add(3 * time.Millisecond), Outcome: providers.InferenceSubmissionInFlight,
		CostState: providers.InferenceCostUnknownBillable,
	}
	if err := journal.UpsertSubmission(submission); err != nil {
		t.Fatal(err)
	}
	if err := journal.MarkAttemptFirstEvent(op.ID, attemptID, submissionID, now.Add(4*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	submission.Outcome = providers.InferenceSubmissionSucceeded
	submission.CostState = providers.InferenceCostKnown
	submission.ReportedUsage = &providers.TokenUsage{InputTokens: 12, OutputTokens: 4, CacheReadTokens: 3}
	submission.OutputBytes = 16
	submission.CompletedAt = now.Add(5 * time.Millisecond)
	if err := journal.UpsertSubmission(submission); err != nil {
		t.Fatal(err)
	}
	if err := journal.CompleteAttempt(providers.InferenceAttemptTerminalRecord{
		OperationID: op.ID, AttemptID: attemptID, Outcome: providers.InferenceOutcomeSucceeded, At: now.Add(6 * time.Millisecond),
	}); err != nil {
		t.Fatal(err)
	}
	if err := journal.CompleteOperation(providers.InferenceOperationTerminalRecord{
		OperationID: op.ID, Outcome: providers.InferenceOutcomeSucceeded, At: now.Add(7 * time.Millisecond),
	}); err != nil {
		t.Fatal(err)
	}

	db, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var owner, storedHash, status, terminal string
	if err := db.QueryRow(`
SELECT owner_id, request_hash, status, terminal_outcome
FROM inference_operations WHERE id = ?`, op.ID).Scan(&owner, &storedHash, &status, &terminal); err != nil {
		t.Fatal(err)
	}
	if owner != "thread-test" || storedHash != hash || status != "succeeded" || terminal != "succeeded" {
		t.Fatalf("operation = owner %q hash %q status %q terminal %q", owner, storedHash, status, terminal)
	}
	var phase string
	var dispatchingAt, sentAt, firstEventAt, terminalAt int64
	if err := db.QueryRow(`
SELECT phase, dispatching_at, sent_at, first_event_at, terminal_at
FROM inference_attempts WHERE id = ?`, attemptID).
		Scan(&phase, &dispatchingAt, &sentAt, &firstEventAt, &terminalAt); err != nil {
		t.Fatal(err)
	}
	if phase != "terminal" || dispatchingAt != 0 || sentAt == 0 || firstEventAt == 0 || terminalAt == 0 {
		t.Fatalf("attempt = phase %q times %d/%d/%d/%d", phase, dispatchingAt, sentAt, firstEventAt, terminalAt)
	}
	var outcome, costState string
	var input, output, cacheRead, hasUsage, outputBytes int
	if err := db.QueryRow(`
SELECT outcome, cost_state, reported_input_tokens, reported_output_tokens,
       reported_cache_read, has_reported_usage, output_bytes
FROM inference_submissions WHERE id = ?`, submissionID).
		Scan(&outcome, &costState, &input, &output, &cacheRead, &hasUsage, &outputBytes); err != nil {
		t.Fatal(err)
	}
	if outcome != "succeeded" || costState != "known" || input != 12 || output != 4 || cacheRead != 3 || hasUsage != 1 || outputBytes != 16 {
		t.Fatalf("submission = %q %q usage=%d/%d/%d present=%d bytes=%d", outcome, costState, input, output, cacheRead, hasUsage, outputBytes)
	}
	if info, err := os.Stat(DBPath(dir)); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("journal database permissions = %o, want no group/other access", info.Mode().Perm())
	}
}

func TestInferenceJournalCompletesWorkflowOnlyAfterOperationsAreTerminal(t *testing.T) {
	dir := t.TempDir()
	runtime, err := NewInferenceJournalRuntime(dir, "workspace-workflow-terminal")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	journal := runtime.ForOwner("thread-terminal")
	now := time.Now().UTC()
	workflowID := "iwf-terminal"
	op := providers.NewInferenceOperation(providers.InferenceOperationAgentRound, providers.InferenceProfileInteractive)
	if err := journal.PrepareOperation(budgetedInferenceOperationRecord(
		op, workflowID, providers.WorkflowBudgetSpec{}, testInferenceHash("terminal"), now,
	)); err != nil {
		t.Fatal(err)
	}
	record := providers.InferenceWorkflowTerminalRecord{
		WorkflowID: workflowID, Outcome: providers.InferenceOutcomeSucceeded, At: now.Add(time.Second),
	}
	if err := journal.CompleteWorkflow(record); err == nil {
		t.Fatal("workflow completed while an operation was active")
	}
	if err := journal.CompleteOperation(providers.InferenceOperationTerminalRecord{
		OperationID: op.ID, Outcome: providers.InferenceOutcomeSucceeded, At: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := journal.CompleteWorkflow(record); err != nil {
		t.Fatal(err)
	}
	if err := journal.CompleteWorkflow(record); err != nil {
		t.Fatalf("idempotent workflow completion: %v", err)
	}
	conflict := record
	conflict.Outcome = providers.InferenceOutcomeFailed
	if err := journal.CompleteWorkflow(conflict); err == nil {
		t.Fatal("workflow terminal outcome changed")
	}

	db, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var status string
	var terminalAt int64
	if err := db.QueryRow(`SELECT status, terminal_at FROM inference_workflows WHERE id = ?`, workflowID).Scan(&status, &terminalAt); err != nil {
		t.Fatal(err)
	}
	if status != string(providers.InferenceOutcomeSucceeded) || terminalAt == 0 {
		t.Fatalf("workflow terminal state = %q/%d", status, terminalAt)
	}
}

func TestInferenceJournalSettlesLateUsageAfterWorkflowCompletion(t *testing.T) {
	dir := t.TempDir()
	runtime, err := NewInferenceJournalRuntime(dir, "workspace-late-usage")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	journal := runtime.ForOwner("thread-late-usage")
	now := time.Now().UTC()
	workflowID := "iwf-late-usage"
	op := providers.NewInferenceOperation(providers.InferenceOperationAgentRound, providers.InferenceProfileInteractive)
	hash := testInferenceHash("late-usage")
	if err := journal.PrepareOperation(budgetedInferenceOperationRecord(op, workflowID, providers.WorkflowBudgetSpec{}, hash, now)); err != nil {
		t.Fatal(err)
	}
	attemptID := op.AttemptID(1)
	if err := journal.PrepareAttempt(providers.InferenceAttemptJournalRecord{
		OperationID: op.ID, WorkflowID: workflowID, AttemptID: attemptID, Ordinal: 1, RequestHash: hash, At: now,
	}); err != nil {
		t.Fatal(err)
	}
	submission := providers.InferenceSubmissionJournalRecord{
		OperationID: op.ID, AttemptID: attemptID, ID: op.ID + "-s1", Ordinal: 1, AttemptOrdinal: 1,
		Outcome: providers.InferenceSubmissionInFlight, CostState: providers.InferenceCostUnknownBillable, StartedAt: now,
	}
	if err := journal.UpsertSubmission(submission); err != nil {
		t.Fatal(err)
	}
	if err := journal.CompleteAttempt(providers.InferenceAttemptTerminalRecord{
		OperationID: op.ID, AttemptID: attemptID, Outcome: providers.InferenceOutcomeSucceeded, At: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := journal.CompleteOperation(providers.InferenceOperationTerminalRecord{
		OperationID: op.ID, Outcome: providers.InferenceOutcomeSucceeded, At: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := journal.CompleteWorkflow(providers.InferenceWorkflowTerminalRecord{
		WorkflowID: workflowID, Outcome: providers.InferenceOutcomeSucceeded, At: now,
	}); err != nil {
		t.Fatal(err)
	}
	submission.Outcome = providers.InferenceSubmissionSucceeded
	submission.CostState = providers.InferenceCostKnown
	submission.ReportedUsage = &providers.TokenUsage{InputTokens: 9, OutputTokens: 3}
	submission.CompletedAt = now.Add(time.Second)
	if err := journal.UpsertSubmission(submission); err != nil {
		t.Fatalf("late usage settlement: %v", err)
	}

	db, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var status string
	var known, unknown, tokens int
	if err := db.QueryRow(`
SELECT status, known_submissions, unknown_billable, known_usage_tokens
FROM inference_workflows WHERE id = ?`, workflowID).Scan(&status, &known, &unknown, &tokens); err != nil {
		t.Fatal(err)
	}
	if status != string(providers.InferenceOutcomeSucceeded) || known != 1 || unknown != 0 || tokens != 12 {
		t.Fatalf("late workflow settlement = status=%q known=%d unknown=%d tokens=%d", status, known, unknown, tokens)
	}
}

func TestInferenceJournalEnforcesSharedReplayAndSubmissionBudget(t *testing.T) {
	dir := t.TempDir()
	runtime, err := NewInferenceJournalRuntime(dir, "workspace-budget")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	journal := runtime.ForOwner("thread-budget")
	now := time.Now().UTC()
	workflowID := "iwf-shared-budget"
	spec := providers.WorkflowBudgetSpec{
		MaxSamePayloadReplays:         providers.LimitedBudget(1),
		MaxSubmissions:                providers.LimitedBudget(1),
		MaxUnknownBillableSubmissions: providers.LimitedBudget(1),
	}

	first := providers.NewInferenceOperation(providers.InferenceOperationAgentRound, providers.InferenceProfileInteractive)
	first.WorkflowID = workflowID
	firstHash := testInferenceHash("workflow-first")
	if err := journal.PrepareOperation(budgetedInferenceOperationRecord(first, workflowID, spec, firstHash, now)); err != nil {
		t.Fatal(err)
	}
	firstAttempt := first.AttemptID(1)
	if err := journal.PrepareAttempt(providers.InferenceAttemptJournalRecord{
		OperationID: first.ID, WorkflowID: workflowID, AttemptID: firstAttempt, Ordinal: 1, RequestHash: firstHash, At: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := journal.CompleteAttempt(providers.InferenceAttemptTerminalRecord{
		OperationID: first.ID, AttemptID: firstAttempt, Outcome: providers.InferenceOutcomeFailed,
		Failure: providers.InferenceJournalFailure{Category: providers.FailureNetwork}, At: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := journal.RecordRecovery(providers.InferenceRecoveryJournalRecord{
		OperationID: first.ID, AttemptID: firstAttempt, Action: providers.RecoveryReplaySame, At: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := journal.PrepareAttempt(providers.InferenceAttemptJournalRecord{
		OperationID: first.ID, WorkflowID: workflowID, AttemptID: first.AttemptID(2), Ordinal: 2, RequestHash: firstHash, At: now,
	}); err != nil {
		t.Fatal(err)
	}

	second := providers.NewInferenceOperation(providers.InferenceOperationAgentRound, providers.InferenceProfileInteractive)
	second.WorkflowID = workflowID
	secondHash := testInferenceHash("workflow-second")
	if err := journal.PrepareOperation(budgetedInferenceOperationRecord(second, workflowID, spec, secondHash, now)); err != nil {
		t.Fatal(err)
	}
	secondAttempt := second.AttemptID(1)
	if err := journal.PrepareAttempt(providers.InferenceAttemptJournalRecord{
		OperationID: second.ID, WorkflowID: workflowID, AttemptID: secondAttempt, Ordinal: 1, RequestHash: secondHash, At: now,
	}); err != nil {
		t.Fatal(err)
	}
	err = journal.PrepareAttempt(providers.InferenceAttemptJournalRecord{
		OperationID: second.ID, WorkflowID: workflowID, AttemptID: second.AttemptID(2), Ordinal: 2, RequestHash: secondHash, At: now,
	})
	requireWorkflowBudgetDimension(t, err, providers.WorkflowBudgetSamePayloadReplays)

	if err := journal.UpsertSubmission(providers.InferenceSubmissionJournalRecord{
		OperationID: first.ID, AttemptID: first.AttemptID(2), ID: first.ID + "-s1", Ordinal: 1, AttemptOrdinal: 2,
		Provider: "test", Transport: "http", Outcome: providers.InferenceSubmissionInFlight,
		CostState: providers.InferenceCostUnknownBillable, StartedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	err = journal.UpsertSubmission(providers.InferenceSubmissionJournalRecord{
		OperationID: second.ID, AttemptID: secondAttempt, ID: second.ID + "-s1", Ordinal: 1, AttemptOrdinal: 1,
		Provider: "test", Transport: "http", Outcome: providers.InferenceSubmissionInFlight,
		CostState: providers.InferenceCostUnknownBillable, StartedAt: now,
	})
	requireWorkflowBudgetDimension(t, err, providers.WorkflowBudgetSubmissions)

	db, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var operations, attempts, submissions, replays, unknown int
	if err := db.QueryRow(`
SELECT used_operations, used_attempts, used_submissions, used_replays, unknown_billable
FROM inference_workflows WHERE id = ?`, workflowID).
		Scan(&operations, &attempts, &submissions, &replays, &unknown); err != nil {
		t.Fatal(err)
	}
	if operations != 2 || attempts != 3 || submissions != 1 || replays != 1 || unknown != 1 {
		t.Fatalf("workflow counters = %d/%d/%d/%d/%d", operations, attempts, submissions, replays, unknown)
	}
	var secondPhase string
	if err := db.QueryRow(`SELECT phase FROM inference_attempts WHERE id = ?`, secondAttempt).Scan(&secondPhase); err != nil {
		t.Fatal(err)
	}
	if secondPhase != "prepared" {
		t.Fatalf("budget-denied submission changed attempt phase to %q", secondPhase)
	}
}

func TestInferenceJournalCostConfidenceSettlementIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	runtime, err := NewInferenceJournalRuntime(dir, "workspace-cost")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	journal := runtime.ForOwner("thread-cost")
	now := time.Now().UTC()
	workflowID := "iwf-cost"
	spec := providers.WorkflowBudgetSpec{
		MaxSubmissions:                providers.LimitedBudget(2),
		MaxUnknownBillableSubmissions: providers.LimitedBudget(1),
	}

	prepare := func(label string) (providers.InferenceOperation, string) {
		op := providers.NewInferenceOperation(providers.InferenceOperationAgentRound, providers.InferenceProfileInteractive)
		op.WorkflowID = workflowID
		hash := testInferenceHash(label)
		if err := journal.PrepareOperation(budgetedInferenceOperationRecord(op, workflowID, spec, hash, now)); err != nil {
			t.Fatal(err)
		}
		attemptID := op.AttemptID(1)
		if err := journal.PrepareAttempt(providers.InferenceAttemptJournalRecord{
			OperationID: op.ID, WorkflowID: workflowID, AttemptID: attemptID, Ordinal: 1, RequestHash: hash, At: now,
		}); err != nil {
			t.Fatal(err)
		}
		return op, attemptID
	}
	first, firstAttempt := prepare("cost-first")
	second, secondAttempt := prepare("cost-second")
	firstSubmission := providers.InferenceSubmissionJournalRecord{
		OperationID: first.ID, AttemptID: firstAttempt, ID: first.ID + "-s1", Ordinal: 1, AttemptOrdinal: 1,
		Outcome: providers.InferenceSubmissionInFlight, CostState: providers.InferenceCostUnknownBillable, StartedAt: now,
	}
	if err := journal.UpsertSubmission(firstSubmission); err != nil {
		t.Fatal(err)
	}
	secondSubmission := providers.InferenceSubmissionJournalRecord{
		OperationID: second.ID, AttemptID: secondAttempt, ID: second.ID + "-s1", Ordinal: 1, AttemptOrdinal: 1,
		Outcome: providers.InferenceSubmissionInFlight, CostState: providers.InferenceCostUnknownBillable, StartedAt: now,
	}
	err = journal.UpsertSubmission(secondSubmission)
	requireWorkflowBudgetDimension(t, err, providers.WorkflowBudgetUnknownBillable)

	firstSubmission.CostState = providers.InferenceCostKnown
	firstSubmission.ReportedUsage = &providers.TokenUsage{InputTokens: 10, OutputTokens: 4}
	if err := journal.UpsertSubmission(firstSubmission); err != nil {
		t.Fatal(err)
	}
	if err := journal.UpsertSubmission(firstSubmission); err != nil {
		t.Fatalf("repeat known settlement: %v", err)
	}
	if err := journal.UpsertSubmission(secondSubmission); err != nil {
		t.Fatalf("submission after known settlement: %v", err)
	}

	db, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var used, known, unknown, knownTokens int
	if err := db.QueryRow(`
SELECT used_submissions, known_submissions, unknown_billable, known_usage_tokens
FROM inference_workflows WHERE id = ?`, workflowID).Scan(&used, &known, &unknown, &knownTokens); err != nil {
		t.Fatal(err)
	}
	if used != 2 || known != 1 || unknown != 1 || knownTokens != 14 {
		t.Fatalf("cost counters = used=%d known=%d unknown=%d tokens=%d", used, known, unknown, knownTokens)
	}
}

func TestInferenceJournalConcurrentLastSubmissionAdmission(t *testing.T) {
	dir := t.TempDir()
	runtime, err := NewInferenceJournalRuntime(dir, "workspace-concurrent-budget")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	journal := runtime.ForOwner("thread-concurrent")
	now := time.Now().UTC()
	workflowID := "iwf-concurrent"
	spec := providers.WorkflowBudgetSpec{MaxSubmissions: providers.LimitedBudget(1)}
	type candidate struct {
		op        providers.InferenceOperation
		attemptID string
	}
	candidates := make([]candidate, 2)
	for i := range candidates {
		op := providers.NewInferenceOperation(providers.InferenceOperationAgentRound, providers.InferenceProfileInteractive)
		op.WorkflowID = workflowID
		hash := testInferenceHash(string(rune('a' + i)))
		if err := journal.PrepareOperation(budgetedInferenceOperationRecord(op, workflowID, spec, hash, now)); err != nil {
			t.Fatal(err)
		}
		attemptID := op.AttemptID(1)
		if err := journal.PrepareAttempt(providers.InferenceAttemptJournalRecord{
			OperationID: op.ID, WorkflowID: workflowID, AttemptID: attemptID, Ordinal: 1, RequestHash: hash, At: now,
		}); err != nil {
			t.Fatal(err)
		}
		candidates[i] = candidate{op: op, attemptID: attemptID}
	}

	var wg sync.WaitGroup
	results := make(chan error, len(candidates))
	for _, candidate := range candidates {
		candidate := candidate
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- journal.UpsertSubmission(providers.InferenceSubmissionJournalRecord{
				OperationID: candidate.op.ID, AttemptID: candidate.attemptID,
				ID: candidate.op.ID + "-s1", Ordinal: 1, AttemptOrdinal: 1,
				Outcome: providers.InferenceSubmissionInFlight, CostState: providers.InferenceCostUnknownBillable, StartedAt: now,
			})
		}()
	}
	wg.Wait()
	close(results)
	succeeded := 0
	for err := range results {
		if err == nil {
			succeeded++
			continue
		}
		requireWorkflowBudgetDimension(t, err, providers.WorkflowBudgetSubmissions)
	}
	if succeeded != 1 {
		t.Fatalf("successful submissions = %d, want 1", succeeded)
	}
}

func TestInferenceJournalCrashRecoveryMatrix(t *testing.T) {
	dir := t.TempDir()
	oldRuntime, err := NewInferenceJournalRuntime(dir, "workspace-recovery")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 13, 11, 0, 0, 0, time.UTC)

	type fixture struct {
		name       string
		profile    providers.InferenceWorkloadProfile
		phase      string
		wantAction providers.RecoveryActionKind
		wantResult providers.InferenceTerminalOutcome
	}
	fixtures := []fixture{
		{name: "operation-prepared", profile: providers.InferenceProfileInteractive, phase: "", wantAction: providers.RecoveryRescheduleSafe, wantResult: providers.InferenceOutcomeInterrupted},
		{name: "attempt-prepared", profile: providers.InferenceProfileContinuationCritical, phase: "prepared", wantAction: providers.RecoveryRescheduleSafe, wantResult: providers.InferenceOutcomeInterrupted},
		{name: "sent", profile: providers.InferenceProfileBackgroundAgent, phase: "sent", wantAction: providers.RecoveryBlockAmbiguous, wantResult: providers.InferenceOutcomeBlocked},
		{name: "streaming", profile: providers.InferenceProfileInteractive, phase: "streaming", wantAction: providers.RecoveryBlockAmbiguous, wantResult: providers.InferenceOutcomeBlocked},
		{name: "best-effort-sent", profile: providers.InferenceProfileBestEffort, phase: "sent", wantAction: providers.RecoveryStop, wantResult: providers.InferenceOutcomeAbandoned},
	}
	ops := make(map[string]providers.InferenceOperation, len(fixtures))
	for index, fixture := range fixtures {
		op := providers.NewInferenceOperation(providers.InferenceOperationAgentRound, fixture.profile)
		if fixture.profile == providers.InferenceProfileBestEffort {
			op.Kind = providers.InferenceOperationTitle
		}
		ops[fixture.name] = op
		journal := oldRuntime.ForOwner("thread-" + fixture.name)
		hash := testInferenceHash(fixture.name)
		if err := journal.PrepareOperation(providers.InferenceOperationJournalRecord{Operation: op, RequestHash: hash, At: now.Add(time.Duration(index) * time.Second)}); err != nil {
			t.Fatalf("%s prepare operation: %v", fixture.name, err)
		}
		if fixture.phase == "" {
			continue
		}
		attemptID := op.AttemptID(1)
		if err := journal.PrepareAttempt(providers.InferenceAttemptJournalRecord{OperationID: op.ID, AttemptID: attemptID, Ordinal: 1, RequestHash: hash, At: now}); err != nil {
			t.Fatalf("%s prepare attempt: %v", fixture.name, err)
		}
		if fixture.phase == "prepared" {
			continue
		}
		submissionID := op.ID + "-s1"
		if err := journal.UpsertSubmission(providers.InferenceSubmissionJournalRecord{
			OperationID: op.ID, AttemptID: attemptID, ID: submissionID, Ordinal: 1, AttemptOrdinal: 1,
			Provider: "test", Transport: "http", StartedAt: now.Add(2 * time.Millisecond),
			Outcome: providers.InferenceSubmissionInFlight, CostState: providers.InferenceCostUnknownBillable,
		}); err != nil {
			t.Fatalf("%s send: %v", fixture.name, err)
		}
		if fixture.phase == "streaming" {
			if err := journal.MarkAttemptFirstEvent(op.ID, attemptID, submissionID, now.Add(3*time.Millisecond)); err != nil {
				t.Fatalf("%s first event: %v", fixture.name, err)
			}
		}
	}

	otherRuntime, err := NewInferenceJournalRuntime(dir, "other-workspace")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = otherRuntime.Close() })
	otherOp := providers.NewInferenceOperation(providers.InferenceOperationAgentRound, providers.InferenceProfileInteractive)
	if err := otherRuntime.ForOwner("other").PrepareOperation(providers.InferenceOperationJournalRecord{Operation: otherOp, RequestHash: testInferenceHash("other"), At: now}); err != nil {
		t.Fatal(err)
	}
	crashInferenceRuntimeForTest(t, oldRuntime, now.Add(-time.Hour))

	newRuntime, err := NewInferenceJournalRuntime(dir, "workspace-recovery")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = newRuntime.Close() })
	recovered, err := newRuntime.ReconcileOrphans(now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(recovered) != len(fixtures) {
		t.Fatalf("recoveries = %d, want %d: %+v", len(recovered), len(fixtures), recovered)
	}
	byID := make(map[string]InferenceCrashRecovery, len(recovered))
	for _, item := range recovered {
		byID[item.OperationID] = item
	}
	for _, fixture := range fixtures {
		item := byID[ops[fixture.name].ID]
		if item.Action != fixture.wantAction || item.Outcome != fixture.wantResult || item.PriorPhase != fixture.phase {
			t.Errorf("%s recovery = %+v, want action=%q outcome=%q phase=%q", fixture.name, item, fixture.wantAction, fixture.wantResult, fixture.phase)
		}
	}

	db, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, fixture := range fixtures {
		var status, outcome, action string
		if err := db.QueryRow(`
SELECT status, terminal_outcome, recovery_action FROM inference_operations WHERE id = ?`, ops[fixture.name].ID).
			Scan(&status, &outcome, &action); err != nil {
			t.Fatal(err)
		}
		if status != string(fixture.wantResult) || outcome != string(fixture.wantResult) || action != string(fixture.wantAction) {
			t.Errorf("%s persisted = %q/%q/%q", fixture.name, status, outcome, action)
		}
		var workflowStatus string
		if err := db.QueryRow(`SELECT status FROM inference_workflows WHERE id = ?`, "iwf-"+ops[fixture.name].ID).Scan(&workflowStatus); err != nil {
			t.Fatal(err)
		}
		if workflowStatus != string(fixture.wantResult) {
			t.Errorf("%s workflow status = %q, want %q", fixture.name, workflowStatus, fixture.wantResult)
		}
		if fixture.phase == "sent" || fixture.phase == "streaming" {
			var submissionOutcome, costState string
			if err := db.QueryRow(`
SELECT outcome, cost_state FROM inference_submissions WHERE operation_id = ?`, ops[fixture.name].ID).
				Scan(&submissionOutcome, &costState); err != nil {
				t.Fatal(err)
			}
			wantSubmission := string(providers.InferenceSubmissionInterrupted)
			if fixture.profile == providers.InferenceProfileBestEffort {
				wantSubmission = string(providers.InferenceSubmissionAbandoned)
			}
			if submissionOutcome != wantSubmission || costState != string(providers.InferenceCostUnknownBillable) {
				t.Errorf("%s submission = %q/%q, want %q/unknown_but_billable", fixture.name, submissionOutcome, costState, wantSubmission)
			}
		}
	}
	var otherStatus string
	if err := db.QueryRow(`SELECT status FROM inference_operations WHERE id = ?`, otherOp.ID).Scan(&otherStatus); err != nil {
		t.Fatal(err)
	}
	if otherStatus != "active" {
		t.Fatalf("other workspace operation status = %q, want active", otherStatus)
	}
	again, err := newRuntime.ReconcileOrphans(now.Add(2 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Fatalf("second recovery should be idempotent, got %+v", again)
	}
}

func TestInferenceJournalReconcilesCrashBetweenOperationAndWorkflowCompletion(t *testing.T) {
	dir := t.TempDir()
	oldRuntime, err := NewInferenceJournalRuntime(dir, "workspace-workflow-crash-window")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	journal := oldRuntime.ForOwner("thread-crash-window")
	workflowID := "iwf-crash-window"
	op := providers.NewInferenceOperation(providers.InferenceOperationAgentRound, providers.InferenceProfileInteractive)
	if err := journal.PrepareOperation(budgetedInferenceOperationRecord(
		op, workflowID, providers.WorkflowBudgetSpec{}, testInferenceHash("crash-window"), now,
	)); err != nil {
		t.Fatal(err)
	}
	if err := journal.CompleteOperation(providers.InferenceOperationTerminalRecord{
		OperationID: op.ID, Outcome: providers.InferenceOutcomeSucceeded, At: now,
	}); err != nil {
		t.Fatal(err)
	}
	crashInferenceRuntimeForTest(t, oldRuntime, now.Add(-time.Hour))

	newRuntime, err := NewInferenceJournalRuntime(dir, "workspace-workflow-crash-window")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = newRuntime.Close() })
	recoveries, err := newRuntime.ReconcileOrphans(now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(recoveries) != 0 {
		t.Fatalf("operation was already terminal, got recoveries %+v", recoveries)
	}
	db, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var status string
	if err := db.QueryRow(`SELECT status FROM inference_workflows WHERE id = ?`, workflowID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != string(providers.InferenceOutcomeInterrupted) {
		t.Fatalf("workflow status = %q, want interrupted without outer completion evidence", status)
	}
}

func TestInferenceJournalReconcileDoesNotTreatRecoveredFailureAsWorkflowFailure(t *testing.T) {
	dir := t.TempDir()
	oldRuntime, err := NewInferenceJournalRuntime(dir, "workspace-recovered-failure-window")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	journal := oldRuntime.ForOwner("thread-recovered-failure")
	workflowID := "iwf-recovered-failure"
	prepareTerminal := func(label string, kind providers.InferenceOperationKind, parent string, outcome providers.InferenceTerminalOutcome) providers.InferenceOperation {
		op := providers.NewInferenceOperation(kind, providers.InferenceProfileInteractive)
		op.WorkflowID = workflowID
		op.ParentOperationID = parent
		if err := journal.PrepareOperation(budgetedInferenceOperationRecord(
			op, workflowID, providers.WorkflowBudgetSpec{}, testInferenceHash(label), now,
		)); err != nil {
			t.Fatal(err)
		}
		if err := journal.CompleteOperation(providers.InferenceOperationTerminalRecord{
			OperationID: op.ID, Outcome: outcome, At: now,
		}); err != nil {
			t.Fatal(err)
		}
		return op
	}
	overflow := prepareTerminal("overflow", providers.InferenceOperationAgentRound, "", providers.InferenceOutcomeFailed)
	compaction := prepareTerminal("compaction", providers.InferenceOperationCompaction, overflow.ID, providers.InferenceOutcomeSucceeded)
	prepareTerminal("resumed", providers.InferenceOperationAgentRound, compaction.ID, providers.InferenceOutcomeSucceeded)
	crashInferenceRuntimeForTest(t, oldRuntime, now.Add(-time.Hour))

	newRuntime, err := NewInferenceJournalRuntime(dir, "workspace-recovered-failure-window")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = newRuntime.Close() })
	if _, err := newRuntime.ReconcileOrphans(now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	db, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var status string
	if err := db.QueryRow(`SELECT status FROM inference_workflows WHERE id = ?`, workflowID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != string(providers.InferenceOutcomeInterrupted) {
		t.Fatalf("workflow status = %q, recovered historical failure must not force failed", status)
	}
}

func TestInferenceJournalRejectsMetadataMutation(t *testing.T) {
	runtime, err := NewInferenceJournalRuntime(t.TempDir(), "workspace-test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	journal := runtime.ForOwner("thread-test")
	op := providers.NewInferenceOperation(providers.InferenceOperationAgentRound, providers.InferenceProfileInteractive)
	first := providers.InferenceOperationJournalRecord{Operation: op, RequestHash: testInferenceHash("first")}
	if err := journal.PrepareOperation(first); err != nil {
		t.Fatal(err)
	}
	first.RequestHash = testInferenceHash("changed")
	if err := journal.PrepareOperation(first); err == nil {
		t.Fatal("expected changed request hash to be rejected")
	}
	otherOwner := runtime.ForOwner("other-thread")
	first.RequestHash = testInferenceHash("first")
	if err := otherOwner.PrepareOperation(first); err == nil {
		t.Fatal("expected changed owner to be rejected")
	}
}

func TestInferenceJournalDoesNotRecoverLiveRuntime(t *testing.T) {
	dir := t.TempDir()
	first, err := NewInferenceJournalRuntime(dir, "workspace-live")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	op := providers.NewInferenceOperation(providers.InferenceOperationAgentRound, providers.InferenceProfileInteractive)
	if err := first.ForOwner("thread").PrepareOperation(providers.InferenceOperationJournalRecord{
		Operation: op, RequestHash: testInferenceHash("live"), At: time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	second, err := NewInferenceJournalRuntime(dir, "workspace-live")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	recovered, err := second.ReconcileOrphans(time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(recovered) != 0 {
		t.Fatalf("live runtime was treated as crashed: %+v", recovered)
	}
	db, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var status string
	if err := db.QueryRow(`SELECT status FROM inference_operations WHERE id = ?`, op.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "active" {
		t.Fatalf("live operation status = %q, want active", status)
	}
}

func TestInferenceJournalRecoversTerminalRetryCheckpoint(t *testing.T) {
	dir := t.TempDir()
	oldRuntime, err := NewInferenceJournalRuntime(dir, "workspace-checkpoint")
	if err != nil {
		t.Fatal(err)
	}
	journal := oldRuntime.ForOwner("thread")
	now := time.Now().UTC()
	op := providers.NewInferenceOperation(providers.InferenceOperationAgentRound, providers.InferenceProfileInteractive)
	hash := testInferenceHash("checkpoint")
	attemptID := op.AttemptID(1)
	if err := journal.PrepareOperation(providers.InferenceOperationJournalRecord{Operation: op, RequestHash: hash, At: now}); err != nil {
		t.Fatal(err)
	}
	if err := journal.PrepareAttempt(providers.InferenceAttemptJournalRecord{OperationID: op.ID, AttemptID: attemptID, Ordinal: 1, RequestHash: hash, At: now}); err != nil {
		t.Fatal(err)
	}
	if err := journal.CompleteAttempt(providers.InferenceAttemptTerminalRecord{
		OperationID: op.ID, AttemptID: attemptID, Outcome: providers.InferenceOutcomeFailed,
		Failure: providers.InferenceJournalFailure{Category: providers.FailureNetwork}, At: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := journal.RecordRecovery(providers.InferenceRecoveryJournalRecord{
		OperationID: op.ID, AttemptID: attemptID, Action: providers.RecoveryReplaySame, At: now,
	}); err != nil {
		t.Fatal(err)
	}
	crashInferenceRuntimeForTest(t, oldRuntime, now.Add(-time.Hour))
	newRuntime, err := NewInferenceJournalRuntime(dir, "workspace-checkpoint")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = newRuntime.Close() })
	recovered, err := newRuntime.ReconcileOrphans(now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(recovered) != 1 || recovered[0].PriorPhase != "terminal" || recovered[0].PriorOutcome != "failed" ||
		recovered[0].PriorRecovery != providers.RecoveryReplaySame || recovered[0].Action != providers.RecoveryRescheduleSafe {
		t.Fatalf("checkpoint recovery = %+v", recovered)
	}
	db, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var attemptOutcome, operationStatus string
	if err := db.QueryRow(`SELECT terminal_outcome FROM inference_attempts WHERE id = ?`, attemptID).Scan(&attemptOutcome); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT status FROM inference_operations WHERE id = ?`, op.ID).Scan(&operationStatus); err != nil {
		t.Fatal(err)
	}
	if attemptOutcome != "failed" || operationStatus != "interrupted" {
		t.Fatalf("checkpoint persisted attempt=%q operation=%q", attemptOutcome, operationStatus)
	}
}

func TestInferenceJournalSubmissionEvidenceIsMonotonic(t *testing.T) {
	dir := t.TempDir()
	runtime, err := NewInferenceJournalRuntime(dir, "workspace-monotonic")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	journal := runtime.ForOwner("thread")
	now := time.Now().UTC()
	op := providers.NewInferenceOperation(providers.InferenceOperationAgentRound, providers.InferenceProfileInteractive)
	hash := testInferenceHash("monotonic")
	attemptID := op.AttemptID(1)
	submissionID := op.ID + "-s1"
	if err := journal.PrepareOperation(providers.InferenceOperationJournalRecord{Operation: op, RequestHash: hash, At: now}); err != nil {
		t.Fatal(err)
	}
	if err := journal.PrepareAttempt(providers.InferenceAttemptJournalRecord{OperationID: op.ID, AttemptID: attemptID, Ordinal: 1, RequestHash: hash, At: now}); err != nil {
		t.Fatal(err)
	}
	base := providers.InferenceSubmissionJournalRecord{
		OperationID: op.ID, AttemptID: attemptID, ID: submissionID, Ordinal: 1, AttemptOrdinal: 1,
		StartedAt: now, Outcome: providers.InferenceSubmissionSucceeded, CostState: providers.InferenceCostKnown,
		ReportedUsage: &providers.TokenUsage{InputTokens: 20, OutputTokens: 8}, OutputBytes: 32, CompletedAt: now.Add(time.Second),
	}
	if err := journal.UpsertSubmission(base); err != nil {
		t.Fatal(err)
	}
	stale := base
	stale.Outcome = providers.InferenceSubmissionInFlight
	stale.CostState = providers.InferenceCostUnknownBillable
	stale.ReportedUsage = nil
	stale.OutputBytes = 4
	stale.CompletedAt = time.Time{}
	if err := journal.UpsertSubmission(stale); err != nil {
		t.Fatal(err)
	}
	db, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var outcome, cost string
	var input, output, outputBytes int
	var completedAt int64
	if err := db.QueryRow(`
SELECT outcome, cost_state, reported_input_tokens, reported_output_tokens,
       output_bytes, completed_at
FROM inference_submissions WHERE id = ?`, submissionID).
		Scan(&outcome, &cost, &input, &output, &outputBytes, &completedAt); err != nil {
		t.Fatal(err)
	}
	if outcome != "succeeded" || cost != "known" || input != 20 || output != 8 || outputBytes != 32 || completedAt == 0 {
		t.Fatalf("monotonic evidence regressed: %q %q usage=%d/%d bytes=%d completed=%d", outcome, cost, input, output, outputBytes, completedAt)
	}
}

func TestInferenceJournalEnforcesOwnerAndDigestBoundaries(t *testing.T) {
	dir := t.TempDir()
	runtime, err := NewInferenceJournalRuntime(dir, "workspace-owner")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	owner := runtime.ForOwner("thread-a")
	op := providers.NewInferenceOperation(providers.InferenceOperationAgentRound, providers.InferenceProfileInteractive)
	hash := testInferenceHash("owner")
	if err := owner.PrepareOperation(providers.InferenceOperationJournalRecord{Operation: op, RequestHash: hash}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.ForOwner("thread-b").CompleteOperation(providers.InferenceOperationTerminalRecord{
		OperationID: op.ID, Outcome: providers.InferenceOutcomeSucceeded,
	}); err == nil {
		t.Fatal("different owner terminalized operation")
	}
	otherRuntime, err := NewInferenceJournalRuntime(dir, "workspace-owner")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = otherRuntime.Close() })
	if err := otherRuntime.ForOwner("thread-a").CompleteOperation(providers.InferenceOperationTerminalRecord{
		OperationID: op.ID, Outcome: providers.InferenceOutcomeSucceeded,
	}); err == nil {
		t.Fatal("different runtime terminalized operation")
	}
	raw := providers.NewInferenceOperation(providers.InferenceOperationAgentRound, providers.InferenceProfileInteractive)
	if err := owner.PrepareOperation(providers.InferenceOperationJournalRecord{
		Operation: raw, RequestHash: "this is raw prompt text",
	}); err == nil {
		t.Fatal("non-digest request identity was persisted")
	}
}

func TestInferenceJournalPrunesOnlyOldTerminalOperations(t *testing.T) {
	dir := t.TempDir()
	runtime, err := NewInferenceJournalRuntime(dir, "workspace-prune")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	journal := runtime.ForOwner("thread")
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	var ids []string
	for _, age := range []time.Duration{31 * 24 * time.Hour, 29 * 24 * time.Hour} {
		op := providers.NewInferenceOperation(providers.InferenceOperationTitle, providers.InferenceProfileBestEffort)
		ids = append(ids, op.ID)
		at := now.Add(-age)
		if err := journal.PrepareOperation(providers.InferenceOperationJournalRecord{Operation: op, RequestHash: testInferenceHash("hash"), At: at}); err != nil {
			t.Fatal(err)
		}
		if err := journal.CompleteOperation(providers.InferenceOperationTerminalRecord{OperationID: op.ID, Outcome: providers.InferenceOutcomeAbandoned, At: at}); err != nil {
			t.Fatal(err)
		}
		if err := journal.CompleteWorkflow(providers.InferenceWorkflowTerminalRecord{
			WorkflowID: "iwf-" + op.ID, Outcome: providers.InferenceOutcomeAbandoned, At: at,
		}); err != nil {
			t.Fatal(err)
		}
	}
	active := providers.NewInferenceOperation(providers.InferenceOperationAgentRound, providers.InferenceProfileInteractive)
	if err := journal.PrepareOperation(providers.InferenceOperationJournalRecord{Operation: active, RequestHash: testInferenceHash("hash"), At: now.Add(-40 * 24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}

	deleted, err := runtime.Prune(now)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 2 {
		t.Fatalf("deleted = %d, want old operation and workflow", deleted)
	}
	db, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT id FROM inference_operations ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var remaining []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		remaining = append(remaining, id)
	}
	sort.Strings(remaining)
	want := []string{ids[1], active.ID}
	sort.Strings(want)
	if len(remaining) != len(want) || remaining[0] != want[0] || remaining[1] != want[1] {
		t.Fatalf("remaining = %v, want %v", remaining, want)
	}
}

func testInferenceHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func crashInferenceRuntimeForTest(t *testing.T, runtime *InferenceJournalRuntime, heartbeat time.Time) {
	t.Helper()
	runtime.closeOnce.Do(func() {
		close(runtime.heartbeatStop)
		<-runtime.heartbeatDone
	})
	db, err := openStore(runtime.sessDir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()
	if _, err := db.Exec(`
UPDATE inference_journal_runtimes
SET pid = ?, heartbeat_at = ?, closed_at = 0
WHERE id = ?`, 99999999, heartbeat.UTC().UnixMilli(), runtime.runtimeID); err != nil {
		t.Fatal(err)
	}
}
