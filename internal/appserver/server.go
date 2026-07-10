package appserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/config"
	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/runtime"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/subagent"
)

var errShutdown = errors.New("app-server shutdown requested")

type threadState struct {
	ID               string
	ParentID         string
	AgentPath        string
	History          []providers.ChatMessage
	CreatedAt        time.Time
	UpdatedAt        time.Time
	LastAccessedAt   time.Time
	Title            string
	ModelProvider    string
	Model            string
	CWD              string
	WorkspaceKind    WorkspaceKind
	ForkedFromID     string
	ForkedFromTurnID string
	ForkedFromItemID string
	WorktreePath     string
	WorktreeBaseHEAD string
	WorktreeBaseRepo string
	PinnedAt         *time.Time
	ArchivedAt       *time.Time
	DMParticipantID  string
	Group            bool
	// FocusWorkspace mirrors session.Session.FocusWorkspace: the workspace
	// focus this chat-style thread most recently declared ("" = all
	// registered workspaces, "~" = agent home only, otherwise a registered
	// workspace name). Kept in sync with the session store on every switch.
	FocusWorkspace string
	// focusDeclarationStale is set when a context-compaction pass folds this
	// thread's history into a summary that may not have preserved the
	// current focus declaration (2026-07-03-workspace-focus.md §7). While
	// set, the next applyTurnWorkspaceFocus call re-declares the focus even
	// if the requested value matches the stored one, and clears the flag.
	focusDeclarationStale bool
	Turns                 []Turn
	PersistHistory        bool
	ReadOnly              bool
	Ephemeral             bool
	BrowserState          ThreadBrowserState

	execRuntime          *runtime.ThreadRuntime
	pendingRuntimeUpdate *threadRuntimeUpdate
	runtimeSubscription  *threadRuntimeSubscription

	mu                  sync.Mutex
	running             bool
	currentTurn         string
	currentTurnKind     TurnKind
	runningProviderName string
	runningModel        string
	cancel              context.CancelFunc
	pendingSteers       []providers.ChatMessage

	nextItemIndex         int
	activeAgentItemID     string
	activeReasoningItemID string
	toolItems             map[string]string
	hiddenToolEvent       bool
}

type threadRuntimeSubscription struct {
	statusCh             chan subagent.Notification
	streamCh             chan subagent.StreamNotification
	participantMessageCh chan agentcontrol.ParticipantMessage
	done                 chan struct{}
	wg                   sync.WaitGroup
	once                 sync.Once
}

func (sub *threadRuntimeSubscription) stop() {
	if sub == nil {
		return
	}
	sub.once.Do(func() {
		close(sub.done)
	})
	sub.wg.Wait()
}

type threadRuntimeUpdate struct {
	ProviderName     string
	RuleProviderName string
	Model            string
	APIModel         string
	SystemPrompt     string
}

