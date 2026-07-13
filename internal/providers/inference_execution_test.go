package providers

import "testing"

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
