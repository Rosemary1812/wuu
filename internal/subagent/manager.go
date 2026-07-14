package subagent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/blueberrycongee/wuu/internal/agent"
	wuucontext "github.com/blueberrycongee/wuu/internal/context"
	"github.com/blueberrycongee/wuu/internal/provideroptions"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/toolledger"
)

// Manager registers and orchestrates sub-agents. It is safe for
// concurrent use from multiple goroutines.
type Manager struct {
	client                 providers.StreamClient
	defaultModel           string
	defaultEffort          string
	defaultProviderOptions map[string]any
	defaultContextWindow   int
	defaultMaxInputTokens  int
	defaultOutputReserve   int
	defaultCompactTokens   int
	defaultTemperature     float64
	defaultCompactPct      float64
	defaultKeepRecent      int
	defaultDisableCompact  bool
	defaultJournal         providers.InferenceJournal
	toolLedgerFactory      func(string) (*toolledger.Ledger, error)

	mu        sync.Mutex
	agents    map[string]*SubAgent
	listeners []chan<- Notification
	streams   []chan<- StreamNotification
	// terminalObserver runs synchronously after the final snapshot is
	// persisted and before lossy status subscribers are notified. The
	// coordinator uses it for durable terminal bookkeeping that must complete
	// before another process can take ownership of the worker.
	terminalObserver func(Notification) error
	// terminalPrepare runs before a final worker snapshot is persisted. A
	// coordinator that needs crash recovery uses it to durably record the exact
	// terminal generation that the snapshot is about to publish.
	terminalPrepare func(Notification) error
}

type ManagerOptions struct {
	DefaultEffort           string
	DefaultProviderOptions  map[string]any
	ContextWindowOverride   int
	MaxInputTokens          int
	OutputReserveTokens     int
	CompactThresholdTokens  int
	CompactThresholdPct     float64
	Temperature             float64
	CompactKeepRecentTokens int
	DisableAutoCompact      bool
	// InferenceJournal is an infrastructure dependency fixed at manager
	// construction. UpdateDefaults intentionally does not replace it.
	InferenceJournal  providers.InferenceJournal
	ToolLedgerFactory func(ownerID string) (*toolledger.Ledger, error)
}

type managerDefaults struct {
	client            providers.StreamClient
	model             string
	effort            string
	options           map[string]any
	contextWindow     int
	maxInputTokens    int
	outputReserve     int
	compactTokens     int
	compactPct        float64
	temperature       float64
	keepRecent        int
	disableCompact    bool
	journal           providers.InferenceJournal
	toolLedgerFactory func(string) (*toolledger.Ledger, error)
}

type toolContextBlockProvider interface {
	ContextBlocks() []wuucontext.Block
}

// NewManager constructs a Manager backed by the given streaming LLM
// client. defaultModel is used when SpawnOptions.Model is empty.
func NewManager(client providers.StreamClient, defaultModel string) *Manager {
	return NewManagerWithOptions(client, defaultModel, ManagerOptions{})
}

// NewManagerWithOptions constructs a Manager with default request options for
// workers that do not override them per spawn.
func NewManagerWithOptions(client providers.StreamClient, defaultModel string, opts ManagerOptions) *Manager {
	return &Manager{
		client:                 client,
		defaultModel:           defaultModel,
		defaultEffort:          strings.TrimSpace(opts.DefaultEffort),
		defaultProviderOptions: provideroptions.Clone(opts.DefaultProviderOptions),
		defaultContextWindow:   opts.ContextWindowOverride,
		defaultMaxInputTokens:  opts.MaxInputTokens,
		defaultOutputReserve:   opts.OutputReserveTokens,
		defaultCompactTokens:   opts.CompactThresholdTokens,
		defaultTemperature:     opts.Temperature,
		defaultCompactPct:      opts.CompactThresholdPct,
		defaultKeepRecent:      opts.CompactKeepRecentTokens,
		defaultDisableCompact:  opts.DisableAutoCompact,
		defaultJournal:         opts.InferenceJournal,
		toolLedgerFactory:      opts.ToolLedgerFactory,
		agents:                 make(map[string]*SubAgent),
	}
}

// UpdateDefaults changes the defaults used by future sub-agent spawns. Running
// agents keep the runner they were started with.
func (m *Manager) UpdateDefaults(client providers.StreamClient, defaultModel string, opts ManagerOptions) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if client != nil {
		m.client = client
	}
	if strings.TrimSpace(defaultModel) != "" {
		m.defaultModel = strings.TrimSpace(defaultModel)
	}
	m.defaultEffort = strings.TrimSpace(opts.DefaultEffort)
	m.defaultProviderOptions = provideroptions.Clone(opts.DefaultProviderOptions)
	m.defaultContextWindow = opts.ContextWindowOverride
	m.defaultMaxInputTokens = opts.MaxInputTokens
	m.defaultOutputReserve = opts.OutputReserveTokens
	m.defaultCompactTokens = opts.CompactThresholdTokens
	m.defaultTemperature = opts.Temperature
	m.defaultCompactPct = opts.CompactThresholdPct
	m.defaultKeepRecent = opts.CompactKeepRecentTokens
	m.defaultDisableCompact = opts.DisableAutoCompact
}