type Server struct {
	rt      *runtime.Session
	out     io.Writer
	writeMu sync.Mutex

	// pushRegistrar is the host-side hook invoked by the device/push_*
	// methods. The desktop main pipeline leaves it nil so the methods
	// respond with "remote-only" errors; the remote-host package binds
	// a per-device registrar via WithPushRegistrar in RunStdioForDevice.
	pushRegistrar PushRegistrar

	mu      sync.Mutex
	threads map[string]*threadState

	pendingMu       sync.Mutex
	nextServerReqID int64
	pendingRequests map[string]chan clientResponse

	agentCompletionMu            sync.Mutex
	pendingAgentCompletionTurns  map[string][]agentCompletionTurn
	drainingAgentCompletionTurns map[string]bool

	queuedTurnMu        sync.Mutex
	pendingQueuedTurns  map[string][]queuedTurn
	drainingQueuedTurns map[string]bool

	goalContinuationMu       sync.Mutex
	drainingGoalContinuation map[string]bool

	residentDrainMu       sync.Mutex
	drainingResidentAgent map[string]bool

	idleUnreadWakeMu           sync.Mutex
	idleUnreadWakeTimers       map[string]*time.Timer
	idleUnreadWakeWaveByThread map[string]int
	idleUnreadWakeLastSpeaker  map[string]string
	idleUnreadWakeDelayForTest func(wave int) time.Duration
	idleUnreadWakeRand         *rand.Rand

	// residentTurnSpeech maps a participant id to its current turn's speech
	// limiter, so afterResidentTurn can tell whether the resident already spoke
	// through post_message/decline before deciding to fire the plain-text
	// fallback. One resident turn at a time (drain lock + th.running) keeps the
	// per-key writes sequential.
	residentTurnSpeech sync.Map

	// threadPostMu serializes the freshness-check-then-publish critical section
	// per target thread (threadID -> *sync.Mutex). The held-draft check reads
	// the thread tail and then publishParticipantMessage appends to it as two
	// steps; without this lock, N residents racing to post the same thing all
	// pass the staleness read before any of them commits, then all publish
	// duplicates (TOCTOU). Holding the per-thread lock across both makes each
	// racer's freshness check observe the prior committed post and hold.
	threadPostMu sync.Map

	// participantBusyMu guards participantBusy, the registry of named
	// agents currently executing a task run (decision-five
	// concurrency lock). A named agent is "busy" for exactly the lifetime
	// of a live participant run: acquired when the run reports Running (or
	// synchronously at participant/start), released when it terminates.
	// While busy, a second task-pull is refused and an @-mention chat drain
	// is deferred, so two callers never grab the same resident agent at
	// once. See internal/appserver/participant_busy.go.
	participantBusyMu sync.Mutex
	participantBusy   map[string]participantBusyEntry

	// forkMergeMu guards forkMergeLocks, the per-母体 merge-back queue
	// (decision six). A fork's memory回流 into its母体 acquires the母体's
	// lock so concurrent merges (母体 forked twice, both副本 retiring at once)
	// serialize their append-merge — deterministic same-topic conflict
	// resolution requires it (internal/memdir has no locking). See
	// internal/appserver/participant_fork.go.
	forkMergeMu    sync.Mutex
	forkMergeLocks map[string]*sync.Mutex

	// allChannelMu serializes ensureAllChannel so concurrent thread/list
	// calls cannot race to create two "all" group threads.
	allChannelMu sync.Mutex

	codexModelsMu   sync.Mutex
	codexModelCache map[string]map[string]config.ProviderModelConfig

	// participantSummaryCache memoizes participant store lookups keyed
	// by participant ID. Ephemeral participants are immutable after
	// creation, so entries never need invalidation; failed lookups are
	// not cached, so a late store write is picked up on the next
	// resolve.
	participantMu           sync.Mutex
	participantSummaryCache map[string]participant.Summary

	// memoryOverviewCache memoizes the settings-panel overview essay per
	// notebook, keyed by (scope, participant_id). An entry is served only
	// while the notebook's MEMORY.md mtime is unchanged (memory-redesign
	// §8.1); memory/chat drops the affected entries after a successful
	// manager run.
	memoryOverviewMu    sync.Mutex
	memoryOverviewCache map[string]memoryOverviewCacheEntry
}

