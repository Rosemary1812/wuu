package guardian

import (
	"sync"
	"testing"
)

func TestRejectionCircuitBreaker_FirstDenialContinues(t *testing.T) {
	b := NewRejectionCircuitBreaker()
	if got := b.RecordDenial("turn-1"); got != BreakerContinue {
		t.Fatalf("first denial should continue, got %v", got)
	}
}

func TestRejectionCircuitBreaker_TripsOnThreeConsecutiveDenials(t *testing.T) {
	b := NewRejectionCircuitBreaker()
	for i := 0; i < MaxConsecutiveDenials-1; i++ {
		if got := b.RecordDenial("turn-1"); got != BreakerContinue {
			t.Fatalf("denial %d/%d should continue, got %v", i+1, MaxConsecutiveDenials, got)
		}
	}
	if got := b.RecordDenial("turn-1"); got != BreakerInterruptTurn {
		t.Fatalf("denial %d should interrupt, got %v", MaxConsecutiveDenials, got)
	}
}

func TestRejectionCircuitBreaker_ApprovalResetsConsecutive(t *testing.T) {
	b := NewRejectionCircuitBreaker()
	b.RecordDenial("turn-1")
	b.RecordDenial("turn-1")
	b.RecordApproval("turn-1") // resets consecutive
	if got := b.RecordDenial("turn-1"); got != BreakerContinue {
		t.Fatalf("after approval reset, denial should continue, got %v", got)
	}
}

func TestRejectionCircuitBreaker_TripsOnRecentWindow(t *testing.T) {
	b := NewRejectionCircuitBreaker()
	// Interleave denials and approvals so consecutive stays at 1 (it gets
	// reset on every approval). The recent window fills up to
	// MaxRecentDenials denied entries across MaxRecentDenials reviews.
	for i := 0; i < MaxRecentDenials-1; i++ {
		if got := b.RecordDenial("turn-1"); got != BreakerContinue {
			t.Fatalf("denial %d/%d should not trip, got %v", i+1, MaxRecentDenials, got)
		}
		b.RecordApproval("turn-1")
	}
	// The MaxRecentDenials-th denial pushes the recent-window count to the
	// threshold even though consecutive is still just 1.
	if got := b.RecordDenial("turn-1"); got != BreakerInterruptTurn {
		t.Fatalf("recent-window trip should interrupt, got %v", got)
	}
}

func TestRejectionCircuitBreaker_TrippedStaysTripped(t *testing.T) {
	b := NewRejectionCircuitBreaker()
	b.RecordDenial("turn-1")
	b.RecordDenial("turn-1")
	b.RecordDenial("turn-1") // trips via consecutive
	for i := 0; i < 5; i++ {
		if got := b.RecordDenial("turn-1"); got != BreakerContinue {
			t.Fatalf("post-trip denial %d should not re-interrupt, got %v", i+1, got)
		}
	}
}

func TestRejectionCircuitBreaker_ApprovalDoesNotClearTripped(t *testing.T) {
	b := NewRejectionCircuitBreaker()
	b.RecordDenial("turn-1")
	b.RecordDenial("turn-1")
	b.RecordDenial("turn-1") // trips
	b.RecordApproval("turn-1")
	// Approval slides the recent window but does not reset tripped; a new
	// denial must still return Continue (no re-interrupt) and the turn is
	// effectively blocked until ClearTurn.
	if got := b.RecordDenial("turn-1"); got != BreakerContinue {
		t.Fatalf("post-trip approval + denial should continue, got %v", got)
	}
}

func TestRejectionCircuitBreaker_ClearTurnResets(t *testing.T) {
	b := NewRejectionCircuitBreaker()
	b.RecordDenial("turn-1")
	b.RecordDenial("turn-1")
	b.RecordDenial("turn-1") // trips
	b.ClearTurn("turn-1")
	// After ClearTurn the turn can trip again from scratch.
	if got := b.RecordDenial("turn-1"); got != BreakerContinue {
		t.Fatalf("after ClearTurn, fresh denial should continue, got %v", got)
	}
}

func TestRejectionCircuitBreaker_PerTurnIsolation(t *testing.T) {
	b := NewRejectionCircuitBreaker()
	for i := 0; i < MaxConsecutiveDenials; i++ {
		b.RecordDenial("turn-A")
	}
	if got := b.RecordDenial("turn-B"); got != BreakerContinue {
		t.Fatalf("turn-B should be independent of tripped turn-A, got %v", got)
	}
}

func TestRejectionCircuitBreaker_NilReceiverSafe(t *testing.T) {
	//nolint:staticcheck // explicit nil-receiver test
	var b *RejectionCircuitBreaker
	if got := b.RecordDenial("turn-1"); got != BreakerContinue {
		t.Fatalf("nil receiver should continue, got %v", got)
	}
	b.RecordApproval("turn-1")
	b.ClearTurn("turn-1")
}

func TestRejectionCircuitBreaker_EmptyTurnIDSafe(t *testing.T) {
	b := NewRejectionCircuitBreaker()
	if got := b.RecordDenial(""); got != BreakerContinue {
		t.Fatalf("empty turnID should continue, got %v", got)
	}
	b.RecordApproval("")
	b.ClearTurn("")
	if got := b.RecordDenial("   "); got != BreakerContinue {
		t.Fatalf("whitespace turnID should continue, got %v", got)
	}
}

func TestRejectionCircuitBreaker_ConcurrentSafe(t *testing.T) {
	b := NewRejectionCircuitBreaker()
	var wg sync.WaitGroup
	const goroutines = 16
	const iterations = 200
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				b.RecordDenial("turn-x")
				b.RecordApproval("turn-x")
			}
		}()
	}
	wg.Wait()
}