func (m *Manager) defaultsSnapshot() managerDefaults {
	m.mu.Lock()
	defer m.mu.Unlock()
	return managerDefaults{
		client:            m.client,
		model:             m.defaultModel,
		effort:            m.defaultEffort,
		options:           provideroptions.Clone(m.defaultProviderOptions),
		contextWindow:     m.defaultContextWindow,
		maxInputTokens:    m.defaultMaxInputTokens,
		outputReserve:     m.defaultOutputReserve,
		compactTokens:     m.defaultCompactTokens,
		temperature:       m.defaultTemperature,
		compactPct:        m.defaultCompactPct,
		keepRecent:        m.defaultKeepRecent,
		disableCompact:    m.defaultDisableCompact,
		journal:           m.defaultJournal,
		toolLedgerFactory: m.toolLedgerFactory,
	}
}

// RuntimeDefaults is the exported subset of the manager's worker defaults
// needed by out-of-band model calls that run beside the worker runtime (for
// example the HelpMe parent-journal extraction): the shared stream client,
// the session's default model, and the context budget dimensions used to
// chunk long inputs.
type RuntimeDefaults struct {
	Client              providers.StreamClient
	Model               string
	ContextWindow       int
	MaxInputTokens      int
	OutputReserveTokens int
}

// RuntimeDefaults returns a snapshot of the manager's current defaults. The
// values track UpdateDefaults, so callers should fetch a fresh snapshot per
// use instead of caching one.
func (m *Manager) RuntimeDefaults() RuntimeDefaults {
	if m == nil {
		return RuntimeDefaults{}
	}
	d := m.defaultsSnapshot()
	return RuntimeDefaults{
		Client:              d.client,
		Model:               d.model,
		ContextWindow:       d.contextWindow,
		MaxInputTokens:      d.maxInputTokens,
		OutputReserveTokens: d.outputReserve,
	}
}

// Subscribe registers a channel that will receive notifications when
// sub-agent statuses change. The channel must be drained promptly;
// notifications are dropped if the channel is full (to avoid blocking
// the runner goroutine).
func (m *Manager) Subscribe(ch chan<- Notification) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listeners = append(m.listeners, ch)
}

func (m *Manager) Unsubscribe(ch chan<- Notification) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, listener := range m.listeners {
		if listener == ch {
			m.listeners = append(m.listeners[:i], m.listeners[i+1:]...)
			return
		}
	}
}

// SetTerminalObserver installs the reliable terminal-state consumer. A nil
// error acknowledges the transition; an error leaves it unacknowledged and is
// never converted into a best-effort status event. Status subscribers remain
// UI/event streams and receive the terminal notification only after the
// observer acknowledges it.
func (m *Manager) SetTerminalObserver(observer func(Notification) error) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.terminalObserver = observer
	m.mu.Unlock()
}

// SetTerminalPrepareObserver installs the first phase of reliable terminal
// publication. The hook is retried until it acknowledges the exact terminal
// generation; no final worker snapshot is persisted before that happens. A
// plain Manager with no hook preserves the original behavior.
func (m *Manager) SetTerminalPrepareObserver(prepare func(Notification) error) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.terminalPrepare = prepare
	m.mu.Unlock()
}

// SubscribeStream registers a channel that receives every stream event emitted
// by sub-agent turns. The receiver must keep draining the channel; stream
// notifications are not dropped because dropping deltas would corrupt the
// visible child-agent transcript.
func (m *Manager) SubscribeStream(ch chan<- StreamNotification) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.streams = append(m.streams, ch)
}

func (m *Manager) UnsubscribeStream(ch chan<- StreamNotification) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, listener := range m.streams {
		if listener == ch {
			m.streams = append(m.streams[:i], m.streams[i+1:]...)
			return
		}
	}
}

// Spawn launches a new sub-agent asynchronously. The returned SubAgent
// has Status == StatusPending or StatusRunning; callers can poll via
// Snapshot or wait via Wait.
func (m *Manager) Spawn(ctx context.Context, opts SpawnOptions) (*SubAgent, error) {
	if opts.Toolkit == nil {
		return nil, errors.New("toolkit is required")
	}
	if opts.Prompt == "" {
		return nil, errors.New("prompt is required")
	}

	defaults := m.defaultsSnapshot()
	model := opts.Model
	if model == "" {
		model = defaults.model
	}
	if model == "" {
		return nil, errors.New("no model configured")
	}
	client := opts.Client
	if client == nil {
		client = defaults.client
	}
	if client == nil {
		return nil, errors.New("no stream client configured")
	}

	id := strings.TrimSpace(opts.ID)
	if id == "" {
		id = newAgentID(opts.Type)
	}
	var toolLedger *toolledger.Ledger
	if defaults.toolLedgerFactory != nil {
		var err error
		toolLedger, err = defaults.toolLedgerFactory(id)
		if err != nil {
			return nil, fmt.Errorf("open tool ledger for subagent %q: %w", id, err)
		}
	}
	lifetime := opts.MaxLifetime
	subCtx, cancel := context.WithCancel(ctx)
	if lifetime > 0 {
		subCtx, cancel = context.WithTimeout(ctx, lifetime)
	}
	history := initialTurnHistory(opts)

	sa := &SubAgent{
		ID:              id,
		ParticipantID:   opts.ParticipantID,
		Type:            opts.Type,
		TaskName:        opts.TaskName,
		AgentProfile:    opts.AgentProfile,
		AgentPath:       opts.AgentPath,
		ParentID:        opts.ParentID,
		Description:     opts.Description,
		Status:          StatusRunning, // set synchronously so CountRunning sees it immediately
		StartedAt:       time.Now(),
		prompt:          opts.Prompt,
		systemPrompt:    opts.SystemPrompt,
		model:           model,
		modelPin:        opts.ModelPin,
		workerRoot:      opts.WorkerRoot,
		ultra:           opts.Ultra,
		toolkit:         opts.Toolkit,
		historyPath:     opts.HistoryPath,
		initialHistory:  opts.InitialHistory,
		history:         providers.CloneChatMessages(history),
		maxSteps:        opts.MaxSteps,
		maxLifetime:     lifetime,
		runtimeDefaults: defaults,
		client:          client,
		toolLedger:      toolLedger,
		cancelFunc:      cancel,
		doneCh:          make(chan struct{}),
	}

	m.mu.Lock()
	if _, exists := m.agents[id]; exists {
		m.mu.Unlock()
		cancel()
		return nil, fmt.Errorf("subagent %q already exists", id)
	}
	m.agents[id] = sa
	doneCh := sa.doneCh
	m.mu.Unlock()

	go m.runTurn(subCtx, cancel, sa, opts.MaxSteps, history, doneCh, defaults)

	return sa, nil
}

