package appserver

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/agentthread"
	"github.com/blueberrycongee/wuu/internal/compact"
	"github.com/blueberrycongee/wuu/internal/config"
	wuucontext "github.com/blueberrycongee/wuu/internal/context"
	"github.com/blueberrycongee/wuu/internal/contextbudget"
	"github.com/blueberrycongee/wuu/internal/goalruntime"
	"github.com/blueberrycongee/wuu/internal/imageproc"
	"github.com/blueberrycongee/wuu/internal/insight"
	"github.com/blueberrycongee/wuu/internal/memdir"
	"github.com/blueberrycongee/wuu/internal/modelbudget"
	"github.com/blueberrycongee/wuu/internal/modelcatalog"
	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/process"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/runtime"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/sessiontrace"
	"github.com/blueberrycongee/wuu/internal/stringutil"
	"github.com/blueberrycongee/wuu/internal/subagent"
	"github.com/blueberrycongee/wuu/internal/toolctx"
	"github.com/blueberrycongee/wuu/internal/workspaces"
)

type queuedTurn struct {
	id       string
	msg      providers.ChatMessage
	snapshot turnRuntimeSnapshot
}

type agentCompletionTurn struct {
	agentID   string
	resultID  string
	processID string
	msg       providers.ChatMessage
	snapshot  *subagent.SubAgentSnapshot
}

type startedThreadTurn struct {
	ctx        context.Context
	cancel     context.CancelFunc
	turnID     string
	turn       Turn
	runtime    turnRuntimeSnapshot
	history    []providers.ChatMessage
	admittedAt time.Time
	// userMsgSeq is the persisted seq of this turn's user message (0 when not
	// persisted). Carried so group/member routing can stamp it onto envelopes
	// for read receipts and reactions.
	userMsgSeq int
}

type turnReadOnlyPolicy int

const (
	turnReadOnlyIgnore turnReadOnlyPolicy = iota
	turnReadOnlySkip
	turnReadOnlyFail
)

type turnAdmissionHooks struct {
	// afterLease runs without th.mu after the durable lease has been acquired
	// and disk state refreshed. The lease remains attached to th, so helpers may
	// take th.mu and perform admission-only side effects without racing another
	// model turn.
	afterLease func(*threadState, *providers.ChatMessage) error
	// beforeUserAppendLocked may rewrite history while th.mu and the durable
	// lease are held. Its commit callback runs only after the synthetic/user
	// message has been durably appended.
	beforeUserAppendLocked func(*threadState) (func() error, error)
	// residentReadReceipts are committed in the same transaction as the
	// resident user marker and push-envelope consumption. They are empty for
	// ordinary user, queued, goal, and completion turns.
	residentReadReceipts []session.MessageMark
}

type turnRuntimeSnapshot struct {
	ProviderName       string
	Model              string
	PermissionMode     string
	PermissionExplicit bool
	// ForceCompact makes the turn run one compaction pass at entry. For
	// control-plane /compact this is paired with CompactOnly; for recovery
	// flows it can still precede a normal provider request. Rides the snapshot
	// so it survives turn queueing.
	ForceCompact bool
	// CompactOnly stops after the forced compaction pass instead of sending a
	// normal provider request. This is the control-plane /compact operation.
	CompactOnly bool
	// HistoryBaselineSeq is the last durable model-context record owned by
	// this turn at admission. Whole-history rewrites preserve every record
	// appended after it and order those concurrent posts after the model result.
	HistoryBaselineSeq       int
	AgentCompletionResultIDs []string
	ProcessCompletionIDs     []string
	// Ultra is the turn's effective Ultra value. User turns snapshot the
	// session setting at admission; synthetic completion turns reuse the
	// completing worker's inherited value instead of re-reading a session
	// value that may have changed while the orchestration tree ran. Rides
	// the snapshot so queueing and wakeups keep the admitted value.
	Ultra bool
}

