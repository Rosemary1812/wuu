package providers

import (
	"context"
	"time"
)

// InferenceJournal is the durable, metadata-only write-ahead boundary for one
// inference operation. Implementations must commit each method before it
// returns: callers use an error to stop before the next wire transition.
//
// None of these records may contain prompts, credentials, request/response
// bodies, or raw provider errors. RequestHash is the only durable payload
// identity.
type InferenceJournal interface {
	PrepareOperation(InferenceOperationJournalRecord) error
	PrepareAttempt(InferenceAttemptJournalRecord) error
	UpsertSubmission(InferenceSubmissionJournalRecord) error
	MarkAttemptFirstEvent(operationID, attemptID, submissionID string, at time.Time) error
	CompleteAttempt(InferenceAttemptTerminalRecord) error
	RecordRecovery(InferenceRecoveryJournalRecord) error
	CompleteOperation(InferenceOperationTerminalRecord) error
	CompleteWorkflow(InferenceWorkflowTerminalRecord) error
}

type InferenceOperationJournalRecord struct {
	Operation   InferenceOperation
	Workflow    InferenceWorkflowJournalRecord
	RequestHash string
	At          time.Time
}

type InferenceWorkflowJournalRecord struct {
	ID        string
	Profile   InferenceWorkloadProfile
	Budget    WorkflowBudgetSpec
	StartedAt time.Time
}

type InferenceAttemptJournalRecord struct {
	OperationID string
	WorkflowID  string
	AttemptID   string
	Ordinal     int
	RequestHash string
	At          time.Time
}

type InferenceSubmissionJournalRecord struct {
	OperationID     string
	AttemptID       string
	ID              string
	Ordinal         int
	AttemptOrdinal  int
	Provider        string
	Protocol        string
	Transport       string
	Mode            string
	Reason          string
	StartedAt       time.Time
	CompletedAt     time.Time
	Outcome         InferenceSubmissionOutcome
	FailureCategory FailureCategory
	CostState       InferenceCostState
	ReportedUsage   *TokenUsage
	EstimatedUsage  *TokenUsage
	OutputBytes     int
}

type InferenceAttemptTerminalRecord struct {
	OperationID string
	AttemptID   string
	Outcome     InferenceTerminalOutcome
	Failure     InferenceJournalFailure
	At          time.Time
}

type InferenceRecoveryJournalRecord struct {
	OperationID string
	AttemptID   string
	Action      RecoveryActionKind
	RetryAt     time.Time
	At          time.Time
}

type InferenceOperationTerminalRecord struct {
	OperationID string
	Outcome     InferenceTerminalOutcome
	Failure     InferenceJournalFailure
	At          time.Time
}

type InferenceWorkflowTerminalRecord struct {
	WorkflowID string
	Outcome    InferenceTerminalOutcome
	At         time.Time
}

// InferenceJournalFailure is the durable allowlist of failure metadata. Raw
// bodies and Cause deliberately do not have fields here.
type InferenceJournalFailure struct {
	Origin         FailureOrigin
	Category       FailureCategory
	ProviderFamily string
	ProviderCode   string
	HTTPStatus     int
	Confidence     ClassificationConfidence
}

func DurableInferenceFailure(failure NormalizedFailure) InferenceJournalFailure {
	return InferenceJournalFailure{
		Origin:         failure.Origin,
		Category:       failure.Category,
		ProviderFamily: failure.ProviderFamily,
		ProviderCode:   failure.ProviderCode,
		HTTPStatus:     failure.HTTPStatus,
		Confidence:     failure.ClassificationConfidence,
	}
}

type InferenceTerminalOutcome string

const (
	InferenceOutcomeSucceeded   InferenceTerminalOutcome = "succeeded"
	InferenceOutcomeFailed      InferenceTerminalOutcome = "failed"
	InferenceOutcomeCanceled    InferenceTerminalOutcome = "canceled"
	InferenceOutcomeAbandoned   InferenceTerminalOutcome = "abandoned"
	InferenceOutcomeInterrupted InferenceTerminalOutcome = "interrupted"
	InferenceOutcomeBlocked     InferenceTerminalOutcome = "blocked"
)

type inferenceJournalContextKey struct{}

// WithInferenceJournal binds a journal to every inference operation created
// under ctx, including nested compaction calls.
func WithInferenceJournal(ctx context.Context, journal InferenceJournal) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if journal == nil {
		return ctx
	}
	return context.WithValue(ctx, inferenceJournalContextKey{}, journal)
}

func InferenceJournalFromContext(ctx context.Context) InferenceJournal {
	if ctx == nil {
		return nil
	}
	journal, _ := ctx.Value(inferenceJournalContextKey{}).(InferenceJournal)
	return journal
}
