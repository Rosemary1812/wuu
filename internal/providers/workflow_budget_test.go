package providers

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func testInferenceWorkflow(spec WorkflowBudgetSpec) *InferenceWorkflow {
	return newInferenceWorkflowWithIdentity("iwf-test", InferenceProfileInteractive, spec, time.Unix(1, 0))
}

func TestWorkflowBudgetSharesReplayAllowanceAcrossOperations(t *testing.T) {
	workflow := testInferenceWorkflow(WorkflowBudgetSpec{
		MaxSamePayloadReplays: LimitedBudget(1),
	})
	ctx := WithInferenceWorkflow(context.Background(), workflow)

	first, err := EnsureInferenceExecutionContext(ctx, ChatRequest{
		Operation: NewInferenceOperation(InferenceOperationAgentRound, InferenceProfileInteractive),
	}, InferenceOperationAgentRound, InferenceProfileInteractive)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Execution.BeginAttemptChecked(); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Execution.BeginAttemptChecked(); err != nil {
		t.Fatal(err)
	}

	second, err := EnsureInferenceExecutionContext(ctx, ChatRequest{
		Operation: NewInferenceOperation(InferenceOperationAgentRound, InferenceProfileInteractive),
	}, InferenceOperationAgentRound, InferenceProfileInteractive)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.Execution.BeginAttemptChecked(); err != nil {
		t.Fatal(err)
	}
	_, err = second.Execution.BeginAttemptChecked()
	var exceeded *WorkflowBudgetExceededError
	if !errors.As(err, &exceeded) || exceeded.Dimension != WorkflowBudgetSamePayloadReplays {
		t.Fatalf("second operation replay error = %v", err)
	}

	snapshot := workflow.SpendSnapshot()
	if snapshot.Operations != 2 || snapshot.Attempts != 3 || snapshot.SamePayloadReplays != 1 {
		t.Fatalf("workflow snapshot = %+v", snapshot)
	}
}

func TestInferenceOperationLineageChainsSequentialChildren(t *testing.T) {
	workflow := testInferenceWorkflow(WorkflowBudgetSpec{})
	ctx := WithInferenceWorkflow(context.Background(), workflow)
	root, err := EnsureInferenceExecutionContext(ctx, ChatRequest{
		Operation: NewInferenceOperation(InferenceOperationAgentRound, InferenceProfileInteractive),
	}, InferenceOperationAgentRound, InferenceProfileInteractive)
	if err != nil {
		t.Fatal(err)
	}
	lineageCtx, lineage := BeginInferenceOperationLineage(ctx, root.Operation.ID)
	first, err := EnsureInferenceExecutionContext(lineageCtx, ChatRequest{
		Operation: NewInferenceOperation(InferenceOperationCompaction, InferenceProfileContinuationCritical),
	}, InferenceOperationCompaction, InferenceProfileContinuationCritical)
	if err != nil {
		t.Fatal(err)
	}
	second, err := EnsureInferenceExecutionContext(lineageCtx, ChatRequest{
		Operation: NewInferenceOperation(InferenceOperationCompaction, InferenceProfileContinuationCritical),
	}, InferenceOperationCompaction, InferenceProfileContinuationCritical)
	if err != nil {
		t.Fatal(err)
	}

	if first.Operation.ParentOperationID != root.Operation.ID {
		t.Fatalf("first parent = %q, want %q", first.Operation.ParentOperationID, root.Operation.ID)
	}
	if second.Operation.ParentOperationID != first.Operation.ID {
		t.Fatalf("second parent = %q, want %q", second.Operation.ParentOperationID, first.Operation.ID)
	}
	if lineage.LastOperationID() != second.Operation.ID {
		t.Fatalf("lineage tail = %q, want %q", lineage.LastOperationID(), second.Operation.ID)
	}
}