func (s *Server) handleTurnStart(ctx context.Context, req Request) error {
	var params TurnStartParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	params.ThreadID = strings.TrimSpace(params.ThreadID)
	params.Prompt = strings.TrimSpace(params.Prompt)
	if params.ThreadID == "" {
		return s.writeResponse(req.ID, nil, errors.New("thread_id is required"))
	}
	images, err := normalizeTurnStartImages(params.Images)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	files, err := normalizeTurnStartFiles(params.Files)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if params.Prompt == "" && len(images) == 0 && len(files) == 0 {
		return s.writeResponse(req.ID, nil, errors.New("prompt or attachment is required"))
	}
	permissions, err := s.resolveTurnPermissions(params.PermissionMode)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	th, err := s.ensureResidentThread(params.ThreadID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	th.mu.Lock()
	dmParticipantID := strings.TrimSpace(th.DMParticipantID)
	isResidentDM := dmParticipantID != ""
	isGroup := th.Group
	th.mu.Unlock()
	if isResidentDM {
		// Retire cleanup protocol: a retired participant's DM history stays
		// browsable, but the conversation is frozen — no new turns.
		if p, err := session.GetParticipant(s.rt.SessionDir, dmParticipantID); err == nil && p.RetiredAt != nil {
			return s.writeResponse(req.ID, nil, fmt.Errorf("participant %q is retired; this conversation is read-only", firstNonEmpty(p.Name, dmParticipantID)))
		}
	}
	if isManualCompactPrompt(params.Prompt) {
		if len(images) > 0 || len(files) > 0 {
			return s.writeResponse(req.ID, nil, errors.New("compact does not accept attachments"))
		}
		return s.startThreadCompactTurn(ctx, req, th, params.Prompt)
	}
	if isGroup {
		return s.handleGroupTurnStart(req, th, params, images, files)
	}
	userMsg, err := userMessageFromPrompt(params.Prompt, images, files, s.rt.ExperimentalHelpMe)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	snapshot := turnRuntimeSnapshot{}.withPermissions(permissions)
	snapshot.PermissionExplicit = params.PermissionMode != nil
	snapshot.ForceCompact = isManualCompactPrompt(params.Prompt)
	// The user turn snapshots the session-level Ultra setting once, at
	// admission. Mid-turn changes affect only the next user turn.
	snapshot.Ultra = s.rt.UltraMode()
	var mentioned map[string]bool
	var threadRuntime *runtime.ThreadRuntime
	started, ok, err := s.startThreadUserTurnWithAdmission(
		ctx,
		th,
		userMsg,
		snapshot,
		true,
		turnReadOnlyIgnore,
		turnAdmissionHooks{afterLease: func(admitted *threadState, _ *providers.ChatMessage) error {
			// Focus declarations, membership changes, and runtime construction are
			// durable/model-visible side effects. A busy contender must perform
			// none of them, so all three happen only after lease ownership and the
			// full disk-state refresh.
			if isResidentDM && params.FocusWorkspace != nil {
				if err := s.applyTurnWorkspaceFocus(admitted, params.FocusWorkspace); err != nil {
					return err
				}
			}
			var err error
			mentioned, err = s.prepareThreadMentions(admitted, params.Mentions)
			if err != nil {
				return err
			}
			threadRuntime, err = s.ensureThreadRuntimeAfterAdmission(admitted)
			if err != nil {
				return err
			}
			s.foldFrozenWorkerTree(admitted, threadRuntime)
			return nil
		}},
	)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if !ok {
		return s.writeResponse(req.ID, nil, fmt.Errorf("thread %q already has a running turn", params.ThreadID))
	}
	launch, accepted := s.reserveBackground(func() {
		s.runTurn(started.ctx, th, threadRuntime, started.turnID, started.runtime, started.history)
	})
	if !accepted {
		persistErr := s.abortStartedThreadTurnDurably(th, started, errServerClosed)
		return s.writeResponse(req.ID, nil, errors.Join(errServerClosed, persistErr))
	}
	defer launch.Cancel()

	if err := s.writeResponse(req.ID, TurnStartResult{Turn: started.turn}, nil); err != nil {
		return errors.Join(err, s.abortStartedThreadTurnDurably(th, started, err))
	}
	if err := s.writeNotification(NotificationTurnStarted, TurnStartedNotification{
		ThreadID: params.ThreadID,
		Turn:     started.turn,
	}); err != nil {
		return errors.Join(err, s.abortStartedThreadTurnDurably(th, started, err))
	}
	launch.Commit()
	if !isResidentDM {
		s.routeUserMessageToResidents(th, userMsg, mentioned, started.userMsgSeq)
	}
	return nil
}

func (s *Server) ensureThreadRuntimeAfterAdmission(th *threadState) (*runtime.ThreadRuntime, error) {
	threadRuntime, err := s.ensureThreadRuntime(th)
	if err != nil || threadRuntime == nil {
		return threadRuntime, err
	}
	th.mu.Lock()
	history := cloneHistory(th.History)
	threadID := th.ID
	th.mu.Unlock()
	if threadRuntime.Toolkit != nil {
		if _, restoreErr := threadRuntime.Toolkit.RestorePlanFromHistory(history); restoreErr != nil {
			providers.DebugLogf("restore update_plan for refreshed thread %q: %v", threadID, restoreErr)
		}
	}
	if threadRuntime.StreamRunner != nil {
		threadRuntime.StreamRunner.SynchronizeConversationUsage(history, s.latestRetainedContextTokens(threadID))
	}
	return threadRuntime, nil
}

func (s *Server) handleThreadCompactStart(ctx context.Context, req Request) error {
	var params ThreadCompactStartParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	params.ThreadID = strings.TrimSpace(params.ThreadID)
	if params.ThreadID == "" {
		return s.writeResponse(req.ID, nil, errors.New("thread_id is required"))
	}
	th, err := s.ensureResidentThread(params.ThreadID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.startThreadCompactTurn(ctx, req, th, "")
}

func (s *Server) startThreadCompactTurn(ctx context.Context, req Request, th *threadState, displayPrompt string) error {
	if th == nil {
		return s.writeResponse(req.ID, nil, errors.New("thread not found"))
	}
	displayPrompt = strings.TrimSpace(displayPrompt)
	if displayPrompt == "" {
		displayPrompt = "/" + manualCompactSlashCommandName
	}
	displayMsg := providers.ChatMessage{Role: "user", Content: displayPrompt}
	th.mu.Lock()
	threadID := th.ID
	readOnly := th.ReadOnly
	isGroup := th.Group
	running := th.running
	th.mu.Unlock()
	if readOnly {
		return s.writeResponse(req.ID, nil, errors.New("thread is read-only"))
	}
	if isGroup {
		return s.writeResponse(req.ID, nil, errors.New("group threads do not support context compaction"))
	}
	if running {
		return s.writeResponse(req.ID, nil, fmt.Errorf("thread %q already has a running turn", threadID))
	}
	turnID := session.NewID()
	turnCtx, cancel := context.WithCancel(ctx)
	now := time.Now().UTC()
	th.mu.Lock()
	if th.ReadOnly {
		th.mu.Unlock()
		cancel()
		return s.writeResponse(req.ID, nil, errors.New("thread is read-only"))
	}
	if th.Group {
		th.mu.Unlock()
		cancel()
		return s.writeResponse(req.ID, nil, errors.New("group threads do not support context compaction"))
	}
	if th.running {
		threadID = th.ID
		th.mu.Unlock()
		cancel()
		return s.writeResponse(req.ID, nil, fmt.Errorf("thread %q already has a running turn", threadID))
	}
	acquired, err := s.tryAcquireThreadExecutionLeaseLocked(th)
	if err != nil {
		th.mu.Unlock()
		cancel()
		return s.writeResponse(req.ID, nil, err)
	}
	if !acquired {
		threadID = th.ID
		th.mu.Unlock()
		cancel()
		return s.writeResponse(req.ID, nil, threadExecutionBusyError(threadID))
	}
	if err := s.refreshDurableThreadHistoryLocked(th); err != nil {
		th.releaseThreadExecutionLeaseLocked()
		th.mu.Unlock()
		cancel()
		return s.writeResponse(req.ID, nil, err)
	}
	if th.ReadOnly {
		th.releaseThreadExecutionLeaseLocked()
		th.mu.Unlock()
		cancel()
		return s.writeResponse(req.ID, nil, errors.New("thread is read-only"))
	}
	th.mu.Unlock()

	// Runtime construction and restoration must use the durable snapshot read
	// under this admission lease. A resumed app-server may otherwise compact
	// stale history or reuse a toolkit rooted at metadata another owner changed.
	threadRuntime, err := s.ensureThreadRuntimeAfterAdmission(th)
	if err != nil {
		th.mu.Lock()
		th.releaseThreadExecutionLeaseLocked()
		th.mu.Unlock()
		cancel()
		return s.writeResponse(req.ID, nil, err)
	}

	th.mu.Lock()
	if th.ReadOnly {
		th.releaseThreadExecutionLeaseLocked()
		th.mu.Unlock()
		cancel()
		return s.writeResponse(req.ID, nil, errors.New("thread is read-only"))
	}
	if th.Group {
		th.releaseThreadExecutionLeaseLocked()
		th.mu.Unlock()
		cancel()
		return s.writeResponse(req.ID, nil, errors.New("group threads do not support context compaction"))
	}
	if th.running {
		threadID = th.ID
		th.releaseThreadExecutionLeaseLocked()
		th.mu.Unlock()
		cancel()
		return s.writeResponse(req.ID, nil, fmt.Errorf("thread %q already has a running turn", threadID))
	}
	history := cloneHistory(th.History)
	th.cancel = cancel
	turn := th.startCompactTurnLocked(turnID, displayMsg, now)
	turnRuntime := turnRuntimeSnapshotLocked(th)
	turnRuntime.ForceCompact = true
	turnRuntime.CompactOnly = true
	turnRuntime.HistoryBaselineSeq = th.historyHeadSeq
	threadID = th.ID
	th.mu.Unlock()
	started := startedThreadTurn{
		ctx:     turnCtx,
		cancel:  cancel,
		turnID:  turnID,
		turn:    turn,
		runtime: turnRuntime,
		history: history,
	}
	launch, accepted := s.reserveBackground(func() {
		s.runTurn(turnCtx, th, threadRuntime, turnID, turnRuntime, history)
	})
	if !accepted {
		abortStartedThreadTurn(th, started, errServerClosed)
		return s.writeResponse(req.ID, nil, errServerClosed)
	}
	defer launch.Cancel()

	if err := s.writeResponse(req.ID, ThreadCompactStartResult{Turn: turn}, nil); err != nil {
		abortStartedThreadTurn(th, started, err)
		return err
	}
	if err := s.writeNotification(NotificationTurnStarted, TurnStartedNotification{
		ThreadID: threadID,
		Turn:     turn,
	}); err != nil {
		abortStartedThreadTurn(th, started, err)
		return err
	}

	launch.Commit()
	return nil
}

func (s *Server) handleTurnQueue(req Request) error {
	var params TurnQueueParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	params.ThreadID = strings.TrimSpace(params.ThreadID)
	params.Prompt = strings.TrimSpace(params.Prompt)
	if params.ThreadID == "" {
		return s.writeResponse(req.ID, nil, errors.New("thread_id is required"))
	}
	images, err := normalizeTurnStartImages(params.Images)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	files, err := normalizeTurnStartFiles(params.Files)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if params.Prompt == "" && len(images) == 0 && len(files) == 0 {
		return s.writeResponse(req.ID, nil, errors.New("prompt or attachment is required"))
	}
	if isManualCompactPrompt(params.Prompt) {
		if len(images) > 0 || len(files) > 0 {
			return s.writeResponse(req.ID, nil, errors.New("compact does not accept attachments"))
		}
		return s.writeResponse(req.ID, nil, errors.New("compact cannot be queued; wait for the current turn to finish"))
	}
	permissions, err := s.resolveTurnPermissions(params.PermissionMode)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	th, err := s.ensureResidentThread(params.ThreadID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	th.mu.Lock()
	readOnly := th.ReadOnly
	isGroup := th.Group
	th.mu.Unlock()
	if readOnly {
		return s.writeResponse(req.ID, nil, errors.New("thread is read-only"))
	}
	if isGroup {
		return s.writeResponse(req.ID, nil, errors.New("group threads do not support queued turns"))
	}

	queueID := strings.TrimSpace(params.ClientID)
	if queueID == "" {
		queueID = session.NewID()
	}
	msg, err := userMessageFromPrompt(params.Prompt, images, files, s.rt.ExperimentalHelpMe)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	msg.ClientID = queueID
	entry := queuedTurn{id: queueID, msg: msg, snapshot: turnRuntimeSnapshot{}.withPermissions(permissions)}
	entry.snapshot.PermissionExplicit = params.PermissionMode != nil
	entry.snapshot.ForceCompact = isManualCompactPrompt(params.Prompt)
	queued := queuedTurnSummary(params.ThreadID, entry)
	s.enqueueQueuedUserTurn(params.ThreadID, entry)
	if err := s.writeResponse(req.ID, TurnQueueResult{Queued: queued}, nil); err != nil {
		return err
	}
	_ = s.writeNotification(NotificationTurnQueued, TurnQueuedNotification{Queued: queued})
	s.kickQueuedTurnDrain(params.ThreadID)
	return nil
}

func (s *Server) handleTurnUpdateQueued(req Request) error {
	var params TurnUpdateQueuedParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	params.ThreadID = strings.TrimSpace(params.ThreadID)
	params.QueueID = strings.TrimSpace(params.QueueID)
	params.Prompt = strings.TrimSpace(params.Prompt)
	if params.ThreadID == "" {
		return s.writeResponse(req.ID, nil, errors.New("thread_id is required"))
	}
	if params.QueueID == "" {
		return s.writeResponse(req.ID, nil, errors.New("queue_id is required"))
	}
	images, err := normalizeTurnStartImages(params.Images)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	files, err := normalizeTurnStartFiles(params.Files)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if params.Prompt == "" && len(images) == 0 && len(files) == 0 {
		return s.writeResponse(req.ID, nil, errors.New("prompt or attachment is required"))
	}
	th, err := s.ensureResidentThread(params.ThreadID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	th.mu.Lock()
	readOnly := th.ReadOnly
	th.mu.Unlock()
	if readOnly {
		return s.writeResponse(req.ID, nil, errors.New("thread is read-only"))
	}

	msg, err := userMessageFromPrompt(params.Prompt, images, files, s.rt.ExperimentalHelpMe)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	updated, ok := s.replaceQueuedUserTurn(params.ThreadID, params.QueueID, msg)
	result := TurnUpdateQueuedResult{OK: ok}
	if ok {
		result.Queued = queuedTurnSummary(params.ThreadID, updated)
	}
	if err := s.writeResponse(req.ID, result, nil); err != nil {
		return err
	}
	return nil
}

func (s *Server) handleTurnDequeue(req Request) error {
	var params TurnDequeueParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	threadID := strings.TrimSpace(params.ThreadID)
	queueID := strings.TrimSpace(params.QueueID)
	if threadID == "" {
		return s.writeResponse(req.ID, nil, errors.New("thread_id is required"))
	}
	if queueID == "" {
		return s.writeResponse(req.ID, nil, errors.New("queue_id is required"))
	}
	removed := s.removeQueuedUserTurn(threadID, queueID)
	if err := s.writeResponse(req.ID, OKResult{OK: removed}, nil); err != nil {
		return err
	}
	if removed {
		_ = s.writeNotification(NotificationTurnDequeued, TurnDequeuedNotification{
			ThreadID: threadID,
			QueueID:  queueID,
		})
	}
	return nil
}

func (s *Server) handleTurnSteer(req Request) error {
	var params TurnSteerParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	params.ThreadID = strings.TrimSpace(params.ThreadID)
	params.Prompt = strings.TrimSpace(params.Prompt)
	params.ExpectedTurnID = strings.TrimSpace(params.ExpectedTurnID)
	if params.ThreadID == "" {
		return s.writeResponse(req.ID, nil, errors.New("thread_id is required"))
	}
	if params.ExpectedTurnID == "" {
		return s.writeResponse(req.ID, nil, errors.New("expected_turn_id is required"))
	}
	images, err := normalizeTurnStartImages(params.Images)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	files, err := normalizeTurnStartFiles(params.Files)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if params.Prompt == "" && len(images) == 0 && len(files) == 0 {
		return s.writeResponse(req.ID, nil, errors.New("prompt or attachment is required"))
	}
	th, err := s.ensureResidentThread(params.ThreadID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	clientID := strings.TrimSpace(params.ClientID)
	if clientID == "" {
		clientID = session.NewID()
	}

	th.mu.Lock()
	if !th.running || th.currentTurn == "" {
		th.mu.Unlock()
		return s.writeResponse(req.ID, nil, errors.New("no active turn to steer"))
	}
	if params.ExpectedTurnID != th.currentTurn {
		actual := th.currentTurn
		th.mu.Unlock()
		return s.writeResponse(req.ID, nil, fmt.Errorf("expected active turn id `%s` but found `%s`", params.ExpectedTurnID, actual))
	}
	if th.currentTurnKind == TurnKindCompact {
		th.mu.Unlock()
		return s.writeResponse(req.ID, nil, errors.New("cannot steer a compact turn"))
	}
	turnID := th.currentTurn
	steerMsg, err := userMessageFromPrompt(params.Prompt, images, files, s.rt.ExperimentalHelpMe)
	if err != nil {
		th.mu.Unlock()
		return s.writeResponse(req.ID, nil, err)
	}
	steerMsg.ClientID = clientID
	steerMsg.Steered = true
	removedQueued := s.removeQueuedUserTurn(params.ThreadID, clientID)
	th.pendingSteers = append(th.pendingSteers, steerMsg)
	th.mu.Unlock()
	if removedQueued {
		_ = s.writeNotification(NotificationTurnDequeued, TurnDequeuedNotification{
			ThreadID: params.ThreadID,
			QueueID:  clientID,
		})
	}
	return s.writeResponse(req.ID, TurnSteerResult{TurnID: turnID}, nil)
}

func (s *Server) handleTurnUnsteer(req Request) error {
	var params TurnUnsteerParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	threadID := strings.TrimSpace(params.ThreadID)
	steerID := strings.TrimSpace(params.SteerID)
	if threadID == "" {
		return s.writeResponse(req.ID, nil, errors.New("thread_id is required"))
	}
	if steerID == "" {
		return s.writeResponse(req.ID, nil, errors.New("steer_id is required"))
	}
	th := s.thread(threadID)
	if th == nil {
		return s.writeResponse(req.ID, nil, fmt.Errorf("thread %q not found", threadID))
	}
	th.mu.Lock()
	removed := th.removePendingSteerLocked(steerID)
	th.mu.Unlock()
	return s.writeResponse(req.ID, OKResult{OK: removed}, nil)
}

func (s *Server) ensureThreadRuntime(th *threadState) (*runtime.ThreadRuntime, error) {
	if th == nil {
		return nil, errors.New("thread is required")
	}
	if s == nil || s.closed.Load() {
		return nil, errServerClosed
	}
	th.mu.Lock()
	existing := th.execRuntime
	running := th.running
	var detached detachedThreadRuntime
	if existing != nil && th.pendingRuntimeReset && !running && !threadRuntimeHasOutstandingAgentWork(existing) {
		detached = detachThreadRuntimeLocked(th)
		existing = nil
	}
	history := cloneHistory(th.History)
	rootDir := th.CWD
	th.mu.Unlock()
	if detached.runtime != nil || detached.subscription != nil {
		releaseDetachedThreadRuntime(detached)
	}
	if existing != nil {
		if !running {
			if err := s.configureResidentThreadRuntime(th, existing); err != nil {
				return nil, err
			}
		}
		return existing, nil
	}
	if s.rt == nil {
		return nil, errors.New("runtime session is required")
	}
	threadRuntime, err := s.rt.NewThreadRuntimeForRoot(th.ID, firstNonEmpty(rootDir, s.rt.RootDir))
	if err != nil {
		return nil, err
	}
	if threadRuntime.Toolkit != nil {
		if _, restoreErr := threadRuntime.Toolkit.RestorePlanFromHistory(history); restoreErr != nil {
			providers.DebugLogf("restore update_plan for thread %q: %v", th.ID, restoreErr)
		}
	}
	// A rebuilt runtime over existing history (resume/reopen) starts with an
	// empty usage tracker; seed it from the last persisted ContextTokens so
	// the pre-first-response occupancy reads real ground truth instead of a
	// pessimistic full-history re-estimate.
	if threadRuntime.StreamRunner != nil && len(history) > 0 {
		if retained := s.latestRetainedContextTokens(th.ID); retained > 0 {
			threadRuntime.StreamRunner.SeedConversationUsageBaseline(retained, len(history))
		}
	}
	sub := s.subscribeThreadRuntime(th.ID, threadRuntime)
	if err := s.configureResidentThreadRuntime(th, threadRuntime); err != nil {
		releaseThreadRuntimeSubscription(threadRuntime, sub)
		return nil, err
	}
	th.mu.Lock()
	if s.closed.Load() {
		th.mu.Unlock()
		releaseThreadRuntimeSubscription(threadRuntime, sub)
		return nil, errServerClosed
	}
	if th.execRuntime == nil {
		th.execRuntime = threadRuntime
		th.runtimeSubscription = sub
		th.mu.Unlock()
		if threadRuntime.AgentControl != nil {
			threadRuntime.AgentControl.StartWorkerTerminalRecovery()
			// New only restores durable queue metadata. Start restored workers after
			// this runtime has won installation, resident configuration is complete,
			// and subscribeThreadRuntime has attached the model resolver and reliable
			// terminal finalizer. A zero-latency worker can now safely resolve and
			// finalize against the installed thread runtime.
			threadRuntime.AgentControl.StartQueuedWork()
			s.replayPendingAgentCompletions(th.ID, threadRuntime)
		}
		s.replayPendingProcessCompletions(th.ID, threadRuntime.AgentControl, threadRuntime.ProcessManager)
		return threadRuntime, nil
	}
	existing = th.execRuntime
	th.mu.Unlock()
	releaseThreadRuntimeSubscription(threadRuntime, sub)
	return existing, nil
}

func (s *Server) replayPendingAgentCompletions(threadID string, threadRuntime *runtime.ThreadRuntime) {
	if s == nil || threadRuntime == nil || threadRuntime.AgentControl == nil {
		return
	}
	pending, err := threadRuntime.AgentControl.PendingRootAgentCompletions()
	if err != nil {
		providers.DebugLogf("restore pending agent completions for thread %q: %v", threadID, err)
		return
	}
	for _, completion := range pending {
		s.enqueueAgentCompletionTurn(
			threadID,
			completion.Snapshot.ID,
			completion.ResultID,
			threadRuntime.AgentControl.AgentCompletionChatMessage(completion.Snapshot, agentthread.RootPath),
			&completion.Snapshot,
		)
	}
}

func (s *Server) configureResidentThreadRuntime(th *threadState, threadRuntime *runtime.ThreadRuntime) error {
	if s == nil || s.rt == nil || th == nil || threadRuntime == nil {
		return nil
	}
	th.mu.Lock()
	participantID := strings.TrimSpace(th.DMParticipantID)
	threadCWD := th.CWD
	focusWorkspace := th.FocusWorkspace
	running := th.running
	th.mu.Unlock()
	if participantID == "" || running {
		return nil
	}
	p, err := session.GetParticipant(s.rt.SessionDir, participantID)
	if err != nil {
		return err
	}
	if p.Kind != participant.KindNamed {
		return fmt.Errorf("dm participant %q is not a named agent", participantID)
	}
	prompt, err := s.residentPromptForParticipant(p)
	if err != nil {
		return err
	}

	providerName := strings.TrimSpace(s.rt.ProviderName)
	modelName := strings.TrimSpace(s.rt.Model)
	apiModel := ""
	var client providers.StreamClient
	if s.rt.StreamRunner != nil {
		if modelName == "" {
			modelName = strings.TrimSpace(s.rt.StreamRunner.Model)
		}
		apiModel = strings.TrimSpace(s.rt.StreamRunner.APIModel)
		client = s.rt.StreamRunner.Client
	}
	if apiModel == "" {
		apiModel = modelName
	}
	pinnedBudget := modelbudget.Budget{}
	havePinnedBudget := false
	if rawPin := strings.TrimSpace(p.Model); rawPin != "" {
		pinProvider, _ := parseParticipantModelPin(rawPin)
		modelOverride, clientOverride, err := resolveParticipantModelOverride(
			newRuntimeSessionReference(s.rt),
			p.Name,
			rawPin,
			providerName,
		)
		if err != nil {
			return err
		}
		if strings.TrimSpace(pinProvider) != "" {
			providerName = strings.TrimSpace(pinProvider)
		}
		if strings.TrimSpace(modelOverride) != "" {
			modelName = strings.TrimSpace(modelOverride)
			apiModel = modelName
		}
		if clientOverride != nil {
			client = clientOverride
		}
		// A pinned model may have a different context window than the global
		// default the thread runner was cloned with. Resolve the pinned
		// model's real budget the same way the Session model switcher does
		// (handleConfigAdvancedUpdate) so proactive compaction and the
		// context meter both key off this agent's actual window rather than
		// the default's. An unknown window leaves the cloned global budget in
		// place instead of zeroing the ceiling.
		pinnedBudget, havePinnedBudget = s.residentModelBudget(providerName, modelName)
	}
	if threadRuntime.StreamRunner != nil {
		if client != nil {
			threadRuntime.StreamRunner.Client = client
		}
		if modelName != "" {
			threadRuntime.StreamRunner.Model = modelName
		}
		threadRuntime.StreamRunner.APIModel = apiModel
		threadRuntime.StreamRunner.UpdateSystemPrompt(prompt)
		if havePinnedBudget {
			threadRuntime.StreamRunner.ContextWindowOverride = pinnedBudget.ContextWindowTokens
			threadRuntime.StreamRunner.MaxInputTokens = pinnedBudget.InputLimitTokens
			threadRuntime.StreamRunner.OutputReserveTokens = pinnedBudget.OutputReserveTokens
			threadRuntime.StreamRunner.CompactThresholdTokens = pinnedBudget.CompactThresholdTokens
		}
	}
	if threadRuntime.Toolkit != nil {
		threadRuntime.Toolkit.SetParticipantIdentity(participantID)
		threadRuntime.Toolkit.SetParticipantSpeechEnabled(true)
		threadRuntime.Toolkit.SetResidentParticipantEnabled(true)
		threadRuntime.Toolkit.SetParticipantSpeech(s.residentParticipantSpeech(participantID))
		threadRuntime.Toolkit.SetGroupManager(s.residentGroupManager(participantID))
		threadRuntime.Toolkit.SetTaskManager(s.residentTaskManager(participantID))
		// Resident file tools work inside the home + registered workspaces
		// + temp whitelist (design doc §5.2), plus the agent's own identity
		// memory notebook (memory-redesign §3); reads and writes alike. The
		// user notebook is deliberately NOT added: residents only see its
		// index read-only via the prompt.
		threadRuntime.Toolkit.SetFileScopeRoots(workspaces.BoundaryRoots(
			threadCWD,
			s.rt.WuuHome,
			memdir.ParticipantMemdir(s.rt.WuuHome, participantID),
		))
		// The declared workspace focus decides where tools work by default
		// (2026-07-03-workspace-focus.md §5): a workspace focus roots them
		// at that workspace, "" (all) and "~" (home) keep the agent home.
		// The file-scope whitelist above stays untouched — focus does not
		// narrow what the resident may read or write.
		if roster, err := workspaces.List(s.rt.WuuHome); err == nil {
			threadRuntime.Toolkit.SetRootDir(focusWorkspaceRoot(focusWorkspace, threadCWD, roster))
		}
	}
	if threadRuntime.AgentControl != nil {
		threadRuntime.AgentControl.EnableParticipantSpeech(participantID)
		threadRuntime.AgentControl.SetAgentParticipantID(participantID, participantID)
	}
	th.mu.Lock()
	if !th.running {
		th.ModelProvider = providerName
		if modelName != "" {
			th.Model = modelName
		}
		th.History = ensureResidentSystemPrompt(th.History, prompt)
	}
	th.mu.Unlock()
	return nil
}

// residentModelBudget resolves the context/input budget for a resident DM
// thread's actual (provider, model) using the same pipeline the Session model
// switcher uses (handleConfigAdvancedUpdate): load config, resolve + enrich the
// provider, then ResolveModelBudget. It returns ok=false when the model's
// window is unknown (or config cannot be read) so callers keep whatever budget
// the thread runner already carries instead of zeroing the ceiling — which
// would disable proactive compaction entirely.
func (s *Server) residentModelBudget(providerName, modelName string) (modelbudget.Budget, bool) {
	providerName = strings.TrimSpace(providerName)
	modelName = strings.TrimSpace(modelName)
	if s == nil || s.rt == nil || providerName == "" || modelName == "" {
		return modelbudget.Budget{}, false
	}
	cfg, _, err := s.rt.LoadEffectiveConfig()
	if err != nil {
		return modelbudget.Budget{}, false
	}
	providerCfg, resolvedName, err := cfg.ResolveProvider(providerName)
	if err != nil {
		return modelbudget.Budget{}, false
	}
	providerCfg = s.withCachedCodexModels(resolvedName, providerCfg)
	_, ruleProviderCfg := modelcatalog.EnrichProvider(resolvedName, providerCfg, modelName)
	budget := runtime.ResolveModelBudget(modelName, ruleProviderCfg, cfg.Agent.MaxContextTokens)
	if budget.ContextWindowTokens <= 0 && budget.InputLimitTokens <= 0 {
		return modelbudget.Budget{}, false
	}
	return budget, true
}

func (s *Server) subscribeThreadRuntime(threadID string, threadRuntime *runtime.ThreadRuntime) *threadRuntimeSubscription {
	if threadRuntime == nil || (threadRuntime.AgentControl == nil && threadRuntime.ProcessManager == nil) {
		return nil
	}
	control := threadRuntime.AgentControl
	if control != nil {
		control.SetParticipantRoster(s.participantRosterForTool())
	}
	// Per-participant model pins persist the raw pin but not the stream
	// client (StreamClient is not serializable). When a queued spawn
	// restores after a process restart the AgentControl calls the
	// installed resolver to rebuild the client; otherwise it would
	// silently fall back to the worker default client and route the
	// request to the wrong provider. Capture workerProviderName in the
	// closure so the resolver sees the same value the original spawn
	// saw.
	if s.rt != nil && control != nil {
		workerProvider := workerProviderName(s.rt)
		control.SetWorkerProviderName(workerProvider)
		ref := newRuntimeSessionReference(s.rt)
		control.SetModelPinClientResolver(func(rawPin string) (string, providers.StreamClient, error) {
			return resolveParticipantModelOverride(ref, "queued-restore", rawPin, workerProvider)
		})
	}
	sub := &threadRuntimeSubscription{
		statusCh:             make(chan subagent.Notification, 64),
		streamCh:             make(chan subagent.StreamNotification, 256),
		participantMessageCh: make(chan agentcontrol.ParticipantMessage, 64),
		processCh:            make(chan process.Event, 64),
		processManager:       threadRuntime.ProcessManager,
		done:                 make(chan struct{}),
	}
	if control != nil {
		sub.terminalUnsubscribe = control.SubscribeWorkerTerminalFinalizer(func(notification subagent.Notification) error {
			return s.finalizeAgentTerminal(threadID, control, notification)
		})
		control.Subscribe(sub.statusCh)
		sub.wg.Add(1)
		go func() {
			defer sub.wg.Done()
			s.forwardAgentNotifications(threadID, control, sub.statusCh, sub.done)
		}()

		control.SubscribeStream(sub.streamCh)
		sub.wg.Add(1)
		go func() {
			defer sub.wg.Done()
			s.forwardAgentStreamNotifications(threadID, control, sub.streamCh, sub.done)
		}()

		control.SubscribeParticipantMessages(sub.participantMessageCh)
		sub.wg.Add(1)
		go func() {
			defer sub.wg.Done()
			s.forwardParticipantMessages(threadID, control, sub.participantMessageCh, sub.done)
		}()
	}
	if threadRuntime.ProcessManager != nil {
		threadRuntime.ProcessManager.Subscribe(sub.processCh)
		sub.wg.Add(1)
		go func() {
			defer sub.wg.Done()
			s.forwardProcessNotifications(threadID, control, threadRuntime.ProcessManager, sub.processCh, sub.done)
		}()
	}
	return sub
}

func releaseThreadRuntime(th *threadState) {
	if th == nil {
		return
	}
	th.mu.Lock()
	detached := detachThreadRuntimeLocked(th)
	th.mu.Unlock()
	releaseDetachedThreadRuntime(detached)
}

type detachedThreadRuntime struct {
	runtime      *runtime.ThreadRuntime
	subscription *threadRuntimeSubscription
}

func detachThreadRuntimeLocked(th *threadState) detachedThreadRuntime {
	if th == nil {
		return detachedThreadRuntime{}
	}
	detached := detachedThreadRuntime{
		runtime:      th.execRuntime,
		subscription: th.runtimeSubscription,
	}
	th.execRuntime = nil
	th.runtimeSubscription = nil
	th.pendingRuntimeUpdate = nil
	th.pendingRuntimeReset = false
	return detached
}

func releaseDetachedThreadRuntime(detached detachedThreadRuntime) {
	releaseThreadRuntimeSubscription(detached.runtime, detached.subscription)
}

func threadRuntimeHasOutstandingAgentWork(threadRuntime *runtime.ThreadRuntime) bool {
	if threadRuntime == nil || threadRuntime.AgentControl == nil {
		return false
	}
	control := threadRuntime.AgentControl
	if control.HasOwnedWorkerExecutions() || control.HasPendingWorkerTerminalFinalizations() {
		return true
	}
	for _, snapshot := range control.List() {
		if !subagent.IsTerminal(snapshot.Status) {
			return true
		}
	}
	return false
}

func releaseThreadRuntimeSubscription(threadRuntime *runtime.ThreadRuntime, sub *threadRuntimeSubscription) {
	if threadRuntime != nil && threadRuntime.AgentControl != nil && sub != nil {
		threadRuntime.AgentControl.Unsubscribe(sub.statusCh)
		threadRuntime.AgentControl.UnsubscribeStream(sub.streamCh)
		threadRuntime.AgentControl.UnsubscribeParticipantMessages(sub.participantMessageCh)
	}
	if sub != nil && sub.processManager != nil {
		sub.processManager.Unsubscribe(sub.processCh)
	}
	if sub != nil && sub.terminalUnsubscribe != nil {
		sub.terminalUnsubscribe()
	}
	if sub != nil {
		sub.stop()
	}
	if threadRuntime != nil && threadRuntime.AgentControl != nil {
		threadRuntime.AgentControl.Close()
	}
}

func normalizeTurnStartImages(images []TurnStartImage) ([]providers.InputImage, error) {
	if len(images) == 0 {
		return nil, nil
	}
	out := make([]providers.InputImage, 0, len(images))
	for index, image := range images {
		mediaType := strings.TrimSpace(image.MediaType)
		data := strings.TrimSpace(image.Data)
		if data == "" {
			return nil, fmt.Errorf("image %d data is required", index+1)
		}
		var err error
		mediaType, data, err = normalizeImagePayload(mediaType, data)
		if err != nil {
			return nil, fmt.Errorf("image %d: %w", index+1, err)
		}
		rawBytes, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			return nil, fmt.Errorf("image %d: base64 decode: %w", index+1, err)
		}
		mode := imageproc.ModeDefault
		if image.Original {
			mode = imageproc.ModeOriginal
		}
		result, err := imageproc.Encode("", rawBytes, imageproc.Options{Mode: mode})
		if err != nil {
			return nil, fmt.Errorf("image %d: %w", index+1, err)
		}
		out = append(out, providers.InputImage{
			MediaType: result.MediaType,
			Data:      base64.StdEncoding.EncodeToString(result.Bytes),
			Width:     result.Width,
			Height:    result.Height,
		})
	}
	return out, nil
}

func normalizeTurnStartFiles(files []TurnStartFile) ([]providers.InputFile, error) {
	if len(files) == 0 {
		return nil, nil
	}
	out := make([]providers.InputFile, 0, len(files))
	for index, file := range files {
		mediaType := strings.TrimSpace(file.MediaType)
		data := strings.TrimSpace(file.Data)
		if data == "" {
			return nil, fmt.Errorf("file %d data is required", index+1)
		}
		var err error
		mediaType, data, err = normalizeFilePayload(mediaType, data)
		if err != nil {
			return nil, fmt.Errorf("file %d: %w", index+1, err)
		}
		out = append(out, providers.InputFile{
			MediaType: mediaType,
			Data:      data,
			Filename:  strings.TrimSpace(file.Filename),
		})
	}
	return out, nil
}

func userMessageFromPrompt(prompt string, images []providers.InputImage, files []providers.InputFile, helpMeEnabled bool) (providers.ChatMessage, error) {
	if disabledExperimentalSlashCommand(prompt, helpMeEnabled) {
		return providers.ChatMessage{}, errors.New("HelpMe is disabled; set agent.experimental_helpme to true to enable it")
	}
	content, display, ok := renderLightweightSlashCommandPrompt(prompt, helpMeEnabled)
	msg := providers.ChatMessage{
		Role:    "user",
		Content: content,
		Images:  images,
		Files:   files,
	}
	if ok {
		msg.DisplayContent = display
	}
	return msg, nil
}

func normalizeImagePayload(mediaType, data string) (string, string, error) {
	if strings.HasPrefix(strings.ToLower(data), "data:") {
		header, payload, ok := strings.Cut(data, ",")
		if !ok {
			return "", "", errors.New("invalid data URL")
		}
		if !strings.Contains(strings.ToLower(header), ";base64") {
			return "", "", errors.New("image data URL must be base64")
		}
		if mediaType == "" {
			mediaType = strings.TrimPrefix(strings.Split(header, ";")[0], "data:")
		}
		data = strings.TrimSpace(payload)
	}
	if mediaType == "" {
		mediaType = "image/png"
	}
	if !strings.HasPrefix(strings.ToLower(mediaType), "image/") {
		return "", "", fmt.Errorf("unsupported media type %q", mediaType)
	}
	if _, err := base64.StdEncoding.DecodeString(data); err != nil {
		return "", "", fmt.Errorf("invalid base64 data: %w", err)
	}
	return mediaType, data, nil
}

func normalizeFilePayload(mediaType, data string) (string, string, error) {
	if strings.HasPrefix(strings.ToLower(data), "data:") {
		header, payload, ok := strings.Cut(data, ",")
		if !ok {
			return "", "", errors.New("invalid data URL")
		}
		if !strings.Contains(strings.ToLower(header), ";base64") {
			return "", "", errors.New("file data URL must be base64")
		}
		if mediaType == "" {
			mediaType = strings.TrimPrefix(strings.Split(header, ";")[0], "data:")
		}
		data = strings.TrimSpace(payload)
	}
	mediaType = strings.TrimSpace(strings.ToLower(mediaType))
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	if mediaType != "application/pdf" {
		return "", "", fmt.Errorf("unsupported file media type %q", mediaType)
	}
	if _, err := base64.StdEncoding.DecodeString(data); err != nil {
		return "", "", fmt.Errorf("invalid base64 data: %w", err)
	}
	return mediaType, data, nil
}

func (s *Server) handleTurnInterrupt(req Request) error {
	var params TurnInterruptParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	threadID := strings.TrimSpace(params.ThreadID)
	th := s.thread(threadID)
	if th == nil {
		return s.writeResponse(req.ID, nil, fmt.Errorf("thread %q not found", params.ThreadID))
	}
	th.mu.Lock()
	cancel := th.cancel
	if cancel == nil {
		th.mu.Unlock()
		return s.writeResponse(req.ID, nil, errors.New("thread has no running turn"))
	}
	th.pendingSteers = nil
	th.workerTreeFrozen = true
	control := threadAgentControlLocked(th)
	th.mu.Unlock()
	discardedQueueIDs := s.discardQueuedUserWork(threadID)
	cancel()
	// turn/interrupt means "freeze this work", not "leave background workers
	// running": cancel the whole anonymous-worker tree, clear its queued
	// spawns, and keep partial results as resumable state. The next
	// user-initiated turn lifts the freeze with a whole-tree snapshot.
	if control != nil {
		control.FreezeWorkerTree()
	}
	if err := s.writeResponse(req.ID, OKResult{OK: true}, nil); err != nil {
		return err
	}
	s.notifyQueuedTurnsDequeued(threadID, discardedQueueIDs)
	return nil
}

// threadAgentControlLocked returns the thread's orchestration control.
// Caller holds th.mu.
func threadAgentControlLocked(th *threadState) *agentcontrol.AgentControl {
	if th == nil || th.execRuntime == nil {
		return nil
	}
	return th.execRuntime.AgentControl
}

// foldFrozenWorkerTree lifts a turn/interrupt freeze for this user turn: it
// takes the held synthetic completion turns, settles the control's frozen
// results, and stages the whole-tree snapshot as request-only context plus
// the answered-marking result ids. Workers are not restarted; the root
// resumes selected ones with send_message.
func (s *Server) foldFrozenWorkerTree(th *threadState, threadRuntime *runtime.ThreadRuntime) {
	if s == nil || th == nil {
		return
	}
	th.mu.Lock()
	frozen := th.workerTreeFrozen
	th.workerTreeFrozen = false
	control := threadAgentControlLocked(th)
	th.mu.Unlock()
	if !frozen {
		return
	}
	if control == nil && threadRuntime != nil {
		control = threadRuntime.AgentControl
	}
	pending := s.takePendingAgentCompletionTurns(th.ID)
	var frozenResults []agentcontrol.FrozenWorkerResult
	var workers []subagent.SubAgentSnapshot
	if control != nil {
		frozenResults = control.ResolveFrozenWorkerTree()
		if manager := control.Manager(); manager != nil {
			workers = manager.List()
		}
	}
	resultIDs := make([]string, 0, len(pending))
	for _, turn := range pending {
		if id := strings.TrimSpace(turn.resultID); id != "" {
			resultIDs = append(resultIDs, id)
		}
	}
	block := frozenWorkerTreeBlock(pending, frozenResults, workers)
	th.mu.Lock()
	th.frozenTreeContext = agent.RequestOnlyContextBlocks([]wuucontext.Block{block})
	th.frozenTreeResultIDs = resultIDs
	th.mu.Unlock()
}

const frozenTreeResultTextLimit = 2000

// frozenWorkerTreeBlock renders the whole-tree status snapshot the root
// receives after an interrupt: completed results, cancelled workers' partial
// results, and resume hints.
func frozenWorkerTreeBlock(pending []agentCompletionTurn, frozen []agentcontrol.FrozenWorkerResult, workers []subagent.SubAgentSnapshot) wuucontext.Block {
	var b strings.Builder
	b.WriteString("The previous turn was interrupted and its whole anonymous-worker tree was frozen: running workers were cancelled with their partial results preserved, and queued spawns were cleared. Nothing restarts automatically. Review the tree status below; resume a specific worker with send_message when its work should continue.\n")
	seen := map[string]struct{}{}
	writeResult := func(id, task, status, result, errText string) {
		if id == "" {
			return
		}
		if _, dup := seen[id]; dup {
			return
		}
		seen[id] = struct{}{}
		b.WriteString("\n- ")
		b.WriteString(id)
		if task != "" {
			b.WriteString(" (")
			b.WriteString(task)
			b.WriteString(")")
		}
		b.WriteString(": ")
		b.WriteString(status)
		if errText != "" {
			b.WriteString(" — error: ")
			b.WriteString(errText)
		}
		if result != "" {
			if len(result) > frozenTreeResultTextLimit {
				result = result[:frozenTreeResultTextLimit] + "… (truncated)"
			}
			b.WriteString("\n  result: ")
			b.WriteString(result)
		}
		switch status {
		case string(subagent.StatusCancelled), string(subagent.StatusInterrupted):
			b.WriteString("\n  resumable: send_message to this worker continues from its preserved state")
		}
	}
	for _, turn := range pending {
		if turn.snapshot == nil {
			continue
		}
		snap := turn.snapshot
		errText := ""
		if snap.Error != nil {
			errText = snap.Error.Error()
		}
		writeResult(snap.ID, snap.TaskName, string(snap.Status), snap.Result, errText)
	}
	for _, fr := range frozen {
		errText := ""
		if fr.Snapshot.Error != nil {
			errText = fr.Snapshot.Error.Error()
		}
		writeResult(fr.Snapshot.ID, fr.Snapshot.TaskName, string(fr.Snapshot.Status), fr.Snapshot.Result, errText)
	}
	for _, snap := range workers {
		errText := ""
		if snap.Error != nil {
			errText = snap.Error.Error()
		}
		writeResult(snap.ID, snap.TaskName, string(snap.Status), snap.Result, errText)
	}
	return wuucontext.Block{
		Kind:    wuucontext.BlockTaskState,
		Title:   "Frozen worker tree",
		Source:  "turn-interrupt",
		Content: b.String(),
	}
}

func (s *Server) runTurn(ctx context.Context, th *threadState, threadRuntime *runtime.ThreadRuntime, turnID string, turnRuntime turnRuntimeSnapshot, history []providers.ChatMessage) {
	s.runTurnWithRequestContext(ctx, th, threadRuntime, turnID, turnRuntime, history, nil, nil)
}

func turnRuntimeSnapshotLocked(th *threadState) turnRuntimeSnapshot {
	if th == nil {
		return turnRuntimeSnapshot{}
	}
	return turnRuntimeSnapshot{
		ProviderName: th.ModelProvider,
		Model:        th.Model,
	}
}

func (snapshot turnRuntimeSnapshot) permissions() config.ResolvedPermissions {
	return normalizeTurnPermissions(config.ResolvedPermissions{Mode: snapshot.PermissionMode})
}

func (snapshot turnRuntimeSnapshot) withPermissions(permissions config.ResolvedPermissions) turnRuntimeSnapshot {
	permissions = normalizeTurnPermissions(permissions)
	snapshot.PermissionMode = permissions.Mode
	return snapshot
}

func (snapshot turnRuntimeSnapshot) hasPermissions() bool {
	return strings.TrimSpace(snapshot.PermissionMode) != ""
}

func normalizeTurnPermissions(permissions config.ResolvedPermissions) config.ResolvedPermissions {
	return config.ResolvedPermissions{Mode: config.NormalizePermissionMode(permissions.Mode)}
}

func (s *Server) resolveTurnPermissions(permissionMode *string) (config.ResolvedPermissions, error) {
	if permissionMode != nil {
		return config.ResolvedPermissions{Mode: config.NormalizePermissionMode(*permissionMode)}, nil
	}
	if s != nil && s.rt != nil {
		return normalizeTurnPermissions(s.rt.Permissions), nil
	}
	return normalizeTurnPermissions(config.ResolvedPermissions{}), nil
}

func usageContextWindowTokens(runner *agent.StreamRunner) int {
	if runner == nil {
		return 0
	}
	if runner.MaxInputTokens > 0 && (runner.ContextWindowOverride <= 0 || runner.MaxInputTokens < runner.ContextWindowOverride) {
		return runner.MaxInputTokens
	}
	if runner.ContextWindowOverride > 0 {
		return runner.ContextWindowOverride
	}
	return 0
}

func (s *Server) runTurnWithRequestContext(ctx context.Context, th *threadState, threadRuntime *runtime.ThreadRuntime, turnID string, turnRuntime turnRuntimeSnapshot, history []providers.ChatMessage, requestContext []agent.ContextSegment, residentEnvelopes []MessageEnvelope) {
	notify := func(method string, params any) {
		_ = s.writeNotification(method, params)
	}
	// Batches built under the threadState lock flush through the server's
	// group-broadcast outlet so a thread/updated snapshot in the batch is
	// re-wrapped with group Members (see notifyOutboundBatch).
	notifyBatch := s.notifyOutboundBatch
	runner := s.rt.StreamRunner
	if threadRuntime != nil && threadRuntime.StreamRunner != nil {
		runner = threadRuntime.StreamRunner
	}
	residentParticipantID := ""
	turnWorktreePath := ""
	var frozenTreeContext []agent.ContextSegment
	if th != nil {
		th.mu.Lock()
		residentParticipantID = strings.TrimSpace(th.DMParticipantID)
		turnWorktreePath = strings.TrimSpace(th.WorktreePath)
		frozenTreeContext = th.frozenTreeContext
		th.frozenTreeContext = nil
		th.mu.Unlock()
	}
	if len(frozenTreeContext) > 0 {
		requestContext = append(append([]agent.ContextSegment(nil), requestContext...), frozenTreeContext...)
	}
	baseTurnTools := runner.Tools
	baseForceInitialCompact := runner.ForceInitialCompact
	baseCompactOnly := runner.CompactOnly
	baseBeforeStep := runner.BeforeStep
	baseBeforeRequestContext := runner.BeforeRequestContext
	baseOnRequestContext := runner.OnRequestContext
	baseOnCompactAttempt := runner.OnCompactAttempt
	baseOnToolBatchRejected := runner.OnToolBatchRejected
	baseOnUsage := runner.OnUsage
	baseOnTokenUsage := runner.OnTokenUsage
	var restoreRunnerOnce sync.Once
	restoreRunner := func() {
		restoreRunnerOnce.Do(func() {
			runner.Tools = baseTurnTools
			runner.ForceInitialCompact = baseForceInitialCompact
			runner.CompactOnly = baseCompactOnly
			runner.BeforeStep = baseBeforeStep
			runner.BeforeRequestContext = baseBeforeRequestContext
			runner.OnRequestContext = baseOnRequestContext
			runner.OnCompactAttempt = baseOnCompactAttempt
			runner.OnToolBatchRejected = baseOnToolBatchRejected
			runner.OnUsage = baseOnUsage
			runner.OnTokenUsage = baseOnTokenUsage
		})
	}
	defer restoreRunner()
	if s.taskLeadManagementTurn(residentParticipantID, residentEnvelopes) {
		runner.Tools = allowlistedToolExecutor{base: baseTurnTools, allowed: taskLeadManagementTools}
	}
	// Fork-to-worktree step 5: bind the thread's isolated checkout into the
	// tool execution context. All turn variants funnel through here, so a
	// worktree-bound thread's file/shell tools switch their execution CWD to
	// the checkout (after their ordinary sandbox checks) even when the
	// runtime happens to be rooted at the parent repo.
	if turnWorktreePath != "" {
		ctx = toolctx.WithWorktreePath(ctx, turnWorktreePath)
	}
	turnPermissions := turnRuntime.permissions()
	turnRuntime = turnRuntime.withPermissions(turnPermissions)
	// Publish the turn's effective Ultra value to the orchestration control
	// before the loop can execute spawn_agent, and inject the root policy as
	// request-only context. Workers snapshot this value at spawn, so the
	// whole subtree stays on the admitted capability.
	if threadRuntime != nil && threadRuntime.AgentControl != nil {
		threadRuntime.AgentControl.SetTurnUltra(turnRuntime.Ultra)
	} else if s.rt != nil && s.rt.AgentControl != nil {
		s.rt.AgentControl.SetTurnUltra(turnRuntime.Ultra)
	}
	if turnRuntime.Ultra {
		requestContext = append(append([]agent.ContextSegment(nil), requestContext...), agent.RequestOnlyContextBlockSegment([]wuucontext.Block{{
			Kind:    wuucontext.BlockToolPolicy,
			Title:   "Ultra mode",
			Source:  "ultra",
			Content: agentcontrol.UltraRootPolicy(s.rt.MaxParallel()),
		}}))
	}
	// Assigned unconditionally every turn: the runner is per-thread and
	// long-lived, so a /compact turn must not leave the force flag armed
	// for the turns that follow it.
	runner.ForceInitialCompact = turnRuntime.ForceCompact
	runner.CompactOnly = turnRuntime.CompactOnly
	if threadRuntime != nil && threadRuntime.Toolkit != nil {
		runtime.ConfigureToolkitPermissions(threadRuntime.Toolkit, turnPermissions)
	}
	// Resolve the real runtime context ceiling for the active provider/model
	// so turn/usage notifications can drive a "已用 / 总数" meter in the UI.
	// Captured once per turn: the runner is per-thread for its lifetime,
	// so the model identity does not change between usage samples.
	contextWindowTokens := usageContextWindowTokens(runner)
	toolRecordStart := 0
	if threadRuntime != nil && threadRuntime.Toolkit != nil {
		toolRecordStart = len(threadRuntime.Toolkit.ToolTelemetry())
	}
	// Forward provider-reported token usage into throttled "turn/usage"
	// notifications so live UIs can render a real token-speed gauge when the
	// provider exposes stream-time cumulative usage. We keep completed calls
	// separate from the in-flight call because stream usage snapshots are
	// cumulative for the current provider request, not deltas.
	const usageNotifyInterval = 100 * time.Millisecond
	var usagePushMu sync.Mutex
	var lastUsagePushAt time.Time
	var lastUsagePushed providers.TokenUsage
	var completedUsage providers.TokenUsage
	var liveUsage providers.TokenUsage
	addUsage := func(a, b providers.TokenUsage) providers.TokenUsage {
		return providers.TokenUsage{
			InputTokens:         a.InputTokens + b.InputTokens,
			OutputTokens:        a.OutputTokens + b.OutputTokens,
			CacheCreationTokens: a.CacheCreationTokens + b.CacheCreationTokens,
			CacheReadTokens:     a.CacheReadTokens + b.CacheReadTokens,
		}
	}
	notifyUsage := func(snapshot providers.TokenUsage, force bool) {
		now := time.Now()
		usagePushMu.Lock()
		// The stream's final EventUsage and the loop's per-call
		// OnTokenUsage both report the same cumulative totals; pushing
		// an unchanged snapshot again is pure noise for consumers.
		if snapshot == lastUsagePushed && !lastUsagePushAt.IsZero() {
			usagePushMu.Unlock()
			return
		}
		shouldPush := force || lastUsagePushAt.IsZero() || now.Sub(lastUsagePushAt) >= usageNotifyInterval
		if shouldPush {
			lastUsagePushAt = now
			lastUsagePushed = snapshot
		}
		usagePushMu.Unlock()
		if !shouldPush {
			return
		}
		notify(NotificationTurnUsage, TurnUsageNotification{
			ThreadID:            th.ID,
			TurnID:              turnID,
			Model:               runner.Model,
			InputTokens:         snapshot.InputTokens,
			OutputTokens:        snapshot.OutputTokens,
			CacheCreationTokens: snapshot.CacheCreationTokens,
			CacheReadTokens:     snapshot.CacheReadTokens,
			ContextWindowTokens: contextWindowTokens,
		})
	}
	runner.OnUsage = func(inputTokens, outputTokens int) {
		if baseOnUsage != nil {
			baseOnUsage(inputTokens, outputTokens)
		}
	}
	var contextRequests []sessiontrace.RequestContextRecord
	var providerStates []sessiontrace.ProviderStateRecord
	var compactAttempts []sessiontrace.CompactRecord
	var barrierRejections []sessiontrace.BarrierToolBatchRejectionRecord
	runner.OnTokenUsage = func(usage providers.TokenUsage) {
		if baseOnTokenUsage != nil {
			baseOnTokenUsage(usage)
		}
		attachUsageToLatestRequestContext(contextRequests, usage)
		usagePushMu.Lock()
		completedUsage = addUsage(completedUsage, usage)
		liveUsage = providers.TokenUsage{}
		usageSnapshot := completedUsage
		usagePushMu.Unlock()
		notifyUsage(usageSnapshot, true)
	}
	runner.OnRequestContext = func(info agent.RequestContextInfo) {
		if baseOnRequestContext != nil {
			baseOnRequestContext(info)
		}
		contextRequests = append(contextRequests, sessiontrace.RequestContextRecord{
			StepIndex:                info.StepIndex,
			TransientMessages:        info.TransientMessages,
			ContentBytes:             info.ContentBytes,
			BlockKinds:               append([]string(nil), info.BlockKinds...),
			BlockKindCounts:          cloneStringIntMap(info.BlockKindCounts),
			BlockKindBytes:           cloneStringIntMap(info.BlockKindBytes),
			SegmentLifecycleCounts:   cloneStringIntMap(info.SegmentLifecycleCounts),
			SegmentPlacementCounts:   cloneStringIntMap(info.SegmentPlacementCounts),
			SegmentCachePolicyCounts: cloneStringIntMap(info.SegmentCachePolicyCounts),
			MessageCount:             info.MessageCount,
			SystemMessages:           info.SystemMessages,
			HiddenMessages:           info.HiddenMessages,
			ToolCount:                info.ToolCount,
			StablePrefix:             info.StablePrefix,
			TurnPrefix:               info.TurnPrefix,
			DynamicBytes:             info.DynamicBytes,
			SystemBytes:              info.SystemBytes,
			StablePrefixBytes:        info.StablePrefixBytes,
			TurnPrefixBytes:          info.TurnPrefixBytes,
			MessageBytes:             info.MessageBytes,
			ToolSchemaBytes:          info.ToolSchemaBytes,
			LoadableToolCount:        info.LoadableToolCount,
			LoadableToolSchemaBytes:  info.LoadableToolSchemaBytes,
			LoadableToolSurfaceHash:  info.LoadableToolSurfaceHash,
			SystemHash:               info.SystemHash,
			StablePrefixHash:         info.StablePrefixHash,
			TurnPrefixHash:           info.TurnPrefixHash,
			ToolSurfaceHash:          info.ToolSurfaceHash,
			PromptCacheKey:           info.PromptCacheKey,
			InputTokens:              info.InputTokens,
			OutputTokens:             info.OutputTokens,
			CacheCreationTokens:      info.CacheCreationTokens,
			CacheReadTokens:          info.CacheReadTokens,
			SystemSections:           requestContextSystemSections(info.SystemSections),
		})
	}
	runner.OnCompactAttempt = func(info agent.CompactAttemptInfo) {
		if baseOnCompactAttempt != nil {
			baseOnCompactAttempt(info)
		}
		compactAttempts = append(compactAttempts, compactRecord(info))
	}
	runner.OnToolBatchRejected = func(info agent.ToolBatchRejectionInfo) {
		if baseOnToolBatchRejected != nil {
			baseOnToolBatchRejected(info)
		}
		barrierRejections = append(barrierRejections, barrierToolBatchRejectionRecord(info))
	}
	runner.BeforeStep = func() []providers.ChatMessage {
		var messages []providers.ChatMessage
		if baseBeforeStep != nil {
			messages = append(messages, baseBeforeStep()...)
		}
		th.mu.Lock()
		steers, batch := th.takePendingSteersLocked(turnID, time.Now().UTC())
		th.mu.Unlock()
		notifyBatch(batch)
		if len(steers) > 0 {
			messages = append(messages, steers...)
		}
		return messages
	}
	turnRequestContext := cloneContextSegments(requestContext)
	runner.BeforeRequestContext = func() []agent.ContextSegment {
		var segments []agent.ContextSegment
		if baseBeforeRequestContext != nil {
			segments = append(segments, baseBeforeRequestContext()...)
		}
		if len(turnRequestContext) > 0 {
			segments = append(segments, cloneContextSegments(turnRequestContext)...)
		}
		return segments
	}
	// Node liveness wiring (plan §T9): if this resident turn was dispatched to
	// run a plan node, resolve that node ONCE now (not per stream event). A tool
	// call during the turn then refreshes the node's LastActivityAt (activity =
	// "still alive"), kept strictly distinct from progress ("real headway").
	// resolveExecutingNode returns ok=false for any non-node turn (ordinary
	// DM/chat, a lead's planning/wrap-up wake), so the activity hook below no-ops
	// there.
	nodeTaskID, nodePieceID, nodeAttemptID, nodeExecuting := "", "", "", false
	if residentParticipantID != "" && len(residentEnvelopes) > 0 {
		nodeTaskID, nodePieceID, nodeAttemptID, nodeExecuting = s.resolveExecutingAttempt(residentParticipantID, residentEnvelopes)
	}
	res, err := runner.RunWithCallback(ctx, history, func(ev providers.StreamEvent) {
		if ev.Type == providers.EventUsage && ev.Usage != nil {
			usagePushMu.Lock()
			liveUsage = *ev.Usage
			usageSnapshot := addUsage(completedUsage, liveUsage)
			usagePushMu.Unlock()
			notifyUsage(usageSnapshot, false)
		}
		if ev.Type == providers.EventProviderState && ev.ProviderState != nil {
			providerStates = append(providerStates, providerStateRecord(ev.ProviderState))
		}
		th.mu.Lock()
		batch := th.applyStreamEventLocked(turnID, ev, time.Now().UTC())
		th.mu.Unlock()
		// Node liveness (plan §T9): every observable action by the assignee is an
		// activity ("still alive") signal that refreshes the executing node's
		// LastActivityAt — a tool call, its result, and assistant commentary —
		// each kept strictly apart from progress ("real headway", noteNodeProgress).
		// This runtime fuses a tool call and its result into one EventToolUseEnd
		// (the call plus ev.ToolResult), and streams commentary as content deltas
		// plus a per-message EventMessage; we take the per-message event as the
		// commentary signal so the trace is one entry per narration, not one per
		// delta. Do the store writes AFTER releasing th.mu — noteNodeActivity
		// acquires the store write lock, which must never nest under th.mu. The
		// guard no-ops on empty ids, so an ordinary DM/chat turn (which resolves no
		// executing node) is untouched.
		if nodeExecuting {
			switch ev.Type {
			case providers.EventToolUseEnd:
				if ev.ToolCall != nil {
					s.noteNodeActivity(nodeTaskID, nodePieceID, session.TaskEventToolCall, strings.TrimSpace(ev.ToolCall.Name), nodeAttemptID)
				}
				if strings.TrimSpace(ev.ToolResult) != "" {
					toolName := ""
					if ev.ToolCall != nil {
						toolName = strings.TrimSpace(ev.ToolCall.Name)
					}
					s.noteNodeActivity(nodeTaskID, nodePieceID, session.TaskEventToolResult, toolName, nodeAttemptID)
				}
			case providers.EventMessage:
				if ev.Message != nil && ev.Message.Role == "assistant" && strings.TrimSpace(ev.Message.Content) != "" {
					s.noteNodeActivity(nodeTaskID, nodePieceID, session.TaskEventCommentary, commentaryTraceSummary(ev.Message.Content), nodeAttemptID)
				}
			}
		}
		notifyBatch(batch)
		notify(NotificationTurnEvent, TurnEventNotification{
			ThreadID: th.ID,
			TurnID:   turnID,
			Event:    sanitizeStreamEvent(ev),
		})
	})
	// RunWithCallback has consumed every per-turn callback. Restore the
	// long-lived runner before the thread can become idle and admit another
	// turn; the deferred call remains as panic-safe cleanup.
	restoreRunner()

	now := time.Now().UTC()
	completionAnswerReady := false
	if err == nil && len(turnRuntime.AgentCompletionResultIDs) > 0 {
		completionAnswerReady = markAgentCompletionAnswer(&res, turnRuntime.AgentCompletionResultIDs)
	}
	processCompletionAnswerReady := false
	if err == nil && len(turnRuntime.ProcessCompletionIDs) > 0 {
		processCompletionAnswerReady = markProcessCompletionAnswer(&res, turnRuntime.ProcessCompletionIDs)
	}
	th.mu.Lock()
	var historyErr error
	var persistErr error
	// D3: an interrupted turn must persist its partial assistant text and the
	// synthesized aborted tool results the loop already put in NewMessages, not
	// just token usage — otherwise the streamed output the user already saw on
	// screen vanishes on reload, and the disk/memory histories diverge.
	interrupted := errors.Is(err, context.Canceled)
	persistNewMessages := err == nil ||
		(interrupted && len(res.NewMessages) > 0 && th.PersistHistory)
	if persistNewMessages {
		rewriteHistory := res.HistoryRewritten
		if res.HistoryRewritten {
			th.History = cloneHistory(res.NewMessages)
		} else {
			th.History = append(th.History, res.NewMessages...)
		}
		if repaired, nerr := providers.RepairAndValidateToolCallHistory(th.History); nerr != nil {
			historyErr = nerr
		} else if !reflect.DeepEqual(repaired, th.History) {
			th.History = repaired
			rewriteHistory = true
		}
		if historyErr != nil {
			persistErr = historyErr
		} else {
			persistErr = s.persistTurnResultLocked(th, res, rewriteHistory, turnRuntime.ProviderName, turnRuntime.Model, turnRuntime.HistoryBaselineSeq)
		}
	} else if res.HistoryRewritten && len(res.NewMessages) > 0 && th.PersistHistory {
		th.History = cloneHistory(res.NewMessages)
		if repaired, nerr := providers.RepairAndValidateToolCallHistory(th.History); nerr != nil {
			historyErr = nerr
		} else if !reflect.DeepEqual(repaired, th.History) {
			th.History = repaired
		}
		if historyErr != nil {
			persistErr = historyErr
		} else {
			persistErr = s.persistTurnResultLocked(th, res, true, turnRuntime.ProviderName, turnRuntime.Model, turnRuntime.HistoryBaselineSeq)
		}
	} else {
		if usageErr := appendTokenUsage(s.rt.SessionDir, th.ID, turnRuntime.ProviderName, turnRuntime.Model, providers.TokenUsage{
			InputTokens:         res.InputTokens,
			OutputTokens:        res.OutputTokens,
			CacheCreationTokens: res.CacheCreationTokens,
			CacheReadTokens:     res.CacheReadTokens,
		}, res.ContextTokens); usageErr != nil {
			persistErr = usageErr
		} else if th.PersistHistory {
			persistErr = session.UpdateIndex(s.rt.SessionDir, th.ID, persistableMessageCount(th.History), threadPreview(th.History))
		}
	}
	status := TurnStatusCompleted
	if err != nil {
		status = TurnStatusFailed
		if errors.Is(err, context.Canceled) {
			status = TurnStatusInterrupted
		}
	}
	if err == nil && persistErr != nil {
		err = persistErr
		status = TurnStatusFailed
	} else if err != nil && persistErr != nil {
		err = errors.Join(err, persistErr)
	}
	var titleHistory []providers.ChatMessage
	if err == nil {
		titleHistory = cloneHistory(th.History)
	}
	accountingTurn := th.ensureTurnLocked(turnID, now)
	goalUsageDelta := goalUsageDeltaForTurn(accountingTurn, now, res)
	th.mu.Unlock()

	if accountErr := accountActiveGoalTurn(threadRuntime, goalUsageDelta, now); accountErr != nil {
		if err != nil {
			err = errors.Join(err, accountErr)
		} else {
			err = accountErr
			status = TurnStatusFailed
			titleHistory = nil
		}
	}
	if stopErr := stopActiveGoalAfterTurnError(threadRuntime, err, now); stopErr != nil {
		if err != nil {
			err = errors.Join(err, stopErr)
		} else {
			err = stopErr
			status = TurnStatusFailed
			titleHistory = nil
		}
	}

	th.mu.Lock()
	turn := th.finishTurnLocked(turnID, status, err, now, string(res.FinishReason), res.StopReason, res.Truncated)
	applyTokenUsageToTurn(&turn, providers.TokenUsage{
		InputTokens:         res.InputTokens,
		OutputTokens:        res.OutputTokens,
		CacheCreationTokens: res.CacheCreationTokens,
		CacheReadTokens:     res.CacheReadTokens,
	}, res.ContextTokens, turnRuntime.Model)
	th.replaceTurnLocked(turn)
	th.mu.Unlock()

	tracePath, traceErr := s.persistTurnTrace(threadRuntime, runner, th.ID, turnRuntime, turn, res, err, toolRecordStart, contextRequests, providerStates, compactAttempts, barrierRejections)
	if traceErr != nil {
		tracePath = ""
	}

	// Surface the Go core's typed error classification to clients while
	// preserving the raw Error string inside the structured payload.
	var structured *TurnError
	if err != nil {
		value := BuildTurnError(err, turnRuntime.ProviderName)
		structured = &value
		turn.Error = structured
	}
	th.mu.Lock()
	if structured != nil {
		th.replaceTurnLocked(turn)
	}
	// Keep the execution lease through trace persistence and runner cleanup.
	// Applying deferred runtime updates and releasing the lease under the same
	// lock prevents the next turn from observing a half-restored runtime.
	unconsumedSteers := th.drainPendingSteersLocked()
	if len(unconsumedSteers) > 0 {
		s.prependQueuedUserTurns(th.ID, queuedTurnsFromSteers(unconsumedSteers))
	}
	s.applyPendingThreadRuntimeLocked(th)
	completionClaimFailed := len(turnRuntime.AgentCompletionResultIDs) > 0
	if err == nil && completionAnswerReady && persistErr == nil && threadRuntime != nil && threadRuntime.AgentControl != nil {
		completionClaimFailed = false
		for _, resultID := range turnRuntime.AgentCompletionResultIDs {
			resultID = strings.TrimSpace(resultID)
			if resultID == "" {
				continue
			}
			claimed, consumedBy, claimErr := threadRuntime.AgentControl.ClaimAgentResultDeliveryID(resultID, "auto_completion")
			if claimErr != nil {
				completionClaimFailed = true
				providers.DebugLogf("persist completed agent result claim %q for thread %q: %v", resultID, th.ID, claimErr)
				continue
			}
			if !claimed && consumedBy == "" {
				completionClaimFailed = true
				providers.DebugLogf("completed agent result %q disappeared before claim for thread %q", resultID, th.ID)
			}
		}
	}
	processCompletionClaimFailed := len(turnRuntime.ProcessCompletionIDs) > 0
	if err == nil && processCompletionAnswerReady && persistErr == nil && threadRuntime != nil && threadRuntime.ProcessManager != nil {
		processCompletionClaimFailed = false
		for _, processID := range turnRuntime.ProcessCompletionIDs {
			processID = strings.TrimSpace(processID)
			if processID == "" {
				continue
			}
			if _, markErr := threadRuntime.ProcessManager.MarkCompletionDelivered(processID, "auto_completion"); markErr != nil {
				processCompletionClaimFailed = true
				providers.DebugLogf("persist completed process delivery %q for thread %q: %v", processID, th.ID, markErr)
			}
		}
	}
	residentTurn := strings.TrimSpace(residentParticipantID) != ""
	if residentTurn {
		// Receipts, task-attempt settlement, and fallback delivery are part of
		// this turn's durable finalization. Keep the execution lease while they
		// run so another process cannot admit the successor against a half-
		// finalized resident turn. They may take th.mu, so do not hold it here.
		th.mu.Unlock()
		if s.beforeResidentTurnFinalizeForTest != nil {
			s.beforeResidentTurnFinalizeForTest(th.ID)
		}
		s.afterResidentTurn(th, residentParticipantID, residentEnvelopes, turn, now)
		th.mu.Lock()
		th.releaseTurnExecutionLocked(turnID)
		th.mu.Unlock()
	} else {
		th.releaseTurnExecutionLocked(turnID)
		th.mu.Unlock()
	}
	// A terminal notification is the client-visible completion barrier. By the
	// time it is published all durable resident settlement and runtime cleanup
	// is finished, and the execution lease is available for the next request.
	// Local queue drains are kicked only after this write, preserving terminal
	// before successor ordering on this server without making an immediate user
	// turn spuriously fail as busy.
	awaitingAutoContinuation := err == nil && threadRuntime != nil &&
		(threadRuntimeHasOutstandingAgentWork(threadRuntime) ||
			threadHasOutstandingProcessCompletion(th.ID, threadRuntime.AgentControl, threadRuntime.ProcessManager))
	if err != nil {
		notify(NotificationTurnError, TurnErrorNotification{
			ThreadID:   th.ID,
			TurnID:     turnID,
			Error:      structured.Message,
			Code:       structured.Code,
			Category:   string(structured.Category),
			Provider:   structured.Provider,
			StatusCode: structured.StatusCode,
			Turn:       turn,
		})
	} else {
		notify(NotificationTurnCompleted, TurnCompletedNotification{
			ThreadID:                 th.ID,
			Turn:                     turn,
			Content:                  res.Content,
			InputTokens:              res.InputTokens,
			OutputTokens:             res.OutputTokens,
			ContextTokens:            res.ContextTokens,
			CacheCreationTokens:      res.CacheCreationTokens,
			CacheReadTokens:          res.CacheReadTokens,
			FinishReason:             string(res.FinishReason),
			StopReason:               res.StopReason,
			Truncated:                res.Truncated,
			TracePath:                tracePath,
			AwaitingAutoContinuation: awaitingAutoContinuation,
		})
	}
	if residentTurn {
		s.kickResidentAgent(residentParticipantID)
	}
	if completionClaimFailed {
		s.scheduleThreadExecutionLeaseRetry(func() {
			s.replayPendingAgentCompletions(th.ID, threadRuntime)
		})
	}
	if processCompletionClaimFailed {
		s.scheduleThreadExecutionLeaseRetry(func() {
			s.replayPendingProcessCompletions(th.ID, threadRuntime.AgentControl, threadRuntime.ProcessManager)
		})
	}
	if err != nil {
		s.kickAgentCompletionDrain(th.ID)
		s.kickQueuedTurnDrain(th.ID)
		return
	}
	if !turnRuntime.CompactOnly {
		_ = s.startBackground(func() { s.generateThreadTitle(th.ID, titleHistory) })
	}
	s.kickAgentCompletionDrain(th.ID)
	s.kickQueuedTurnDrain(th.ID)
	s.kickGoalContinuation(th.ID)
}

func goalUsageDeltaForTurn(turn Turn, completedAt time.Time, res agent.LoopResult) goalruntime.UsageDelta {
	elapsed := time.Duration(0)
	if turn.StartedAt != nil && completedAt.After(*turn.StartedAt) {
		elapsed = completedAt.Sub(*turn.StartedAt)
	}
	return goalruntime.UsageDelta{
		// Goal budgets track fresh model work, not prompt-cache reads. Cache
		// reads still count toward context-window pressure in the agent loop.
		Tokens:  res.InputTokens + res.OutputTokens,
		Elapsed: elapsed,
		Turns:   1,
	}
}

func accountActiveGoalTurn(threadRuntime *runtime.ThreadRuntime, delta goalruntime.UsageDelta, completedAt time.Time) error {
	if threadRuntime == nil || threadRuntime.GoalRuntime == nil {
		return nil
	}
	if _, _, err := threadRuntime.GoalRuntime.AccountActiveUsage(delta, completedAt); err != nil {
		return fmt.Errorf("account active goal usage: %w", err)
	}
	return nil
}

func stopActiveGoalAfterTurnError(threadRuntime *runtime.ThreadRuntime, turnErr error, completedAt time.Time) error {
	if threadRuntime == nil || threadRuntime.GoalRuntime == nil || turnErr == nil {
		return nil
	}
	if errors.Is(turnErr, context.Canceled) {
		return nil
	}
	goal, err := threadRuntime.GoalRuntime.CurrentGoal()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load active goal after turn error: %w", err)
	}
	if goal.Status != goalruntime.StatusActive {
		return nil
	}
	if _, err := threadRuntime.GoalRuntime.SetSystemStatus(goalruntime.StatusBlocked, completedAt); err != nil {
		return fmt.Errorf("stop active goal after turn error: %w", err)
	}
	return nil
}

func (s *Server) persistTurnTrace(threadRuntime *runtime.ThreadRuntime, runner *agent.StreamRunner, threadID string, turnRuntime turnRuntimeSnapshot, turn Turn, res agent.LoopResult, runErr error, toolRecordStart int, contextRequests []sessiontrace.RequestContextRecord, providerStates []sessiontrace.ProviderStateRecord, compactAttempts []sessiontrace.CompactRecord, barrierRejectionsArg ...[]sessiontrace.BarrierToolBatchRejectionRecord) (string, error) {
	if threadRuntime == nil || threadRuntime.Toolkit == nil {
		return "", nil
	}
	tracePath := sessiontrace.Path(threadRuntime.Toolkit.SessionDir())
	if strings.TrimSpace(tracePath) == "" {
		return "", nil
	}
	providerName := strings.TrimSpace(turnRuntime.ProviderName)
	if s != nil && s.rt != nil {
		providerName = firstNonEmpty(providerName, s.rt.ProviderName)
	}
	permissions := turnRuntime.permissions()
	model := ""
	apiModel := ""
	if runner != nil {
		model = runner.Model
		apiModel = runner.APIModel
	}
	modelBudget := threadRuntime.ModelBudget
	errorText := ""
	if runErr != nil {
		errorText = runErr.Error()
	}
	turnRecord := sessiontrace.TurnRecord{
		ThreadID:            threadID,
		TurnID:              turn.ID,
		Status:              string(turn.Status),
		ProviderName:        providerName,
		Model:               model,
		APIModel:            apiModel,
		ModelProfile:        sessiontrace.NewModelProfileRecordWithBudget(providerName, model, apiModel, modelBudget),
		PermissionMode:      permissions.Mode,
		StartedAt:           turn.StartedAt,
		CompletedAt:         turn.CompletedAt,
		DurationMS:          turn.DurationMS,
		InputTokens:         res.InputTokens,
		OutputTokens:        res.OutputTokens,
		CacheCreationTokens: res.CacheCreationTokens,
		CacheReadTokens:     res.CacheReadTokens,
		FinishReason:        string(res.FinishReason),
		StopReason:          res.StopReason,
		Truncated:           res.Truncated,
		HistoryRewritten:    res.HistoryRewritten,
		Error:               errorText,
	}
	finalRecord := sessiontrace.FinalRecord{
		Status:              string(turn.Status),
		InputTokens:         res.InputTokens,
		OutputTokens:        res.OutputTokens,
		CacheCreationTokens: res.CacheCreationTokens,
		CacheReadTokens:     res.CacheReadTokens,
		FinishReason:        string(res.FinishReason),
		StopReason:          res.StopReason,
		Truncated:           res.Truncated,
		FinalAnswerPreview:  res.Content,
		Error:               errorText,
	}
	records := threadRuntime.Toolkit.ToolTelemetry()
	if toolRecordStart > 0 && toolRecordStart < len(records) {
		records = records[toolRecordStart:]
	} else if toolRecordStart >= len(records) {
		records = nil
	}
	var barrierRejections []sessiontrace.BarrierToolBatchRejectionRecord
	if len(barrierRejectionsArg) > 0 {
		barrierRejections = barrierRejectionsArg[0]
	}
	if err := sessiontrace.AppendTurn(tracePath, turnRecord, finalRecord, threadRuntime.Toolkit.ToolInfos(), records, contextRequests, providerStates, compactAttempts, barrierRejections); err != nil {
		return "", err
	}
	return tracePath, nil
}

func compactRecord(info agent.CompactAttemptInfo) sessiontrace.CompactRecord {
	return sessiontrace.CompactRecord{
		Reason:                        string(info.Reason),
		Status:                        string(info.Status),
		TokensBefore:                  info.TokensBefore,
		MessagesBefore:                info.MessagesBefore,
		MessagesAfter:                 info.MessagesAfter,
		AnchorID:                      info.AnchorID,
		MessagesRemoved:               info.MessagesRemoved,
		AnchorDistance:                info.AnchorDistance,
		PreservedUserMessages:         info.PreservedUserMessages,
		PreservedUserMessageBytes:     info.PreservedUserMessageBytes,
		PreservedUserSuffixStartIndex: info.PreservedUserSuffixStartIndex,
		SummaryBytes:                  info.SummaryBytes,
		Error:                         info.Error,
	}
}

func barrierToolBatchRejectionRecord(info agent.ToolBatchRejectionInfo) sessiontrace.BarrierToolBatchRejectionRecord {
	return sessiontrace.BarrierToolBatchRejectionRecord{
		StepIndex:     info.StepIndex,
		BarrierTool:   info.BarrierTool,
		SiblingTools:  append([]string(nil), info.SiblingTools...),
		ToolCallCount: info.ToolCallCount,
	}
}

func providerStateRecord(state *providers.ProviderStateSummary) sessiontrace.ProviderStateRecord {
	if state == nil {
		return sessiontrace.ProviderStateRecord{}
	}
	return sessiontrace.ProviderStateRecord{
		StepIndex:              state.StepIndex,
		Provider:               state.Provider,
		Protocol:               state.Protocol,
		Transport:              state.Transport,
		ReplayMode:             state.ReplayMode,
		PreviousResponseIDUsed: state.PreviousResponseIDUsed,
		ConnectionReused:       state.ConnectionReused,
		Diagnostic:             state.Diagnostic,
		TransportFailurePhase:  state.TransportFailurePhase,
		FallbackTransport:      state.FallbackTransport,
		EventsEmitted:          state.EventsEmitted,
		FallbackActive:         state.FallbackActive,
		FallbackReason:         state.FallbackReason,
		InputItems:             state.InputItems,
		FullInputItems:         state.FullInputItems,
		DeltaInputItems:        state.DeltaInputItems,
	}
}

func attachUsageToLatestRequestContext(records []sessiontrace.RequestContextRecord, usage providers.TokenUsage) {
	if len(records) == 0 {
		return
	}
	record := &records[len(records)-1]
	record.InputTokens = usage.InputTokens
	record.OutputTokens = usage.OutputTokens
	record.CacheCreationTokens = usage.CacheCreationTokens
	record.CacheReadTokens = usage.CacheReadTokens
}

func (s *Server) enqueueAgentCompletionTurn(threadID, agentID, resultID string, msg providers.ChatMessage, snapshot *subagent.SubAgentSnapshot) {
	if s == nil || s.closed.Load() {
		return
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" || !chatMessageHasUserPayload(msg) {
		return
	}
	th := s.thread(threadID)
	if th == nil || !canResumeAgentCompletionThread(th) {
		return
	}
	if strings.TrimSpace(msg.Role) == "" {
		msg.Role = "user"
	}

	s.agentCompletionMu.Lock()
	if s.closed.Load() {
		s.agentCompletionMu.Unlock()
		return
	}
	if s.pendingAgentCompletionTurns == nil {
		s.pendingAgentCompletionTurns = make(map[string][]agentCompletionTurn)
	}
	resultID = strings.TrimSpace(resultID)
	if resultID != "" {
		for _, pending := range s.pendingAgentCompletionTurns[threadID] {
			if strings.TrimSpace(pending.resultID) == resultID {
				s.agentCompletionMu.Unlock()
				return
			}
		}
	}
	s.pendingAgentCompletionTurns[threadID] = append(s.pendingAgentCompletionTurns[threadID], agentCompletionTurn{
		agentID:  strings.TrimSpace(agentID),
		resultID: resultID,
		msg:      msg,
		snapshot: cloneSubAgentSnapshot(snapshot),
	})
	s.agentCompletionMu.Unlock()

	s.kickAgentCompletionDrain(threadID)
}

func (s *Server) enqueueQueuedUserTurn(threadID string, entry queuedTurn) {
	if s == nil || s.closed.Load() {
		return
	}
	threadID = strings.TrimSpace(threadID)
	entry.id = strings.TrimSpace(entry.id)
	if threadID == "" || entry.id == "" || !chatMessageHasUserPayload(entry.msg) {
		return
	}
	if strings.TrimSpace(entry.msg.Role) == "" {
		entry.msg.Role = "user"
	}
	entry.msg.ClientID = entry.id
	entry.msg.Steered = false

	s.queuedTurnMu.Lock()
	if s.closed.Load() {
		s.queuedTurnMu.Unlock()
		return
	}
	if s.pendingQueuedTurns == nil {
		s.pendingQueuedTurns = make(map[string][]queuedTurn)
	}
	s.pendingQueuedTurns[threadID] = append(s.pendingQueuedTurns[threadID], entry)
	s.queuedTurnMu.Unlock()
}

func (s *Server) removeQueuedUserTurn(threadID, queueID string) bool {
	threadID = strings.TrimSpace(threadID)
	queueID = strings.TrimSpace(queueID)
	if threadID == "" || queueID == "" {
		return false
	}
	s.queuedTurnMu.Lock()
	defer s.queuedTurnMu.Unlock()
	pending := s.pendingQueuedTurns[threadID]
	next := pending[:0]
	removed := false
	for _, entry := range pending {
		if !removed && entry.id == queueID {
			removed = true
			continue
		}
		next = append(next, entry)
	}
	if len(next) == 0 {
		delete(s.pendingQueuedTurns, threadID)
	} else {
		s.pendingQueuedTurns[threadID] = next
	}
	return removed
}

func (s *Server) replaceQueuedUserTurn(threadID, queueID string, msg providers.ChatMessage) (queuedTurn, bool) {
	threadID = strings.TrimSpace(threadID)
	queueID = strings.TrimSpace(queueID)
	if threadID == "" || queueID == "" || !chatMessageHasUserPayload(msg) {
		return queuedTurn{}, false
	}
	if strings.TrimSpace(msg.Role) == "" {
		msg.Role = "user"
	}
	msg.ClientID = queueID
	msg.Steered = false

	s.queuedTurnMu.Lock()
	defer s.queuedTurnMu.Unlock()
	pending := s.pendingQueuedTurns[threadID]
	for index, entry := range pending {
		if entry.id != queueID {
			continue
		}
		updated := queuedTurn{id: queueID, msg: msg, snapshot: entry.snapshot}
		pending[index] = updated
		s.pendingQueuedTurns[threadID] = pending
		return updated, true
	}
	return queuedTurn{}, false
}

func (s *Server) kickQueuedTurnDrain(threadID string) {
	if s == nil || s.closed.Load() {
		return
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return
	}

	s.queuedTurnMu.Lock()
	if s.closed.Load() {
		s.queuedTurnMu.Unlock()
		return
	}
	if len(s.pendingQueuedTurns[threadID]) == 0 || s.drainingQueuedTurns[threadID] {
		s.queuedTurnMu.Unlock()
		return
	}
	if s.drainingQueuedTurns == nil {
		s.drainingQueuedTurns = make(map[string]bool)
	}
	s.drainingQueuedTurns[threadID] = true
	s.queuedTurnMu.Unlock()

	_ = s.startBackground(func() { s.drainQueuedTurns(threadID) })
}

func (s *Server) drainQueuedTurns(threadID string) {
	if s == nil {
		return
	}
	if s.closed.Load() {
		s.discardQueuedUserTurns(threadID)
		s.clearQueuedTurnDrain(threadID)
		return
	}
	th := s.thread(threadID)
	if th == nil {
		s.discardQueuedUserTurns(threadID)
		s.clearQueuedTurnDrain(threadID)
		return
	}
	if threadIsRunning(th) {
		s.clearQueuedTurnDrain(threadID)
		return
	}

	entry, ok := s.takeNextQueuedUserTurn(threadID)
	if !ok {
		s.clearQueuedTurnDrain(threadID)
		return
	}
	started, err := s.startQueuedTurn(context.Background(), threadID, entry)
	executionBusy := errors.Is(err, errThreadExecutionBusy)
	if err != nil && !executionBusy {
		providers.DebugLogf("start queued turn for thread %q: %v", threadID, err)
	}
	requeued := false
	if !started && (err == nil || executionBusy) {
		s.prependQueuedUserTurns(threadID, []queuedTurn{entry})
		requeued = true
	}
	s.clearQueuedTurnDrain(threadID)
	if requeued && executionBusy {
		s.scheduleThreadExecutionLeaseRetry(func() { s.kickQueuedTurnDrain(threadID) })
		return
	}
	if requeued || s.hasQueuedUserTurns(threadID) {
		s.kickQueuedTurnDrain(threadID)
	}
}

func (s *Server) startThreadUserTurn(ctx context.Context, th *threadState, userMsg providers.ChatMessage, snapshot turnRuntimeSnapshot, failIfRunning bool, readOnlyPolicy turnReadOnlyPolicy) (startedThreadTurn, bool, error) {
	return s.startThreadUserTurnWithAdmission(ctx, th, userMsg, snapshot, failIfRunning, readOnlyPolicy, turnAdmissionHooks{})
}

// startThreadUserTurnWithAdmission owns every durable pre-turn side effect.
// It acquires the cross-process lease and refreshes disk state before running
// hooks, then keeps ownership through the user append and turn lifecycle.
func (s *Server) startThreadUserTurnWithAdmission(ctx context.Context, th *threadState, userMsg providers.ChatMessage, snapshot turnRuntimeSnapshot, failIfRunning bool, readOnlyPolicy turnReadOnlyPolicy, hooks turnAdmissionHooks) (startedThreadTurn, bool, error) {
	if th == nil {
		return startedThreadTurn{}, false, errors.New("thread is required")
	}
	if s == nil || s.closed.Load() {
		return startedThreadTurn{}, false, errServerClosed
	}
	if strings.TrimSpace(userMsg.Role) == "" {
		userMsg.Role = "user"
	}
	if !chatMessageHasUserPayload(userMsg) {
		return startedThreadTurn{}, false, nil
	}
	turnID := session.NewID()
	turnCtx, cancel := context.WithCancel(ctx)
	now := time.Now().UTC()

	th.mu.Lock()
	if s.closed.Load() {
		th.mu.Unlock()
		cancel()
		return startedThreadTurn{}, false, errServerClosed
	}
	if th.running {
		th.mu.Unlock()
		cancel()
		if failIfRunning {
			return startedThreadTurn{}, false, fmt.Errorf("thread %q already has a running turn", th.ID)
		}
		return startedThreadTurn{}, false, nil
	}
	if th.ReadOnly {
		switch readOnlyPolicy {
		case turnReadOnlyFail:
			th.mu.Unlock()
			cancel()
			return startedThreadTurn{}, false, errors.New("thread is read-only")
		case turnReadOnlySkip:
			th.mu.Unlock()
			cancel()
			return startedThreadTurn{}, false, nil
		}
	}
	acquired, err := s.tryAcquireThreadExecutionLeaseLocked(th)
	if err != nil {
		th.mu.Unlock()
		cancel()
		return startedThreadTurn{}, false, err
	}
	if !acquired {
		threadID := th.ID
		th.mu.Unlock()
		cancel()
		return startedThreadTurn{}, false, threadExecutionBusyError(threadID)
	}
	if err := s.refreshDurableThreadHistoryLocked(th); err != nil {
		th.releaseThreadExecutionLeaseLocked()
		th.mu.Unlock()
		cancel()
		return startedThreadTurn{}, false, err
	}
	if th.ReadOnly {
		switch readOnlyPolicy {
		case turnReadOnlyFail:
			th.releaseThreadExecutionLeaseLocked()
			th.mu.Unlock()
			cancel()
			return startedThreadTurn{}, false, errors.New("thread is read-only")
		case turnReadOnlySkip:
			th.releaseThreadExecutionLeaseLocked()
			th.mu.Unlock()
			cancel()
			return startedThreadTurn{}, false, nil
		}
	}
	th.mu.Unlock()

	abortAdmission := func() {
		th.mu.Lock()
		th.releaseThreadExecutionLeaseLocked()
		th.mu.Unlock()
		cancel()
	}
	if hooks.afterLease != nil {
		if err := hooks.afterLease(th, &userMsg); err != nil {
			abortAdmission()
			return startedThreadTurn{}, false, err
		}
	}

	th.mu.Lock()
	threadID := th.ID
	threadCWD := th.CWD
	th.mu.Unlock()
	if s.rt != nil {
		transformed, err := s.rt.TransformUserMessage(ctx, threadID, threadCWD, userMsg)
		if err != nil {
			abortAdmission()
			return startedThreadTurn{}, false, fmt.Errorf("transform user message: %w", err)
		}
		userMsg = transformed
	}
	if !chatMessageHasUserPayload(userMsg) {
		abortAdmission()
		return startedThreadTurn{}, false, nil
	}

	th.mu.Lock()
	if s.closed.Load() {
		th.releaseThreadExecutionLeaseLocked()
		th.mu.Unlock()
		cancel()
		return startedThreadTurn{}, false, errServerClosed
	}
	// running cannot become true while executionLease is our local admission
	// reservation, but retain the guard so future non-turn writers fail closed.
	if th.running {
		th.releaseThreadExecutionLeaseLocked()
		th.mu.Unlock()
		cancel()
		if failIfRunning {
			return startedThreadTurn{}, false, fmt.Errorf("thread %q already has a running turn", th.ID)
		}
		return startedThreadTurn{}, false, nil
	}
	if th.ReadOnly {
		switch readOnlyPolicy {
		case turnReadOnlyFail:
			th.releaseThreadExecutionLeaseLocked()
			th.mu.Unlock()
			cancel()
			return startedThreadTurn{}, false, errors.New("thread is read-only")
		case turnReadOnlySkip:
			th.releaseThreadExecutionLeaseLocked()
			th.mu.Unlock()
			cancel()
			return startedThreadTurn{}, false, nil
		}
	}
	var commitAfterAppend func() error
	if hooks.beforeUserAppendLocked != nil {
		var err error
		commitAfterAppend, err = hooks.beforeUserAppendLocked(th)
		if err != nil {
			th.releaseThreadExecutionLeaseLocked()
			th.mu.Unlock()
			cancel()
			return startedThreadTurn{}, false, err
		}
	}
	var userMsgSeq int
	userAlreadyPersisted := false
	if clientID := strings.TrimSpace(userMsg.ClientID); clientID != "" {
		for _, existing := range th.History {
			if strings.TrimSpace(existing.ClientID) == clientID {
				userAlreadyPersisted = true
				userMsgSeq = existing.Seq
				break
			}
		}
	}
	if th.PersistHistory && userAlreadyPersisted && (len(userMsg.ConsumeResidentEnvelopeIDs) > 0 || len(hooks.residentReadReceipts) > 0) {
		if err := session.CommitExistingResidentAdmission(s.rt.SessionDir, userMsg.ConsumeResidentEnvelopeIDs, hooks.residentReadReceipts, now); err != nil {
			th.releaseThreadExecutionLeaseLocked()
			th.mu.Unlock()
			cancel()
			return startedThreadTurn{}, false, err
		}
	}
	if th.PersistHistory && !userAlreadyPersisted {
		var seq int
		var err error
		if len(hooks.residentReadReceipts) > 0 {
			seq, err = appendResidentAdmissionChatMessage(s.rt.SessionDir, th.ID, userMsg, hooks.residentReadReceipts, now)
		} else {
			seq, err = appendChatMessage(s.rt.SessionDir, th.ID, userMsg)
		}
		if err != nil {
			th.releaseThreadExecutionLeaseLocked()
			th.mu.Unlock()
			cancel()
			return startedThreadTurn{}, false, err
		}
		userMsgSeq = seq
		th.historyHeadSeq = max(th.historyHeadSeq, seq)
	}
	userMsg.Seq = userMsgSeq
	if commitAfterAppend != nil {
		if err := commitAfterAppend(); err != nil {
			th.releaseThreadExecutionLeaseLocked()
			th.mu.Unlock()
			cancel()
			return startedThreadTurn{}, false, errors.Join(errRetryableTurnAdmission, err)
		}
	}
	history := cloneHistory(th.History)
	if !userAlreadyPersisted {
		history = append(history, userMsg)
	}
	th.History = history
	th.cancel = cancel
	var turn Turn
	resumed := false
	if userAlreadyPersisted {
		turn, resumed = th.resumePersistedUserTurnLocked(userMsg.ClientID, now)
	}
	if !resumed {
		if th.PersistHistory {
			// Persisted turns are reconstructed from conversation order after a
			// restart or cross-process refresh. Give the live turn that same stable
			// ID now so item/turn references returned to clients remain valid after
			// the next admission refresh.
			turnID = fmt.Sprintf("%s-turn-%04d", th.ID, len(th.Turns)+1)
		}
		turn = th.startTurnLocked(turnID, userMsg, now)
	} else {
		turnID = turn.ID
	}
	turnRuntime := turnRuntimeSnapshotLocked(th)
	if snapshot.hasPermissions() || snapshot.PermissionExplicit {
		turnRuntime = turnRuntime.withPermissions(snapshot.permissions())
		turnRuntime.PermissionExplicit = snapshot.PermissionExplicit
	}
	turnRuntime.ForceCompact = snapshot.ForceCompact
	turnRuntime.CompactOnly = snapshot.CompactOnly
	turnRuntime.HistoryBaselineSeq = th.historyHeadSeq
	turnRuntime.AgentCompletionResultIDs = append([]string(nil), snapshot.AgentCompletionResultIDs...)
	turnRuntime.ProcessCompletionIDs = append([]string(nil), snapshot.ProcessCompletionIDs...)
	// Completion results folded from a lifted tree freeze are answered by
	// this user turn (foldFrozenWorkerTree staged them under the same lock
	// discipline as the snapshot fields).
	if len(th.frozenTreeResultIDs) > 0 {
		turnRuntime.AgentCompletionResultIDs = append(turnRuntime.AgentCompletionResultIDs, th.frozenTreeResultIDs...)
		th.frozenTreeResultIDs = nil
	}
	turnRuntime.Ultra = snapshot.Ultra
	th.mu.Unlock()

	return startedThreadTurn{
		ctx:        turnCtx,
		cancel:     cancel,
		turnID:     turnID,
		turn:       turn,
		runtime:    turnRuntime,
		history:    history,
		admittedAt: now,
		userMsgSeq: userMsgSeq,
	}, true, nil
}

func (s *Server) startQueuedTurn(ctx context.Context, threadID string, entry queuedTurn) (bool, error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return false, errors.New("thread_id is required")
	}
	if strings.TrimSpace(entry.msg.Role) == "" {
		entry.msg.Role = "user"
	}
	if !chatMessageHasUserPayload(entry.msg) {
		return false, nil
	}
	entry.id = strings.TrimSpace(entry.id)
	if entry.id == "" {
		entry.id = session.NewID()
	}
	entry.msg.ClientID = entry.id
	entry.msg.Steered = false

	th := s.thread(threadID)
	if th == nil {
		return false, fmt.Errorf("thread %q not found", threadID)
	}
	th.mu.Lock()
	isGroup := th.Group
	th.mu.Unlock()
	if isGroup {
		return false, errors.New("group threads do not support queued turns")
	}
	var threadRuntime *runtime.ThreadRuntime
	var err error
	permissions := entry.snapshot.permissions()
	if !entry.snapshot.hasPermissions() {
		permissions, err = s.resolveTurnPermissions(nil)
		if err != nil {
			return false, err
		}
	}

	snapshot := turnRuntimeSnapshot{}.withPermissions(permissions)
	snapshot.PermissionExplicit = entry.snapshot.PermissionExplicit
	snapshot.ForceCompact = entry.snapshot.ForceCompact
	snapshot.CompactOnly = entry.snapshot.CompactOnly
	started, ok, err := s.startThreadUserTurnWithAdmission(
		ctx,
		th,
		entry.msg,
		snapshot,
		false,
		turnReadOnlyFail,
		turnAdmissionHooks{afterLease: func(admitted *threadState, _ *providers.ChatMessage) error {
			var runtimeErr error
			threadRuntime, runtimeErr = s.ensureThreadRuntimeAfterAdmission(admitted)
			return runtimeErr
		}},
	)
	if err != nil || !ok {
		return ok, err
	}

	if s.beforeQueuedTurnBackgroundForTest != nil {
		s.beforeQueuedTurnBackgroundForTest()
	}
	launch, accepted := s.reserveBackground(func() {
		s.runTurn(started.ctx, th, threadRuntime, started.turnID, started.runtime, started.history)
	})
	if !accepted {
		persistErr := s.abortStartedThreadTurnDurably(th, started, errServerClosed)
		return false, errors.Join(errServerClosed, persistErr)
	}
	defer launch.Cancel()

	if err := s.writeNotification(NotificationTurnStarted, TurnStartedNotification{
		ThreadID: threadID,
		Turn:     started.turn,
		QueueID:  entry.id,
	}); err != nil {
		return false, errors.Join(err, s.abortStartedThreadTurnDurably(th, started, err))
	}
	launch.Commit()
	return true, nil
}

func (s *Server) takeNextQueuedUserTurn(threadID string) (queuedTurn, bool) {
	s.queuedTurnMu.Lock()
	defer s.queuedTurnMu.Unlock()
	pending := s.pendingQueuedTurns[threadID]
	if len(pending) == 0 {
		return queuedTurn{}, false
	}
	entry := pending[0]
	if len(pending) == 1 {
		delete(s.pendingQueuedTurns, threadID)
	} else {
		s.pendingQueuedTurns[threadID] = append([]queuedTurn(nil), pending[1:]...)
	}
	return entry, true
}

func (s *Server) prependQueuedUserTurns(threadID string, entries []queuedTurn) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" || len(entries) == 0 {
		return
	}
	s.queuedTurnMu.Lock()
	defer s.queuedTurnMu.Unlock()
	if s.closed.Load() {
		return
	}
	if s.pendingQueuedTurns == nil {
		s.pendingQueuedTurns = make(map[string][]queuedTurn)
	}
	existing := append([]queuedTurn(nil), s.pendingQueuedTurns[threadID]...)
	s.pendingQueuedTurns[threadID] = append(append([]queuedTurn(nil), entries...), existing...)
}

