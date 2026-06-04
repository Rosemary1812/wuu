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
	"github.com/blueberrycongee/wuu/internal/providers"
)

// Manager registers and orchestrates sub-agents. It is safe for
// concurrent use from multiple goroutines.
type Manager struct {
	client       providers.StreamClient
	defaultModel string

	mu        sync.Mutex
	agents    map[string]*SubAgent
	listeners []chan<- Notification
	streams   []chan<- StreamNotification
}

// NewManager constructs a Manager backed by the given streaming LLM
// client. defaultModel is used when SpawnOptions.Model is empty.
func NewManager(client providers.StreamClient, defaultModel string) *Manager {
	return &Manager{
		client:       client,
		defaultModel: defaultModel,
		agents:       make(map[string]*SubAgent),
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

	model := opts.Model
	if model == "" {
		model = m.defaultModel
	}
	if model == "" {
		return nil, errors.New("no model configured")
	}

	id := strings.TrimSpace(opts.ID)
	if id == "" {
		id = newAgentID(opts.Type)
	}
	lifetime := opts.MaxLifetime
	if lifetime <= 0 {
		lifetime = DefaultMaxLifetime
	}
	subCtx, cancel := context.WithTimeout(ctx, lifetime)
	history := initialTurnHistory(opts)

	sa := &SubAgent{
		ID:             id,
		Type:           opts.Type,
		TaskName:       opts.TaskName,
		AgentProfile:   opts.AgentProfile,
		AgentPath:      opts.AgentPath,
		ParentID:       opts.ParentID,
		Description:    opts.Description,
		Status:         StatusRunning, // set synchronously so CountRunning sees it immediately
		StartedAt:      time.Now(),
		prompt:         opts.Prompt,
		systemPrompt:   opts.SystemPrompt,
		model:          model,
		toolkit:        opts.Toolkit,
		historyPath:    opts.HistoryPath,
		initialHistory: opts.InitialHistory,
		history:        append([]providers.ChatMessage(nil), history...),
		maxSteps:       opts.MaxSteps,
		maxLifetime:    lifetime,
		client:         m.client,
		cancelFunc:     cancel,
		doneCh:         make(chan struct{}),
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

	go m.runTurn(subCtx, cancel, sa, opts.MaxSteps, history, doneCh)

	return sa, nil
}

// runTurn executes one turn for a sub-agent in a goroutine.
func (m *Manager) runTurn(ctx context.Context, cancel context.CancelFunc, sa *SubAgent, maxSteps int, history []providers.ChatMessage, doneCh chan struct{}) {
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
		Client:       sa.client,
		Tools:        sa.toolkit,
		Model:        sa.model,
		SystemPrompt: sa.systemPrompt,
		MaxSteps:     maxSteps,
		Temperature:  0.2,
		OnUsage:      onUsage,
	}

	beforeStep := func() []providers.ChatMessage {
		return sa.popPendingUserMessages()
	}

	runner.BeforeStep = beforeStep
	res, err := runner.RunWithCallback(ctx, history, onEvent)
	content := res.Content
	nextHistory := append([]providers.ChatMessage(nil), history...)
	if res.HistoryRewritten {
		nextHistory = append([]providers.ChatMessage(nil), res.NewMessages...)
	} else if len(res.NewMessages) > 0 {
		nextHistory = append(nextHistory, res.NewMessages...)
	}

	sa.mu.Lock()
	sa.history = nextHistory
	sa.CompletedAt = time.Now()
	if err != nil {
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			sa.Status = StatusFailed
			sa.Error = fmt.Errorf("worker exceeded max lifetime (%s)", sa.maxLifetime)
		case errors.Is(err, context.Canceled):
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
	snap := sa.Snapshot()
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
	sa.pushPendingMessage(message)
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
	case StatusCancelled:
		snap := snapshotLocked(sa)
		sa.mu.Unlock()
		return snap, fmt.Errorf("subagent %q is cancelled and cannot receive follow-up messages", id)
	}

	history := append([]providers.ChatMessage(nil), sa.history...)
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
	if lifetime <= 0 {
		lifetime = DefaultMaxLifetime
	}
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), lifetime)
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
	sa.mu.Unlock()

	go m.runTurn(runCtx, cancel, sa, maxSteps, history, doneCh)
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
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pendingMessages = append(s.pendingMessages, trimmed)
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
		ID:           s.ID,
		Type:         s.Type,
		TaskName:     s.TaskName,
		AgentProfile: s.AgentProfile,
		AgentPath:    s.AgentPath,
		ParentID:     s.ParentID,
		Description:  s.Description,
		Status:       s.Status,
		StartedAt:    s.StartedAt,
		CompletedAt:  s.CompletedAt,
		Result:       s.Result,
		Error:        s.Error,
		InputTokens:  s.InputTokens,
		OutputTokens: s.OutputTokens,
		Activity:     s.Activity,
		ActivityAt:   s.ActivityAt,
	}
}