// runTurn executes one turn for a sub-agent in a goroutine.
func (m *Manager) runTurn(ctx context.Context, cancel context.CancelFunc, sa *SubAgent, maxSteps int, history []providers.ChatMessage, doneCh chan struct{}, defaults managerDefaults) {
	ownsDone := true
	defer func() {
		if ownsDone {
			close(doneCh)
		}
	}()
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			sa.mu.Lock()
			sa.Status = StatusFailed
			sa.Error = fmt.Errorf("worker panic: %v", r)
			sa.CompletedAt = time.Now()
			sa.mu.Unlock()
			m.prepareTerminal(sa, StatusFailed)
			if sa.historyPath != "" {
				_ = persistHistory(sa)
			}
			m.notify(sa, StatusFailed)
		}
	}()

	// Status was already set to StatusRunning in Spawn (so CountRunning
	// sees it synchronously). Just notify listeners.
	m.notify(sa, StatusRunning)
	ctx, workflow := providers.StartInferenceWorkflow(ctx, providers.InferenceProfileBackgroundAgent)

	// Live token accumulation: every LLM round-trip updates the
	// SubAgent's running totals so the activity panel can display
	// progress while the worker is still going.
	onUsage := func(input, output int) {
		sa.mu.Lock()
		sa.InputTokens += input
		sa.OutputTokens += output
		sa.mu.Unlock()
		m.BroadcastSnapshot(sa)
	}
	onTokenUsage := func(usage providers.TokenUsage) {
		sa.mu.Lock()
		sa.CacheCreationTokens += usage.CacheCreationTokens
		sa.CacheReadTokens += usage.CacheReadTokens
		sa.mu.Unlock()
		if usage.CacheCreationTokens != 0 || usage.CacheReadTokens != 0 {
			m.BroadcastSnapshot(sa)
		}
	}

	// Activity tracker: watch the worker's stream events and update
	// sa.Activity on phase transitions only (thinking → responding,
	// new tool call, tool finished). Event-per-delta would flood the
	// UI, so the callback short-circuits when the derived phrase
	// matches what's already set — the observer only sees changes.
	onEvent := func(ev providers.StreamEvent) {
		m.notifyStream(sa, ev)
		act := deriveWorkerActivity(ev)
		if act == "" {
			return
		}
		sa.mu.Lock()
		if sa.Activity == act {
			sa.mu.Unlock()
			return
		}
		sa.Activity = act
		sa.ActivityAt = time.Now()
		sa.mu.Unlock()
		m.BroadcastSnapshot(sa)
	}

	runner := &agent.StreamRunner{
		Client:                   sa.client,
		Tools:                    sa.toolkit,
		ToolLedger:               sa.toolLedger,
		Model:                    sa.model,
		SystemPrompt:             sa.systemPrompt,
		MaxSteps:                 maxSteps,
		Temperature:              defaults.temperature,
		ContextWindowOverride:    defaults.contextWindow,
		MaxInputTokens:           defaults.maxInputTokens,
		OutputReserveTokens:      defaults.outputReserve,
		CompactThresholdTokens:   defaults.compactTokens,
		CompactThresholdPct:      defaults.compactPct,
		CompactKeepRecentTokens:  defaults.keepRecent,
		DisableAutoCompact:       defaults.disableCompact,
		Effort:                   defaults.effort,
		ProviderOptions:          provideroptions.Clone(defaults.options),
		InferenceOperationKind:   providers.InferenceOperationAgentRound,
		InferenceWorkloadProfile: providers.InferenceProfileBackgroundAgent,
		InferenceJournal:         defaults.journal,
		OnUsage:                  onUsage,
		OnTokenUsage:             onTokenUsage,
	}
	if provider, ok := sa.toolkit.(toolContextBlockProvider); ok {
		runner.BeforeRequestContext = func() []agent.ContextSegment {
			blocks := provider.ContextBlocks()
			if len(blocks) == 0 {
				return nil
			}
			return agent.RequestOnlyContextBlocks(blocks)
		}
	}

	beforeStep := func() []providers.ChatMessage {
		return sa.popPendingUserMessages()
	}

	runner.BeforeStep = beforeStep
	runner.ForceToolFirstStep = sa.takeForceToolNextTurn()
	res, err := runner.RunWithCallback(ctx, history, onEvent)
	content := res.Content
	nextHistory := mergeTurnHistory(history, res)

	if err == nil {
		// Result delivery is unconditional: the parent always receives the
		// worker's final text. Recover it through a layered, gently degrading
		// fallback so an empty final turn never yields an empty handoff.
		//
		//  1. Tail fallback walk (Claude Code pattern): if the final assistant
		//     turn carried no text (e.g. it was pure tool_use), walk back to
		//     the most recent assistant message that did.
		if strings.TrimSpace(content) == "" {
			content = lastAssistantText(nextHistory)
		}
		//  2. One mechanical re-entry nudge: still empty means the worker
		//     stopped without saying anything. Ask once for a final summary,
		//     reusing the same turn machinery in place (no parallel loop).
		if strings.TrimSpace(content) == "" && ctx.Err() == nil {
			if nudged, nudgedHistory, ok := m.runFinalSummaryNudge(ctx, runner, sa, nextHistory, onEvent); ok {
				nextHistory = nudgedHistory
				if strings.TrimSpace(nudged) != "" {
					content = nudged
				}
			}
		}
		//  3. Clearly labelled placeholder: only if the worker genuinely
		//     produced nothing. A placeholder is not a fabricated statement,
		//     so it does not violate "the runtime never answers for the model".
		if strings.TrimSpace(content) == "" {
			content = emptyWorkerResultPlaceholder
		}
	}
	workflowOutcome := providers.InferenceOutcomeSucceeded
	if err != nil {
		workflowOutcome = providers.InferenceOutcomeFailed
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			workflowOutcome = providers.InferenceOutcomeCanceled
		}
	}
	if workflowErr := providers.CompleteInferenceWorkflow(workflow, defaults.journal, workflowOutcome); workflowErr != nil {
		err = errors.Join(err, workflowErr)
	}

	sa.mu.Lock()
	// Close the last-step mailbox gap atomically with the terminal transition.
	// A follow-up that wins this lock first is drained into an immediate
	// continuation; one that arrives after Status becomes terminal starts the
	// normal resumed turn. No successful Followup can remain parked in memory
	// with no future BeforeStep to consume it.
	if ctx.Err() == nil && len(sa.pendingMessages) > 0 {
		continuationHistory := append(providers.CloneChatMessages(nextHistory), sa.popPendingUserMessagesLocked()...)
		lifetime := sa.maxLifetime
		continuationCtx, continuationCancel := context.WithCancel(context.WithoutCancel(ctx))
		if lifetime > 0 {
			continuationCtx, continuationCancel = context.WithTimeout(context.WithoutCancel(ctx), lifetime)
		}
		sa.history = providers.CloneChatMessages(continuationHistory)
		sa.Status = StatusRunning
		sa.CompletedAt = time.Time{}
		sa.Error = nil
		sa.Result = ""
		sa.Activity = ""
		sa.ActivityAt = time.Time{}
		sa.cancelFunc = continuationCancel
		sa.mu.Unlock()
		ownsDone = false
		go m.runTurn(continuationCtx, continuationCancel, sa, maxSteps, continuationHistory, doneCh, defaults)
		return
	}
	sa.history = nextHistory
	sa.CompletedAt = time.Now()
	if err != nil {
		switch {
		case errors.Is(err, context.DeadlineExceeded) && sa.maxLifetime > 0:
			sa.Status = StatusFailed
			sa.Error = fmt.Errorf("worker exceeded max lifetime (%s)", sa.maxLifetime)
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			sa.Status = StatusCancelled
		default:
			sa.Status = StatusFailed
			sa.Error = err
		}
		// Salvage the partial result: alongside the error (and the resume
		// hint on the mailbox) the parent sees how far the worker got —
		// the most recent assistant text in the retained history.
		if partial := lastAssistantText(nextHistory); strings.TrimSpace(partial) != "" {
			sa.Result = partial
		}
	} else {
		sa.Status = StatusCompleted
		sa.Result = content
	}
	finalStatus := sa.Status
	sa.mu.Unlock()
	m.prepareTerminal(sa, finalStatus)

	if sa.historyPath != "" {
		if persistErr := persistHistory(sa); persistErr != nil {
			sa.mu.Lock()
			sa.Status = StatusFailed
			sa.Error = errors.Join(sa.Error, fmt.Errorf("persist final worker snapshot: %w", persistErr))
			finalStatus = sa.Status
			sa.mu.Unlock()
			// The failed persistence changes the terminal generation. Prepare that
			// exact failure before a best-effort second snapshot write or terminal
			// observer can expose it.
			m.prepareTerminal(sa, finalStatus)
			_ = persistHistory(sa)
		} else if sa.toolLedger != nil {
			_ = sa.toolLedger.MarkProjected(context.WithoutCancel(ctx), toolInvocationIDs(nextHistory))
		}
	}

	m.notify(sa, finalStatus)
}

