package providers

import (
	"context"
	"errors"
	"time"
)

// InferenceRequestPreparer performs provider-specific, wire-affecting request
// normalization before the request hash and journal operation are committed.
// It must not acquire provider capacity, refresh credentials, or send data.
type InferenceRequestPreparer interface {
	PrepareInferenceRequest(context.Context, ChatRequest) (ChatRequest, error)
}

func prepareInferenceRequest(ctx context.Context, client Client, req ChatRequest) (ChatRequest, error) {
	preparer, ok := client.(InferenceRequestPreparer)
	if !ok {
		return req, nil
	}
	return preparer.PrepareInferenceRequest(ctx, req)
}

// ExecuteChat owns one complete unary operation attempt. Protocol clients are
// responsible only for one physical send; this executor owns durable prepare,
// terminal attempt, and terminal operation transitions.
func ExecuteChat(
	ctx context.Context,
	client Client,
	req ChatRequest,
	kind InferenceOperationKind,
	profile InferenceWorkloadProfile,
) (ChatResponse, error) {
	cfg := NormalizeRetryConfig(RetryConfigForProfile(profile))
	retriesUsed := 0
	for {
		resp, prepared, callErr := ExecuteChatAttempt(ctx, client, req, kind, profile)
		if prepared.Execution == nil {
			return resp, callErr
		}
		outcome, failure := inferenceTerminalFromError(callErr)
		if callErr == nil {
			if journalErr := prepared.Execution.Complete(outcome, failure); journalErr != nil {
				return resp, journalErr
			}
			return resp, nil
		}

		plan := PlanRecovery(failure)
		canRetry := retriesUsed < cfg.MaxRetries && unaryRecoverySupported(client, plan)
		if ctx.Err() != nil || !canRetry {
			if journalErr := prepared.Execution.Complete(outcome, failure); journalErr != nil {
				return resp, errors.Join(callErr, journalErr)
			}
			return resp, callErr
		}
		delay := time.Duration(0)
		if plan.Action != RecoveryRefreshAuth {
			delay = backoffDelay(retriesUsed, cfg.InitialDelay, cfg.MaxDelay, callErr)
		}
		if journalErr := prepared.Attempt.RecordRecovery(plan, time.Now().Add(delay)); journalErr != nil {
			if IsWorkflowBudgetError(journalErr) {
				budgetFailure := NormalizeFailure(journalErr)
				completeErr := prepared.Execution.Complete(InferenceOutcomeFailed, budgetFailure)
				return resp, errors.Join(callErr, journalErr, completeErr)
			}
			return resp, errors.Join(callErr, journalErr)
		}
		if cancelErr := inferenceContextError(ctx); cancelErr != nil {
			journalErr := prepared.Execution.Complete(InferenceOutcomeCanceled, NormalizeFailure(cancelErr))
			return resp, errors.Join(cancelErr, journalErr)
		}
		if plan.Action == RecoveryRefreshAuth {
			applier := client.(InferenceRecoveryApplier)
			if applyErr := applier.ApplyInferenceRecovery(ctx, plan); applyErr != nil {
				journalErr := prepared.Execution.Complete(InferenceOutcomeFailed, NormalizeFailure(applyErr))
				return resp, errors.Join(callErr, applyErr, journalErr)
			}
		}
		if delay > 0 {
			if waitErr := waitWithContext(ctx, delay); waitErr != nil {
				journalErr := prepared.Execution.Complete(InferenceOutcomeCanceled, NormalizeFailure(waitErr))
				return resp, errors.Join(waitErr, journalErr)
			}
		}
		retriesUsed++
		req.Operation = prepared.Operation
		req.Execution = prepared.Execution
		req.Attempt = InferenceAttempt{}
	}
}

// InferenceRecoveryApplier performs a protocol-local mutation selected by the
// shared planner, such as refreshing an OAuth credential. It must not submit
// the inference request; the executor creates the next attempt afterwards.
type InferenceRecoveryApplier interface {
	ApplyInferenceRecovery(context.Context, RecoveryPlan) error
}

func unaryRecoverySupported(client Client, plan RecoveryPlan) bool {
	switch plan.Action {
	case RecoveryReplaySame, RecoveryWaitThenReplay:
		return true
	case RecoveryRefreshAuth:
		_, ok := client.(InferenceRecoveryApplier)
		return ok
	default:
		return false
	}
}

// ExecuteChatAttempt runs and terminalizes exactly one unary attempt while
// leaving the operation open for an operation-specific completion validator.
func ExecuteChatAttempt(
	ctx context.Context,
	client Client,
	req ChatRequest,
	kind InferenceOperationKind,
	profile InferenceWorkloadProfile,
) (ChatResponse, ChatRequest, error) {
	if client == nil {
		return ChatResponse{}, ChatRequest{}, errors.New("chat client is required")
	}
	var err error
	req, err = prepareInferenceRequest(ctx, client, req)
	if err != nil {
		return ChatResponse{}, req, err
	}
	prepared, err := EnsureInferenceAttemptContext(ctx, req, kind, profile)
	if err != nil {
		return ChatResponse{}, prepared, err
	}
	resp, callErr := client.Chat(ctx, prepared)
	outcome, failure := inferenceTerminalFromError(callErr)
	if journalErr := prepared.Attempt.Complete(outcome, failure); journalErr != nil {
		if callErr != nil {
			return resp, prepared, errors.Join(callErr, journalErr)
		}
		return resp, prepared, journalErr
	}
	return resp, prepared, callErr
}

func inferenceTerminalFromError(err error) (InferenceTerminalOutcome, NormalizedFailure) {
	if err == nil {
		return InferenceOutcomeSucceeded, NormalizedFailure{}
	}
	failure := NormalizeFailure(err)
	outcome := InferenceOutcomeFailed
	if failure.Category == FailureCanceled || failure.Category == FailureDeadline {
		outcome = InferenceOutcomeCanceled
	}
	return outcome, failure
}