func (s *Server) hasQueuedUserTurns(threadID string) bool {
	s.queuedTurnMu.Lock()
	defer s.queuedTurnMu.Unlock()
	return len(s.pendingQueuedTurns[threadID]) > 0
}

func (s *Server) hasQueuedUserWork(threadID string) bool {
	s.queuedTurnMu.Lock()
	defer s.queuedTurnMu.Unlock()
	return len(s.pendingQueuedTurns[threadID]) > 0 || s.drainingQueuedTurns[threadID]
}

func (s *Server) discardQueuedUserTurns(threadID string) []string {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil
	}
	s.queuedTurnMu.Lock()
	defer s.queuedTurnMu.Unlock()
	queueIDs := queuedTurnIDs(s.pendingQueuedTurns[threadID])
	delete(s.pendingQueuedTurns, threadID)
	return queueIDs
}

func (s *Server) discardQueuedUserWork(threadID string) []string {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil
	}
	s.queuedTurnMu.Lock()
	defer s.queuedTurnMu.Unlock()
	queueIDs := queuedTurnIDs(s.pendingQueuedTurns[threadID])
	delete(s.pendingQueuedTurns, threadID)
	delete(s.drainingQueuedTurns, threadID)
	return queueIDs
}

func queuedTurnIDs(entries []queuedTurn) []string {
	if len(entries) == 0 {
		return nil
	}
	queueIDs := make([]string, 0, len(entries))
	for _, entry := range entries {
		if id := strings.TrimSpace(entry.id); id != "" {
			queueIDs = append(queueIDs, id)
		}
	}
	return queueIDs
}