// Terminal-prepare retries are bounded so a persistently failing observer
// store cannot wedge the worker's terminal transition and the execution lease
// it holds; on stop or exhaustion the transition proceeds and later phases
// surface the missing prepared record.
const (
	terminalPrepareRetryDelay    = 10 * time.Millisecond
	terminalPrepareRetryAttempts = 500
)

// ErrTerminalPrepareStopped is returned by a terminal-prepare observer whose
// owner is shutting down and has handed recovery to its durable state; the
// prepare retry loop stops immediately instead of waiting out its bound.
var ErrTerminalPrepareStopped = errors.New("terminal prepare observer stopped")

func (m *Manager) prepareTerminal(sa *SubAgent, status Status) {
	if m == nil || sa == nil || !IsTerminal(status) {
		return
	}
	var lastErr error
	for attempt := 1; attempt <= terminalPrepareRetryAttempts; attempt++ {
		m.mu.Lock()
		prepare := m.terminalPrepare
		m.mu.Unlock()
		if prepare == nil {
			return
		}
		n := Notification{AgentID: sa.ID, Status: status, Snapshot: sa.Snapshot()}
		lastErr = prepare(n)
		if lastErr == nil {
			return
		}
		if errors.Is(lastErr, ErrTerminalPrepareStopped) {
			providers.DebugLogf("subagent: terminal prepare for %s stopped: %v", sa.ID, lastErr)
			return
		}
		if attempt == 1 || attempt%10 == 0 {
			providers.DebugLogf("subagent: prepare terminal intent for %s attempt %d: %v", sa.ID, attempt, lastErr)
		}
		time.Sleep(terminalPrepareRetryDelay)
	}
	providers.DebugLogf("subagent: prepare terminal intent for %s abandoned after %d attempts: %v", sa.ID, terminalPrepareRetryAttempts, lastErr)
}