func New(rt *runtime.Session, out io.Writer) *Server {
	s := &Server{
		rt:              rt,
		out:             out,
		threads:         make(map[string]*threadState),
		pendingRequests: make(map[string]chan clientResponse),

		pendingAgentCompletionTurns:  make(map[string][]agentCompletionTurn),
		drainingAgentCompletionTurns: make(map[string]bool),
		pendingQueuedTurns:           make(map[string][]queuedTurn),
		drainingQueuedTurns:          make(map[string]bool),
		drainingGoalContinuation:     make(map[string]bool),
		drainingResidentAgent:        make(map[string]bool),
		idleUnreadWakeTimers:         make(map[string]*time.Timer),
		idleUnreadWakeWaveByThread:   make(map[string]int),
		idleUnreadWakeLastSpeaker:    make(map[string]string),
		idleUnreadWakeRand:           rand.New(rand.NewSource(time.Now().UnixNano())),
		participantBusy:              make(map[string]participantBusyEntry),
		codexModelCache:              make(map[string]map[string]config.ProviderModelConfig),
		memoryOverviewCache:          make(map[string]memoryOverviewCacheEntry),
	}
	if rt != nil {
		// Only seed Andy when the runtime is actually usable: test-only
		// sessions frequently leave SessionDir/WuuHome empty, and the seed
		// would otherwise log a workspace error for every unrelated
		// appserver test.
		if strings.TrimSpace(rt.SessionDir) != "" && strings.TrimSpace(rt.WuuHome) != "" {
			logDefaultParticipantSeedError(s.ensureDefaultParticipant())
		}
	}
	// Production-schema migration (issue #3 pivot): pre-existing
	// databases from before the pivot don't have the `expired_at`
	// column (CREATE TABLE IF NOT EXISTS is a no-op for existing
	// tables). One-shot ALTER closes that gap; idempotent for
	// newDBs (the migration helper itself swallows "duplicate
	// column" as success). Settle then runs against the
	// now-up-to-date schema.
	if err := session.MigrateResidentInboxExpiredAt(s.rt.SessionDir); err != nil {
		providers.DebugLogf("resident_inbox expired_at migration: %v", err)
	}
	// Boot-time settle (issue #3 pivot, user spec: "重启不替过去补课",
	// "不起任何回合、不烧一个 token"). A previous process may have left
	// envelopes pending in the resident inbox or read receipts in_progress
	// when it died. settleOnBoot flips both to terminal expired states
	// without kicking the resident or starting any turn — boot must not
	// burn tokens for the previous process's unfinished work.
	s.settleOnBoot()
	return s
}

// settleOnBoot is the issue #3 pivot's boot-time replace for the
// previous "drain and replay" path (issue #3 round 1). The user pivot
// was "replay → settle/expire": boot can't burn tokens for the
// previous process's unprocessed envelopes. Two passes run, each
// against a single SQL statement:
//
//	pass 1: scan resident_inbox WHERE consumed_at IS NULL AND
//	        expired_at IS NULL, set expired_at=now (terminal "expired"
//	        state, distinguishable from "failed" by the front-end).
//	pass 2: scan message_marks WHERE kind='seen' AND status='in_progress',
//	        set status='expired_unprocessed' (terminal "we didn't get a
//	        turn" state, distinguishable from "failed" by the front-end).
//	pass 3: interrupt queued/running task attempts, clear their node binding,
//	        and pause each Task for its lead without starting a turn.
//
// ❌ Does NOT call kickResidentAgent.
// ❌ Does NOT start any turn.
// ❌ Does NOT burn any token.
//
// Errors per pass are logged + swallowed — boot settle is best-effort,
// a transient DB error must not block New() (issue #3 spec: "settle
// 自身失败不能阻塞 New() 返回"). Both passes use the same `now`
// timestamp so an audit log shows the boot moment uniformly.
func (s *Server) settleOnBoot() {
	if s == nil || s.rt == nil {
		return
	}
	now := time.Now().UTC()
	if n, err := session.MarkPendingResidentEnvelopesExpired(s.rt.SessionDir, now); err != nil {
		providers.DebugLogf("settleOnBoot pass1 (envelope expire): %v", err)
	} else if n > 0 {
		providers.DebugLogf("settleOnBoot pass1: %d envelope(s) expired", n)
	}
	if n, err := session.MarkStuckInProgressReadReceiptsExpired(s.rt.SessionDir, now); err != nil {
		providers.DebugLogf("settleOnBoot pass2 (receipt settle): %v", err)
	} else if n > 0 {
		providers.DebugLogf("settleOnBoot pass2: %d receipt(s) flipped to expired_unprocessed", n)
	}
	attempts, err := session.SettleActiveTaskAttempts(s.rt.SessionDir, now)
	if err != nil {
		providers.DebugLogf("settleOnBoot pass3 (task attempts): %v", err)
		return
	}
	for _, attempt := range attempts {
		task, loadErr := session.FindConversationThreadByID(s.rt.SessionDir, attempt.TaskID)
		if loadErr != nil {
			providers.DebugLogf("settleOnBoot load interrupted task %q: %v", attempt.TaskID, loadErr)
			continue
		}
		s.recordTaskEventForAttempt(task, attempt.NodeID, attempt.ID, session.TaskEventBlocked,
			attempt.AssigneeID, "attempt interrupted by app restart", "")
		s.notifySubthreadUpdated(task.SessionID, task.ID)
	}
	if len(attempts) > 0 {
		providers.DebugLogf("settleOnBoot pass3: %d task attempt(s) interrupted", len(attempts))
	}
}

