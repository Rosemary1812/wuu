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
	defaultCompactPct      float64
	defaultKeepRecent      int
	defaultDisableCompact  bool

	mu        sync.Mutex
	agents    map[string]*SubAgent
	listeners []chan<- Notification
	streams   []chan<- StreamNotification
}

type ManagerOptions struct {
	DefaultEffort           string
	DefaultProviderOptions  map[string]any
	ContextWindowOverride   int
	MaxInputTokens          int
	OutputReserveTokens     int
	CompactThresholdPct     float64
	CompactKeepRecentTokens int
	DisableAutoCompact      bool
}

type managerDefaults struct {
	client         providers.StreamClient
	model          string
	effort         string
	options        map[string]any
	contextWindow  int
	maxInputTokens int
	outputReserve  int
	compactPct     float64
	keepRecent     int
	disableCompact bool
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
		defaultCompactPct:      opts.CompactThresholdPct,
		defaultKeepRecent:      opts.CompactKeepRecentTokens,
		defaultDisableCompact:  opts.DisableAutoCompact,
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
	m.defaultCompactPct = opts.CompactThresholdPct
	m.defaultKeepRecent = opts.CompactKeepRecentTokens
	m.defaultDisableCompact = opts.DisableAutoCompact
}

func (m *Manager) defaultsSnapshot() managerDefaults {
	m.mu.Lock()
	defer m.mu.Unlock()
	return managerDefaults{
		client:         m.client,
		model:          m.defaultModel,
		effort:         m.defaultEffort,
		options:        provideroptions.Clone(m.defaultProviderOptions),
		contextWindow:  m.defaultContextWindow,
		maxInputTokens: m.defaultMaxInputTokens,
		outputReserve:  m.defaultOutputReserve,
		compactPct:     m.defaultCompactPct,
		keepRecent:     m.defaultKeepRecent,
		disableCompact: m.defaultDisableCompact,
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
		toolkit:         opts.Toolkit,
		historyPath:     opts.HistoryPath,
		initialHistory:  opts.InitialHistory,
		history:         providers.CloneChatMessages(history),
		maxSteps:        opts.MaxSteps,
		maxLifetime:     lifetime,
		runtimeDefaults: defaults,
		client:          client,
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
	defer close(doneCh)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			sa.mu.Lock()
			sa.Status = StatusFailed
			sa.Error = fmt.Errorf("worker panic: %v", r)
			sa.CompletedAt = time.Now()
			sa.mu.Unlock()
			m.notify(sa, StatusFailed)
		}
	}()

	// Status was already set to StatusRunning in Spawn (so CountRunning
	// sees it synchronously). Just notify listeners.
	m.notify(sa, StatusRunning)

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
		Client:                  sa.client,
		Tools:                   sa.toolkit,
		Model:                   sa.model,
		SystemPrompt:            sa.systemPrompt,
		MaxSteps:                maxSteps,
		Temperature:             0.2,
		ContextWindowOverride:   defaults.contextWindow,
		MaxInputTokens:          defaults.maxInputTokens,
		OutputReserveTokens:     defaults.outputReserve,
		CompactThresholdPct:     defaults.compactPct,
		CompactKeepRecentTokens: defaults.keepRecent,
		DisableAutoCompact:      defaults.disableCompact,
		Effort:                  defaults.effort,
		ProviderOptions:         provideroptions.Clone(defaults.options),
		ReconnectConfig:         providers.RetryConfig{MaxRetries: 5},
		OnUsage:                 onUsage,
		OnTokenUsage:            onTokenUsage,
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
	res, err := runner.RunWithCallback(ctx, history, onEvent)
	content := res.Content
	nextHistory := providers.CloneChatMessages(history)
	if res.HistoryRewritten {
		nextHistory = providers.CloneChatMessages(res.NewMessages)
	} else if len(res.NewMessages) > 0 {
		nextHistory = append(nextHistory, providers.CloneChatMessages(res.NewMessages)...)
	}

	sa.mu.Lock()
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
	} else {
		sa.Status = StatusCompleted
		sa.Result = content
	}
	finalStatus := sa.Status
	sa.mu.Unlock()

	if sa.historyPath != "" {
		_ = persistHistory(sa)
	}

	m.notify(sa, finalStatus)
}

// notify pushes a notification to all listeners. Drops on full channels.
func (m *Manager) notify(sa *SubAgent, status Status) {
	m.notifySnapshot(sa, status, sa.Snapshot())
}

func (m *Manager) notifySnapshot(sa *SubAgent, status Status, snap SubAgentSnapshot) {
	n := Notification{AgentID: sa.ID, Status: status, Snapshot: snap}

	m.mu.Lock()
	listeners := append([]chan<- Notification(nil), m.listeners...)
	m.mu.Unlock()

	for _, ch := range listeners {
		select {
		case ch <- n:
		default:
		}
	}
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
	sa.cancelFunc()
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
		sa.cancelFunc()
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

func (s *SubAgent) popPendingUserMessages() []providers.ChatMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
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
