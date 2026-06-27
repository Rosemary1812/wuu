// Package eventbus provides a typed publish-subscribe event bus for decoupling
// the agent core from UI frontends (GUI, headless, web, etc.).
//
// Design aligned with Kimi CLI's Wire protocol and Codex's JSON-RPC app-server:
// the core agent loop emits events; any number of subscribers consume them.
// This lets the same core drive GUI sessions, headless CI runs, or remote
// clients without code changes to the loop.
package eventbus

import (
	"context"
	"sync"

	"github.com/blueberrycongee/wuu/internal/providers"
)

// EventType classifies events flowing through the bus.
type EventType string

const (
	// Control flow
	TurnBegin   EventType = "turn_begin"
	TurnEnd     EventType = "turn_end"
	StepBegin   EventType = "step_begin"
	StepEnd     EventType = "step_end"
	StreamStart EventType = "stream_start"
	StreamEnd   EventType = "stream_end"

	// Content
	TextDelta       EventType = "text_delta"
	TextReplace     EventType = "text_replace"
	ThinkingDelta   EventType = "thinking_delta"
	ThinkingReplace EventType = "thinking_replace"
	ThinkingDone    EventType = "thinking_done"
	ToolCallStart   EventType = "tool_call_start"
	ToolCallDelta   EventType = "tool_call_delta"
	ToolCallEnd     EventType = "tool_call_end"
	Message         EventType = "message"

	// Lifecycle / system
	Lifecycle      EventType = "lifecycle"
	Compact        EventType = "compact"
	RequestContext EventType = "request_context"
	ProviderState  EventType = "provider_state"
	Error          EventType = "error"
	Done           EventType = "done"

	// Worker / process / MCP status
	WorkerStatusChange  EventType = "worker_status_change"
	ProcessStatusChange EventType = "process_status_change"
	MCPStatusChange     EventType = "mcp_status_change"
)

// Event is a single typed message on the bus.
type Event struct {
	Type EventType

	// Content carries text deltas, compact notices, error strings, etc.
	Content string
	Phase   providers.MessagePhase

	// Message carries a full chat message (for TurnBegin/TurnEnd/Message).
	Message *providers.ChatMessage

	// ToolCall carries tool invocation state.
	ToolCall   *providers.ToolCall
	ToolResult string

	// Lifecycle carries stream connection metadata.
	Lifecycle *providers.StreamLifecycle
	// RequestContext carries metadata-only context compilation details.
	RequestContext *providers.RequestContextSummary
	// ProviderState carries metadata-only provider replay/request-state details.
	ProviderState *providers.ProviderStateSummary

	// Usage carries token consumption for a completed turn.
	Usage        *providers.TokenUsage
	StopReason   string
	FinishReason providers.FinishReason
	Truncated    bool

	// Error carries fatal or non-fatal errors.
	Error error

	// Worker / process / MCP identifiers.
	WorkerID  string
	ProcessID string
	MCPName   string
	MCPStatus string
}

// Handler receives events from the bus.
type Handler func(ev Event)

// Bus is a simple in-memory pub-sub event bus.
// It is safe for concurrent use.
type Bus struct {
	mu       sync.RWMutex
	handlers []Handler
	closed   bool
}

// New creates a new event bus.
func New() *Bus {
	return &Bus{}
}

// Subscribe registers a handler. The handler MUST NOT block; if slow work is
// needed, spawn a goroutine or use a channel buffer.
func (b *Bus) Subscribe(h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.handlers = append(b.handlers, h)
}

// Publish emits an event to all subscribers.
func (b *Bus) Publish(ev Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return
	}
	for _, h := range b.handlers {
		if h != nil {
			h(ev)
		}
	}
}

// Close prevents new subscriptions and future publishes are dropped.
func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	b.handlers = nil
}