func RunStdio(ctx context.Context, rt *runtime.Session, in io.Reader, out io.Writer) error {
	if rt == nil {
		return errors.New("runtime session is required")
	}
	s := New(rt, out)
	return runStdioScanner(ctx, s, in)
}

func (s *Server) handleLine(ctx context.Context, raw []byte) error {
	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		return s.writeResponse(nil, nil, fmt.Errorf("parse request: %w", err))
	}
	if strings.TrimSpace(req.Method) == "" {
		return s.handleClientResponse(raw)
	}
	switch req.Method {
	case MethodInitialize:
		return s.handleInitialize(req)
	case MethodConfigRead:
		return s.handleConfigRead(req)
	case MethodConfigModelUpdate:
		return s.handleConfigModelUpdate(req)
	case MethodConfigAdvancedUpdate:
		return s.handleConfigAdvancedUpdate(req)
	case MethodConfigGeneralUpdate:
		return s.handleConfigGeneralUpdate(req)
	case MethodConfigCodexModels:
		return s.handleConfigCodexModels(ctx, req)
	case MethodConfigProviderRemove:
		return s.handleConfigProviderRemove(req)
	case MethodSkillList:
		return s.handleSkillList(req)
	case MethodGoalActiveSummary:
		return s.handleGoalActiveSummary(req)
	case MethodGoalPause:
		return s.handleGoalPause(req)
	case MethodGoalResume:
		return s.handleGoalResume(req)
	case MethodGoalClear:
		return s.handleGoalClear(req)
	case MethodGoalUpdateText:
		return s.handleGoalUpdateText(req)
	case MethodThreadStart:
		return s.handleThreadStart(req)
	case MethodThreadResume:
		return s.handleThreadResume(req)
	case MethodThreadFork:
		return s.handleThreadFork(req)
	case MethodThreadEditMessage:
		return s.handleThreadEditMessage(req)
	case MethodThreadContextComposition:
		return s.handleThreadContextComposition(req)
	case MethodInstructionsList:
		return s.handleInstructionsList(req)
	case MethodThreadOpenSub:
		return s.handleThreadOpenSub(req)
	case MethodThreadListSub:
		return s.handleThreadListSub(req)
	case MethodThreadResolveSub:
		return s.handleThreadResolveSub(req)
	case MethodThreadEscalateSub:
		return s.handleThreadEscalateSub(req)
	case MethodThreadTaskEvents:
		return s.handleThreadTaskEvents(req)
	case MethodThreadList:
		return s.handleThreadList(req)
	case MethodThreadSearch:
		return s.handleThreadSearch(req)
	case MethodThreadPreview:
		return s.handleThreadPreview(req)
	case MethodThreadPin:
		return s.handleThreadPin(req)
	case MethodThreadArchive:
		return s.handleThreadArchive(req)
	case MethodThreadCompactStart:
		return s.handleThreadCompactStart(ctx, req)
	case MethodThreadRename:
		return s.handleThreadRename(req)
	case MethodThreadDelete:
		return s.handleThreadDelete(req)
	case MethodWorkspaceStateCleanup:
		return s.handleWorkspaceStateCleanup(req)
	case MethodThreadMembersAdd:
		return s.handleThreadMembersAdd(req)
	case MethodThreadMembersRemove:
		return s.handleThreadMembersRemove(req)
	case MethodThreadMarks:
		return s.handleThreadMarks(req)
	case MethodMessageReact:
		return s.handleMessageReact(req)
	case MethodMessagePostSubthread:
		return s.handleMessagePostSubthread(req)
	case MethodThreadRegenerateTitle:
		return s.handleThreadRegenerateTitle(ctx, req)
	case MethodParticipantStart:
		return s.handleParticipantStart(ctx, req)
	case MethodParticipantList:
		return s.handleParticipantList(req)
	case MethodParticipantSave:
		return s.handleParticipantSave(req)
	case MethodParticipantFeedback:
		return s.handleParticipantFeedback(req)
	case MethodParticipantReset:
		return s.handleParticipantReset(req)
	case MethodParticipantRetire:
		return s.handleParticipantRetire(req)
	case MethodMemoryRead:
		return s.handleMemoryRead(req)
	case MethodMemoryOverview:
		return s.handleMemoryOverview(req)
	case MethodMemoryChat:
		return s.handleMemoryChat(req)
	case MethodTurnStart:
		return s.handleTurnStart(ctx, req)
	case MethodTurnQueue:
		return s.handleTurnQueue(req)
	case MethodTurnUpdateQueued:
		return s.handleTurnUpdateQueued(req)
	case MethodTurnDequeue:
		return s.handleTurnDequeue(req)
	case MethodTurnSteer:
		return s.handleTurnSteer(req)
	case MethodTurnUnsteer:
		return s.handleTurnUnsteer(req)
	case MethodTurnInterrupt:
		return s.handleTurnInterrupt(req)
	case MethodProcessList:
		return s.handleProcessList(req)
	case MethodProcessStop:
		return s.handleProcessStop(req)
	case MethodMCPList:
		return s.handleMCPList(req)
	case MethodMCPConnect:
		return s.handleMCPConnect(ctx, req)
	case MethodMCPDisconnect:
		return s.handleMCPDisconnect(req)
	case MethodMCPRefresh:
		return s.handleMCPRefresh(ctx, req)
	case MethodShutdown:
		if err := s.writeResponse(req.ID, OKResult{OK: true}, nil); err != nil {
			return err
		}
		return errShutdown
	case MethodSettingsUsage:
		return s.handleSettingsUsage(req)
	case MethodDevicePushRegister:
		return s.handleDevicePushRegister(req)
	case MethodDevicePushUnregister:
		return s.handleDevicePushUnregister(req)
	default:
		return s.writeResponse(req.ID, nil, fmt.Errorf("unknown method %q", req.Method))
	}
}