func (s *Server) notifyQueuedTurnsDequeued(threadID string, queueIDs []string) {
	for _, queueID := range queueIDs {
		_ = s.writeNotification(NotificationTurnDequeued, TurnDequeuedNotification{
			ThreadID: threadID,
			QueueID:  queueID,
		})
	}
}

func (s *Server) clearQueuedTurnDrain(threadID string) {
	s.queuedTurnMu.Lock()
	defer s.queuedTurnMu.Unlock()
	delete(s.drainingQueuedTurns, threadID)
}

func (s *Server) kickAgentCompletionDrain(threadID string) {
	if s == nil || s.closed.Load() {
		return
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return
	}

	s.agentCompletionMu.Lock()
	if s.closed.Load() {
		s.agentCompletionMu.Unlock()
		return
	}
	if len(s.pendingAgentCompletionTurns[threadID]) == 0 || s.drainingAgentCompletionTurns[threadID] {
		s.agentCompletionMu.Unlock()
		return
	}
	if s.drainingAgentCompletionTurns == nil {
		s.drainingAgentCompletionTurns = make(map[string]bool)
	}
	s.drainingAgentCompletionTurns[threadID] = true
	s.agentCompletionMu.Unlock()

	_ = s.startBackground(func() { s.drainAgentCompletionTurns(threadID) })
}

