package providers

import (
	"errors"
	"testing"
)

func TestInferenceExecutionTracksAttemptsAndSubmissions(t *testing.T) {
	op := NewInferenceOperation(InferenceOperationAgentRound, InferenceProfileInteractive)
	execution := NewInferenceExecution(op)
	first := execution.BeginAttempt()
	firstSubmission := first.RecordSubmission(InferenceSubmissionMeta{
		Provider: "openai", Protocol: "responses", Transport: "websocket", Mode: "stream",
	})
	secondSubmission := first.RecordSubmission(InferenceSubmissionMeta{
		Provider: "openai", Protocol: "responses", Transport: "http", Mode: "fallback", Reason: "websocket_failed",
	})
	second := execution.BeginAttempt()

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
	if got := first.SubmissionSnapshot(); len(got) != 2 || got[1].Transport != "http" {
		t.Fatalf("first attempt submissions = %+v", got)
	}
}

func TestEnsureInferenceAttemptPreservesOuterAttempt(t *testing.T) {
	req := EnsureInferenceExecution(ChatRequest{}, InferenceOperationTitle, InferenceProfileBestEffort)
	req = BeginInferenceAttempt(req, InferenceOperationTitle, InferenceProfileBestEffort)
	got := EnsureInferenceAttempt(req, InferenceOperationAuxiliary, InferenceProfileInteractive)
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
	submission := attempt.RecordSubmission(InferenceSubmissionMeta{Provider: "openai", Transport: "http"})
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
	execution.BeginAttempt().RecordSubmission(InferenceSubmissionMeta{Provider: "openai", Transport: "http"})
	estimated := execution.BeginAttempt().RecordSubmission(InferenceSubmissionMeta{Provider: "openai", Transport: "http"})
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
	attempt := execution.BeginAttempt()
	websocket := attempt.RecordSubmission(InferenceSubmissionMeta{Transport: "websocket"})
	websocket.CompleteFallback(NormalizeFailure(errors.New("websocket unavailable")))
	attempt.RecordSubmission(InferenceSubmissionMeta{Transport: "http", Mode: "fallback"})
	attempt.ObserveStreamEvent(StreamEvent{Type: EventContentDelta, Content: "hello"})
	attempt.ObserveStreamEvent(StreamEvent{Type: EventDone, Usage: &TokenUsage{InputTokens: 3, OutputTokens: 1}})

	submissions := execution.Snapshot().Submissions
	if len(submissions) != 2 || submissions[0].Outcome != InferenceSubmissionFallback || submissions[1].Outcome != InferenceSubmissionSucceeded || submissions[1].CostState != InferenceCostKnown || submissions[1].OutputBytes != len("hello") {
		t.Fatalf("submissions = %+v", submissions)
	}
}
