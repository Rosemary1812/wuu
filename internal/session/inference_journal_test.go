package session

import (
	"os"
	"sort"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
)

func TestInferenceJournalPersistsMetadataOnlyLifecycle(t *testing.T) {
	dir := t.TempDir()
	runtime, err := NewInferenceJournalRuntime(dir, "workspace-test")
	if err != nil {
		t.Fatal(err)
	}
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
	if err := journal.MarkAttemptDispatching(op.ID, attemptID, now.Add(2*time.Millisecond)); err != nil {
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
	if phase != "terminal" || dispatchingAt == 0 || sentAt == 0 || firstEventAt == 0 || terminalAt == 0 {
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
		{name: "dispatching", profile: providers.InferenceProfileInteractive, phase: "dispatching", wantAction: providers.RecoveryBlockAmbiguous, wantResult: providers.InferenceOutcomeBlocked},
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
		hash := "hash-" + fixture.name
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
		if err := journal.MarkAttemptDispatching(op.ID, attemptID, now.Add(time.Millisecond)); err != nil {
			t.Fatalf("%s dispatch: %v", fixture.name, err)
		}
		if fixture.phase == "dispatching" {
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
	otherOp := providers.NewInferenceOperation(providers.InferenceOperationAgentRound, providers.InferenceProfileInteractive)
	if err := otherRuntime.ForOwner("other").PrepareOperation(providers.InferenceOperationJournalRecord{Operation: otherOp, RequestHash: "other-hash", At: now}); err != nil {
		t.Fatal(err)
	}

	newRuntime, err := NewInferenceJournalRuntime(dir, "workspace-recovery")
	if err != nil {
		t.Fatal(err)
	}
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

func TestInferenceJournalRejectsMetadataMutation(t *testing.T) {
	runtime, err := NewInferenceJournalRuntime(t.TempDir(), "workspace-test")
	if err != nil {
		t.Fatal(err)
	}
	journal := runtime.ForOwner("thread-test")
	op := providers.NewInferenceOperation(providers.InferenceOperationAgentRound, providers.InferenceProfileInteractive)
	first := providers.InferenceOperationJournalRecord{Operation: op, RequestHash: "first"}
	if err := journal.PrepareOperation(first); err != nil {
		t.Fatal(err)
	}
	first.RequestHash = "changed"
	if err := journal.PrepareOperation(first); err == nil {
		t.Fatal("expected changed request hash to be rejected")
	}
	otherOwner := runtime.ForOwner("other-thread")
	first.RequestHash = "first"
	if err := otherOwner.PrepareOperation(first); err == nil {
		t.Fatal("expected changed owner to be rejected")
	}
}

func TestInferenceJournalPrunesOnlyOldTerminalOperations(t *testing.T) {
	dir := t.TempDir()
	runtime, err := NewInferenceJournalRuntime(dir, "workspace-prune")
	if err != nil {
		t.Fatal(err)
	}
	journal := runtime.ForOwner("thread")
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	var ids []string
	for _, age := range []time.Duration{31 * 24 * time.Hour, 29 * 24 * time.Hour} {
		op := providers.NewInferenceOperation(providers.InferenceOperationTitle, providers.InferenceProfileBestEffort)
		ids = append(ids, op.ID)
		at := now.Add(-age)
		if err := journal.PrepareOperation(providers.InferenceOperationJournalRecord{Operation: op, RequestHash: "hash", At: at}); err != nil {
			t.Fatal(err)
		}
		if err := journal.CompleteOperation(providers.InferenceOperationTerminalRecord{OperationID: op.ID, Outcome: providers.InferenceOutcomeAbandoned, At: at}); err != nil {
			t.Fatal(err)
		}
	}
	active := providers.NewInferenceOperation(providers.InferenceOperationAgentRound, providers.InferenceProfileInteractive)
	if err := journal.PrepareOperation(providers.InferenceOperationJournalRecord{Operation: active, RequestHash: "hash", At: now.Add(-40 * 24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}

	deleted, err := runtime.Prune(now)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
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