func (s *Server) drainAgentCompletionTurns(threadID string) {
	if s == nil {
		return
	}
	if s.closed.Load() {
		s.discardPendingAgentCompletionTurns(threadID)
		s.clearAgentCompletionDrain(threadID)
		return
	}
	th := s.thread(threadID)
	if th == nil || !canResumeAgentCompletionThread(th) {
		s.discardPendingAgentCompletionTurns(threadID)
		s.clearAgentCompletionDrain(threadID)
		return
	}
	if threadIsRunning(th) {
		s.clearAgentCompletionDrain(threadID)
		return
	}
	// User-authored work wins over automatic completion wakeups. The user turn
	// will kick this drain again after it reaches a terminal state.
	if s.hasQueuedUserWork(threadID) {
		s.clearAgentCompletionDrain(threadID)
		s.kickQueuedTurnDrain(threadID)
		return
	}
	// A frozen tree holds its pending completion turns: the next user turn
	// consumes them as part of the whole-tree snapshot instead of synthetic
	// turns waking a frozen orchestration (turn/interrupt tree freeze).
	th.mu.Lock()
	frozen := th.workerTreeFrozen
	th.mu.Unlock()
	if frozen {
		s.clearAgentCompletionDrain(threadID)
		return
	}

	pending := s.takePendingAgentCompletionTurns(threadID)
	if len(pending) == 0 {
		s.clearAgentCompletionDrain(threadID)
		return
	}
	// One durable result id per synthetic wakeup gives retries a stable
	// idempotency key independent of process-local batching boundaries.
	current := pending[:1]
	if len(pending) > 1 {
		s.prependPendingAgentCompletionTurns(threadID, pending[1:])
	}

	started, err := s.startSyntheticTurn(context.Background(), threadID, combineAgentCompletionMessages(current), current)
	executionBusy := errors.Is(err, errThreadExecutionBusy)
	retryableAdmission := errors.Is(err, errRetryableTurnAdmission)
	if err != nil && !executionBusy {
		providers.DebugLogf("start agent completion turn for thread %q: %v", threadID, err)
	}
	requeued := false
	if !started && (err == nil || executionBusy || retryableAdmission) {
		s.prependPendingAgentCompletionTurns(threadID, current)
		requeued = true
	}
	s.clearAgentCompletionDrain(threadID)
	if requeued {
		if executionBusy || retryableAdmission {
			s.scheduleThreadExecutionLeaseRetry(func() { s.kickAgentCompletionDrain(threadID) })
			return
		}
		s.kickAgentCompletionDrain(threadID)
	}
}

