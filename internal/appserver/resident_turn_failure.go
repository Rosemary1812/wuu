package appserver

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/session"
)

// isRetryableTurnFailure decides whether a failed resident turn is a transient
// runtime failure worth retrying the plan node against its budget (plan §T8).
// It is deliberately narrow: only a genuinely failed turn whose classified
// category is network or provider (a dropped connection, a rate-limit, an
// overloaded provider) is retryable. Cancelled (the user interrupted), auth
// (needs a human to re-auth), and local/internal (a logic bug in our own code
// or tools) are NEVER retried — re-running the same node would not fix them.
// There are no timeouts in this decision (red line §4.7): only the turn's own
// terminal error classifies it.
func isRetryableTurnFailure(turn Turn) bool {
	if turn.Status != TurnStatusFailed || turn.Error == nil {
		return false
	}
	switch strings.TrimSpace(turn.Error.Category) {
	case "network", "provider":
		return true
	default:
		return false
	}
}

// planDispatchTaskIDs returns the distinct task cth ids a consumed envelope batch
// dispatched — the mechanical guard for "this turn was executing a plan node". A
// plan dispatch is a system envelope from "task plan" naming a task cth
// (SourceSubthreadID); anything else (a human DM, an @mention, a board wake whose
// sender is "task board") is not a node execution. It is the shared guard for
// both the failure hook (T8, retryOrFailTaskNodesAfterTurn) and the activity
// resolver (resolveExecutingNode, plan §T9) so the two never drift.
func planDispatchTaskIDs(envs []MessageEnvelope) map[string]bool {
	taskIDs := map[string]bool{}
	for _, env := range envs {
		if !strings.EqualFold(strings.TrimSpace(env.SenderKind), "system") {
			continue
		}
		if strings.TrimSpace(env.SenderName) != "task plan" {
			continue
		}
		if id := strings.TrimSpace(env.SourceSubthreadID); id != "" {
			taskIDs[id] = true
		}
	}
	return taskIDs
}

// resolveExecutingNode returns the task id and piece id of the plan node a
// resident turn is executing — the participant's active/retrying piece in a task
// its consumed batch dispatched (see planDispatchTaskIDs for the mechanical
// guard). It is resolved ONCE per turn (not per stream event) to drive the
// activity/progress liveness wiring (plan §T9). ok is false for any turn that is
// not executing a node: an ordinary DM/chat turn (no plan dispatch in the batch),
// a lead's planning or wrap-up wake (the lead holds no active piece), a board
// wake (a different sender). When several dispatched tasks each carry an active
// piece for this participant, the first found is returned — the liveness signal
// is a bare timestamp refresh, so attributing a shared turn to one of its nodes
// is sufficient.
func (s *Server) resolveExecutingNode(participantID string, envs []MessageEnvelope) (taskID, pieceID string, ok bool) {
	if s == nil || s.rt == nil {
		return "", "", false
	}
	participantID = strings.TrimSpace(participantID)
	if participantID == "" {
		return "", "", false
	}
	taskIDs := planDispatchTaskIDs(envs)
	if len(taskIDs) == 0 {
		return "", "", false
	}
	for id := range taskIDs {
		task, err := session.FindConversationThreadByID(s.rt.SessionDir, id)
		if err != nil {
			providers.DebugLogf("resolveExecutingNode load task %q: %v", id, err)
			continue
		}
		if piece := activePieceForAssignee(task.Plan, participantID); piece != nil {
			return task.ID, piece.ID, true
		}
	}
	return "", "", false
}

