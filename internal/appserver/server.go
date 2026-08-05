package appserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/blueberrycongee/wuu/internal/activity"
	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/channels"
	"github.com/blueberrycongee/wuu/internal/config"
	"github.com/blueberrycongee/wuu/internal/credentialstore"
	"github.com/blueberrycongee/wuu/internal/execution"
	"github.com/blueberrycongee/wuu/internal/mcp"
	"github.com/blueberrycongee/wuu/internal/modelcatalog"
	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/process"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/runtime"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/sidethread"
	"github.com/blueberrycongee/wuu/internal/statepath"
	"github.com/blueberrycongee/wuu/internal/subagent"
)

var (
	errServerClosed        = errors.New("app-server is closed")
	errShutdown            = errors.New("app-server shutdown requested")
	errExecutionRunChanged = errors.New("thread execution belongs to a different Run")
)

type threadState struct {
	ID           string
	Source       string
	NamedAgentID string
	ParentID     string
	AgentPath    string
	History      []providers.ChatMessage
	// historyHeadSeq is the physical append-only session_messages head that
	// History was reconstructed through. It must not be derived from the
	// logical messages: a checkpoint may retain no records or only old seqs.
	historyHeadSeq           int
	CreatedAt                time.Time
	UpdatedAt                time.Time
	LastAccessedAt           time.Time
	Title                    string
	ModelProvider            string
	Model                    string
	ModelVariant             string
	ModelEffort              string
	PermissionMode           string
	CWD                      string
	WorkspaceKind            WorkspaceKind
	ForkedFromID             string
	ForkedFromTurnID         string
	ForkedFromItemID         string
	WorktreePath             string
	WorktreeBaseHEAD         string
	WorktreeBaseRepo         string
	WorkspaceID              string
	PinnedAt                 *time.Time
	ArchivedAt               *time.Time
	Turns                    []Turn
	PersistHistory           bool
	ReadOnly                 bool
	Ephemeral                bool
	execRuntime              *runtime.ThreadRuntime
	pendingRuntimeReset      bool
	runtimeSelectionMutation bool
	runtimeSubscription      *threadRuntimeSubscription

	mu                     sync.Mutex
	running                bool
	currentTurn            string
	currentTurnKind        TurnKind
	currentExecutionRunID  string
	currentTurnResumed     bool
	runningProviderName    string
	runningModel           string
	cancel                 context.CancelFunc
	executionLease         *session.ThreadExecutionLease
	admissionReserved      bool
	pendingSteers          []providers.ChatMessage
	steerWake              chan struct{}
	steerWakeClosed        bool
	activeSteerDocument    *ActiveDocument
	activeSteerContextSet  bool
	steerDocumentOverrides []activeDocumentOverride
	interrupting           bool
	namedAgentRoomIDs      []string
	// Worker-tree freeze (turn/interrupt): while set, agent-completion drains
	// hold their pending synthetic turns. The next user-initiated turn folds
	// the whole-tree snapshot into its request (frozenTreeContext) and marks
	// the held completion results answered (frozenTreeResultIDs).
	workerTreeFrozen    bool
	frozenTreeContext   []agent.ContextSegment
	frozenTreeResultIDs []string

	nextItemIndex         int
	activeAgentItemID     string
	activeReasoningItemID string
	toolItems             map[string]string
	hiddenToolEvent       bool
}