// applyHelpMeCompletionRewriteLocked replaces the parent history with the
// bounded HelpMe joint compact. The synthetic-turn admission path calls it
// with th.mu and the durable execution lease held, after refreshing history.
// HelpMeCompletionRewrite is one-shot, so it must not run before ownership is
// acquired or a losing app-server could consume and persist the rewrite.
func (s *Server) prepareHelpMeCompletionRewriteLocked(threadID string, th *threadState, control *agentcontrol.AgentControl, pending []agentCompletionTurn) (func() error, error) {
	if th == nil || control == nil {
		return nil, nil
	}
	if control.Manager() == nil {
		return nil, nil
	}
	for _, turn := range pending {
		agentID := strings.TrimSpace(turn.agentID)
		if agentID == "" {
			continue
		}
		var snapshot subagent.SubAgentSnapshot
		if turn.snapshot != nil {
			snapshot = *cloneSubAgentSnapshot(turn.snapshot)
		} else if sa := control.Manager().Get(agentID); sa != nil {
			snapshot = sa.Snapshot()
		} else {
			continue
		}
		rewrite := control.PrepareHelpMeCompletionRewrite(snapshot)
		if rewrite == nil {
			continue
		}
		commit := func() error {
			applied, err := control.MarkHelpMeRecoveryApplied(rewrite.AgentID)
			if err != nil {
				return fmt.Errorf("persist helpme recovery commit for %q: %w", rewrite.AgentID, err)
			}
			if !applied {
				if recovery, ok := control.HelpMeRecoveryForHelper(rewrite.AgentID); ok && recovery.Applied {
					return nil
				}
				return fmt.Errorf("helpme recovery %q was not available to commit", rewrite.AgentID)
			}
			return nil
		}
		for _, msg := range th.History {
			if compact.IsHelpMeJointCompactContent(msg.Content) && msg.Content == rewrite.Content {
				// A prior attempt committed the rewrite but failed before the
				// wakeup append/Applied marker. Reuse the exact compact instead of
				// nesting it into another summary.
				return commit, nil
			}
		}
		baselineSeq := th.historyHeadSeq
		rewritten := compact.RewriteHistoryWithHelpMeCompact(th.History, rewrite.Content)
		persist := th.PersistHistory
		providerName := th.ModelProvider
		modelName := th.Model
		var runner *agent.StreamRunner
		if th.execRuntime != nil {
			runner = th.execRuntime.StreamRunner
		}
		if persist {
			if err := s.rewriteChatHistoryUnderExecutionLease(s.rt.SessionDir, th.ID, rewritten, baselineSeq); err != nil {
				return nil, errors.Join(
					errRetryableTurnAdmission,
					fmt.Errorf("persist helpme completion rewrite for thread %q: %w", threadID, err),
				)
			}
		}
		th.History = rewritten
		// This compaction happens outside the loop, so it never runs the loop's
		// own usage.Reset()+RecordPendingMessages. Invalidate the runner's shared
		// cross-turn usage baseline only after the durable rewrite succeeds.
		if runner != nil {
			runner.ResetConversationUsage(rewritten)
		}
		if persist {
			// Persist a fresh token_usage meta reflecting the compacted history so
			// latestRetainedContextTokens drops immediately, without waiting for the
			// next synthetic turn's response to land a smaller ContextTokens.
			ctxTokens := contextbudget.EstimateMessagesTokens(rewritten)
			if err := appendTokenUsage(s.rt.SessionDir, th.ID, providerName, modelName, providers.TokenUsage{}, ctxTokens); err != nil {
				providers.DebugLogf("append helpme compaction token usage for thread %q: %v", threadID, err)
			}
		}
		// The caller commits the one-shot only after the completion wakeup user
		// message is durable. A failed append then leaves Applied=false and the
		// idempotent rewrite can be retried safely.
		return commit, nil
	}
	return nil, nil
}