// retryOrFailTaskNodesAfterTurn reacts to a resident turn that was executing a
// plan node (plan §T8). It runs from afterResidentTurn after read receipts and
// before the final re-kick. The guard is mechanical: it does nothing unless the
// consumed batch carries a plan dispatch — a system envelope from "task plan"
// naming a task cth (SourceSubthreadID). From those envelopes it collects the
// distinct task ids the turn was executing, and for each task acts on the
// caller's active/retrying nodes:
//
//   - a retryable transient failure with budget left → consume one attempt,
//     mark the node retrying, and re-dispatch it (same prompt + handoff);
//   - a retryable failure with the budget exhausted, OR a hard failure that
//     retrying cannot fix (auth/local/internal) → fail the node, block the
//     task, and wake the lead to recover;
//   - a cancelled turn (user interrupt) → leave the node untouched: the user
//     chose to stop, so no budget is spent, no trace is written, nobody is
//     woken;
//   - a completed turn → nothing (the node advances through piece_done, not
//     here).
func (s *Server) retryOrFailTaskNodesAfterTurn(participantID string, envs []MessageEnvelope, turn Turn) {
	if s == nil || s.rt == nil {
		return
	}
	participantID = strings.TrimSpace(participantID)
	if participantID == "" {
		return
	}
	// Guard: only a plan-dispatch batch (system "task plan" envelope carrying a
	// task id) is a node execution turn.
	taskIDs := planDispatchTaskIDs(envs)
	if len(taskIDs) == 0 {
		return
	}

	retryable := isRetryableTurnFailure(turn)
	// A hard failure is one a retry cannot fix — auth (needs a human),
	// local/internal (our own bug). It fails the node immediately like an
	// exhausted budget, but WITHOUT spending a retry. Cancelled is excluded on
	// purpose: it is the user's interrupt, not a node fault.
	hardFailure := false
	category := ""
	message := ""
	if turn.Status == TurnStatusFailed && turn.Error != nil {
		category = strings.TrimSpace(turn.Error.Category)
		message = strings.TrimSpace(turn.Error.Message)
		switch category {
		case "auth", "local", "internal":
			hardFailure = true
		}
	}
	// A completed turn, or a cancelled one, leaves every node as-is.
	if !retryable && !hardFailure {
		return
	}

	for taskID := range taskIDs {
		task, err := session.FindConversationThreadByID(s.rt.SessionDir, taskID)
		if err != nil {
			providers.DebugLogf("retryOrFailTaskNodesAfterTurn load task %q: %v", taskID, err)
			continue
		}
		for _, piece := range task.Plan {
			if piece.Assignee != participantID {
				continue
			}
			if piece.Status != session.TaskPieceActive && piece.Status != session.TaskPieceRetrying {
				continue
			}
			s.handleNodeTurnFailure(task, piece, retryable, category, message)
		}
	}
}

// handleNodeTurnFailure applies the retry-or-fail decision to one plan node
// whose executing turn died. retryable is whether the failure is a transient
// runtime failure (a hard failure passes retryable=false and is failed
// immediately without spending budget). category/message describe the failure
// for the trace and the node's FailureReason.
func (s *Server) handleNodeTurnFailure(task session.ConversationThread, piece session.TaskPiece, retryable bool, category, message string) {
	reason := strings.TrimSpace(message)
	if reason == "" {
		reason = firstNonEmpty(strings.TrimSpace(category), "runtime failure")
	}
	reason = truncate(reason, 500)

	if retryable && piece.Attempts < piece.RetryBudget {
		// Budget remains: consume one attempt and re-dispatch the same node.
		updated, err := session.UpdateTaskPiece(s.rt.SessionDir, task.ID, piece.ID, func(p *session.TaskPiece) {
			p.Attempts++
			p.Status = session.TaskPieceRetrying
			p.FailureReason = reason
			p.LastActivityAt = time.Now().UTC()
		})
		if err != nil {
			providers.DebugLogf("retry node %q/%q: %v", task.ID, piece.ID, err)
			return
		}
		fresh := findPieceByID(updated.Plan, piece.ID)
		if fresh == nil {
			return
		}
		s.recordTaskEventFor(updated, piece.ID, session.TaskEventRetrying, piece.Assignee,
			fmt.Sprintf("attempt %d/%d after runtime failure", fresh.Attempts, fresh.RetryBudget), "")
		s.wakePieceAssignee(updated, *fresh)
		s.notifySubthreadUpdated(updated.SessionID, updated.ID)
		return
	}

	// Budget exhausted, or a hard failure retrying cannot fix: the node is dead.
	// Fail it, pause the task (blocked), and wake the lead to recover.
	updated, err := session.UpdateTaskPiece(s.rt.SessionDir, task.ID, piece.ID, func(p *session.TaskPiece) {
		p.Status = session.TaskPieceFailed
		p.FailureReason = reason
		p.LastActivityAt = time.Now().UTC()
	})
	if err != nil {
		providers.DebugLogf("fail node %q/%q: %v", task.ID, piece.ID, err)
		return
	}
	payload, err := json.Marshal(map[string]string{"category": category, "message": reason})
	if err != nil {
		payload = nil
	}
	s.recordTaskEventFor(updated, piece.ID, session.TaskEventNodeFailed, piece.Assignee,
		fmt.Sprintf("node %q failed: %s", firstNonEmpty(strings.TrimSpace(piece.Title), piece.ID), reason), string(payload))
	if err := session.SetConversationThreadExecState(s.rt.SessionDir, updated.ID, session.ExecStateBlocked); err != nil {
		providers.DebugLogf("block task %q after node failure: %v", updated.ID, err)
	} else {
		updated.ExecState = session.ExecStateBlocked
	}
	s.notifySubthreadUpdated(updated.SessionID, updated.ID)
	fresh := findPieceByID(updated.Plan, piece.ID)
	if fresh == nil {
		fresh = &piece
	}
	s.wakePlanLeadOnFailure(updated, *fresh, reason)
}