type threadRuntimeSubscription struct {
	statusCh            chan subagent.Notification
	streamCh            chan subagent.StreamNotification
	processCh           chan process.Event
	processManager      *process.Manager
	terminalUnsubscribe func()
	done                chan struct{}
	wg                  sync.WaitGroup
	once                sync.Once
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

type backgroundLaunch struct {
	decision chan bool
	once     sync.Once
}

func (l *backgroundLaunch) Commit() {
	if l == nil {
		return
	}
	l.once.Do(func() { l.decision <- true })
}

func (l *backgroundLaunch) Cancel() {
	if l == nil {
		return
	}
	l.once.Do(func() { l.decision <- false })
}

type Server struct {
	rt      *runtime.Session
	out     io.Writer
	writeMu sync.Mutex

	settingsUsageMu           sync.Mutex
	settingsUsageCache        *settingsUsageCacheEntry
	channelAgentInsightsMu    sync.Mutex
	channelAgentInsightsCache *channelAgentInsightsCacheEntry

	// clientCalls is the pending table for server-initiated requests over the
	// negotiated reverse-RPC channel. Keyed by the
	// "srv-<seq>" id the core mints; each value is a buffered(1) chan that the
	// scanner goroutine delivers exactly one clientResponse into. clientCallMu
	// guards both the map and clientCallSeq; see callClient for the strict
	// register/deliver/delete deadlock discipline these fields require.
	clientCallMu  sync.Mutex
	clientCalls   map[string]chan clientResponse
	clientCallSeq uint64
	clientMethods map[string]struct{}

	// pushRegistrar is the host-side hook invoked by the device/push_*
	// methods. The desktop main pipeline leaves it nil so the methods
	// respond with "remote-only" errors; the remote-host package binds
	// a per-device registrar via WithPushRegistrar in RunStdioForDevice.
	pushRegistrar PushRegistrar

	mu      sync.Mutex
	threads map[string]*threadState

	runMu             sync.Mutex
	runStore          *execution.Store
	runs              map[string]*runTracker
	activeRunByThread map[string]string

	agentTerminalFinalizationMu sync.Mutex
	agentTerminalFinalizations  map[agentTerminalFinalizationKey]struct{}

	agentCompletionMu            sync.Mutex
	pendingAgentCompletionTurns  map[string][]agentCompletionTurn
	drainingAgentCompletionTurns map[string]bool

	queuedTurnMu        sync.Mutex
	pendingQueuedTurns  map[string][]queuedTurn
	drainingQueuedTurns map[string]bool
	heldUserWorkMu      sync.Mutex

	goalContinuationMu       sync.Mutex
	drainingGoalContinuation map[string]bool

	rewriteChatHistoryForTest           func(string, string, []providers.ChatMessage) error
	afterLifecycleHistoryAppendForTest  func(threadID string)
	deleteSessionForTest                func(string) (session.Session, error)
	afterWorkerShutdownStopWavesForTest func()
	beforeQueuedTurnBackgroundForTest   func()

	codexModelsMu   sync.Mutex
	codexModelCache map[string]map[string]config.ProviderModelConfig

	modelCatalogHTTPClient *http.Client
	modelCatalogCachePath  string
	modelCatalogURL        string

	// participantSummaryCache memoizes participant store lookups keyed
	// by participant ID. Ephemeral participants are immutable after
	// creation, so entries never need invalidation; failed lookups are
	// not cached, so a late store write is picked up on the next
	// resolve.
	participantMu           sync.Mutex
	participantSummaryCache map[string]participant.Summary

	// memoryOverviewCache memoizes the settings-panel overview essay per
	// notebook, keyed by (scope, participant_id). Entries are also persisted
	// under WuuHome so reopening the desktop does not immediately spend
	// another inference; automatic refreshes are limited to once per 12 hours.
	memoryOverviewMu             sync.Mutex
	memoryOverviewCache          map[string]memoryOverviewCacheEntry
	inferenceMaintenanceStop     chan struct{}
	inferenceMaintenanceDone     chan struct{}
	inferenceMaintenanceStopOnce sync.Once
	activityUnsubscribe          func()
	backgroundMu                 sync.Mutex
	backgroundWG                 sync.WaitGroup
	closeOnce                    sync.Once
	closed                       atomic.Bool
	presenceLease                *session.AppServerPresenceLease
	startupErr                   error

	// sideThreadStore persists side threads (1:<=1 binding per main
	// thread). Nil when SessionDir is unset; handleSideThreadOpen /
	// handleSideThreadGetHistory treat nil as the "feature off" path.
	sideThreadStore            *sidethread.Store
	channelService             *channels.Service
	channelMaintenanceStop     chan struct{}
	channelMaintenanceDone     chan struct{}
	channelMaintenanceStopOnce sync.Once
	namedAgentMu               sync.Mutex
	sideTurnMu                 sync.Mutex
	sideTurns                  map[string]*sideThreadTurn
}

func New(rt *runtime.Session, out io.Writer) *Server {
	store, err := credentialstore.NewDesktopStore()
	if err != nil {
		providers.DebugLogf("desktop credential store: %v", err)
	}
	return NewWithCredentialStore(rt, out, store, http.DefaultClient)
}

func NewWithCredentialStore(rt *runtime.Session, out io.Writer, store credentialstore.Store, httpClient *http.Client) *Server {
	s := &Server{
		rt:      rt,
		out:     out,
		threads: make(map[string]*threadState),

		pendingAgentCompletionTurns:  make(map[string][]agentCompletionTurn),
		drainingAgentCompletionTurns: make(map[string]bool),
		pendingQueuedTurns:           make(map[string][]queuedTurn),
		drainingQueuedTurns:          make(map[string]bool),
		drainingGoalContinuation:     make(map[string]bool),
		codexModelCache:              make(map[string]map[string]config.ProviderModelConfig),
		memoryOverviewCache:          make(map[string]memoryOverviewCacheEntry),
		inferenceMaintenanceStop:     make(chan struct{}),
		channelMaintenanceStop:       make(chan struct{}),
		sideTurns:                    make(map[string]*sideThreadTurn),
		clientCalls:                  make(map[string]chan clientResponse),
		clientMethods:                make(map[string]struct{}),
		runs:                         make(map[string]*runTracker),
		activeRunByThread:            make(map[string]string),
		modelCatalogHTTPClient:       httpClient,
	}
	if rt != nil {
		catalogHome := strings.TrimSpace(rt.WuuHome)
		if catalogHome == "" && strings.TrimSpace(rt.ConfigPath) != "" {
			catalogHome = filepath.Dir(rt.ConfigPath)
		}
		if catalogHome != "" {
			s.modelCatalogCachePath = filepath.Join(catalogHome, "modelcatalog.json")
		}
	}
	if s.modelCatalogCachePath != "" {
		if err := modelcatalog.LoadCache(s.modelCatalogCachePath); err != nil {
			providers.DebugLogf("model catalog cache: %v", err)
		}
	}
	bootOwner := false
	if rt != nil && strings.TrimSpace(rt.SessionDir) != "" {
		lease, first, err := session.AcquireAppServerPresence(rt.SessionDir)
		if err != nil {
			s.startupErr = fmt.Errorf("acquire app-server presence: %w", err)
			return s
		}
		s.presenceLease = lease
		bootOwner = first
	}
	if rt != nil && strings.TrimSpace(rt.SessionDir) != "" {
		s.sideThreadStore = sidethread.NewStore(filepath.Join(rt.SessionDir, "sidethreads"))
		runStore, err := execution.NewStore(rt.SessionDir)
		if err != nil {
			s.startupErr = fmt.Errorf("open execution run store: %w", err)
			return s
		}
		s.runStore = runStore
	}
	if rt != nil && strings.TrimSpace(rt.WuuHome) != "" {
		channelService, err := channels.Open(statepath.ChannelsDir(rt.WuuHome), nil)
		if err != nil {
			s.startupErr = fmt.Errorf("open channels store: %w", err)
			return s
		}
		s.channelService = channelService
		channelService.SetWakeSink(s)
		s.startChannelMaintenance()
	}
	if bootOwner {
		s.recoverSideThreadsOnBoot()
		s.settleOnBoot()
	}
	if s.presenceLease != nil {
		if err := s.presenceLease.FinalizeStartup(); err != nil {
			// Retain the startup/exclusive lock until Close. Blocking a peer is
			// safer than letting it misclassify this live server as crashed.
			s.startupErr = fmt.Errorf("finalize app-server presence: %w", err)
			return s
		}
	}
	if store != nil && rt != nil && rt.Toolkit != nil {
		if manager := rt.Toolkit.MCPManager(); manager != nil {
			manager.SetOAuthManager(mcp.NewOAuthManager(store, httpClient))
		}
	}
	if rt != nil && rt.ActivityRegistry != nil {
		s.activityUnsubscribe = rt.ActivityRegistry.Subscribe(func(event activity.Event) {
			s.notifyActivityEvent(event)
		})
	}
	s.startInferenceJournalMaintenance()
	if rt != nil && rt.AutomationManager != nil {
		if err := rt.AutomationManager.Start(s); err != nil {
			s.startupErr = fmt.Errorf("start automation manager: %w", err)
			return s
		}
	}
	if s.channelService != nil {
		s.startBackground(s.restoreNamedAgentWakes)
	}
	return s
}

// settleOnBoot reconciles orphaned provider operations without starting a turn.
// Recovery is best-effort so a transient journal error does not block startup.
func (s *Server) settleOnBoot() {
	if s == nil || s.rt == nil {
		return
	}
	now := time.Now().UTC()
	if s.rt.InferenceJournalRuntime != nil {
		recoveries, recoverErr := s.rt.InferenceJournalRuntime.ReconcileOrphans(now)
		if recoverErr != nil {
			providers.DebugLogf("settleOnBoot pass4 (inference journal): %v", recoverErr)
		} else if len(recoveries) > 0 {
			var safe, blocked, abandoned int
			for _, recovery := range recoveries {
				switch recovery.Action {
				case providers.RecoveryRescheduleSafe:
					safe++
				case providers.RecoveryBlockAmbiguous:
					blocked++
				default:
					abandoned++
				}
			}
			providers.DebugLogf("settleOnBoot pass4: inference operations recovered (safe=%d blocked=%d abandoned=%d)", safe, blocked, abandoned)
		}
		if pruned, pruneErr := s.rt.InferenceJournalRuntime.Prune(now); pruneErr != nil {
			providers.DebugLogf("settleOnBoot pass5 (inference journal retention): %v", pruneErr)
		} else if pruned > 0 {
			providers.DebugLogf("settleOnBoot pass5: pruned %d old inference operation(s)", pruned)
		}
		if s.runStore != nil {
			recovered, err := s.runStore.ReconcileOrphans(context.Background(), s.rt.InferenceJournalRuntime.RuntimeID(), now)
			if err != nil {
				providers.DebugLogf("settleOnBoot pass6 (execution runs): %v", err)
			} else if len(recovered) > 0 {
				providers.DebugLogf("settleOnBoot pass6: interrupted %d orphan execution run(s)", len(recovered))
			}
		}
	}
}

func (s *Server) startInferenceJournalMaintenance() {
	if s == nil || s.rt == nil || s.rt.InferenceJournalRuntime == nil {
		return
	}
	journalRuntime := s.rt.InferenceJournalRuntime
	s.inferenceMaintenanceDone = make(chan struct{})
	go func() {
		defer close(s.inferenceMaintenanceDone)
		ticker := time.NewTicker(session.InferenceJournalRecoveryInterval())
		defer ticker.Stop()
		for {
			select {
			case now := <-ticker.C:
				recoveries, err := journalRuntime.ReconcileOrphans(now.UTC())
				if err != nil {
					providers.DebugLogf("inference journal maintenance: %v", err)
					continue
				}
				if len(recoveries) > 0 {
					providers.DebugLogf("inference journal maintenance: recovered %d orphan operation(s)", len(recoveries))
				}
			case <-s.inferenceMaintenanceStop:
				return
			}
		}
	}()
}

func (s *Server) stopInferenceJournalMaintenance() {
	if s == nil || s.inferenceMaintenanceStop == nil {
		return
	}
	s.inferenceMaintenanceStopOnce.Do(func() {
		close(s.inferenceMaintenanceStop)
	})
	if s.inferenceMaintenanceDone != nil {
		<-s.inferenceMaintenanceDone
	}
}

const channelMaintenanceInterval = time.Minute

func (s *Server) startChannelMaintenance() {
	if s == nil || s.channelService == nil {
		return
	}
	s.channelMaintenanceDone = make(chan struct{})
	go func() {
		defer close(s.channelMaintenanceDone)
		if err := s.channelService.ExpireDrafts(context.Background()); err != nil {
			log.Printf("wuu: channels maintenance: %v", err)
		}
		if _, err := s.channelService.FireDueReminders(context.Background()); err != nil {
			log.Printf("wuu: channel reminders: %v", err)
		}
		ticker := time.NewTicker(channelMaintenanceInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := s.channelService.ExpireDrafts(context.Background()); err != nil {
					log.Printf("wuu: channels maintenance: %v", err)
				}
				if _, err := s.channelService.FireDueReminders(context.Background()); err != nil {
					log.Printf("wuu: channel reminders: %v", err)
				}
			case <-s.channelMaintenanceStop:
				return
			}
		}
	}()
}