func (s *Server) handleClientResponse(raw []byte) error {
	var resp ClientResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return s.writeResponse(nil, nil, fmt.Errorf("parse response: %w", err))
	}
	key := requestIDKey(resp.ID)
	if key == "" {
		return s.writeResponse(nil, nil, errors.New("response id is required"))
	}
	s.pendingMu.Lock()
	ch := s.pendingRequests[key]
	if ch != nil {
		delete(s.pendingRequests, key)
	}
	s.pendingMu.Unlock()
	if ch == nil {
		return nil
	}
	ch <- clientResponse{result: resp.Result, err: resp.Error}
	return nil
}

func (s *Server) thread(id string) *threadState {
	s.mu.Lock()
	th := s.threads[id]
	s.mu.Unlock()
	if th == nil {
		return nil
	}
	th.mu.Lock()
	th.LastAccessedAt = time.Now().UTC()
	th.mu.Unlock()
	return th
}

type clientResponse struct {
	result json.RawMessage
	err    *ResponseError
}

func (s *Server) requestClient(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := s.nextServerRequestID()
	rawID := json.RawMessage(strconv.Quote(id))
	key := requestIDKey(rawID)
	ch := make(chan clientResponse, 1)

	s.pendingMu.Lock()
	s.pendingRequests[key] = ch
	s.pendingMu.Unlock()

	if err := s.writeJSON(ServerRequest{ID: rawID, Method: method, Params: params}); err != nil {
		s.pendingMu.Lock()
		delete(s.pendingRequests, key)
		s.pendingMu.Unlock()
		return nil, err
	}

	select {
	case resp := <-ch:
		if resp.err != nil {
			return nil, errors.New(resp.err.Message)
		}
		return resp.result, nil
	case <-ctx.Done():
		s.pendingMu.Lock()
		delete(s.pendingRequests, key)
		s.pendingMu.Unlock()
		return nil, ctx.Err()
	}
}

