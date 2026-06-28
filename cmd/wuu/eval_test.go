package main

import (
	"testing"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/providers"
)

func TestEvalContextRequestObservationsIncludePerRequestUsage(t *testing.T) {
	records := []agent.RequestContextInfo{{
		StepIndex:        1,
		MessageCount:     3,
		SystemHash:       "system-hash",
		StablePrefixHash: "prefix-hash",
	}}
	attachUsageToLatestEvalRequestContext(records, providers.TokenUsage{
		InputTokens:         11,
		OutputTokens:        3,
		CacheCreationTokens: 5,
		CacheReadTokens:     7,
	})

	observations := evalContextRequestObservations(records)
	if len(observations) != 1 {
		t.Fatalf("expected one observation, got %+v", observations)
	}
	got := observations[0]
	if got.InputTokens != 11 ||
		got.OutputTokens != 3 ||
		got.CacheCreationTokens != 5 ||
		got.CacheReadTokens != 7 {
		t.Fatalf("observation missed per-request usage: %+v", got)
	}
}