func toolInvocationIDs(messages []providers.ChatMessage) []string {
	ids := make([]string, 0)
	for _, message := range messages {
		if id := strings.TrimSpace(message.ToolInvocationID); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

// emptyWorkerResultPlaceholder is the clearly-labelled stand-in delivered when
// a worker completes without producing any final text, even after a nudge. It
// records the runtime fact ("no output") rather than inventing an answer.
const emptyWorkerResultPlaceholder = "(Subagent completed but returned no output.)"

// mergeTurnHistory extends the pre-turn history with the messages a turn
// produced, honoring an auto-compaction rewrite.
func mergeTurnHistory(history []providers.ChatMessage, res agent.LoopResult) []providers.ChatMessage {
	if res.HistoryRewritten {
		return providers.CloneChatMessages(res.NewMessages)
	}
	next := providers.CloneChatMessages(history)
	if len(res.NewMessages) > 0 {
		next = append(next, providers.CloneChatMessages(res.NewMessages)...)
	}
	return next
}

// lastAssistantText walks history backward and returns the most recent
// assistant message that carries visible text. This is the Claude Code
// tail-fallback: when the final assistant turn is empty or pure tool_use, the
// deliverable is the last thing the model actually said.
func lastAssistantText(history []providers.ChatMessage) string {
	for i := len(history) - 1; i >= 0; i-- {
		msg := history[i]
		if msg.Role != "assistant" {
			continue
		}
		if strings.TrimSpace(msg.Content) != "" {
			return msg.Content
		}
	}
	return ""
}

// runFinalSummaryNudge runs exactly one additional turn asking a completed
// worker that produced no text for a final summary. It reuses the same runner
// and turn loop in place (no parallel goroutine) and reports the recovered
// text plus the extended history. Best-effort: a nudge that errors leaves the
// worker's completed result and history untouched.
func (m *Manager) runFinalSummaryNudge(ctx context.Context, runner *agent.StreamRunner, sa *SubAgent, history []providers.ChatMessage, onEvent agent.StreamCallback) (string, []providers.ChatMessage, bool) {
	nudge := providers.ChatMessage{
		Role:    "user",
		Content: "You have finished the task but returned no final message. Provide your final summary now: state what you did, what you found or changed, and anything left undone.",
	}
	turnHistory := append(providers.CloneChatMessages(history), nudge)
	res, err := runner.RunWithCallback(ctx, turnHistory, onEvent)
	if err != nil {
		return "", history, false
	}
	nextHistory := mergeTurnHistory(turnHistory, res)
	content := res.Content
	if strings.TrimSpace(content) == "" {
		content = lastAssistantText(nextHistory)
	}
	return content, nextHistory, true
}

// notify pushes a notification to all listeners. Drops on full channels.
func (m *Manager) notify(sa *SubAgent, status Status) error {
	return m.notifySnapshot(sa, status, sa.Snapshot())
}

func (m *Manager) notifySnapshot(sa *SubAgent, status Status, snap SubAgentSnapshot) error {
	n := Notification{AgentID: sa.ID, Status: status, Snapshot: snap}

	m.mu.Lock()
	listeners := append([]chan<- Notification(nil), m.listeners...)
	terminalObserver := m.terminalObserver
	m.mu.Unlock()

	if IsTerminal(status) && terminalObserver != nil {
		if err := terminalObserver(n); err != nil {
			return err
		}
	}

	for _, ch := range listeners {
		select {
		case ch <- n:
		default:
		}
	}
	return nil
}

// BroadcastSnapshot publishes the agent's current usage/state without
// changing its status. Used for live worker token updates.
func (m *Manager) BroadcastSnapshot(sa *SubAgent) {
	if sa == nil {
		return
	}
	m.notify(sa, sa.Snapshot().Status)
}

func (m *Manager) notifyStream(sa *SubAgent, ev providers.StreamEvent) {
	if sa == nil {
		return
	}
	n := StreamNotification{
		AgentID:  sa.ID,
		Snapshot: sa.Snapshot(),
		Event:    cloneStreamEvent(ev),
	}

	m.mu.Lock()
	streams := append([]chan<- StreamNotification(nil), m.streams...)
	m.mu.Unlock()

	for _, ch := range streams {
		ch <- n
	}
}

// Get returns the sub-agent with the given ID, or nil if unknown.
func (m *Manager) Get(id string) *SubAgent {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.agents[id]
}

// List returns snapshots of all currently-tracked sub-agents.
func (m *Manager) List() []SubAgentSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]SubAgentSnapshot, 0, len(m.agents))
	for _, sa := range m.agents {
		out = append(out, sa.Snapshot())
	}
	return out
}