func (s *Server) nextServerRequestID() string {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	s.nextServerReqID++
	return fmt.Sprintf("server-%d", s.nextServerReqID)
}

func requestIDKey(raw json.RawMessage) string {
	return strings.TrimSpace(string(raw))
}

func sanitizeStreamEvent(ev providers.StreamEvent) StreamEventPayload {
	out := StreamEventPayload{
		Type:      ev.Type,
		Content:   ev.Content,
		Truncated: ev.Truncated,
	}
	if ev.Message != nil && !ev.Message.Hidden {
		out.Message = ev.Message
	}
	if ev.ToolCall != nil {
		out.ToolCall = ev.ToolCall
	}
	if ev.ToolResult != "" {
		out.ToolResult = ev.ToolResult
	}
	if ev.PlanUpdate != nil {
		out.PlanUpdate = ev.PlanUpdate
	}
	if ev.Lifecycle != nil {
		out.Lifecycle = sanitizeStreamLifecycle(ev.Lifecycle)
	}
	if ev.RequestContext != nil {
		out.RequestContext = ev.RequestContext
	}
	if ev.ProviderState != nil {
		out.ProviderState = ev.ProviderState
	}
	if ev.Usage != nil {
		out.Usage = ev.Usage
	}
	if ev.StopReason != "" {
		out.StopReason = ev.StopReason
	}
	if ev.FinishReason != "" {
		out.FinishReason = string(ev.FinishReason)
	}
	if ev.Error != nil {
		out.Error = ev.Error.Error()
	}
	return out
}

func sanitizeStreamLifecycle(lifecycle *providers.StreamLifecycle) *StreamLifecyclePayload {
	if lifecycle == nil {
		return nil
	}
	return &StreamLifecyclePayload{
		Phase:      string(lifecycle.Phase),
		Attempt:    lifecycle.Attempt,
		RetryCount: lifecycle.RetryCount,
		MaxRetries: lifecycle.MaxRetries,
		RetryInMS:  durationMilliseconds(lifecycle.RetryIn),
		Reason:     lifecycle.Reason,
	}
}

func durationMilliseconds(duration time.Duration) int64 {
	if duration <= 0 {
		return 0
	}
	return duration.Milliseconds()
}

func decodeParams(raw json.RawMessage, dst any) error {
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}
	return nil
}

func (s *Server) writeResponse(id json.RawMessage, result any, err error) error {
	resp := Response{ID: id, Result: result}
	if err != nil {
		resp.Result = nil
		resp.Error = &ResponseError{
			Code:    "error",
			Message: err.Error(),
		}
	}
	return s.writeJSON(resp)
}

func (s *Server) writeNotification(method string, params any) error {
	return s.writeJSON(Notification{
		Method: method,
		Params: params,
	})
}

func (s *Server) writeJSON(v any) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	enc := json.NewEncoder(s.out)
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("write app-server message: %w", err)
	}
	return nil
}
