package providers

import (
	"strings"
	"testing"
)

func TestNewInferenceOperationNormalizesIdentityAndPayloadVersion(t *testing.T) {
	op := NewInferenceOperation(InferenceOperationTitle, InferenceProfileBestEffort)
	if !strings.HasPrefix(op.ID, "iop-") {
		t.Fatalf("operation id = %q", op.ID)
	}
	if op.Kind != InferenceOperationTitle || op.WorkloadProfile != InferenceProfileBestEffort || op.PayloadVersion != 1 {
		t.Fatalf("operation = %+v", op)
	}
	if first, second := op.AttemptID(1), op.AttemptID(2); first == second || !strings.HasSuffix(first, "-a1") || !strings.HasSuffix(second, "-a2") {
		t.Fatalf("attempt ids = %q / %q", first, second)
	}
}

func TestEnsureInferenceOperationPreservesExistingIdentity(t *testing.T) {
	op := EnsureInferenceOperation(InferenceOperation{
		ID:             "iop-existing",
		PayloadVersion: 3,
	}, InferenceOperationCompaction, InferenceProfileContinuationCritical)
	if op.ID != "iop-existing" || op.PayloadVersion != 3 || op.Kind != InferenceOperationCompaction || op.WorkloadProfile != InferenceProfileContinuationCritical {
		t.Fatalf("operation = %+v", op)
	}
}

func TestInferenceOperationAttemptIDRejectsInvalidOrdinal(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("AttemptID(0) did not panic")
		}
	}()
	NewInferenceOperation(InferenceOperationAgentRound, InferenceProfileInteractive).AttemptID(0)
}