// History returns a copy of the given sub-agent's current conversation.
func (m *Manager) History(id string) ([]providers.ChatMessage, bool) {
	sa := m.Get(id)
	if sa == nil {
		return nil, false
	}
	return sa.HistorySnapshot(), true
}

// Stop cancels the sub-agent with the given ID. Does nothing if it's
// already done. Returns false if no such agent.
func (m *Manager) Stop(id string) bool {
	sa := m.Get(id)
	if sa == nil {
		return false
	}
	sa.cancel()
	return true
}

// StopAll cancels every running sub-agent. Used for Ctrl+C handling.
func (m *Manager) StopAll() {
	m.mu.Lock()
	agents := make([]*SubAgent, 0, len(m.agents))
	for _, sa := range m.agents {
		agents = append(agents, sa)
	}
	m.mu.Unlock()
	for _, sa := range agents {
		sa.cancel()
	}
}

// QueueMessage appends a follow-up user instruction for a running
// sub-agent. The message is injected before the next model round.
// Returns false if the agent is unknown.
func (m *Manager) QueueMessage(id, message string) bool {
	sa := m.Get(id)
	if sa == nil {
		return false
	}
	snap, queued := sa.pushPendingMessageSnapshot(message)
	if queued {
		m.notifySnapshot(sa, snap.Status, snap)
	}
	return true
}

// Followup starts a new turn for an idle sub-agent or queues the
// message for the current turn if it is still running.
func (m *Manager) Followup(ctx context.Context, id, message string) (SubAgentSnapshot, error) {
	return m.followup(ctx, id, message, "")
}

// FollowupForcingTool is Followup with the next turn's first request pinned
// to the named tool via forced tool_choice. Used for mechanical closing
// turns (e.g. requiring agent_report from a requires_report worker). The
// force applies only when the follow-up starts a new turn; if the run is
// still active the message queues normally and the force is dropped.
func (m *Manager) FollowupForcingTool(ctx context.Context, id, message, forceTool string) (SubAgentSnapshot, error) {
	return m.followup(ctx, id, message, forceTool)
}

func (m *Manager) followup(ctx context.Context, id, message, forceTool string) (SubAgentSnapshot, error) {
	sa := m.Get(id)
	if sa == nil {
		return SubAgentSnapshot{}, fmt.Errorf("subagent %q not found", id)
	}
	msg := strings.TrimSpace(message)
	if msg == "" {
		return SubAgentSnapshot{}, errors.New("message is required")
	}

	sa.mu.Lock()
	switch sa.Status {
	case StatusRunning, StatusPending:
		sa.pendingMessages = append(sa.pendingMessages, msg)
		snap := snapshotLocked(sa)
		sa.mu.Unlock()
		m.BroadcastSnapshot(sa)
		return snap, nil
	}
	// Every terminal state (completed, failed, cancelled) is resumable:
	// the run keeps its full history, so a follow-up starts a new turn
	// from where it stopped. A cancelled run means the user stopped it,
	// and a later message is an explicit request to revive it.

	history := providers.CloneChatMessages(sa.history)
	if len(history) == 0 {
		sa.mu.Unlock()
		return SubAgentSnapshot{}, fmt.Errorf("subagent %q has no history to resume", id)
	}
	for _, pending := range sa.pendingMessages {
		history = append(history, providers.ChatMessage{Role: "user", Content: pending})
	}
	sa.pendingMessages = nil
	history = append(history, providers.ChatMessage{Role: "user", Content: msg})

	lifetime := sa.maxLifetime
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	if lifetime > 0 {
		runCtx, cancel = context.WithTimeout(context.WithoutCancel(ctx), lifetime)
	}
	doneCh := make(chan struct{})
	sa.Status = StatusRunning
	sa.CompletedAt = time.Time{}
	sa.Error = nil
	sa.Result = ""
	sa.Activity = ""
	sa.ActivityAt = time.Time{}
	sa.cancelFunc = cancel
	sa.doneCh = doneCh
	sa.forceToolNextTurn = strings.TrimSpace(forceTool)
	snap := snapshotLocked(sa)
	maxSteps := sa.maxSteps
	defaults := sa.runtimeDefaults
	if defaults.client == nil {
		defaults = m.defaultsSnapshot()
	}
	// Preserve the spawn-time client override across followup turns
	// so a per-participant model pin keeps its dedicated provider
	// even if the manager's defaults move on.
	if sa.client != nil {
		defaults.client = sa.client
	} else if defaults.client == nil {
		defaults.client = m.defaultsSnapshot().client
	}
	sa.mu.Unlock()

	go m.runTurn(runCtx, cancel, sa, maxSteps, history, doneCh, defaults)
	return snap, nil
}

// RestoreOptions rebuilds a terminal sub-agent from a persisted snapshot so
// it can accept a follow-up after its live run was lost (typically across a
// process restart). Restore does not run a turn — it re-registers the
// dormant SubAgent so the normal Followup path resumes it. The caller owns
// validation (version gate, working-directory existence) and toolkit
// construction.
type RestoreOptions struct {
	Run     PersistedRun
	Toolkit agent.ToolExecutor
	// Model, when set, overrides Run.Model (e.g. a re-resolved model pin).
	Model string
	// Client, when non-nil, pins the resumed run to a specific stream
	// client (e.g. a rebuilt cross-provider client). Nil lets the next
	// turn fall back to the manager defaults.
	Client      providers.StreamClient
	HistoryPath string
	MaxSteps    int
	MaxLifetime time.Duration
}

