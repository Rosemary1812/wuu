package agent

import (
	"strings"
	"sync"

	"github.com/blueberrycongee/wuu/internal/providers"
)

// UsageTracker estimates how many tokens of context the current
// conversation occupies. It is provider-agnostic by design: instead of
// shipping a tokenizer per model, it treats the most recent successful
// API response's `usage.input_tokens + output_tokens` as ground truth
// for everything sent up to and including that round, then adds a
// cheap byte-based estimate for messages added afterwards (the user's
// next prompt + tool results that haven't been sent yet).
//
// This is the same approach Claude Code and Codex CLI use. Errors are
// bounded: the estimate only ever covers messages from the LAST round
// onward, so the worst-case undercount is one turn's delta. The next
// successful API call collapses the delta to zero by overwriting the
// ground truth.
//
// UsageTracker is safe for concurrent reads/writes.
type UsageTracker struct {
	mu sync.Mutex
	// lastResponseTotal is the most recent (input+output) token count
	// reported by the provider. Zero means no successful round yet.
	lastResponseTotal int
	// pendingDelta is an estimate of tokens added to `messages` since
	// the last response was recorded. Reset every time RecordResponse
	// is called.
	pendingDelta int
}

// NewUsageTracker constructs a fresh tracker with no recorded usage.
func NewUsageTracker() *UsageTracker {
	return &UsageTracker{}
}

// RecordResponse stores the per-call usage from a successful API
// response. Callers should pass the same numbers they hand to
// LoopConfig.OnUsage. nil is a no-op.
//
// The "total" we store here uses TokenUsage.TotalContextTokens(),
// which includes CacheReadTokens. Cached tokens still occupy the
// model's context window, so dropping them would make a Claude
// session with prompt caching look almost empty and the auto-compact
// trigger would never fire.
func (t *UsageTracker) RecordResponse(usage *providers.TokenUsage) {
	if t == nil || usage == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lastResponseTotal = usage.TotalContextTokens()
	// The new ground truth already includes everything we'd been
	// estimating, so the pending delta resets to zero.
	t.pendingDelta = 0
}

// RecordPendingMessages adds an estimate for messages that have been
// queued onto the conversation since the last response. This is what
// the loop calls after appending the assistant's tool calls + tool
// result messages within a single round, before the next request.
func (t *UsageTracker) RecordPendingMessages(msgs []providers.ChatMessage) {
	if t == nil || len(msgs) == 0 {
		return
	}
	add := estimateMessages(msgs)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pendingDelta += add
}

// EstimateCurrent returns the best-effort current token usage of the
// conversation: ground truth from the last response plus the pending
// delta accumulated since.
func (t *UsageTracker) EstimateCurrent() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lastResponseTotal + t.pendingDelta
}

// LastResponseTotal returns the most recently recorded ground truth.
// Useful for tests and diagnostics.
func (t *UsageTracker) LastResponseTotal() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lastResponseTotal
}

// HasGroundTruth reports whether the tracker has seen at least one
// successful provider response and therefore holds a real usage
// baseline instead of pure local estimates.
func (t *UsageTracker) HasGroundTruth() bool {
	return t.LastResponseTotal() > 0
}

// PendingDelta returns the current estimate for messages added since
// the last response. Useful for tests and diagnostics.
func (t *UsageTracker) PendingDelta() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.pendingDelta
}

// Reset clears all recorded state. Used after a compact pass replaces
// the conversation history with a summary.
func (t *UsageTracker) Reset() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lastResponseTotal = 0
	t.pendingDelta = 0
}

// Clone returns a snapshot copy of the current tracker state. Useful
// when a caller wants to speculate on one run and only commit the
// updated usage state if that run's history is actually adopted.
func (t *UsageTracker) Clone() *UsageTracker {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return &UsageTracker{
		lastResponseTotal: t.lastResponseTotal,
		pendingDelta:      t.pendingDelta,
	}
}

// estimateMessages computes a rough token count for a slice of chat
// messages. Used only for the delta between API calls — never as
// ground truth. The heuristic is intentionally cheap and slightly
// pessimistic so it tends to over-count rather than miss a compact
// trigger.
func estimateMessages(msgs []providers.ChatMessage) int {
	total := 0
	for _, m := range msgs {
		// 4 bytes per token is the standard rough rule for English.
		// CJK packs ~2 bytes per token, but byte-based counting
		// already accounts for that since CJK runes are 3 bytes in
		// UTF-8 and pack to 1.5-2 tokens.
		total += len(m.Content) / 4
		total += len(m.ReasoningContent) / 4
		// Per-message overhead (role marker, separators, JSON wrapping
		// in providers' wire format).
		total += 4
		for _, tc := range m.ToolCalls {
			total += len(tc.Name) / 4
			// Tool call arguments are JSON — denser, ~2 bytes/token.
			total += len(tc.Arguments) / 2
			// Tool call envelope cost.
			total += 8
		}
		for _, image := range m.Images {
			total += estimateImageTokens(image)
		}
	}
	return total
}

func estimateImageTokens(image providers.InputImage) int {
	dataLen := len(strings.TrimSpace(image.Data))
	if dataLen == 0 {
		return 0
	}
	payloadEstimate := dataLen / 4
	if payloadEstimate > 2000 {
		return payloadEstimate
	}
	return 2000
}
