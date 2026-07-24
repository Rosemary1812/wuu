package appserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/goalruntime"
	"github.com/blueberrycongee/wuu/internal/statepath"
)

// handleGoalActiveSummary returns the lightweight composer-banner view of
// the most recently updated non-terminal goal. The renderer only needs id,
// text (single-line), status, usage, and timestamps — full goal state (tasks,
// approvals, workflow phases) is intentionally omitted so the renderer cannot
// rebuild the deleted right-side Goal panel from this surface. The timestamps
// are metadata; time_used_seconds is the effective execution time shown by the
// desktop because active usage accounting excludes paused and blocked goals.
func (s *Server) handleGoalActiveSummary(req Request) error {
	var params GoalActiveSummaryParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.writeResponse(req.ID, nil, fmt.Errorf("parse goal active summary params: %w", err))
		}
	}
	summary, err := s.findActiveGoalSummary(params.ThreadID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, GoalActiveSummaryResult{Summary: summary}, nil)
}

func (s *Server) handleGoalPause(req Request) error {
	var params GoalPauseParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.writeResponse(req.ID, nil, fmt.Errorf("parse goal pause params: %w", err))
		}
	}
	if !params.ConfirmUserApproved {
		return s.writeResponse(req.ID, nil, fmt.Errorf("goal pause requires confirm_user_approved=true"))
	}
	_, runtime, err := s.runtimeGoalForControl(params.GoalID, params.ThreadID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if runtime == nil {
		return s.writeResponse(req.ID, nil, fmt.Errorf("active runtime goal not found"))
	}
	if _, err := runtime.SetUserStatus(goalruntime.StatusPaused, time.Now().UTC()); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, GoalPauseResult{OK: true}, nil)
}

func (s *Server) handleGoalResume(req Request) error {
	var params GoalResumeParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.writeResponse(req.ID, nil, fmt.Errorf("parse goal resume params: %w", err))
		}
	}
	if !params.ConfirmUserApproved {
		return s.writeResponse(req.ID, nil, fmt.Errorf("goal resume requires confirm_user_approved=true"))
	}
	runtimeGoal, runtime, err := s.runtimeGoalForControl(params.GoalID, params.ThreadID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if runtime == nil {
		return s.writeResponse(req.ID, nil, fmt.Errorf("active runtime goal not found"))
	}
	if _, err := runtime.SetUserStatus(goalruntime.StatusActive, time.Now().UTC()); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	s.kickGoalContinuation(runtimeGoal.ThreadID)
	return s.writeResponse(req.ID, GoalResumeResult{OK: true}, nil)
}

func (s *Server) handleGoalClear(req Request) error {
	var params GoalClearParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.writeResponse(req.ID, nil, fmt.Errorf("parse goal clear params: %w", err))
		}
	}
	if !params.ConfirmUserApproved {
		return s.writeResponse(req.ID, nil, fmt.Errorf("goal clear requires confirm_user_approved=true"))
	}
	_, runtime, err := s.runtimeGoalForControl(params.GoalID, params.ThreadID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if runtime == nil {
		return s.writeResponse(req.ID, nil, fmt.Errorf("active runtime goal not found"))
	}
	if err := runtime.Clear(); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, GoalClearResult{OK: true}, nil)
}

// handleGoalUpdateText rewrites the goal objective. The renderer is
// expected to obtain explicit user confirmation before invoking this;
// the server enforces confirm_user_approved as a guardrail.
func (s *Server) handleGoalUpdateText(req Request) error {
	var params GoalUpdateTextParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.writeResponse(req.ID, nil, fmt.Errorf("parse goal update text params: %w", err))
		}
	}
	if !params.ConfirmUserApproved {
		return s.writeResponse(req.ID, nil, fmt.Errorf("goal update text requires confirm_user_approved=true"))
	}
	text := strings.TrimSpace(params.Text)
	if text == "" {
		return s.writeResponse(req.ID, nil, fmt.Errorf("goal text is required"))
	}
	_, runtime, err := s.runtimeGoalForControl(params.GoalID, params.ThreadID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if runtime == nil {
		return s.writeResponse(req.ID, nil, fmt.Errorf("active runtime goal not found"))
	}
	if _, err := runtime.EditObjective(text, time.Now().UTC()); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, GoalUpdateTextResult{OK: true}, nil)
}

// findActiveGoalSummary returns the thread runtime Goal's lightweight summary.
// The composer banner intentionally ignores the older durable Goal ledger: that
// ledger is execution evidence and cannot drive runtime continuation.
func (s *Server) findActiveGoalSummary(threadID string) (*GoalActiveSummary, error) {
	runtimeGoal, foundRuntimeGoal, err := s.currentRuntimeGoal(threadID)
	if err != nil {
		return nil, err
	}
	if !foundRuntimeGoal || goalruntime.IsTerminalStatus(runtimeGoal.Status) {
		return nil, nil
	}
	return s.runtimeGoalSummary(runtimeGoal, threadID)
}

