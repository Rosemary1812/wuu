package guardian

import (
	"strings"
	"sync"
)

// RejectionCircuitBreaker thresholds, aligned with Codex's
// GuardianRejectionCircuitBreaker (see
// thirdparty/codex/codex-rs/core/src/guardian/mod.rs).
//
// The host turn is interrupted once either:
//
//   - MaxConsecutiveDenials consecutive denials are observed, or
//   - MaxRecentDenials denials occur within the last WindowSize reviews.
//
// The intent is to stop a runaway agent from looping in an obvious rejection
// loop and to give the user a chance to take over before too many LLM tokens
// are burned on the same broken path.
const (
	MaxConsecutiveDenials = 3
	MaxRecentDenials      = 10
	WindowSize            = 50
)

// BreakerAction is the signal returned by RejectionCircuitBreaker after a
// denial is recorded. BreakerContinue is the zero value (no action needed);
// BreakerInterruptTurn tells the caller that the current turn should be
// interrupted so the user can take over.
type BreakerAction int

const (
	BreakerContinue BreakerAction = iota
	BreakerInterruptTurn
)

// RejectionCircuitBreaker tracks recent auto-review decisions per turn and
// signals when the reviewer has rejected enough requests that the host turn
// should be interrupted.
//
// It mirrors Codex's GuardianRejectionCircuitBreaker semantics: per-turn
// isolation, a once-tripped flag that suppresses further interrupts for the
// same turn, and an approval that resets the consecutive counter.
type RejectionCircuitBreaker struct {
	mu    sync.Mutex
	turns map[string]*turnBreakerState
}

type turnBreakerState struct {
	consecutiveDenials int
	recentDenials      []bool // ring of recent reviews; true == denied
	tripped            bool
}

// NewRejectionCircuitBreaker returns an empty breaker. The zero value is not
// usable because the internal map is lazily created; callers should always
// construct via this constructor.
func NewRejectionCircuitBreaker() *RejectionCircuitBreaker {
	return &RejectionCircuitBreaker{turns: map[string]*turnBreakerState{}}
}

// RecordDenial registers a denial for the given turn and reports whether the
// turn should now be interrupted. A nil receiver or empty turnID is treated
// as a no-op (returns BreakerContinue).
func (b *RejectionCircuitBreaker) RecordDenial(turnID string) BreakerAction {
	if b == nil {
		return BreakerContinue
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return BreakerContinue
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.stateLocked(turnID)
	s.consecutiveDenials++
	s.recordReviewLocked(true)
	if s.tripped {
		return BreakerContinue
	}
	if s.consecutiveDenials >= MaxConsecutiveDenials || s.recentDenialCountLocked() >= MaxRecentDenials {
		s.tripped = true
		return BreakerInterruptTurn
	}
	return BreakerContinue
}

// RecordApproval registers a non-denial (approval or non-actionable error) for
// the given turn. It resets the consecutive counter and slides the recent
// window with a non-denial entry, but does not change the tripped flag: a
// turn that has already tripped must be explicitly cleared by ClearTurn or by
// starting a new turn.
func (b *RejectionCircuitBreaker) RecordApproval(turnID string) {
	if b == nil {
		return
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.stateLocked(turnID)
	s.consecutiveDenials = 0
	s.recordReviewLocked(false)
}

// ClearTurn removes the state for the given turn. The host turn layer should
// call this when a turn ends so the breaker does not leak memory across
// long-running sessions.
func (b *RejectionCircuitBreaker) ClearTurn(turnID string) {
	if b == nil {
		return
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.turns, turnID)
}

func (b *RejectionCircuitBreaker) stateLocked(turnID string) *turnBreakerState {
	s, ok := b.turns[turnID]
	if !ok {
		s = &turnBreakerState{}
		b.turns[turnID] = s
	}
	return s
}

func (s *turnBreakerState) recordReviewLocked(denied bool) {
	s.recentDenials = append(s.recentDenials, denied)
	if len(s.recentDenials) > WindowSize {
		// Trim from the front to keep the ring at most WindowSize entries.
		// A slice copy is used instead of a true ring buffer because
		// WindowSize is small (50) and the per-call allocation cost is
		// negligible compared to a model round-trip.
		s.recentDenials = s.recentDenials[len(s.recentDenials)-WindowSize:]
	}
}

func (s *turnBreakerState) recentDenialCountLocked() int {
	n := 0
	for _, denied := range s.recentDenials {
		if denied {
			n++
		}
	}
	return n
}