// AdaptStreamEvent converts a providers.StreamEvent (legacy wire format used
// by StreamRunner) into the canonical eventbus.Event.
func AdaptStreamEvent(se providers.StreamEvent) Event {
	switch se.Type {
	case providers.EventContentDelta:
		return Event{Type: TextDelta, Content: se.Content, Phase: se.Phase}
	case providers.EventContentReplace:
		return Event{Type: TextReplace, Content: se.Content, Phase: se.Phase}
	case providers.EventThinkingDelta:
		return Event{Type: ThinkingDelta, Content: se.Content}
	case providers.EventThinkingReplace:
		return Event{Type: ThinkingReplace, Content: se.Content}
	case providers.EventThinkingDone:
		return Event{Type: ThinkingDone, Content: se.Content}
	case providers.EventToolUseStart:
		return Event{Type: ToolCallStart, ToolCall: se.ToolCall}
	case providers.EventToolUseDelta:
		return Event{Type: ToolCallDelta, ToolCall: se.ToolCall}
	case providers.EventToolUseEnd:
		return Event{Type: ToolCallEnd, ToolCall: se.ToolCall, ToolResult: se.ToolResult}
	case providers.EventMessage:
		return Event{Type: Message, Message: se.Message}
	case providers.EventLifecycle:
		return Event{Type: Lifecycle, Lifecycle: se.Lifecycle}
	case providers.EventCompact:
		return Event{Type: Compact, Content: se.Content}
	case providers.EventRequestContext:
		return Event{Type: RequestContext, RequestContext: se.RequestContext}
	case providers.EventProviderState:
		return Event{Type: ProviderState, ProviderState: se.ProviderState}
	case providers.EventDone:
		return Event{Type: Done, Usage: se.Usage, StopReason: se.StopReason, FinishReason: se.FinishReason, Truncated: se.Truncated}
	case providers.EventError:
		return Event{Type: Error, Error: se.Error}
	case providers.EventReconnect:
		return Event{Type: Lifecycle, Lifecycle: se.Lifecycle}
	default:
		return Event{Type: Message, Content: se.Content}
	}
}

// ToStreamEvent converts an eventbus.Event back into a providers.StreamEvent.
// Useful for compatibility with stream-based clients during migration.
func ToStreamEvent(ev Event) providers.StreamEvent {
	switch ev.Type {
	case TextDelta:
		return providers.StreamEvent{Type: providers.EventContentDelta, Content: ev.Content, Phase: ev.Phase}
	case TextReplace:
		return providers.StreamEvent{Type: providers.EventContentReplace, Content: ev.Content, Phase: ev.Phase}
	case ThinkingDelta:
		return providers.StreamEvent{Type: providers.EventThinkingDelta, Content: ev.Content}
	case ThinkingReplace:
		return providers.StreamEvent{Type: providers.EventThinkingReplace, Content: ev.Content}
	case ThinkingDone:
		return providers.StreamEvent{Type: providers.EventThinkingDone, Content: ev.Content}
	case ToolCallStart:
		return providers.StreamEvent{Type: providers.EventToolUseStart, ToolCall: ev.ToolCall}
	case ToolCallDelta:
		return providers.StreamEvent{Type: providers.EventToolUseDelta, ToolCall: ev.ToolCall}
	case ToolCallEnd:
		return providers.StreamEvent{Type: providers.EventToolUseEnd, ToolCall: ev.ToolCall, ToolResult: ev.ToolResult}
	case Message:
		return providers.StreamEvent{Type: providers.EventMessage, Message: ev.Message, Content: ev.Content}
	case Lifecycle:
		return providers.StreamEvent{Type: providers.EventLifecycle, Lifecycle: ev.Lifecycle}
	case Compact:
		return providers.StreamEvent{Type: providers.EventCompact, Content: ev.Content}
	case RequestContext:
		return providers.StreamEvent{Type: providers.EventRequestContext, RequestContext: ev.RequestContext}
	case ProviderState:
		return providers.StreamEvent{Type: providers.EventProviderState, ProviderState: ev.ProviderState}
	case Done:
		return providers.StreamEvent{Type: providers.EventDone, Usage: ev.Usage, StopReason: ev.StopReason, FinishReason: ev.FinishReason, Truncated: ev.Truncated}
	case Error:
		return providers.StreamEvent{Type: providers.EventError, Error: ev.Error}
	default:
		return providers.StreamEvent{Type: providers.EventMessage, Content: ev.Content}
	}
}

// ChanPublisher returns a Handler that writes events into a buffered channel.
// If the channel is full, events are dropped (non-blocking).
func ChanPublisher(ch chan<- Event) Handler {
	return func(ev Event) {
		select {
		case ch <- ev:
		default:
		}
	}
}

// StreamAdapter wraps an event bus so that StreamRunner can still use its
// legacy StreamCallback during a gradual migration.
//
// Usage:
//
//	bus := eventbus.New()
//	runner := agent.StreamRunner{
//	    Client:  client,
//	    Tools:   tools,
//	    OnEvent: eventbus.NewStreamAdapter(bus),
//	}
func NewStreamAdapter(bus *Bus) func(providers.StreamEvent) {
	return func(se providers.StreamEvent) {
		bus.Publish(AdaptStreamEvent(se))
	}
}

// ChanAdapter bridges a channel of eventbus.Event into a channel of
// providers.StreamEvent for legacy consumers.
func ChanAdapter(ctx context.Context, in <-chan Event) <-chan providers.StreamEvent {
	out := make(chan providers.StreamEvent, 64)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-in:
				if !ok {
					return
				}
				select {
				case out <- ToStreamEvent(ev):
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out
}