func (s *Server) stopChannelMaintenance() {
	if s == nil || s.channelMaintenanceDone == nil {
		return
	}
	s.channelMaintenanceStopOnce.Do(func() {
		close(s.channelMaintenanceStop)
	})
	<-s.channelMaintenanceDone
}

func (s *Server) startBackground(work func()) bool {
	if s == nil || work == nil {
		return false
	}
	s.backgroundMu.Lock()
	if s.closed.Load() {
		s.backgroundMu.Unlock()
		return false
	}
	s.backgroundWG.Add(1)
	s.backgroundMu.Unlock()
	go func() {
		defer s.backgroundWG.Done()
		work()
	}()
	return true
}

// reserveBackground registers shutdown ownership before a caller publishes a
// started turn. The work remains gated until Commit; Cancel releases the owned
// goroutine without running it. Callers should defer Cancel immediately.
func (s *Server) reserveBackground(work func()) (*backgroundLaunch, bool) {
	if work == nil {
		return nil, false
	}
	launch := &backgroundLaunch{decision: make(chan bool, 1)}
	if !s.startBackground(func() {
		if <-launch.decision {
			work()
		}
	}) {
		return nil, false
	}
	return launch, true
}

// Close synchronously stops work owned by this app-server connection. It does
// not return until locally admitted turns, workers, and their durable terminal
// finalizers have released execution ownership. The shared runtime.Session
// remains owned by the caller and may be cleaned up after Close returns.
func (s *Server) Close() {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() {
		s.closed.Store(true)
		if s.rt != nil && s.rt.AutomationManager != nil {
			s.rt.AutomationManager.Stop()
		}
		s.cancelSideThreads()
		// Synchronize with startBackground so no new owned goroutine can be
		// added after the shutdown waiter begins.
		s.backgroundMu.Lock()
		s.backgroundMu.Unlock()

		s.mu.Lock()
		threads := make([]*threadState, 0, len(s.threads))
		for _, th := range s.threads {
			if th != nil {
				threads = append(threads, th)
			}
		}
		clear(s.threads)
		s.mu.Unlock()

		// Close worker-turn admission before the first cancellation wave. BeginShutdown
		// synchronizes with any Manager.Spawn/Followup already at its commit point,
		// so StopAll cannot miss a worker that appears immediately behind it.
		controls := make(map[*agentcontrol.AgentControl]struct{})
		collectThreadAgentControls(threads, controls)
		for control := range controls {
			control.BeginShutdown()
		}
		// Cancellation is asynchronous, so issue it to all threads first instead
		// of serializing shutdown behind one provider.
		for _, th := range threads {
			th.mu.Lock()
			cancel := th.cancel
			th.mu.Unlock()
			if cancel != nil {
				cancel()
			}
		}
		for control := range controls {
			control.StopAll()
			control.YieldWorkerTerminalFinalizations()
		}

		s.stopInferenceJournalMaintenance()
		s.stopChannelMaintenance()

		// Stop every browser activity this process owns BEFORE dropping the
		// activity subscription below. Stop emits an EventStopped that
		// notifyActivityEvent forwards to the desktop so it can tear down the
		// backing WebContentsView; stdout is still writable here. Ordered after
		// unsubscribe the event would have no listener and the desktop would
		// leak a hidden view plus a ghost activity in the UI.
		s.stopBrowserActivitiesAndEmit()

		if s.activityUnsubscribe != nil {
			s.activityUnsubscribe()
			s.activityUnsubscribe = nil
		}

		// Release any in-flight server-initiated calls. Turn-context
		// cancellation above already unblocks callClient waiters via ctx.Done;
		// this is the belt-and-suspenders sweep for calls whose ctx outlives
		// Close. Delivery is non-blocking (buffered chans) so closeOnce can
		// never wedge the process on a shutdown drain.
		s.failPendingClientCalls()

		s.queuedTurnMu.Lock()
		clear(s.pendingQueuedTurns)
		clear(s.drainingQueuedTurns)
		s.queuedTurnMu.Unlock()
		s.agentCompletionMu.Lock()
		clear(s.pendingAgentCompletionTurns)
		clear(s.drainingAgentCompletionTurns)
		s.agentCompletionMu.Unlock()
		s.goalContinuationMu.Lock()
		clear(s.drainingGoalContinuation)
		s.goalContinuationMu.Unlock()
		s.waitForOwnedShutdown(threads, controls)
		s.interruptAttachedRunsOnClose()
		for _, th := range threads {
			releaseThreadRuntime(th)
		}
		if s.channelService != nil {
			if err := s.channelService.Close(); err != nil {
				log.Printf("wuu: close channels store: %v", err)
			}
			s.channelService = nil
		}
		s.releasePresence()
	})
}

