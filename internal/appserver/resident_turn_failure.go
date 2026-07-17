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
// overloaded provider) is retryable. Invalid requests, cancelled turns, auth
// failures, and local/internal errors are NEVER retried — re-running the same
// node would not fix them.
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
type taskAttemptDispatch struct {
	TaskID    string
	NodeID    string
	AttemptID string
}

func planAttemptDispatches(envs []MessageEnvelope) []taskAttemptDispatch {
	seen := map[string]bool{}
	var out []taskAttemptDispatch
	for _, env := range envs {
		if !strings.EqualFold(strings.TrimSpace(env.SenderKind), "system") || strings.TrimSpace(env.SenderName) != "task plan" {
			continue
		}
		dispatch := taskAttemptDispatch{
			TaskID: strings.TrimSpace(env.SourceSubthreadID), NodeID: strings.TrimSpace(env.TaskNodeID),
			AttemptID: strings.TrimSpace(env.TaskAttemptID),
		}
		if dispatch.TaskID == "" || dispatch.NodeID == "" || dispatch.AttemptID == "" || seen[dispatch.AttemptID] {
			continue
		}
		seen[dispatch.AttemptID] = true
		out = append(out, dispatch)
	}
	return out
}

func planDispatchTaskIDs(envs []MessageEnvelope) map[string]bool {
	taskIDs := map[string]bool{}
	for _, dispatch := range planAttemptDispatches(envs) {
		taskIDs[dispatch.TaskID] = true
	}
	return taskIDs
}

// resolveExecutingAttempt resolves the exact durable attempt carried by a plan
// dispatch envelope. It never guesses from the assignee's first active node.
// A named agent has at most one queued/running attempt, so one resident turn has
// one unambiguous task/node/attempt identity.
func (s *Server) resolveExecutingAttempt(participantID string, envs []MessageEnvelope) (taskID, pieceID, attemptID string, ok bool) {
	if s == nil || s.rt == nil {
		return "", "", "", false
	}
	participantID = strings.TrimSpace(participantID)
	if participantID == "" {
		return "", "", "", false
	}
	for _, dispatch := range planAttemptDispatches(envs) {
		attempt, err := session.TaskAttemptByID(s.rt.SessionDir, dispatch.AttemptID)
		if err != nil {
			providers.DebugLogf("resolve executing attempt %q: %v", dispatch.AttemptID, err)
			continue
		}
		if attempt.TaskID == dispatch.TaskID && attempt.NodeID == dispatch.NodeID && attempt.AssigneeID == participantID &&
			(attempt.Status == session.TaskAttemptQueued || attempt.Status == session.TaskAttemptRunning) {
			return dispatch.TaskID, dispatch.NodeID, dispatch.AttemptID, true
		}
	}
	return "", "", "", false
}

func (s *Server) resolveExecutingNode(participantID string, envs []MessageEnvelope) (taskID, pieceID string, ok bool) {
	taskID, pieceID, _, ok = s.resolveExecutingAttempt(participantID, envs)
	return taskID, pieceID, ok
}