func (s *Server) startSyntheticTurn(ctx context.Context, threadID string, userMsg providers.ChatMessage, pending []agentCompletionTurn) (bool, error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return false, errors.New("thread_id is required")
	}
	if strings.TrimSpace(userMsg.Role) == "" {
		userMsg.Role = "user"
	}
	if !chatMessageHasUserPayload(userMsg) {
		return false, nil
	}

	th := s.thread(threadID)
	if th == nil {
		return false, fmt.Errorf("thread %q not found", threadID)
	}
	if !canResumeAgentCompletionThread(th) {
		return false, nil
	}
	var threadRuntime *runtime.ThreadRuntime
	pending = cloneAgentCompletionTurns(pending)
	completionResultIDs := make([]string, 0, len(pending))
	processCompletionIDs := make([]string, 0, len(pending))
	for _, turn := range pending {
		if resultID := strings.TrimSpace(turn.resultID); resultID != "" {
			completionResultIDs = append(completionResultIDs, resultID)
		}
		if processID := strings.TrimSpace(turn.processID); processID != "" {
			processCompletionIDs = append(processCompletionIDs, processID)
		}
	}

	// A synthetic completion turn belongs to the orchestration tree that
	// produced it: reuse the completing worker's inherited Ultra value
	// instead of the current session setting (turn boundary and inheritance,
	// docs/app-server-protocol.md).
	completionUltra := false
	for _, turn := range pending {
		if turn.snapshot != nil && turn.snapshot.Ultra {
			completionUltra = true
			break
		}
	}
	started, ok, err := s.startThreadUserTurnWithAdmission(
		ctx,
		th,
		userMsg,
		turnRuntimeSnapshot{AgentCompletionResultIDs: completionResultIDs, ProcessCompletionIDs: processCompletionIDs, Ultra: completionUltra},
		false,
		turnReadOnlySkip,
		turnAdmissionHooks{
			afterLease: func(admitted *threadState, admittedMsg *providers.ChatMessage) error {
				var err error
				threadRuntime, err = s.ensureThreadRuntimeAfterAdmission(admitted)
				if err != nil {
					return err
				}
				for _, resultID := range completionResultIDs {
					consumer, err := threadRuntime.AgentControl.AgentResultDeliveryConsumer(resultID)
					if err != nil {
						return errors.Join(errRetryableTurnAdmission, err)
					}
					if consumer != "" {
						return errAgentCompletionAlreadyDelivered
					}
					if agentCompletionMarkerAnswered(admitted.History, resultID) {
						claimed, consumedBy, err := threadRuntime.AgentControl.ClaimAgentResultDeliveryID(resultID, "auto_completion")
						if err != nil {
							return errors.Join(errRetryableTurnAdmission, err)
						}
						if !claimed && consumedBy == "" {
							return errors.Join(errRetryableTurnAdmission, fmt.Errorf("agent result delivery %q is unavailable", resultID))
						}
						return errAgentCompletionAlreadyDelivered
					}
				}
				for _, processID := range processCompletionIDs {
					if threadRuntime.ProcessManager == nil {
						return errors.Join(errRetryableTurnAdmission, errors.New("process completion manager is unavailable"))
					}
					pending, err := threadRuntime.ProcessManager.CompletionPending(processID)
					if err != nil {
						return errors.Join(errRetryableTurnAdmission, err)
					}
					if !pending {
						return errAgentCompletionAlreadyDelivered
					}
					if processCompletionMarkerAnswered(admitted.History, processID) {
						if _, err := threadRuntime.ProcessManager.MarkCompletionDelivered(processID, "history_answer"); err != nil {
							return errors.Join(errRetryableTurnAdmission, err)
						}
						return errAgentCompletionAlreadyDelivered
					}
				}
				*admittedMsg = combineAgentCompletionMessages(pending)
				if clientID := agentCompletionClientID(pending); clientID != "" {
					admittedMsg.ClientID = clientID
				} else if clientID := processCompletionClientID(processCompletionIDs); clientID != "" {
					admittedMsg.ClientID = clientID
				}
				return nil
			},
			beforeUserAppendLocked: func(locked *threadState) (func() error, error) {
				if threadRuntime == nil || threadRuntime.AgentControl == nil {
					return nil, nil
				}
				helpMeCommit, err := s.prepareHelpMeCompletionRewriteLocked(threadID, locked, threadRuntime.AgentControl, pending)
				if err != nil {
					return nil, err
				}
				return func() error {
					if helpMeCommit != nil {
						if err := helpMeCommit(); err != nil {
							return err
						}
					}
					return nil
				}, nil
			},
		},
	)
	if errors.Is(err, errAgentCompletionAlreadyDelivered) {
		return true, nil
	}
	if err != nil || !ok {
		return ok, err
	}

	_ = s.writeNotification(NotificationTurnStarted, TurnStartedNotification{
		ThreadID: threadID,
		Turn:     started.turn,
	})
	if !s.startBackground(func() {
		s.runTurn(started.ctx, th, threadRuntime, started.turnID, started.runtime, started.history)
	}) {
		// The synthetic completion message was already appended durably at
		// admission; a memory-only rollback would leave an orphan user
		// message with no terminal meta after restart, and its completion
		// result could be consumed twice. Record the terminal projection
		// exactly like a rejected ordinary user turn.
		persistErr := s.abortStartedThreadTurnDurably(th, started, errServerClosed)
		return false, errors.Join(errServerClosed, persistErr)
	}
	return true, nil
}

func canResumeAgentCompletionThread(th *threadState) bool {
	if th == nil {
		return false
	}
	th.mu.Lock()
	defer th.mu.Unlock()
	return !th.ReadOnly
}

func threadIsRunning(th *threadState) bool {
	if th == nil {
		return false
	}
	th.mu.Lock()
	defer th.mu.Unlock()
	return th.running
}

func (s *Server) takePendingAgentCompletionTurns(threadID string) []agentCompletionTurn {
	s.agentCompletionMu.Lock()
	defer s.agentCompletionMu.Unlock()
	if s.closed.Load() {
		return nil
	}
	pending := cloneAgentCompletionTurns(s.pendingAgentCompletionTurns[threadID])
	delete(s.pendingAgentCompletionTurns, threadID)
	return pending
}

func (s *Server) prependPendingAgentCompletionTurns(threadID string, turns []agentCompletionTurn) {
	if len(turns) == 0 {
		return
	}
	s.agentCompletionMu.Lock()
	defer s.agentCompletionMu.Unlock()
	if s.closed.Load() {
		return
	}
	if s.pendingAgentCompletionTurns == nil {
		s.pendingAgentCompletionTurns = make(map[string][]agentCompletionTurn)
	}
	existing := cloneAgentCompletionTurns(s.pendingAgentCompletionTurns[threadID])
	s.pendingAgentCompletionTurns[threadID] = append(cloneAgentCompletionTurns(turns), existing...)
}

func (s *Server) discardPendingAgentCompletionTurns(threadID string) {
	s.agentCompletionMu.Lock()
	defer s.agentCompletionMu.Unlock()
	delete(s.pendingAgentCompletionTurns, threadID)
}

func (s *Server) clearAgentCompletionDrain(threadID string) {
	s.agentCompletionMu.Lock()
	defer s.agentCompletionMu.Unlock()
	delete(s.drainingAgentCompletionTurns, threadID)
}

func (s *Server) hasQueuedAgentCompletionWork(threadID string) bool {
	s.agentCompletionMu.Lock()
	defer s.agentCompletionMu.Unlock()
	return len(s.pendingAgentCompletionTurns[threadID]) > 0 || s.drainingAgentCompletionTurns[threadID]
}

func (s *Server) kickGoalContinuation(threadID string) {
	if s == nil || s.closed.Load() {
		return
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return
	}
	// Most threads have no active goal. Read the dedicated goal store before
	// starting a drain so ordinary resume/edit/fork traffic is never blocked by
	// a speculative execution-lease acquisition. The admission path rechecks
	// the goal under ownership, so this is only a side-effect-free fast reject.
	goal, ok, err := s.currentRuntimeGoal(threadID)
	if err != nil {
		providers.DebugLogf("inspect goal continuation for thread %q: %v", threadID, err)
		return
	}
	if !ok || !goal.CanAutoContinue() {
		return
	}

	s.goalContinuationMu.Lock()
	if s.closed.Load() {
		s.goalContinuationMu.Unlock()
		return
	}
	if s.drainingGoalContinuation[threadID] {
		s.goalContinuationMu.Unlock()
		return
	}
	if s.drainingGoalContinuation == nil {
		s.drainingGoalContinuation = make(map[string]bool)
	}
	s.drainingGoalContinuation[threadID] = true
	s.goalContinuationMu.Unlock()

	_ = s.startBackground(func() { s.drainGoalContinuation(threadID) })
}

func (s *Server) drainGoalContinuation(threadID string) {
	if s == nil {
		return
	}
	if s.closed.Load() {
		s.clearGoalContinuationDrain(threadID)
		return
	}
	started, err := s.startGoalContinuationTurn(context.Background(), threadID)
	executionBusy := errors.Is(err, errThreadExecutionBusy)
	if err != nil && !executionBusy {
		providers.DebugLogf("start goal continuation turn for thread %q: %v", threadID, err)
	}
	s.clearGoalContinuationDrain(threadID)
	if executionBusy {
		s.scheduleThreadExecutionLeaseRetry(func() { s.kickGoalContinuation(threadID) })
		return
	}
	if started {
		return
	}
}

func (s *Server) clearGoalContinuationDrain(threadID string) {
	s.goalContinuationMu.Lock()
	defer s.goalContinuationMu.Unlock()
	delete(s.drainingGoalContinuation, threadID)
}

func (s *Server) startGoalContinuationTurn(ctx context.Context, threadID string) (bool, error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return false, errors.New("thread_id is required")
	}
	th := s.thread(threadID)
	if th == nil {
		return false, fmt.Errorf("thread %q not found", threadID)
	}

	turnID := session.NewID()
	turnCtx, cancel := context.WithCancel(ctx)
	now := time.Now().UTC()

	th.mu.Lock()
	if th.running || th.ReadOnly {
		th.mu.Unlock()
		cancel()
		return false, nil
	}
	if s.hasQueuedUserWork(threadID) || s.hasQueuedAgentCompletionWork(threadID) {
		th.mu.Unlock()
		cancel()
		return false, nil
	}
	acquired, err := s.tryAcquireThreadExecutionLeaseLocked(th)
	if err != nil {
		th.mu.Unlock()
		cancel()
		return false, err
	}
	if !acquired {
		th.mu.Unlock()
		cancel()
		return false, threadExecutionBusyError(threadID)
	}
	if err := s.refreshDurableThreadHistoryLocked(th); err != nil {
		th.releaseThreadExecutionLeaseLocked()
		th.mu.Unlock()
		cancel()
		return false, err
	}
	th.mu.Unlock()

	releaseAdmission := func() {
		th.mu.Lock()
		th.releaseThreadExecutionLeaseLocked()
		th.mu.Unlock()
		cancel()
	}

	// Construct and restore the runtime only from the snapshot protected by the
	// execution lease. In particular, plan state, usage, CWD, and model settings
	// must not come from the stale resident copy that lost the prior admission.
	threadRuntime, err := s.ensureThreadRuntimeAfterAdmission(th)
	if err != nil {
		releaseAdmission()
		return false, err
	}
	if threadRuntime == nil || threadRuntime.GoalRuntime == nil {
		releaseAdmission()
		return false, nil
	}

	// The goal and all queue gates may have changed while another app-server
	// owned the thread. Evaluate them only after acquiring ownership.
	decision, err := threadRuntime.GoalRuntime.DecideContinuation(s.goalContinuationInput(th, threadID))
	if err != nil {
		releaseAdmission()
		return false, err
	}
	if !decision.Allowed {
		releaseAdmission()
		return false, nil
	}
	goal, err := threadRuntime.GoalRuntime.CurrentGoal()
	if errors.Is(err, os.ErrNotExist) {
		releaseAdmission()
		return false, nil
	}
	if err != nil {
		releaseAdmission()
		return false, err
	}
	if !goal.CanAutoContinue() {
		releaseAdmission()
		return false, nil
	}

	th.mu.Lock()
	if s.closed.Load() {
		th.releaseThreadExecutionLeaseLocked()
		th.mu.Unlock()
		cancel()
		return false, errServerClosed
	}
	if th.running || th.ReadOnly || s.hasQueuedUserWork(threadID) || s.hasQueuedAgentCompletionWork(threadID) {
		th.releaseThreadExecutionLeaseLocked()
		th.mu.Unlock()
		cancel()
		return false, nil
	}
	history := cloneHistory(th.History)
	th.cancel = cancel
	turn := th.startInternalTurnLocked(turnID, now)
	turnRuntime := turnRuntimeSnapshotLocked(th)
	turnRuntime.HistoryBaselineSeq = th.historyHeadSeq
	th.mu.Unlock()

	_ = s.writeNotification(NotificationTurnStarted, TurnStartedNotification{
		ThreadID: threadID,
		Turn:     turn,
	})
	if !s.startBackground(func() {
		s.runTurnWithRequestContext(turnCtx, th, threadRuntime, turnID, turnRuntime, history, goalContinuationContextSegments(goal), nil)
	}) {
		abortStartedThreadTurn(th, startedThreadTurn{
			ctx:     turnCtx,
			cancel:  cancel,
			turnID:  turnID,
			turn:    turn,
			runtime: turnRuntime,
			history: history,
		}, errServerClosed)
		return false, errServerClosed
	}
	return true, nil
}

func (s *Server) goalContinuationInput(th *threadState, threadID string) goalruntime.ContinuationInput {
	var running, readOnly bool
	if th != nil {
		th.mu.Lock()
		running = th.running
		readOnly = th.ReadOnly
		th.mu.Unlock()
	}
	return goalruntime.ContinuationInput{
		ThreadIdle:      !running,
		ActiveTurn:      running,
		QueuedUserWork:  s.hasQueuedUserWork(threadID),
		QueuedAgentWork: s.hasQueuedAgentCompletionWork(threadID),
		ReadOnly:        readOnly,
	}
}

const (
	// Goal continuations are hidden request-only reminders that can repeat
	// across many automatic turns. Keep the inline objective below a small
	// prompt budget and require goal action=get when the full objective matters.
	goalContinuationObjectiveHeadBytes = 1200
	goalContinuationObjectiveTailBytes = 600
)

func goalContinuationContextSegments(goal goalruntime.Goal) []agent.ContextSegment {
	return agent.RequestOnlyContextBlocks([]wuucontext.Block{goalContinuationBlock(goal)})
}

func goalContinuationBlock(goal goalruntime.Goal) wuucontext.Block {
	objective, objectiveNote := goalContinuationObjectivePreview(goal.Objective)
	if objectiveNote != "" {
		objectiveNote = "\n" + objectiveNote
	}
	content := fmt.Sprintf(strings.Join([]string{
		"<goal_continuation>",
		"Continue working toward the active thread goal.",
		"Objective: %s%s",
		"Status: %s",
		"Tokens used: %d",
		"Time used seconds: %d",
		"Goal turns: %d",
		"",
		"Make concrete progress toward the objective. Do not mark the goal complete unless the objective is actually achieved. If the same blocker prevents progress for multiple continuation turns, use the goal status rules instead of pretending the work is complete.",
		"</goal_continuation>",
	}, "\n"), objective, objectiveNote, goal.Status, goal.TokensUsed, goal.TimeUsedSeconds, goal.GoalTurns)
	return wuucontext.Block{
		Kind:    wuucontext.BlockGoalContinuation,
		Title:   "Active goal continuation",
		Source:  "runtime.goal_continuation",
		Content: content,
	}
}

func goalContinuationObjectivePreview(objective string) (string, string) {
	objective = strings.TrimSpace(objective)
	if len([]byte(objective)) <= goalContinuationObjectiveHeadBytes+goalContinuationObjectiveTailBytes {
		return objective, ""
	}
	preview := stringutil.HeadTail(
		objective,
		goalContinuationObjectiveHeadBytes,
		goalContinuationObjectiveTailBytes,
		"\n\n[objective trimmed; call goal with action=get for the full objective]\n\n",
	)
	return preview, "Full objective omitted from this continuation reminder; call goal with action=get if the missing details matter."
}

func combineAgentCompletionMessages(turns []agentCompletionTurn) providers.ChatMessage {
	if len(turns) == 0 {
		return providers.ChatMessage{Role: "user"}
	}
	if len(turns) == 1 {
		return turns[0].msg
	}
	contents := make([]string, 0, len(turns))
	name := ""
	for _, turn := range turns {
		msg := turn.msg
		if name == "" {
			name = strings.TrimSpace(msg.Name)
		}
		if content := strings.TrimSpace(msg.Content); content != "" {
			contents = append(contents, content)
		}
	}
	return providers.ChatMessage{
		Role:    "user",
		Name:    name,
		Content: strings.Join(contents, "\n\n"),
	}
}

const (
	agentCompletionClientIDPrefix       = "wuu-agent-completion:"
	agentCompletionAnswerClientIDPrefix = "wuu-agent-completion-answer:"
)

func agentCompletionClientID(turns []agentCompletionTurn) string {
	ids := make([]string, 0, len(turns))
	seen := make(map[string]bool, len(turns))
	for _, turn := range turns {
		id := strings.TrimSpace(turn.resultID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return ""
	}
	sort.Strings(ids)
	return agentCompletionClientIDPrefix + strings.Join(ids, ",")
}

func agentCompletionResultIDs(clientID string) []string {
	clientID = strings.TrimSpace(clientID)
	if !strings.HasPrefix(clientID, agentCompletionClientIDPrefix) {
		return nil
	}
	raw := strings.TrimPrefix(clientID, agentCompletionClientIDPrefix)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if id := strings.TrimSpace(part); id != "" {
			out = append(out, id)
		}
	}
	return out
}

func agentCompletionAnswerResultIDs(clientID string) []string {
	clientID = strings.TrimSpace(clientID)
	if !strings.HasPrefix(clientID, agentCompletionAnswerClientIDPrefix) {
		return nil
	}
	return splitAgentCompletionResultIDs(strings.TrimPrefix(clientID, agentCompletionAnswerClientIDPrefix))
}

