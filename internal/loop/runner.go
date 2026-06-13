package loop

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Runner struct {
	Store    *Store
	Verifier Verifier
	Now      func() time.Time
}

func (r *Runner) Init(ctx context.Context, spec Spec) (State, error) {
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	if r == nil || r.Store == nil {
		return State{}, errors.New("loop runner store is required")
	}
	if r.Now != nil {
		r.Store.SetClock(r.Now)
	}
	return r.Store.Init(spec)
}

func (r *Runner) RunDemo(ctx context.Context, spec Spec) (State, error) {
	state, err := r.Init(ctx, spec)
	if err != nil {
		return State{}, err
	}
	if _, err := r.Store.SetStatus(StatusRunning, StepInit, "demo loop started"); err != nil {
		return State{}, err
	}

	if err := r.phase(ctx, StepResearch, "Research completed.", "research.md", renderResearchArtifact(state)); err != nil {
		return State{}, err
	}
	if err := r.phase(ctx, StepPlan, "Plan completed.", "plan.md", renderPlanArtifact(state)); err != nil {
		return State{}, err
	}
	if err := r.phase(ctx, StepPlan, "Todo list written.", "todo.md", renderTodoArtifact(state)); err != nil {
		return State{}, err
	}

	state, err = r.RunVerification(ctx)
	if err != nil {
		return State{}, err
	}
	if state.Status == StatusBlocked || state.Status == StatusNeedsHuman || state.Status == StatusFailed {
		return state, nil
	}

	if err := r.phase(ctx, StepReview, "Review artifact written.", "review.md", renderReviewArtifact(state)); err != nil {
		return State{}, err
	}
	if err := r.phase(ctx, StepIntegration, "Integration artifact written.", "integration.md", renderIntegrationArtifact(state)); err != nil {
		return State{}, err
	}
	if err := r.phase(ctx, StepSummary, "Final artifact written.", "final.md", renderFinalArtifact(state)); err != nil {
		return State{}, err
	}
	return r.Store.SetStatus(StatusCompleted, StepSummary, "demo loop completed")
}

func (r *Runner) RunVerification(ctx context.Context) (State, error) {
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	if r == nil || r.Store == nil {
		return State{}, errors.New("loop runner store is required")
	}
	state, err := r.Store.LoadState()
	if err != nil {
		return State{}, err
	}
	if _, err := r.Store.AddProgress(StepVerification, "Verification started."); err != nil {
		return State{}, err
	}
	verifier := r.Verifier
	if verifier == nil {
		verifier = CommandVerifier{Now: r.Now}
	}
	result, err := verifier.Verify(ctx, state, state.VerificationPolicy)
	if err != nil {
		failure := Failure{
			Step:    StepVerification,
			Kind:    "verification_error",
			Message: err.Error(),
		}
		if _, ferr := r.Store.AddFailure(failure); ferr != nil {
			return State{}, ferr
		}
		return r.Store.LoadState()
	}
	_, verificationPath, err := r.Store.AddArtifact("verification.md", "verification", renderVerificationMarkdown(result))
	if err != nil {
		return State{}, err
	}
	for i := range result.Checks {
		result.Checks[i].CreatedAt = ensureTime(result.Checks[i].CreatedAt, r.now())
	}
	state, err = r.Store.RecordTestResults(result.Checks)
	if err != nil {
		return State{}, err
	}
	if !result.Passed {
		failure := Failure{
			Step:     StepVerification,
			Kind:     "verification_failed",
			Message:  result.Summary,
			Artifact: verificationPath,
		}
		if result.Failure != nil {
			failure = *result.Failure
			failure.Artifact = verificationPath
		}
		if _, err := r.Store.AddFailure(failure); err != nil {
			return State{}, err
		}
		return r.Store.LoadState()
	}
	if _, err := r.Store.MarkStepCompleted(StepVerification); err != nil {
		return State{}, err
	}
	return r.Store.SetStatus(StatusRunning, StepVerification, result.Summary)
}

func (r *Runner) phase(ctx context.Context, step Step, message, artifactName, artifactContent string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := r.Store.AddProgress(step, message); err != nil {
		return err
	}
	if strings.TrimSpace(artifactName) != "" {
		if _, _, err := r.Store.AddArtifact(artifactName, string(step), artifactContent); err != nil {
			return err
		}
	}
	_, err := r.Store.MarkStepCompleted(step)
	return err
}

func (r *Runner) now() time.Time {
	if r != nil && r.Now != nil {
		return r.Now()
	}
	return time.Now().UTC()
}

func renderResearchArtifact(state State) string {
	return fmt.Sprintf(`# Research

Goal: %s

Findings:
- Loop state is durable outside model context.
- Next phases must read .loop/state.json and .loop/failures.md before acting.
`, state.Goal)
}

func renderPlanArtifact(state State) string {
	return fmt.Sprintf(`# Plan

Goal: %s

Plan:
1. Keep the core agent loop unchanged.
2. Persist workflow state and artifacts in .loop.
3. Run verification before final summary.
4. Require reviewer or human approval when policy asks for it.
`, state.Goal)
}

func renderTodoArtifact(_ State) string {
	return `# Todo

- [x] Initialize durable loop state.
- [x] Write research artifact.
- [x] Write plan artifact.
- [ ] Run verification.
- [ ] Write final artifact.
`
}

func renderReviewArtifact(state State) string {
	status := "verification passed"
	if len(state.Failures) > 0 {
		status = "verification has recorded failures"
	}
	return fmt.Sprintf(`# Review

Review status: %s

This demo review records the checker phase. Production workflows should assign
this phase to an independent reviewer role.
`, status)
}

func renderIntegrationArtifact(state State) string {
	return fmt.Sprintf(`# Integration

Loop: %s

Integration status:
- Worktree: %s
- Final merge: not required for demo workflow.
`, state.ID, worktreeSummary(state.Worktree))
}

func renderFinalArtifact(state State) string {
	return fmt.Sprintf(`# Final

Goal: %s

Outcome:
- Durable state: .loop/state.json
- Progress: .loop/progress.md
- Decisions: .loop/decisions.md
- Failures: .loop/failures.md
- Events: .loop/events.jsonl
- Verification: .loop/artifacts/verification.md
`, state.Goal)
}

func worktreeSummary(lease *WorktreeLease) string {
	if lease == nil || strings.TrimSpace(lease.Path) == "" {
		return "none"
	}
	return lease.Path
}

func ensureTime(value, fallback time.Time) time.Time {
	if value.IsZero() {
		return fallback
	}
	return value
}