// dispatchedNodesForTurn snapshots the exact attempt ids a turn may complete.
// Lead-management wakes carry no attempt id and therefore contribute nothing.
func (s *Server) dispatchedNodesForTurn(participantID string, envs []MessageEnvelope) map[string]map[string]string {
	if s == nil || s.rt == nil {
		return nil
	}
	participantID = strings.TrimSpace(participantID)
	if participantID == "" {
		return nil
	}
	dispatches := planAttemptDispatches(envs)
	if len(dispatches) == 0 {
		return nil
	}
	out := map[string]map[string]string{}
	for _, dispatch := range dispatches {
		attempt, err := session.TaskAttemptByID(s.rt.SessionDir, dispatch.AttemptID)
		if err != nil {
			providers.DebugLogf("dispatchedNodesForTurn load attempt %q: %v", dispatch.AttemptID, err)
			continue
		}
		task, err := session.FindConversationThreadByID(s.rt.SessionDir, dispatch.TaskID)
		if err != nil {
			continue
		}
		if task.ExecState != session.ExecStateExecuting {
			continue
		}
		piece := findPieceByID(task.Plan, dispatch.NodeID)
		if attempt.AssigneeID != participantID || attempt.TaskID != task.ID || piece == nil || piece.CurrentAttemptID != attempt.ID {
			continue
		}
		if out[task.ID] == nil {
			out[task.ID] = map[string]string{}
		}
		out[task.ID][piece.ID] = attempt.ID
	}
	return out
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
//   - an interrupted turn is handled separately by interruptTaskAttemptsAfterTurn;
//   - a completed turn → not handled here: turn-end completion capture
//     (autoCompleteTaskNodesAfterTurn) advances the plan for a completed
//     dispatch turn, whether or not the agent filed piece_done.
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
	dispatches := planAttemptDispatches(envs)
	if len(dispatches) == 0 {
		return
	}

	retryable := isRetryableTurnFailure(turn)
	// Any non-transient failed turn terminates the attempt. Auth/local/internal
	// failures are not retried; interrupted turns have their own path.
	hardFailure := false
	category := ""
	message := ""
	if turn.Status == TurnStatusFailed {
		hardFailure = true
		if turn.Error != nil {
			category = strings.TrimSpace(turn.Error.Category)
			message = strings.TrimSpace(turn.Error.Message)
		}
		if retryable {
			hardFailure = false
		}
	}
	// This hook owns only failures. An interrupted or completed turn is not this
	// hook's concern — turn-end
	// completion capture (autoCompleteTaskNodesAfterTurn) advances it.
	if !retryable && !hardFailure {
		return
	}

	for _, dispatch := range dispatches {
		attempt, err := session.TaskAttemptByID(s.rt.SessionDir, dispatch.AttemptID)
		if err != nil {
			providers.DebugLogf("retryOrFailTaskNodesAfterTurn load attempt %q: %v", dispatch.AttemptID, err)
			continue
		}
		task, err := session.FindConversationThreadByID(s.rt.SessionDir, dispatch.TaskID)
		if err != nil {
			continue
		}
		piece := findPieceByID(task.Plan, dispatch.NodeID)
		if piece == nil || piece.Assignee != participantID || piece.CurrentAttemptID != attempt.ID || attempt.AssigneeID != participantID {
			continue
		}
		s.handleNodeTurnFailure(task, *piece, attempt, retryable, category, message)
	}
}

// interruptTaskAttemptsAfterTurn terminates an exact attempt when its resident
// turn ended without a success/failure outcome the scheduler can advance from
// (user interruption or an unresolved question). The Task pauses for its lead;
// no invisible queued/running attempt survives a finished turn.
func (s *Server) interruptTaskAttemptsAfterTurn(participantID string, envs []MessageEnvelope, reason string) {
	participantID = strings.TrimSpace(participantID)
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "worker turn interrupted"
	}
	for _, dispatch := range planAttemptDispatches(envs) {
		attempt, err := session.TaskAttemptByID(s.rt.SessionDir, dispatch.AttemptID)
		if err != nil || attempt.AssigneeID != participantID ||
			(attempt.Status != session.TaskAttemptQueued && attempt.Status != session.TaskAttemptRunning) {
			continue
		}
		_, task, err := session.FinishTaskAttempt(
			s.rt.SessionDir, attempt.ID, session.TaskAttemptInterrupted, session.TaskPieceBlocked,
			"interrupted", reason, time.Now().UTC(),
		)
		if err != nil {
			providers.DebugLogf("interrupt task attempt %q: %v", attempt.ID, err)
			continue
		}
		if err := session.SetConversationThreadExecState(s.rt.SessionDir, task.ID, session.ExecStateBlocked); err != nil {
			providers.DebugLogf("pause interrupted task %q: %v", task.ID, err)
		} else {
			task.ExecState = session.ExecStateBlocked
		}
		s.recordTaskEventForAttempt(task, dispatch.NodeID, attempt.ID, session.TaskEventBlocked, participantID, reason, "")
		s.notifySubthreadUpdated(task.SessionID, task.ID)
		if piece := findPieceByID(task.Plan, dispatch.NodeID); piece != nil {
			s.wakePlanLeadOnFailure(task, *piece, reason)
		}
	}
}