// ownedShutdownDrainTimeout bounds Close's wait for owned turns, workers, and
// their terminal finalizers. A wedged execution then surfaces as a loud log
// and a proceeding shutdown instead of a process that can never exit; durable
// terminal records and execution leases keep the drained state recoverable.
const ownedShutdownDrainTimeout = time.Minute

func (s *Server) waitForOwnedShutdown(threads []*threadState, controls map[*agentcontrol.AgentControl]struct{}) {
	if s == nil {
		return
	}
	deadline := time.Now().Add(ownedShutdownDrainTimeout)
	// Drain/title/side workers can still have admitted a turn immediately before
	// Close marked the server closed. Wait for those launchers first, then for
	// every turn/worker lease this Server owns to be released by its normal
	// terminal path. External owners are deliberately absent from these local
	// snapshots, so shutdown never waits for unrelated app-server processes.
	background := make(chan struct{})
	go func() {
		s.backgroundWG.Wait()
		close(background)
	}()
	select {
	case <-background:
	case <-time.After(ownedShutdownDrainTimeout):
		log.Printf("wuu: shutdown drain timed out after %s: owned background goroutines still running", ownedShutdownDrainTimeout)
	}
	// A launcher already inside startBackground may have attached a thread
	// runtime after the first shutdown snapshot. Once all launchers and turns
	// have stopped, collect those late local controls and cancel their workers
	// before waiting for the final execution leases.
	collectThreadAgentControls(threads, controls)
	for control := range controls {
		control.BeginShutdown()
		control.StopAll()
		control.YieldWorkerTerminalFinalizations()
	}
	if s.afterWorkerShutdownStopWavesForTest != nil {
		s.afterWorkerShutdownStopWavesForTest()
	}
	for shutdownExecutionActive(threads, controls) {
		if !time.Now().Before(deadline) {
			log.Printf("wuu: shutdown drain timed out after %s: releasing with owned executions still active", ownedShutdownDrainTimeout)
			forceReleaseAbandonedThreadExecutions(threads)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// forceReleaseAbandonedThreadExecutions breaks the execution leases a timed-out
// drain would otherwise abandon. In a process-exit shutdown the OS reclaims
// the flocks anyway, but an embedded host (remote device sessions) keeps the
// process alive: a stuck turn goroutine would hold its same-process lock
// forever and every successor app-server would see the thread as busy.
// Releasing the lease makes the durable state read exactly like a crashed
// owner — the turn meta still says running, so the successor's normal
// crash-recovery settles it. The stuck goroutine's own finalizer becomes a
// no-op: releaseTurnExecutionLocked ignores a turn id that no longer matches.
func forceReleaseAbandonedThreadExecutions(threads []*threadState) {
	for _, th := range threads {
		if th == nil {
			continue
		}
		th.mu.Lock()
		if th.executionLease != nil {
			log.Printf("wuu: shutdown abandoning turn %q on thread %q: force-releasing its execution lease for successor recovery", th.currentTurn, th.ID)
			th.releaseTurnExecutionLocked(th.currentTurn)
		}
		th.mu.Unlock()
	}
}

func collectThreadAgentControls(threads []*threadState, controls map[*agentcontrol.AgentControl]struct{}) {
	for _, th := range threads {
		if th == nil {
			continue
		}
		th.mu.Lock()
		threadRuntime := th.execRuntime
		th.mu.Unlock()
		if threadRuntime != nil && threadRuntime.AgentControl != nil {
			controls[threadRuntime.AgentControl] = struct{}{}
		}
	}
}

func (s *Server) releasePresence() {
	if s == nil || s.presenceLease == nil {
		return
	}
	lease := s.presenceLease
	s.presenceLease = nil
	if err := lease.Release(); err != nil {
		providers.DebugLogf("release app-server presence: %v", err)
	}
}

func shutdownExecutionActive(threads []*threadState, controls map[*agentcontrol.AgentControl]struct{}) bool {
	for _, th := range threads {
		if th == nil {
			continue
		}
		th.mu.Lock()
		owned := th.executionLease != nil || th.admissionReserved
		th.mu.Unlock()
		if owned {
			return true
		}
	}
	for control := range controls {
		if control != nil && control.HasOwnedWorkerExecutions() {
			return true
		}
	}
	return false
}

func RunStdio(ctx context.Context, rt *runtime.Session, in io.Reader, out io.Writer) error {
	return RunStdioForDevice(ctx, rt, in, out, nil)
}

// RunStdioForDevice runs the protocol loop for a remote device session,
// binding the device/push_* methods to the transport's per-device registrar.
// Local desktop sessions call RunStdio, which leaves the registrar nil so
// those methods fail explicitly instead of parking tokens nowhere.
func RunStdioForDevice(ctx context.Context, rt *runtime.Session, in io.Reader, out io.Writer, registrar PushRegistrar) error {
	if rt == nil {
		return errors.New("runtime session is required")
	}
	s := New(rt, out)
	s.pushRegistrar = registrar
	defer s.Close()
	if s.startupErr != nil {
		return s.startupErr
	}
	return runStdioScanner(ctx, s, in)
}

func (s *Server) handleLine(ctx context.Context, raw []byte) error {
	if s == nil {
		return errors.New("app-server is required")
	}
	if s.startupErr != nil {
		return s.startupErr
	}
	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		return s.writeResponse(nil, nil, fmt.Errorf("parse request: %w", err))
	}
	// A line with an id but no method is the desktop client's Response to a
	// server-initiated request (browser/*). Route it to the waiting caller
	// before the method switch, otherwise it falls through to default and gets
	// answered with an "unknown method" error, silently dropping the reply.
	if req.Method == "" && len(req.ID) > 0 {
		s.deliverClientResponse(raw)
		return nil
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
	case MethodExtensionCatalogRefresh:
		return s.handleExtensionCatalogRefresh(req)
	case MethodExtensionPackageUpdate:
		return s.handleExtensionPackageUpdate(req)
	case MethodConfigCodexModels:
		// Model discovery performs an external Codex request. Keep it off the
		// serial stdio dispatch loop so unrelated local mutations, especially a
		// model selection made from the same menu, are not queued behind network
		// latency. Response writes and the model cache are independently locked.
		if !s.startBackground(func() {
			if err := s.handleConfigCodexModels(ctx, req); err != nil {
				log.Printf("wuu: config/codex/models: %v", err)
			}
		}) {
			return s.writeResponse(req.ID, nil, errServerClosed)
		}
		return nil
	case MethodConfigCatalogRefresh:
		if !s.startBackground(func() {
			if err := s.handleConfigModelCatalogRefresh(ctx, req); err != nil {
				log.Printf("wuu: config/model-catalog/refresh: %v", err)
			}
		}) {
			return s.writeResponse(req.ID, nil, errServerClosed)
		}
		return nil
	case MethodConfigProviderRemove:
		return s.handleConfigProviderRemove(req)
	case MethodSkillList:
		return s.handleSkillList(req)
	case MethodChannelBootstrap:
		return s.handleChannelBootstrap(ctx, req)
	case MethodChannelAgentList:
		return s.handleChannelAgentList(ctx, req)
	case MethodChannelAgentInsights:
		return s.handleChannelAgentInsights(ctx, req)
	case MethodChannelAgentCreate:
		return s.handleChannelAgentCreate(ctx, req)
	case MethodChannelAgentUpdate:
		return s.handleChannelAgentUpdate(ctx, req)
	case MethodChannelAgentDelete:
		return s.handleChannelAgentDelete(ctx, req)
	case MethodChannelAgentStart:
		return s.handleChannelAgentStart(ctx, req)
	case MethodChannelAgentReset:
		return s.handleChannelAgentReset(ctx, req)
	case MethodChannelRoomList:
		return s.handleChannelRoomList(ctx, req)
	case MethodChannelRoomCreate:
		return s.handleChannelRoomCreate(ctx, req)
	case MethodChannelRoomUpdate:
		return s.handleChannelRoomUpdate(ctx, req)
	case MethodChannelRoomDelete:
		return s.handleChannelRoomDelete(ctx, req)
	case MethodChannelRoomRead:
		return s.handleChannelRoomRead(ctx, req)
	case MethodChannelMessageList:
		return s.handleChannelMessageList(ctx, req)
	case MethodChannelMessageSend:
		return s.handleChannelMessageSend(ctx, req)
	case MethodChannelTaskCreate:
		return s.handleChannelTaskCreate(ctx, req)
	case MethodChannelTaskUpdate:
		return s.handleChannelTaskUpdate(ctx, req)
	case MethodChannelMentionStatus:
		return s.handleChannelHumanMentionStatus(ctx, req)
	case MethodChannelMentionAck:
		return s.handleChannelHumanMentionAck(ctx, req)
	case MethodAutomationList:
		return s.handleAutomationList(req)
	case MethodAutomationRuns:
		return s.handleAutomationRuns(req)
	case MethodAutomationCreate:
		return s.handleAutomationCreate(req)
	case MethodAutomationUpdate:
		return s.handleAutomationUpdate(req)
	case MethodAutomationRemove:
		return s.handleAutomationRemove(req)
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
	case MethodSideThreadOpen:
		return s.handleSideThreadOpen(req)
	case MethodSideThreadGetHistory:
		return s.handleSideThreadGetHistory(req)
	case MethodSideThreadSend:
		return s.handleSideThreadSendMessage(req)
	case MethodSideThreadInterrupt:
		return s.handleSideThreadInterrupt(req)
	case MethodSideThreadReset:
		return s.handleSideThreadReset(req)
	case MethodThreadList:
		return s.handleThreadList(req)
	case MethodThreadListArchived:
		return s.handleThreadListArchived(req)
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
	case MethodThreadRegenerateTitle:
		return s.handleThreadRegenerateTitle(ctx, req)
	case MethodMemoryRead:
		return s.handleMemoryRead(req)
	case MethodMemoryOverview:
		return s.handleMemoryOverview(req)
	case MethodMemoryChat:
		return s.handleMemoryChat(req)
	case MethodTextPolish:
		return s.handleTextPolish(req)
	case MethodGitCommitMessage:
		return s.handleGitCommitMessage(req)
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
	case MethodRunStart:
		return s.handleRunStart(ctx, req)
	case MethodRunInterrupt:
		return s.handleRunInterrupt(ctx, req)
	case MethodProcessList:
		return s.handleProcessList(req)
	case MethodProcessRead:
		return s.handleProcessRead(ctx, req)
	case MethodProcessWrite:
		return s.handleProcessWrite(req)
	case MethodProcessResize:
		return s.handleProcessResize(req)
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
	case MethodMCPAuthStart:
		return s.handleMCPAuthStart(ctx, req)
	case MethodMCPAuthStatus:
		return s.handleMCPAuthStatus(ctx, req)
	case MethodMCPAuthFinish:
		return s.handleMCPAuthFinish(ctx, req)
	case MethodMCPAuthRemove:
		return s.handleMCPAuthRemove(ctx, req)
	case MethodActivityList:
		return s.handleActivityList(req)
	case MethodActivityTakeover:
		return s.handleActivityTakeover(req)
	case MethodActivityRelease:
		return s.handleActivityRelease(req)
	case MethodActivityStop:
		return s.handleActivityStop(req)
	case MethodShutdown:
		if err := s.writeResponse(req.ID, OKResult{OK: true}, nil); err != nil {
			return err
		}
		s.Close()
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
	if ev.ToolResultDetail != nil {
		detail := ev.ToolResultDetail.Clone()
		out.ToolResultDetail = &detail
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
	payload := &StreamLifecyclePayload{
		Phase:           string(lifecycle.Phase),
		OperationID:     lifecycle.OperationID,
		OperationKind:   string(lifecycle.OperationKind),
		WorkloadProfile: string(lifecycle.WorkloadProfile),
		PayloadVersion:  lifecycle.PayloadVersion,
		AttemptID:       lifecycle.AttemptID,
		Attempt:         lifecycle.Attempt,
		MaxAttempts:     lifecycle.MaxAttempts,
		SubmissionID:    lifecycle.SubmissionID,
		SubmissionCount: lifecycle.SubmissionCount,
		RetryCount:      lifecycle.RetryCount,
		MaxRetries:      lifecycle.MaxRetries,
		RetryInMS:       durationMilliseconds(lifecycle.RetryIn),
		ElapsedMS:       durationMilliseconds(lifecycle.Elapsed),
		Reason:          lifecycle.Reason,
		FailureCategory: lifecycle.FailureCategory,
		RecoveryAction:  lifecycle.RecoveryAction,
		BudgetDimension: lifecycle.BudgetDimension,
		ReplayReason:    lifecycle.ReplayReason,
		ResetPartial:    lifecycle.ResetPartial,
	}
	if lifecycle.Workflow.WorkflowID != "" {
		workflow := lifecycle.Workflow
		payload.Workflow = &WorkflowSnapshotPayload{
			ID: workflow.WorkflowID, Operations: workflow.Operations,
			Attempts: workflow.Attempts, Submissions: workflow.Submissions,
			TransportSwitches:          workflow.TransportSwitches,
			CredentialRefreshes:        workflow.CredentialRefreshes,
			PayloadTransforms:          workflow.PayloadTransforms,
			ChildOperations:            workflow.ChildOperations,
			RecoveryWaitMS:             workflow.RecoveryWaitMillis,
			KnownSubmissions:           workflow.KnownSubmissions,
			EstimatedSubmissions:       workflow.EstimatedSubmissions,
			UnknownBillableSubmissions: workflow.UnknownBillableSubmissions,
			KnownInputTokens:           workflow.KnownUsage.InputTokens,
			KnownOutputTokens:          workflow.KnownUsage.OutputTokens,
			EstimatedInputTokens:       workflow.EstimatedUsage.InputTokens,
			EstimatedOutputTokens:      workflow.EstimatedUsage.OutputTokens,
		}
	}
	return payload
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