// Restore re-registers a dormant sub-agent rebuilt from a snapshot. The
// returned SubAgent is in its persisted terminal state with a closed done
// channel; a subsequent Followup starts a fresh turn from its history.
func (m *Manager) Restore(opts RestoreOptions) (*SubAgent, error) {
	run := opts.Run
	id := strings.TrimSpace(run.ID)
	if id == "" {
		return nil, errors.New("restore requires a run id")
	}
	if opts.Toolkit == nil {
		return nil, errors.New("toolkit is required")
	}
	if len(run.Messages) == 0 {
		return nil, fmt.Errorf("subagent %q has no history to resume", id)
	}
	model := strings.TrimSpace(opts.Model)
	if model == "" {
		model = strings.TrimSpace(run.Model)
	}
	if model == "" {
		return nil, fmt.Errorf("subagent %q snapshot has no model to resume with", id)
	}

	// Inherit the manager's current runtime defaults (context window,
	// compaction, provider options) and fall back to its default client
	// when the caller has no per-run override — the resume turn reads
	// sa.client directly, so it must be non-nil.
	defaults := m.defaultsSnapshot()
	client := opts.Client
	if client == nil {
		client = defaults.client
	}
	var toolLedger *toolledger.Ledger
	if defaults.toolLedgerFactory != nil {
		var err error
		toolLedger, err = defaults.toolLedgerFactory(id)
		if err != nil {
			return nil, fmt.Errorf("open tool ledger for subagent %q: %w", id, err)
		}
	}
	if client == nil {
		return nil, fmt.Errorf("subagent %q cannot resume: no stream client configured", id)
	}

	// Terminal runs restore with an already-closed done channel so Wait
	// returns the snapshot immediately until a follow-up starts a turn.
	doneCh := make(chan struct{})
	close(doneCh)
	// A restored run has no goroutine by construction, so it must never be
	// registered in a non-terminal state: a dead "running" entry would
	// permanently pollute CountRunning (and with it every max-parallel
	// gate). A snapshot that still claims to be live was written by a
	// process that died mid-run — register it as interrupted.
	status := run.Status
	if !IsTerminal(status) {
		status = StatusInterrupted
	}
	sa := &SubAgent{
		ID:              id,
		ParticipantID:   run.ParticipantID,
		Type:            run.Type,
		TaskName:        run.TaskName,
		AgentProfile:    run.AgentProfile,
		AgentPath:       run.AgentPath,
		ParentID:        run.ParentID,
		Description:     run.Description,
		Status:          status,
		StartedAt:       run.StartedAt,
		CompletedAt:     run.CompletedAt,
		Result:          run.Result,
		prompt:          run.Prompt,
		model:           model,
		modelPin:        run.ModelPin,
		workerRoot:      run.CWD,
		ultra:           run.Ultra,
		toolkit:         opts.Toolkit,
		historyPath:     opts.HistoryPath,
		history:         providers.CloneChatMessages(run.Messages),
		maxSteps:        opts.MaxSteps,
		maxLifetime:     opts.MaxLifetime,
		runtimeDefaults: defaults,
		client:          client,
		toolLedger:      toolLedger,
		cancelFunc:      func() {},
		doneCh:          doneCh,
	}
	if strings.TrimSpace(run.Error) != "" {
		sa.Error = errors.New(run.Error)
	}

	m.mu.Lock()
	if _, exists := m.agents[id]; exists {
		m.mu.Unlock()
		return nil, fmt.Errorf("subagent %q already exists", id)
	}
	m.agents[id] = sa
	m.mu.Unlock()
	return sa, nil
}

// NextPendingMessage returns and removes the oldest queued follow-up
// message for an agent. Used by tests and diagnostics.
func (m *Manager) NextPendingMessage(id string) (string, bool) {
	sa := m.Get(id)
	if sa == nil {
		return "", false
	}
	return sa.popPendingMessage()
}

// PendingMessageCount reports how many follow-up messages are queued
// for an agent.
func (m *Manager) PendingMessageCount(id string) int {
	sa := m.Get(id)
	if sa == nil {
		return 0
	}
	return sa.pendingCount()
}

// Wait blocks until the sub-agent finishes or the context is cancelled.
// Returns the final snapshot.
func (m *Manager) Wait(ctx context.Context, id string) (SubAgentSnapshot, error) {
	sa := m.Get(id)
	if sa == nil {
		return SubAgentSnapshot{}, fmt.Errorf("subagent %q not found", id)
	}
	sa.mu.Lock()
	doneCh := sa.doneCh
	sa.mu.Unlock()
	select {
	case <-doneCh:
		return sa.Snapshot(), nil
	case <-ctx.Done():
		return sa.Snapshot(), ctx.Err()
	}
}

// CountRunning returns the number of sub-agents currently in
// StatusRunning. Used for concurrency limit checks.
func (m *Manager) CountRunning() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, sa := range m.agents {
		sa.mu.Lock()
		if sa.Status == StatusRunning {
			n++
		}
		sa.mu.Unlock()
	}
	return n
}

func (s *SubAgent) pushPendingMessage(message string) {
	_, _ = s.pushPendingMessageSnapshot(message)
}