func (s *Server) runtimeGoalSummary(goal goalruntime.Goal, threadID string) (*GoalActiveSummary, error) {
	started := ""
	if !goal.CreatedAt.IsZero() {
		started = goal.CreatedAt.UTC().Format(time.RFC3339)
	}
	updated := ""
	if !goal.UpdatedAt.IsZero() {
		updated = goal.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return &GoalActiveSummary{
		ID:                      goal.GoalID,
		ThreadID:                strings.TrimSpace(threadID),
		Text:                    goalSummaryText(goal.Objective),
		Status:                  string(goal.Status),
		StartedAt:               started,
		UpdatedAt:               updated,
		RunningSince:            s.activeGoalRunningSince(goal, threadID),
		StopReason:              runtimeGoalStopReason(goal.Status),
		TokensUsed:              goal.TokensUsed,
		TimeUsedSeconds:         goal.TimeUsedSeconds,
		GoalTurns:               goal.GoalTurns,
		Blocker:                 goal.BlockerAudit.Message,
		BlockerConsecutiveTurns: goal.BlockerAudit.ConsecutiveTurns,
		CanPause:                goal.Status == goalruntime.StatusActive,
		CanResume:               goal.Status == goalruntime.StatusPaused || goal.Status == goalruntime.StatusBlocked,
		CanClear:                !goalruntime.IsTerminalStatus(goal.Status),
	}, nil
}

// activeGoalRunningSince exposes only the currently executing slice of goal
// time. Completed turns are already folded into TimeUsedSeconds; the renderer
// adds this in-flight slice locally so the counter can advance without polling.
func (s *Server) activeGoalRunningSince(goal goalruntime.Goal, threadID string) string {
	if goal.Status != goalruntime.StatusActive {
		return ""
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		threadID = strings.TrimSpace(goal.ThreadID)
	}
	th := s.thread(threadID)
	if th == nil {
		return ""
	}

	th.mu.Lock()
	defer th.mu.Unlock()
	if !th.running || th.currentTurn == "" {
		return ""
	}
	for i := len(th.Turns) - 1; i >= 0; i-- {
		turn := th.Turns[i]
		if turn.ID != th.currentTurn || turn.Status != TurnStatusInProgress || turn.StartedAt == nil {
			continue
		}
		runningSince := turn.StartedAt.UTC()
		if goal.CreatedAt.After(runningSince) {
			runningSince = goal.CreatedAt.UTC()
		}
		return runningSince.Format(time.RFC3339Nano)
	}
	return ""
}

func runtimeGoalStopReason(status goalruntime.Status) string {
	switch status {
	case goalruntime.StatusPaused:
		return "paused"
	case goalruntime.StatusBlocked:
		return "blocked"
	default:
		return ""
	}
}

// goalSummaryText collapses goal.Goal into the first-line form the
// banner renders. Width truncation is a renderer concern so editing a
// long first line does not accidentally persist a server-side truncation.
func goalSummaryText(goal string) string {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return ""
	}
	if idx := strings.IndexAny(goal, "\n\r"); idx >= 0 {
		goal = goal[:idx]
	}
	return goal
}

func (s *Server) runtimeGoalForControl(goalID, threadID string) (goalruntime.Goal, *goalruntime.Runtime, error) {
	runtimeGoal, found, err := s.currentRuntimeGoal(threadID)
	if err != nil || !found {
		return goalruntime.Goal{}, nil, err
	}
	goalID = strings.TrimSpace(goalID)
	if goalID != "" && runtimeGoal.GoalID != goalID {
		return goalruntime.Goal{}, nil, fmt.Errorf("active runtime goal %q does not match requested goal %q", runtimeGoal.GoalID, goalID)
	}
	runtime, ok, err := s.goalRuntimeForThread(threadID)
	if err != nil {
		return goalruntime.Goal{}, nil, err
	}
	if !ok {
		return goalruntime.Goal{}, nil, nil
	}
	return runtimeGoal, runtime, nil
}

func (s *Server) currentRuntimeGoal(threadID string) (goalruntime.Goal, bool, error) {
	runtime, ok, err := s.goalRuntimeForThread(threadID)
	if err != nil || !ok {
		return goalruntime.Goal{}, false, err
	}
	goal, err := runtime.CurrentGoal()
	if errors.Is(err, os.ErrNotExist) {
		return goalruntime.Goal{}, false, nil
	}
	if err != nil {
		return goalruntime.Goal{}, false, err
	}
	return goal, true, nil
}

func (s *Server) goalRuntimeForThread(threadID string) (*goalruntime.Runtime, bool, error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil, false, nil
	}
	if err := validateGoalThreadID(threadID); err != nil {
		return nil, false, err
	}
	if th := s.thread(threadID); th != nil {
		th.mu.Lock()
		threadRuntime := th.execRuntime
		th.mu.Unlock()
		if threadRuntime != nil && threadRuntime.GoalRuntime != nil {
			return threadRuntime.GoalRuntime, true, nil
		}
	}
	stateDir, err := s.workspaceStateDir()
	if err != nil {
		return nil, false, err
	}
	return goalruntime.NewRuntime(goalruntime.NewStore(statepath.ThreadGoalRuntimePath(stateDir, threadID))), true, nil
}

func validateGoalThreadID(threadID string) error {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil
	}
	if threadID == "." || threadID == ".." || filepath.Base(threadID) != threadID || strings.ContainsAny(threadID, `/\`) {
		return fmt.Errorf("thread_id must be a session id, not a path")
	}
	return nil
}
