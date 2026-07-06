package insight

import (
	"math"
	"testing"
)

// TestModelUsage_NormalizedMiniMaxInputPreservesHitRate locks the A3
// invariant that the insight readouts (occupancy + cache hit rate) resolve
// correctly once the provider adapter has normalized an inclusive-input
// endpoint.
//
// MiniMax runs through the anthropic client but reports input_tokens
// *inclusive* of cache_read, while cache_read itself is reported
// cumulatively. The adapter's normalizeInclusiveInput subtracts cache_read
// from input (floored at 0) and preserves cache_read, so the token_usage
// meta rows that feed ModelUsage already carry the clean input. This test
// pins the resulting behaviour: TotalContextTokens equals raw_input+output
// (the cache_read cancels, no double count) and CacheHitRate reflects the
// true cached share without diluting its denominator. cache_read stays
// non-zero, so the hit-rate display is never broken.
func TestModelUsage_NormalizedMiniMaxInputPreservesHitRate(t *testing.T) {
	const (
		freshInput = 19_600  // real prompt tokens this round
		cacheRead  = 113_000 // reported cumulatively by MiniMax
		output     = 2_000
	)
	rawInput := freshInput + cacheRead // MiniMax input_tokens is inclusive

	// Pre-normalization (buggy) view: input still carries cache_read.
	raw := ModelUsage{InputTokens: rawInput, CacheReadTokens: cacheRead, OutputTokens: output}
	// Post-normalization (A1) view: input reduced by cache_read, cache_read kept.
	norm := ModelUsage{InputTokens: freshInput, CacheReadTokens: cacheRead, OutputTokens: output}

	// Occupancy: normalized total is raw_input+output. The raw view
	// double-counts cache_read and over-states occupancy.
	if got, want := norm.TotalContextTokens(), rawInput+output; got != want {
		t.Fatalf("normalized TotalContextTokens = %d, want %d (raw_input+output)", got, want)
	}
	if raw.TotalContextTokens() <= norm.TotalContextTokens() {
		t.Fatalf("expected raw occupancy %d to over-state normalized %d",
			raw.TotalContextTokens(), norm.TotalContextTokens())
	}

	// prompt_tokens (the ContextCompositionCard / trace formula
	// InputTokens+CacheReadTokens) collapses to the true prompt size,
	// not a ~4x-inflated figure.
	if got, want := norm.InputTokens+norm.CacheReadTokens, rawInput; got != want {
		t.Fatalf("normalized promptTokens = %d, want %d (fresh+cache_read)", got, want)
	}

	// Hit rate: cache_read preserved -> non-nil. The normalized denominator
	// is input+cache_read = raw_input, so the rate reflects the true share.
	rate := norm.CacheHitRate()
	if rate == nil {
		t.Fatalf("preserved cache_read must yield a non-nil hit rate")
	}
	want := float64(cacheRead) / float64(freshInput+cacheRead)
	if math.Abs(*rate-want) > 1e-9 {
		t.Fatalf("normalized CacheHitRate = %v, want %v", *rate, want)
	}

	// The buggy raw view dilutes the denominator (rawInput already contains
	// cache_read), understating the hit rate. Normalization corrects it up.
	rawRate := raw.CacheHitRate()
	if rawRate == nil || *rawRate >= *rate {
		t.Fatalf("expected raw hit rate to be diluted below normalized %v, got %v", *rate, rawRate)
	}
}

// TestModelUsage_CacheHitRateNilWithoutPrompt confirms the hit-rate guard:
// with no promptable input the rate is nil (hidden) rather than a divide by
// zero, so an empty bucket never renders a bogus 0% cache figure.
func TestModelUsage_CacheHitRateNilWithoutPrompt(t *testing.T) {
	if rate := (ModelUsage{OutputTokens: 100}).CacheHitRate(); rate != nil {
		t.Fatalf("expected nil hit rate with no prompt tokens, got %v", *rate)
	}
}