func (s *SubAgent) cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancelFunc != nil {
		s.cancelFunc()
	}
}

func (s *SubAgent) pushPendingMessageSnapshot(message string) (SubAgentSnapshot, bool) {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return SubAgentSnapshot{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pendingMessages = append(s.pendingMessages, trimmed)
	return snapshotLocked(s), true
}

func (s *SubAgent) popPendingMessage() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.pendingMessages) == 0 {
		return "", false
	}
	msg := s.pendingMessages[0]
	s.pendingMessages[0] = ""
	s.pendingMessages = s.pendingMessages[1:]
	return msg, true
}

// takeForceToolNextTurn consumes the one-shot forced tool set by
// FollowupForcingTool for the turn that is about to run.
func (s *SubAgent) takeForceToolNextTurn() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	tool := s.forceToolNextTurn
	s.forceToolNextTurn = ""
	return tool
}

func (s *SubAgent) popPendingUserMessages() []providers.ChatMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.popPendingUserMessagesLocked()
}

func (s *SubAgent) popPendingUserMessagesLocked() []providers.ChatMessage {
	if len(s.pendingMessages) == 0 {
		return nil
	}
	out := make([]providers.ChatMessage, 0, len(s.pendingMessages))
	for _, msg := range s.pendingMessages {
		out = append(out, providers.ChatMessage{Role: "user", Content: msg})
	}
	s.pendingMessages = nil
	return out
}

func (s *SubAgent) pendingCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.pendingMessages)
}

// newAgentID generates a short, sortable identifier for a sub-agent.
// Format: <type>-<8 hex chars>.
func newAgentID(typ string) string {
	if typ == "" {
		typ = "agent"
	}
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s-%s", typ, hex.EncodeToString(b))
}

func initialTurnHistory(opts SpawnOptions) []providers.ChatMessage {
	if len(opts.InitialHistory) > 0 {
		history := make([]providers.ChatMessage, 0, len(opts.InitialHistory)+1)
		history = append(history, opts.InitialHistory...)
		history = append(history, providers.ChatMessage{
			Role:    "user",
			Content: opts.Prompt,
		})
		return history
	}
	history := make([]providers.ChatMessage, 0, 2)
	if strings.TrimSpace(opts.SystemPrompt) != "" {
		history = append(history, providers.ChatMessage{Role: "system", Content: opts.SystemPrompt})
	}
	history = append(history, providers.ChatMessage{Role: "user", Content: opts.Prompt})
	return history
}

func cloneStreamEvent(ev providers.StreamEvent) providers.StreamEvent {
	out := ev
	if ev.Message != nil {
		msg := cloneChatMessage(*ev.Message)
		out.Message = &msg
	}
	if ev.ReasoningBlock != nil {
		block := *ev.ReasoningBlock
		out.ReasoningBlock = &block
	}
	if ev.ToolCall != nil {
		call := cloneToolCall(*ev.ToolCall)
		out.ToolCall = &call
	}
	if ev.PlanUpdate != nil {
		update := *ev.PlanUpdate
		update.Plan = append([]providers.PlanStep(nil), ev.PlanUpdate.Plan...)
		out.PlanUpdate = &update
	}
	if ev.Lifecycle != nil {
		lifecycle := *ev.Lifecycle
		out.Lifecycle = &lifecycle
	}
	if ev.Usage != nil {
		usage := *ev.Usage
		out.Usage = &usage
	}
	return out
}

func cloneChatMessage(msg providers.ChatMessage) providers.ChatMessage {
	msg.Images = append([]providers.InputImage(nil), msg.Images...)
	msg.Files = append([]providers.InputFile(nil), msg.Files...)
	msg.ReasoningBlocks = append([]providers.ReasoningBlock(nil), msg.ReasoningBlocks...)
	msg.DiscoveredTools = providers.CloneLoadableToolDefinitions(msg.DiscoveredTools)
	if len(msg.ToolCalls) > 0 {
		msg.ToolCalls = make([]providers.ToolCall, len(msg.ToolCalls))
		for i, call := range msg.ToolCalls {
			msg.ToolCalls[i] = cloneToolCall(call)
		}
	}
	return msg
}

func cloneToolCall(call providers.ToolCall) providers.ToolCall {
	if call.Display != nil {
		display := *call.Display
		call.Display = &display
	}
	return call
}

func snapshotLocked(s *SubAgent) SubAgentSnapshot {
	return SubAgentSnapshot{
		ID:                  s.ID,
		ParticipantID:       s.ParticipantID,
		Type:                s.Type,
		TaskName:            s.TaskName,
		AgentProfile:        s.AgentProfile,
		AgentPath:           s.AgentPath,
		ParentID:            s.ParentID,
		Description:         s.Description,
		WorkerRoot:          s.workerRoot,
		Model:               s.model,
		ModelPin:            s.modelPin,
		Ultra:               s.ultra,
		Status:              s.Status,
		StartedAt:           s.StartedAt,
		CompletedAt:         s.CompletedAt,
		Result:              s.Result,
		Error:               s.Error,
		InputTokens:         s.InputTokens,
		OutputTokens:        s.OutputTokens,
		CacheCreationTokens: s.CacheCreationTokens,
		CacheReadTokens:     s.CacheReadTokens,
		PendingMessageCount: len(s.pendingMessages),
		Activity:            s.Activity,
		ActivityAt:          s.ActivityAt,
	}
}