func splitAgentCompletionResultIDs(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if id := strings.TrimSpace(part); id != "" {
			out = append(out, id)
		}
	}
	return out
}

// markAgentCompletionAnswer stamps the successful final assistant row with a
// durable outcome marker. A streamed partial assistant from a cancelled or
// failed turn never receives this marker, so restart recovery cannot mistake
// visible partial text for a completed consumption of the child result.
func markAgentCompletionAnswer(res *agent.LoopResult, resultIDs []string) bool {
	if res == nil || len(resultIDs) == 0 || len(res.NewMessages) == 0 {
		return false
	}
	clean := make([]string, 0, len(resultIDs))
	seen := make(map[string]bool, len(resultIDs))
	for _, resultID := range resultIDs {
		resultID = strings.TrimSpace(resultID)
		if resultID == "" || seen[resultID] {
			continue
		}
		seen[resultID] = true
		clean = append(clean, resultID)
	}
	if len(clean) == 0 {
		return false
	}
	sort.Strings(clean)
	markerIndex := -1
	for i, msg := range res.NewMessages {
		for _, markerID := range agentCompletionResultIDs(msg.ClientID) {
			if seen[markerID] {
				markerIndex = i
				break
			}
		}
	}
	for i := len(res.NewMessages) - 1; i > markerIndex; i-- {
		msg := &res.NewMessages[i]
		if !strings.EqualFold(strings.TrimSpace(msg.Role), "assistant") || strings.TrimSpace(msg.Content) == "" {
			continue
		}
		msg.ClientID = agentCompletionAnswerClientIDPrefix + strings.Join(clean, ",")
		return true
	}
	return false
}

func agentCompletionMarkerAnswered(history []providers.ChatMessage, resultID string) bool {
	resultID = strings.TrimSpace(resultID)
	if resultID == "" {
		return false
	}
	marker := -1
	for i, msg := range history {
		for _, id := range agentCompletionResultIDs(msg.ClientID) {
			if id == resultID {
				marker = i
				break
			}
		}
	}
	if marker < 0 {
		return false
	}
	for _, msg := range history[marker+1:] {
		for _, answeredID := range agentCompletionAnswerResultIDs(msg.ClientID) {
			if answeredID == resultID {
				return true
			}
		}
		if strings.EqualFold(strings.TrimSpace(msg.Role), "user") &&
			!msg.Hidden && !msg.Steered && len(msg.FocusMeta) == 0 && !compact.IsInternalContextMessage(msg) {
			return false
		}
	}
	return false
}

func cloneAgentCompletionTurns(turns []agentCompletionTurn) []agentCompletionTurn {
	if len(turns) == 0 {
		return nil
	}
	msgs := make([]providers.ChatMessage, 0, len(turns))
	for _, turn := range turns {
		msgs = append(msgs, turn.msg)
	}
	msgs = cloneHistory(msgs)
	out := make([]agentCompletionTurn, 0, len(turns))
	for i, turn := range turns {
		out = append(out, agentCompletionTurn{
			agentID:   turn.agentID,
			resultID:  turn.resultID,
			processID: turn.processID,
			msg:       msgs[i],
			snapshot:  cloneSubAgentSnapshot(turn.snapshot),
		})
	}
	return out
}

func cloneSubAgentSnapshot(snapshot *subagent.SubAgentSnapshot) *subagent.SubAgentSnapshot {
	if snapshot == nil {
		return nil
	}
	clone := *snapshot
	return &clone
}

func queuedTurnSummary(threadID string, entry queuedTurn) QueuedTurn {
	preview := strings.TrimSpace(chatMessageDisplayContent(entry.msg))
	if preview == "" && len(entry.msg.Images) > 0 {
		if len(entry.msg.Images) == 1 {
			preview = "[Image #1]"
		} else {
			preview = fmt.Sprintf("[%d images]", len(entry.msg.Images))
		}
	}
	if preview == "" && len(entry.msg.Files) > 0 {
		if len(entry.msg.Files) == 1 {
			preview = filePreview(entry.msg.Files[0], 1)
		} else {
			preview = fmt.Sprintf("[%d files]", len(entry.msg.Files))
		}
	}
	return QueuedTurn{
		ID:         entry.id,
		ThreadID:   threadID,
		Preview:    preview,
		ImageCount: len(entry.msg.Images),
		FileCount:  len(entry.msg.Files),
	}
}

func chatMessageHasUserPayload(msg providers.ChatMessage) bool {
	return strings.TrimSpace(msg.Content) != "" || len(msg.Images) > 0 || len(msg.Files) > 0
}

func queuedTurnsFromSteers(msgs []providers.ChatMessage) []queuedTurn {
	if len(msgs) == 0 {
		return nil
	}
	out := make([]queuedTurn, 0, len(msgs))
	for _, msg := range msgs {
		id := strings.TrimSpace(msg.ClientID)
		if id == "" {
			id = session.NewID()
		}
		msg.ClientID = id
		msg.Steered = false
		out = append(out, queuedTurn{id: id, msg: msg})
	}
	return out
}

func (s *Server) persistTurnResultLocked(th *threadState, res agent.LoopResult, rewriteHistory bool, providerName, model string, historyBaselineSeq int) error {
	if !th.PersistHistory {
		return nil
	}
	indexHistory := th.History
	if rewriteHistory {
		if err := rewriteChatHistoryAtBaseline(s.rt.SessionDir, th.ID, th.History, historyBaselineSeq); err != nil {
			return err
		}
		// The transaction may have merged participant/meta tail records that
		// arrived while the model ran. Count the committed history rather than
		// overwriting the session index from the turn's pre-merge snapshot.
		if committedRecords, committedHeadSeq, err := loadProviderPersistedMessages(s.rt.SessionDir, th.ID, false); err != nil {
			return err
		} else {
			committed := chatMessagesFromPersistedMessages(committedRecords)
			indexHistory = committed
			th.History = cloneHistory(committed)
			th.historyHeadSeq = committedHeadSeq
		}
		if display, err := loadPersistedMessages(s.rt.SessionDir, th.ID, false); err != nil {
			return err
		} else {
			mergeConcurrentParticipantTailIntoTurnsLocked(th, display, historyBaselineSeq, time.Now().UTC(), s.resolveParticipantSummary)
		}
	} else {
		if err := appendChatMessages(s.rt.SessionDir, th.ID, res.NewMessages); err != nil {
			return err
		}
	}
	if err := appendTokenUsage(s.rt.SessionDir, th.ID, providerName, model, providers.TokenUsage{
		InputTokens:         res.InputTokens,
		OutputTokens:        res.OutputTokens,
		CacheCreationTokens: res.CacheCreationTokens,
		CacheReadTokens:     res.CacheReadTokens,
	}, res.ContextTokens); err != nil {
		return err
	}
	return session.UpdateIndex(s.rt.SessionDir, th.ID, persistableMessageCount(indexHistory), threadPreview(indexHistory))
}

func mergeConcurrentParticipantTailIntoTurnsLocked(th *threadState, records []persistedMessage, baselineSeq int, now time.Time, resolve participantSummaryResolver) {
	if th == nil || th.currentTurn == "" {
		return
	}
	existingSeq := make(map[int]bool)
	turn := th.ensureTurnLocked(th.currentTurn, now)
	for _, item := range turn.Items {
		if item.Seq > 0 {
			existingSeq[item.Seq] = true
		}
	}
	th.nextItemIndex = max(th.nextItemIndex, maxTurnItemIndex(turn))
	changed := false
	for _, rec := range records {
		if rec.Seq <= baselineSeq || rec.Seq <= 0 || existingSeq[rec.Seq] || strings.TrimSpace(rec.ThreadID) != "" || !isParticipantPersistedMessage(rec) {
			continue
		}
		turn.Items = append(turn.Items, participantMessageItem(th.nextItemIDLocked(turn.ID), rec, resolve))
		existingSeq[rec.Seq] = true
		changed = true
	}
	if changed {
		th.replaceTurnLocked(turn)
	}
}

// handleSettingsUsage returns the aggregated token usage snapshot for
// the desktop settings page. Range selects a time window ("all" / "7d"
// / "30d" / "90d"); empty defaults to "all". Range filtering is applied
// to each token_usage row's At timestamp (UTC), not to the session
// CreatedAt, so a long-running session that crosses a range boundary
// contributes only the rows inside the window. Rows with a zero At
// (legacy imports written before the timestamp was added) are kept
// under "all" and dropped from any time-windowed query so they cannot
// masquerade as fresh activity.
func (s *Server) handleSettingsUsage(req Request) error {
	var params SettingsUsageQuery
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	rangeFilter := params.Range
	if rangeFilter == "" {
		rangeFilter = SettingsUsageRangeAll
	}
	if err := validateSettingsUsageRange(rangeFilter); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}

	sessDir := s.rt.SessionDir
	now := time.Now().UTC()
	cutoff := settingsUsageRangeCutoff(rangeFilter, now)

	metas, err := insight.ScanSessions(sessDir, 0)
	if err != nil {
		return s.writeResponse(req.ID, nil, fmt.Errorf("scan sessions: %w", err))
	}
	rows, err := insight.CollectTokenUsageRows(sessDir)
	if err != nil {
		return s.writeResponse(req.ID, nil, fmt.Errorf("collect usage rows: %w", err))
	}
	filteredRows := filterUsageRowsByCutoff(rows, cutoff)

	titleByID := make(map[string]string, len(metas))
	for _, m := range metas {
		if title := strings.TrimSpace(m.FirstUserMsg); title != "" {
			titleByID[m.ID] = truncateUsageTitle(title)
		}
	}
	metrics, days, entries := aggregateUsageRows(filteredRows, titleByID)
	totalSessions := countSessionsInRange(rows, cutoff)

	return s.writeResponse(req.ID, SettingsUsageResponse{
		Range:           rangeFilter,
		TotalSessions:   totalSessions,
		GeneratedAt:     now.Format(time.RFC3339Nano),
		Metrics:         metrics,
		ModelBreakdowns: buildUsageModelBreakdowns(filteredRows),
		Days:            days,
		Entries:         entries,
	}, nil)
}

// filterUsageRowsByCutoff returns rows that fall inside the requested
// range. "all" (cutoff == nil) keeps every row, including zero-At
// legacy imports; time-windowed queries drop zero-At rows so they
// cannot be pinned to "today" or "this week" by accident.
func filterUsageRowsByCutoff(rows []insight.TokenUsageRow, cutoff *time.Time) []insight.TokenUsageRow {
	if cutoff == nil {
		return rows
	}
	out := make([]insight.TokenUsageRow, 0, len(rows))
	for _, r := range rows {
		if r.At.IsZero() {
			continue
		}
		if r.At.Before(*cutoff) {
			continue
		}
		out = append(out, r)
	}
	return out
}

// countSessionsInRange returns the number of distinct session IDs that
// have at least one token_usage row inside the cutoff. Sessions with
// only zero-At legacy rows are excluded from time-windowed counts but
// still counted under "all".
func countSessionsInRange(rows []insight.TokenUsageRow, cutoff *time.Time) int {
	seen := make(map[string]struct{})
	for _, r := range rows {
		if cutoff != nil && r.At.IsZero() {
			continue
		}
		if cutoff != nil && r.At.Before(*cutoff) {
			continue
		}
		seen[r.SessionID] = struct{}{}
	}
	return len(seen)
}

func buildUsageModelBreakdowns(rows []insight.TokenUsageRow) []insight.ModelUsage {
	buckets := make(map[string]*insight.ModelUsage)
	sessionsByBucket := make(map[string]map[string]struct{})
	for _, r := range rows {
		key := r.Provider + "|" + r.Model
		bucket, ok := buckets[key]
		if !ok {
			bucket = &insight.ModelUsage{Provider: r.Provider, Model: r.Model}
			buckets[key] = bucket
		}
		bucket.InputTokens += r.InputTokens
		bucket.OutputTokens += r.OutputTokens
		bucket.CacheCreationTokens += r.CacheCreationTokens
		bucket.CacheReadTokens += r.CacheReadTokens
		if r.SessionID != "" {
			seen := sessionsByBucket[key]
			if seen == nil {
				seen = make(map[string]struct{})
				sessionsByBucket[key] = seen
			}
			seen[r.SessionID] = struct{}{}
		}
	}

	breakdowns := make([]insight.ModelUsage, 0, len(buckets))
	for key, bucket := range buckets {
		bucket.Sessions = len(sessionsByBucket[key])
		breakdowns = append(breakdowns, *bucket)
	}
	sort.Slice(breakdowns, func(i, j int) bool {
		return breakdowns[i].TotalContextTokens() > breakdowns[j].TotalContextTokens()
	})
	return breakdowns
}

// aggregateUsageRows is the single source of truth for the desktop
// usage page's metrics, daily series, and recent-entries list. It
// never reads from session metadata — only the per-row token_usage
// trail — so the headline numbers, the heatmap, and the "最近记录"
// list all stay numerically consistent.
func aggregateUsageRows(rows []insight.TokenUsageRow, titleByID map[string]string) (SettingsUsageMetrics, []SettingsUsageDay, []SettingsUsageEntry) {
	metrics := SettingsUsageMetrics{}
	type dayBucket struct {
		input, output, cacheRead, cacheCreation int
		turns                                   int
	}
	daysByDate := make(map[string]*dayBucket)

	var minAt, maxAt time.Time
	for _, r := range rows {
		metrics.InputTokens += r.InputTokens
		metrics.OutputTokens += r.OutputTokens
		metrics.CacheReadTokens += r.CacheReadTokens
		metrics.CacheCreationTokens += r.CacheCreationTokens
		metrics.Turns++
		if !r.At.IsZero() {
			if minAt.IsZero() || r.At.Before(minAt) {
				minAt = r.At
			}
			if r.At.After(maxAt) {
				maxAt = r.At
			}
			date := r.At.UTC().Format("2006-01-02")
			bucket, ok := daysByDate[date]
			if !ok {
				bucket = &dayBucket{}
				daysByDate[date] = bucket
			}
			bucket.input += r.InputTokens
			bucket.output += r.OutputTokens
			bucket.cacheRead += r.CacheReadTokens
			bucket.cacheCreation += r.CacheCreationTokens
			bucket.turns++
		}
	}

	metrics.PromptTokens = metrics.InputTokens + metrics.CacheReadTokens
	metrics.ContextTokens = metrics.InputTokens + metrics.CacheReadTokens + metrics.OutputTokens
	if metrics.PromptTokens > 0 {
		metrics.CacheHitRate = float64(metrics.CacheReadTokens) / float64(metrics.PromptTokens)
	}
	if !minAt.IsZero() {
		metrics.DateRange = [2]string{minAt.UTC().Format("2006-01-02"), maxAt.UTC().Format("2006-01-02")}
		metrics.ActiveDays = len(daysByDate)
	}

	days := make([]SettingsUsageDay, 0, len(daysByDate))
	for date, b := range daysByDate {
		prompt := b.input + b.cacheRead
		var rate float64
		if prompt > 0 {
			rate = float64(b.cacheRead) / float64(prompt)
		}
		days = append(days, SettingsUsageDay{
			Date:                date,
			InputTokens:         b.input,
			OutputTokens:        b.output,
			CacheCreationTokens: b.cacheCreation,
			CacheReadTokens:     b.cacheRead,
			CacheHitRate:        rate,
			Turns:               b.turns,
		})
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Date < days[j].Date })

	entries := buildUsageEntries(rows, titleByID, 8)
	return metrics, days, entries
}

// buildUsageEntries picks the most recent N token_usage rows (by At)
// and renders them as SettingsUsageEntry rows. Rows with a zero At are
// sorted last so legacy imports never steal the top slots in the
// "最近记录" list.
func buildUsageEntries(rows []insight.TokenUsageRow, titleByID map[string]string, limit int) []SettingsUsageEntry {
	type sortable struct {
		row insight.TokenUsageRow
		ts  time.Time
	}
	items := make([]sortable, 0, len(rows))
	for _, r := range rows {
		items = append(items, sortable{row: r, ts: r.At})
	}
	sort.Slice(items, func(i, j int) bool {
		iZero, jZero := items[i].ts.IsZero(), items[j].ts.IsZero()
		if iZero != jZero {
			return !iZero
		}
		if items[i].ts.Equal(items[j].ts) {
			return items[i].row.SessionID < items[j].row.SessionID
		}
		return items[i].ts.After(items[j].ts)
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	entries := make([]SettingsUsageEntry, 0, len(items))
	for _, it := range items {
		r := it.row
		title := titleByID[r.SessionID]
		if title == "" {
			title = r.SessionID
		}
		entries = append(entries, SettingsUsageEntry{
			ID:                  "turn:" + r.SessionID + "@" + r.At.Format(time.RFC3339Nano),
			Source:              "turn",
			Title:               title,
			Provider:            r.Provider,
			Model:               r.Model,
			At:                  r.At.UTC().Format(time.RFC3339Nano),
			InputTokens:         r.InputTokens,
			OutputTokens:        r.OutputTokens,
			CacheCreationTokens: r.CacheCreationTokens,
			CacheReadTokens:     r.CacheReadTokens,
		})
	}
	return entries
}

// truncateUsageTitle shortens a session's first user message down to a
// reasonable card headline; the desktop may trim further before display.
func truncateUsageTitle(s string) string {
	const max = 60
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

func requestContextSystemSections(sections []agent.SystemPromptSectionInfo) []sessiontrace.SystemSectionRecord {
	if len(sections) == 0 {
		return nil
	}
	out := make([]sessiontrace.SystemSectionRecord, 0, len(sections))
	for _, section := range sections {
		out = append(out, sessiontrace.SystemSectionRecord{
			Key:    section.Key,
			Static: section.Static,
			Bytes:  section.Bytes,
			Hash:   section.Hash,
		})
	}
	return out
}

func cloneStringIntMap(in map[string]int) map[string]int {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]int, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

// validateSettingsUsageRange rejects unknown range strings so the
// desktop gets a clear error instead of a silently empty snapshot.
func validateSettingsUsageRange(r SettingsUsageRange) error {
	switch r {
	case SettingsUsageRangeAll, SettingsUsageRange7d, SettingsUsageRange30d, SettingsUsageRange90d:
		return nil
	}
	return fmt.Errorf("invalid settings/usage range %q", r)
}

// settingsUsageRangeCutoff returns the exclusive lower-bound timestamp
// for the requested range. "all" (and the empty string) returns nil,
// meaning no time filter applies.
func settingsUsageRangeCutoff(r SettingsUsageRange, now time.Time) *time.Time {
	switch r {
	case SettingsUsageRangeAll, "":
		return nil
	case SettingsUsageRange7d:
		c := now.AddDate(0, 0, -7)
		return &c
	case SettingsUsageRange30d:
		c := now.AddDate(0, 0, -30)
		return &c
	case SettingsUsageRange90d:
		c := now.AddDate(0, 0, -90)
		return &c
	}
	return nil
}