func TestWorkflowBudgetUnknownBillableIsNeverZeroCost(t *testing.T) {
	workflow := testInferenceWorkflow(WorkflowBudgetSpec{
		MaxUnknownBillableSubmissions: LimitedBudget(1),
	})
	ctx := WithInferenceWorkflow(context.Background(), workflow)
	newExecution := func() *InferenceExecution {
		req, err := EnsureInferenceExecutionContext(ctx, ChatRequest{
			Operation: NewInferenceOperation(InferenceOperationAgentRound, InferenceProfileInteractive),
		}, InferenceOperationAgentRound, InferenceProfileInteractive)
		if err != nil {
			t.Fatal(err)
		}
		return req.Execution
	}

	firstAttempt := newExecution().BeginAttempt()
	first, err := firstAttempt.RecordSubmission(InferenceSubmissionMeta{Provider: "test"})
	if err != nil {
		t.Fatal(err)
	}
	secondAttempt := newExecution().BeginAttempt()
	if _, err := secondAttempt.RecordSubmission(InferenceSubmissionMeta{Provider: "test"}); err == nil {
		t.Fatal("second unknown-billable submission was admitted")
	} else {
		var exceeded *WorkflowBudgetExceededError
		if !errors.As(err, &exceeded) || exceeded.Dimension != WorkflowBudgetUnknownBillable {
			t.Fatalf("submission error = %v", err)
		}
	}

	first.ObserveUsage(&TokenUsage{InputTokens: 10, OutputTokens: 4})
	second, err := secondAttempt.RecordSubmission(InferenceSubmissionMeta{Provider: "test"})
	if err != nil {
		t.Fatalf("submission after known settlement: %v", err)
	}
	second.ObserveEstimatedUsage(&TokenUsage{InputTokens: 8, OutputTokens: 2})
	second.ObserveEstimatedUsage(&TokenUsage{InputTokens: 8, OutputTokens: 2})

	snapshot := workflow.SpendSnapshot()
	if snapshot.Submissions != 2 || snapshot.KnownSubmissions != 1 || snapshot.EstimatedSubmissions != 1 || snapshot.UnknownBillableSubmissions != 0 {
		t.Fatalf("cost confidence snapshot = %+v", snapshot)
	}
	if snapshot.KnownUsage.InputTokens != 10 || snapshot.KnownUsage.OutputTokens != 4 || snapshot.EstimatedUsage.InputTokens != 8 || snapshot.EstimatedUsage.OutputTokens != 2 {
		t.Fatalf("usage snapshot = %+v", snapshot)
	}
}

