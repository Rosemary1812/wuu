package providers

import (
	"context"
	"errors"
	"testing"
)

func TestInferenceExecutionTracksAttemptsAndSubmissions(t *testing.T) {
	op := NewInferenceOperation(InferenceOperationAgentRound, InferenceProfileInteractive)
	execution := NewInferenceExecution(op)
	first := execution.BeginAttempt()
	firstSubmission, err := first.RecordSubmission(InferenceSubmissionMeta{
		Provider: "openai", Protocol: "responses", Transport: "websocket", Mode: "stream",
	})
	if err != nil {
		t.Fatal(err)
	}
	second := execution.BeginAttempt()
	secondSubmission, err := second.RecordSubmission(InferenceSubmissionMeta{
		Provider: "openai", Protocol: "responses", Transport: "http", Mode: "fallback", Reason: "websocket_failed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || first.Ordinal != 1 || second.Ordinal != 2 {
		t.Fatalf("attempts = %+v / %+v", first, second)
	}
	if firstSubmission.ID == secondSubmission.ID || firstSubmission.AttemptID != first.ID || secondSubmission.Ordinal != 2 {
		t.Fatalf("submissions = %+v / %+v", firstSubmission, secondSubmission)
	}
	snapshot := execution.Snapshot()
	if snapshot.Operation.ID != op.ID || snapshot.Attempts != 2 || len(snapshot.Submissions) != 2 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if got := second.SubmissionSnapshot(); len(got) != 1 || got[0].Transport != "http" {
		t.Fatalf("second attempt submissions = %+v", got)
	}
	if _, err := first.RecordSubmission(InferenceSubmissionMeta{Transport: "http"}); err == nil {
		t.Fatal("second submission on one attempt was accepted")
	}
}

func TestEnsureInferenceAttemptPreservesOuterAttempt(t *testing.T) {
	req, err := BeginInferenceAttemptContext(context.Background(), ChatRequest{}, InferenceOperationTitle, InferenceProfileBestEffort)
	if err != nil {
		t.Fatal(err)
	}
	got, err := EnsureInferenceAttemptContext(context.Background(), req, InferenceOperationAuxiliary, InferenceProfileInteractive)
	if err != nil {
		t.Fatal(err)
	}
	if got.Attempt.ID != req.Attempt.ID || got.Execution != req.Execution || got.Operation.Kind != InferenceOperationTitle {
		t.Fatalf("attempt changed across provider boundary: %+v -> %+v", req.Attempt, got.Attempt)
	}
}

func TestInferenceAttemptExposesOperationProfile(t *testing.T) {
	op := NewInferenceOperation(InferenceOperationTitle, InferenceProfileBestEffort)
	attempt := NewInferenceExecution(op).BeginAttempt()
	if got := attempt.Operation(); got.ID != op.ID || got.WorkloadProfile != InferenceProfileBestEffort {
		t.Fatalf("operation = %+v", got)
	}
	if got := (InferenceAttempt{}).Operation(); got != (InferenceOperation{}) {
		t.Fatalf("invalid attempt operation = %+v", got)
	}
}

func TestInferenceSubmissionTracksCostConfidenceAndOutcome(t *testing.T) {
	execution := NewInferenceExecution(NewInferenceOperation(InferenceOperationAgentRound, InferenceProfileInteractive))
	attempt := execution.BeginAttempt()
	submission, err := attempt.RecordSubmission(InferenceSubmissionMeta{Provider: "openai", Transport: "http"})
	if err != nil {
		t.Fatal(err)
	}
	initial := execution.Snapshot().Submissions[0]
	if initial.Outcome != InferenceSubmissionInFlight || initial.CostState != InferenceCostUnknownBillable {
		t.Fatalf("initial submission = %+v", initial)
	}

	submission.ObserveOutput("partial output")
	submission.CompleteFailure(NormalizedFailure{Category: FailureIncompleteStream})
	failed := execution.Snapshot().Submissions[0]
	if failed.Outcome != InferenceSubmissionFailed || failed.FailureCategory != FailureIncompleteStream || failed.CostState != InferenceCostUnknownBillable || failed.EstimatedUsage == nil || failed.EstimatedUsage.OutputTokens == 0 {
		t.Fatalf("failed submission = %+v", failed)
	}

	// Late provider usage is more authoritative than a local estimate even
	// after the terminal transport outcome was recorded.
	submission.ObserveUsage(&TokenUsage{InputTokens: 10, OutputTokens: 4})
	known := execution.Snapshot().Submissions[0]
	if known.CostState != InferenceCostKnown || known.ReportedUsage == nil || known.ReportedUsage.InputTokens != 10 {
		t.Fatalf("known submission = %+v", known)
	}
	if _, err := execution.BeginAttempt().RecordSubmission(InferenceSubmissionMeta{Provider: "openai", Transport: "http"}); err != nil {
		t.Fatal(err)
	}
	estimated, err := execution.BeginAttempt().RecordSubmission(InferenceSubmissionMeta{Provider: "openai", Transport: "http"})
	if err != nil {
		t.Fatal(err)
	}
	estimated.ObserveOutput("estimated output")
	estimated.ObserveEstimatedUsage(&TokenUsage{InputTokens: 8, OutputTokens: 1})
	estimated.CompleteFailure(NormalizedFailure{Category: FailureIncompleteStream})
	summary := execution.Snapshot().CostSummary()
	if summary.KnownSubmissions != 1 || summary.EstimatedSubmissions != 1 || summary.UnknownBillableSubmissions != 1 || summary.KnownUsage.InputTokens != 10 || summary.KnownUsage.OutputTokens != 4 || summary.EstimatedUsage.InputTokens != 8 {
		t.Fatalf("cost summary = %+v", summary)
	}
}

func TestInferenceAttemptAttributesStreamEventsToLatestSubmission(t *testing.T) {
	execution := NewInferenceExecution(NewInferenceOperation(InferenceOperationAgentRound, InferenceProfileInteractive))
	websocketAttempt := execution.BeginAttempt()
	websocket, err := websocketAttempt.RecordSubmission(InferenceSubmissionMeta{Transport: "websocket"})
	if err != nil {
		t.Fatal(err)
	}
	websocket.CompleteFallback(NormalizeFailure(errors.New("websocket unavailable")))
	httpAttempt := execution.BeginAttempt()
	if _, err := httpAttempt.RecordSubmission(InferenceSubmissionMeta{Transport: "http", Mode: "fallback"}); err != nil {
		t.Fatal(err)
	}
	httpAttempt.ObserveStreamEvent(StreamEvent{Type: EventContentDelta, Content: "hello"})
	httpAttempt.ObserveStreamEvent(StreamEvent{Type: EventDone, Usage: &TokenUsage{InputTokens: 3, OutputTokens: 1}})

	submissions := execution.Snapshot().Submissions
	if len(submissions) != 2 || submissions[0].Outcome != InferenceSubmissionFallback || submissions[1].Outcome != InferenceSubmissionSucceeded || submissions[1].CostState != InferenceCostKnown || submissions[1].OutputBytes != len("hello") {
		t.Fatalf("submissions = %+v", submissions)
	}
}