// handleNodeTurnFailure applies the retry-or-fail decision to one plan node
// whose executing turn died. retryable is whether the failure is a transient
// runtime failure (a hard failure passes retryable=false and is failed
// immediately without spending budget). category/message describe the failure
// for the trace and the node's FailureReason.
func (s *Server) handleNodeTurnFailure(task session.ConversationThread, piece session.TaskPiece, attempt session.TaskAttempt, retryable bool, category, message string) {
	reason := strings.TrimSpace(message)
	if reason == "" {
		reason = firstNonEmpty(strings.TrimSpace(category), "runtime failure")
	}
	reason = truncate(reason, 500)

	if retryable && attempt.Ordinal < piece.RetryBudget {
		// Budget remains: terminate this exact attempt, then let the global
		// scheduler reserve and dispatch a new attempt.
		_, updated, err := session.FinishTaskAttempt(
			s.rt.SessionDir, attempt.ID, session.TaskAttemptFailed, session.TaskPieceRetrying,
			category, reason, time.Now().UTC(),
		)
		if err != nil {
			providers.DebugLogf("retry node %q/%q: %v", task.ID, piece.ID, err)
			return
		}
		s.recordTaskEventForAttempt(updated, piece.ID, attempt.ID, session.TaskEventRetrying, piece.Assignee,
			fmt.Sprintf("attempt %d/%d failed; retry scheduled", attempt.Ordinal, piece.RetryBudget), "")
		s.notifySubthreadUpdated(updated.SessionID, updated.ID)
		s.reconcileExecutingTasks()
		return
	}

	// Budget exhausted, or a hard failure retrying cannot fix: the node is dead.
	// Fail it, pause the task (blocked), and wake the lead to recover.
	_, updated, err := session.FinishTaskAttempt(
		s.rt.SessionDir, attempt.ID, session.TaskAttemptFailed, session.TaskPieceFailed,
		category, reason, time.Now().UTC(),
	)
	if err != nil {
		providers.DebugLogf("fail node %q/%q: %v", task.ID, piece.ID, err)
		return
	}
	payload, err := json.Marshal(map[string]string{"category": category, "message": reason})
	if err != nil {
		payload = nil
	}
	s.recordTaskEventForAttempt(updated, piece.ID, attempt.ID, session.TaskEventNodeFailed, piece.Assignee,
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

// autoCompleteTaskNodesAfterTurn captures a plan node's completion at turn end,
// the way a deterministic workflow captures an agent's final output as its return
// value: a COMPLETED plan-dispatch turn advances the plan even when the agent did
// not file piece_done, so a node can never freeze on a completed turn that simply
// forgot to report done. It is the completed-turn counterpart of
// retryOrFailTaskNodesAfterTurn (which owns the failure/cancel cases) and runs
// right after it in afterResidentTurn.
//
// It completes ONLY the pieces this turn was dispatched to run — the turn-start
// snapshot (dispatchedNodesForTurn) — and only those still Active/Retrying at turn
// end. That is the whole safety story: a node the turn neither ran nor was woken
// for is never in the snapshot, and a node the turn resolved another way falls out
// of it. Specifically it does nothing when:
//
//   - the turn is not completed (a failure/cancel is retryOrFail's, not ours);
//   - the batch is not a plan dispatch (planDispatchTaskIDs empty) — an ordinary
//     DM/chat turn completes no node;
//   - the agent asked a question this turn (askedQuestion) — an explicit "blocked,
//     waiting on an answer" signal: the node stays open, re-woken on the answer;
//   - the task is no longer executing (need_human → needs_human, a conclude →
//     completed): its nodes are not this turn's to complete;
//   - a snapshot piece is no longer Active/Retrying: it either finished
//     (piece_done → done) or bounced (need_upstream → pending) mid-turn, both of
//     which already ran their own handler.
//
// When it does complete a node and the agent filed no handoff, it synthesizes a
// minimal one (Done = the turn's final visible output) so the downstream is not
// left empty-handed, then completes + advances exactly as piece_done would. So
// piece_done is no longer REQUIRED to complete a node — it is how a node completes
// EARLY, or with a rich structured handoff.
func (s *Server) autoCompleteTaskNodesAfterTurn(participantID string, envs []MessageEnvelope, turn Turn, askedQuestion bool, dispatched map[string]map[string]string) {
	if s == nil || s.rt == nil {
		return
	}
	participantID = strings.TrimSpace(participantID)
	if participantID == "" || turn.Status != TurnStatusCompleted {
		return
	}
	taskIDs := planDispatchTaskIDs(envs)
	if len(taskIDs) == 0 {
		return
	}
	if askedQuestion {
		s.interruptTaskAttemptsAfterTurn(participantID, envs, "worker ended the attempt with an unresolved question")
		return
	}
	finalAnswer := finalAgentMessageText(turn)
	for taskID := range taskIDs {
		nodes := dispatched[taskID]
		if len(nodes) == 0 {
			continue // not a node dispatch to this assignee (lead wake, or nothing to capture)
		}
		task, err := session.FindConversationThreadByID(s.rt.SessionDir, taskID)
		if err != nil {
			providers.DebugLogf("autoCompleteTaskNodesAfterTurn load task %q: %v", taskID, err)
			continue
		}
		// A node runs only while the task is executing. If the turn moved it out
		// of executing (need_human -> needs_human, conclude -> completed) or it was
		// never executing, its nodes are not this turn's to complete.
		if task.ExecState != session.ExecStateExecuting {
			continue
		}
		for _, piece := range task.Plan {
			attemptID := strings.TrimSpace(nodes[piece.ID])
			if attemptID == "" || piece.Assignee != participantID || piece.CurrentAttemptID != attemptID {
				continue
			}
			// Still Active/Retrying at turn end = the dispatched node the turn
			// neither finished (piece_done -> done) nor bounced (need_upstream ->
			// pending). done/pending fall through untouched.
			if piece.Status != session.TaskPieceActive && piece.Status != session.TaskPieceRetrying {
				continue
			}
			var handoff *session.TaskHandoff
			if finalAnswer != "" {
				handoff = &session.TaskHandoff{Done: finalAnswer}
			}
			if err := s.completePieceAndAdvance(task, piece, handoff, participantID); err != nil {
				providers.DebugLogf("auto-complete node %q/%q: %v", task.ID, piece.ID, err)
			}
		}
	}
}

// finalAgentMessageText returns the turn's final visible assistant output — the
// last non-empty agent_message item's text — the raw material for a turn-end
// completion capture's synthesized handoff (Done = this text). It reads the same
// ThreadItemAgentMessage channel fallbackDMReplyFromFinalAnswer does; a result
// the assignee published only through post_message (a participant item) is
// deliberately NOT captured — a public post is user-visible progress, never a
// downstream node's input (the Task workflow's two-channel contract), so a turn that
// spoke only through the tool channel yields "" and a nil (input-less) handoff.
func finalAgentMessageText(turn Turn) string {
	finalAnswer := ""
	for _, item := range turn.Items {
		if item.Type == ThreadItemAgentMessage {
			if text := strings.TrimSpace(item.Text); text != "" {
				finalAnswer = text
			}
		}
	}
	return finalAnswer
}