func TestWorkflowBudgetRecoveryReservationsAreTypedAndIdempotent(t *testing.T) {
	workflow := testInferenceWorkflow(WorkflowBudgetSpec{
		MaxCredentialRefreshes: LimitedBudget(1),
		MaxTransportSwitches:   LimitedBudget(0),
		MaxRecoveryWaitMillis:  LimitedBudget(0),
	})
	budget := workflow.spend
	operation := NewInferenceOperation(InferenceOperationAgentRound, InferenceProfileInteractive)
	operation.WorkflowID = workflow.ID
	execution, err := newInferenceExecution(operation, workflow)
	if err != nil {
		t.Fatal(err)
	}
	firstAttempt := execution.BeginAttempt()
	refresh := RecoveryPlan{Action: RecoveryRefreshAuth}
	if err := budget.AdmitRecoveryAttempt(operation, firstAttempt.ID, operation.AttemptID(2), 2, refresh, time.Time{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := budget.AdmitRecoveryAttempt(operation, firstAttempt.ID, operation.AttemptID(2), 2, refresh, time.Time{}, nil); err != nil {
		t.Fatalf("idempotent recovery reservation: %v", err)
	}
	if err := budget.AdmitRecoveryAttempt(operation, operation.AttemptID(2), operation.AttemptID(3), 3, refresh, time.Time{}, nil); err == nil {
		t.Fatal("second credential refresh was admitted")
	}
	if err := budget.AdmitRecoveryAttempt(operation, operation.AttemptID(2), operation.AttemptID(3), 3, RecoveryPlan{Action: RecoverySwitchTransport}, time.Time{}, nil); err == nil {
		t.Fatal("transport switch with a hard zero limit was admitted")
	}
	if err := budget.AdmitRecoveryAttempt(operation, operation.AttemptID(2), operation.AttemptID(3), 3, RecoveryPlan{Action: RecoveryReplaySame}, time.Now().Add(time.Second), nil); err == nil {
		t.Fatal("recovery wait with a hard zero limit was admitted")
	}
	if got := workflow.SpendSnapshot().CredentialRefreshes; got != 1 {
		t.Fatalf("credential refresh reservations = %d", got)
	}
}

func TestWorkflowBudgetHardUsageLimitFailsIndeterminate(t *testing.T) {
	workflow := testInferenceWorkflow(WorkflowBudgetSpec{
		MaxUsageTokens: LimitedBudget(100),
	})
	operation := NewInferenceOperation(InferenceOperationAgentRound, InferenceProfileInteractive)
	operation.WorkflowID = workflow.ID
	execution, err := newInferenceExecution(operation, workflow)
	if err != nil {
		t.Fatal(err)
	}
	attempt := execution.BeginAttempt()
	if _, err := attempt.RecordSubmission(InferenceSubmissionMeta{Provider: "test"}); err != nil {
		t.Fatal(err)
	}

	second := NewInferenceOperation(InferenceOperationCompaction, InferenceProfileContinuationCritical)
	second.WorkflowID = workflow.ID
	_, err = newInferenceExecution(second, workflow)
	var indeterminate *WorkflowCostIndeterminateError
	if !errors.As(err, &indeterminate) || indeterminate.UnknownBillableSubmissions != 1 {
		t.Fatalf("new operation error = %v", err)
	}
	if failure := NormalizeFailure(err); failure.Category != FailureCostIndeterminate || failure.Origin != FailureOriginLocal {
		t.Fatalf("normalized failure = %+v", failure)
	}
}

func TestWorkflowBudgetConcurrentFinalSubmissionAdmission(t *testing.T) {
	workflow := testInferenceWorkflow(WorkflowBudgetSpec{MaxSubmissions: LimitedBudget(1)})
	const contenders = 16
	var wg sync.WaitGroup
	wg.Add(contenders)
	results := make(chan error, contenders)
	for i := 0; i < contenders; i++ {
		go func(i int) {
			defer wg.Done()
			results <- workflow.spend.AdmitSubmission("submission-" + string(rune('a'+i)))
		}(i)
	}
	wg.Wait()
	close(results)
	succeeded := 0
	for err := range results {
		if err == nil {
			succeeded++
		}
	}
	if succeeded != 1 || workflow.SpendSnapshot().Submissions != 1 {
		t.Fatalf("successful admissions = %d, snapshot = %+v", succeeded, workflow.SpendSnapshot())
	}
}

func TestWorkflowBudgetValidatesParentWithinSameWorkflow(t *testing.T) {
	workflow := testInferenceWorkflow(WorkflowBudgetSpec{MaxChildOperations: LimitedBudget(1)})
	parent := NewInferenceOperation(InferenceOperationAgentRound, InferenceProfileInteractive)
	parent.WorkflowID = workflow.ID
	if _, err := newInferenceExecution(parent, workflow); err != nil {
		t.Fatal(err)
	}
	child := NewInferenceOperation(InferenceOperationCompaction, InferenceProfileContinuationCritical)
	child.WorkflowID = workflow.ID
	child.ParentOperationID = parent.ID
	if _, err := newInferenceExecution(child, workflow); err != nil {
		t.Fatal(err)
	}
	missingParent := NewInferenceOperation(InferenceOperationCompaction, InferenceProfileContinuationCritical)
	missingParent.WorkflowID = workflow.ID
	missingParent.ParentOperationID = "iop-missing"
	if _, err := newInferenceExecution(missingParent, workflow); err == nil {
		t.Fatal("child with an unknown parent was admitted")
	}
	if got := workflow.SpendSnapshot().ChildOperations; got != 1 {
		t.Fatalf("child operations = %d", got)
	}
}

func TestWorkflowBudgetReplayAllowanceResetsOnOperationSuccess(t *testing.T) {
	workflow := testInferenceWorkflow(WorkflowBudgetSpec{
		MaxSamePayloadReplays: LimitedBudget(1),
	})
	ctx := WithInferenceWorkflow(context.Background(), workflow)

	// Operation 1: one transient retry (spends the whole replay allowance),
	// then completes successfully — forward progress.
	first, err := EnsureInferenceExecutionContext(ctx, ChatRequest{
		Operation: NewInferenceOperation(InferenceOperationAgentRound, InferenceProfileInteractive),
	}, InferenceOperationAgentRound, InferenceProfileInteractive)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Execution.BeginAttemptChecked(); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Execution.BeginAttemptChecked(); err != nil {
		t.Fatal(err)
	}
	if err := first.Execution.Complete(InferenceOutcomeSucceeded, NormalizedFailure{}); err != nil {
		t.Fatal(err)
	}

	// Operation 2 in the same workflow: a later transient retry must be
	// admitted again — scattered recoveries with progress in between are not
	// a retry storm.
	second, err := EnsureInferenceExecutionContext(ctx, ChatRequest{
		Operation: NewInferenceOperation(InferenceOperationAgentRound, InferenceProfileInteractive),
	}, InferenceOperationAgentRound, InferenceProfileInteractive)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.Execution.BeginAttemptChecked(); err != nil {
		t.Fatal(err)
	}
	if _, err := second.Execution.BeginAttemptChecked(); err != nil {
		t.Fatalf("replay after successful progress must be admitted, got %v", err)
	}

	// Without success in between, the storm gate still holds: a third
	// attempt of the second operation exceeds the (already respent) budget.
	_, err = second.Execution.BeginAttemptChecked()
	var exceeded *WorkflowBudgetExceededError
	if !errors.As(err, &exceeded) || exceeded.Dimension != WorkflowBudgetSamePayloadReplays {
		t.Fatalf("consecutive replays without progress must still trip the budget, got %v", err)
	}
}

func TestWorkflowBudgetReplayAllowanceResetsOnAttemptSuccess(t *testing.T) {
	workflow := testInferenceWorkflow(WorkflowBudgetSpec{
		MaxSamePayloadReplays: LimitedBudget(1),
	})
	ctx := WithInferenceWorkflow(context.Background(), workflow)

	op, err := EnsureInferenceExecutionContext(ctx, ChatRequest{
		Operation: NewInferenceOperation(InferenceOperationAgentRound, InferenceProfileInteractive),
	}, InferenceOperationAgentRound, InferenceProfileInteractive)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := op.Execution.BeginAttemptChecked(); err != nil {
		t.Fatal(err)
	}
	retry, err := op.Execution.BeginAttemptChecked()
	if err != nil {
		t.Fatal(err)
	}
	// The retried attempt eventually succeeds — the streaming path completes
	// the ATTEMPT, not the operation. That alone must reset the replay spend.
	if err := retry.Complete(InferenceOutcomeSucceeded, NormalizedFailure{}); err != nil {
		t.Fatal(err)
	}

	next, err := EnsureInferenceExecutionContext(ctx, ChatRequest{
		Operation: NewInferenceOperation(InferenceOperationAgentRound, InferenceProfileInteractive),
	}, InferenceOperationAgentRound, InferenceProfileInteractive)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := next.Execution.BeginAttemptChecked(); err != nil {
		t.Fatal(err)
	}
	if _, err := next.Execution.BeginAttemptChecked(); err != nil {
		t.Fatalf("replay after attempt-level success must be admitted, got %v", err)
	}
}
