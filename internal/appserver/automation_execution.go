package appserver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/automation"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/runtime"
	"github.com/blueberrycongee/wuu/internal/session"
)

func (s *Server) ExecuteAutomation(ctx context.Context, task automation.Task, run automation.Run) (automation.ExecutionResult, error) {
	if s == nil || s.closed.Load() {
		return automation.ExecutionResult{}, errServerClosed
	}
	prompt := strings.TrimSpace(task.Prompt)
	if prompt == "" {
		return automation.ExecutionResult{}, fmt.Errorf("automation task %q has an empty prompt", task.ID)
	}

	var th *threadState
	var err error
	switch automation.Mode(task.Mode) {
	case automation.ModeNewThread, "":
		th, err = s.createAutomationThread()
	case automation.ModeThreadHeartbeat:
		threadID := strings.TrimSpace(task.HeartbeatThreadID)
		if threadID == "" {
			return automation.ExecutionResult{}, fmt.Errorf("automation task %q has no heartbeat thread", task.ID)
		}
		th, err = s.ensureResidentThread(threadID)
	default:
		return automation.ExecutionResult{}, fmt.Errorf("automation task %q has invalid mode %q", task.ID, task.Mode)
	}
	if err != nil {
		return automation.ExecutionResult{}, err
	}

	msg, err := userMessageFromPrompt(prompt, nil, nil, s.rt.ExperimentalHelpMe)
	if err != nil {
		return automation.ExecutionResult{}, err
	}
	snapshot := turnRuntimeSnapshot{}.withPermissions(normalizeTurnPermissions(s.rt.Permissions))
	snapshot.Ultra = s.rt.UltraMode()
	snapshot.AutomationRunID = run.ID
	snapshot.RequestContext = automationRequestContext(task, run)

	result, started, err := s.startAutomationTurn(ctx, th, msg, snapshot)
	if err != nil {
		return automation.ExecutionResult{}, err
	}
	if started {
		return result, nil
	}

	queueID := strings.TrimSpace(run.ID)
	if queueID == "" {
		queueID = session.NewID()
	}
	msg.ClientID = queueID
	entry := queuedTurn{id: queueID, msg: msg, snapshot: snapshot}
	s.enqueueQueuedUserTurn(th.ID, entry)
	queued := queuedTurnSummary(th.ID, entry)
	_ = s.writeNotification(NotificationTurnQueued, TurnQueuedNotification{Queued: queued})
	s.kickQueuedTurnDrain(th.ID)
	return automation.ExecutionResult{Status: automation.RunStatusQueued, ThreadID: th.ID, QueueID: queueID}, nil
}

func (s *Server) createAutomationThread() (*threadState, error) {
	if s.rt == nil || s.rt.StreamRunner == nil {
		return nil, errors.New("runtime session is required")
	}
	id := session.NewID()
	threadCWD := s.rt.RootDir
	if _, err := session.CreateWithMetadata(s.rt.SessionDir, id, threadCWD); err != nil {
		return nil, err
	}
	if _, err := session.SetSource(s.rt.SessionDir, id, "automation"); err != nil {
		return nil, err
	}
	if workspaceID := strings.TrimSpace(s.rt.WorkspaceID); workspaceID != "" {
		if _, err := session.SetWorkspaceID(s.rt.SessionDir, id, workspaceID); err != nil {
			return nil, err
		}
	}
	history := make([]providers.ChatMessage, 0, 1)
	if prompt := strings.TrimSpace(s.rt.StreamRunner.SystemPrompt); prompt != "" {
		history = append(history, providers.ChatMessage{Role: "system", Content: prompt})
	}
	th := newThreadState(id, history, s.rt.ProviderName, s.rt.Model, threadCWD, true, time.Now().UTC())
	th.Source = "automation"
	th.WorkspaceKind = workspaceKindForCWD(s.rt.WuuHome, threadCWD)
	s.mu.Lock()
	s.threads[id] = th
	s.mu.Unlock()
	th.mu.Lock()
	thread := th.snapshotLocked()
	th.mu.Unlock()
	if err := s.notifyThreadStarted(thread); err != nil {
		return nil, err
	}
	s.pruneResidentThreads(id)
	return th, nil
}

func (s *Server) startAutomationTurn(ctx context.Context, th *threadState, msg providers.ChatMessage, snapshot turnRuntimeSnapshot) (automation.ExecutionResult, bool, error) {
	var threadRuntime *runtime.ThreadRuntime
	started, ok, err := s.startThreadUserTurnWithAdmission(
		ctx, th, msg, snapshot, false, turnReadOnlyFail,
		turnAdmissionHooks{afterLease: func(admitted *threadState, _ *providers.ChatMessage) error {
			var runtimeErr error
			threadRuntime, runtimeErr = s.ensureThreadRuntimeAfterAdmission(admitted)
			if runtimeErr == nil {
				s.foldFrozenWorkerTree(admitted, threadRuntime)
			}
			return runtimeErr
		}},
	)
	if err != nil {
		if errors.Is(err, errThreadExecutionBusy) {
			return automation.ExecutionResult{}, false, nil
		}
		return automation.ExecutionResult{}, false, err
	}
	if !ok {
		return automation.ExecutionResult{}, false, nil
	}
	launch, accepted := s.reserveBackground(func() {
		s.runTurn(started.ctx, th, threadRuntime, started.turnID, started.runtime, started.history)
	})
	if !accepted {
		return automation.ExecutionResult{}, false, errors.Join(errServerClosed, s.abortStartedThreadTurnDurably(th, started, errServerClosed))
	}
	defer launch.Cancel()
	if err := s.writeNotification(NotificationTurnStarted, TurnStartedNotification{ThreadID: th.ID, Turn: started.turn}); err != nil {
		return automation.ExecutionResult{}, false, errors.Join(err, s.abortStartedThreadTurnDurably(th, started, err))
	}
	launch.Commit()
	return automation.ExecutionResult{Status: automation.RunStatusRunning, ThreadID: th.ID, TurnID: started.turnID}, true, nil
}

func automationRequestContext(task automation.Task, run automation.Run) []agent.ContextSegment {
	content := fmt.Sprintf(
		"Automation trigger\n- task_id: %s\n- run_id: %s\n- triggered_at: %s\n- schedule: %s\n- timezone: %s\n- mode: %s",
		strings.TrimSpace(task.ID), strings.TrimSpace(run.ID), run.TriggeredAt.UTC().Format(time.RFC3339),
		strings.TrimSpace(task.Cron), strings.TrimSpace(task.Timezone), strings.TrimSpace(task.Mode),
	)
	return agent.RequestOnlyContextMessages([]providers.ChatMessage{{
		Role: "system", Name: "automation_info", Content: content,
	}})
}

var _ automation.Executor = (*Server)(nil)
