package eventbus

import (
	"reflect"
	"testing"

	"github.com/blueberrycongee/wuu/internal/providers"
)

func TestAdaptStreamEventPreservesRequestContext(t *testing.T) {
	summary := &providers.RequestContextSummary{
		StepIndex:         2,
		TransientMessages: 1,
		ContentBytes:      2048,
		BlockKinds:        []string{"ENVIRONMENT", "REPO_MAP"},
		MessageCount:      5,
		SystemMessages:    1,
		HiddenMessages:    2,
		ToolCount:         3,
		StablePrefix:      2,
		DynamicBytes:      2048,
		SystemHash:        "sys-hash",
		StablePrefixHash:  "prefix-hash",
		ToolSurfaceHash:   "tool-hash",
		PromptCacheKey:    "thread-cache-key",
	}

	event := AdaptStreamEvent(providers.StreamEvent{
		Type:           providers.EventRequestContext,
		RequestContext: summary,
	})
	if event.Type != RequestContext || event.RequestContext == nil {
		t.Fatalf("request context not adapted: %+v", event)
	}
	if !reflect.DeepEqual(event.RequestContext, summary) {
		t.Fatalf("request context changed during adaptation: got %+v want %+v", event.RequestContext, summary)
	}

	stream := ToStreamEvent(event)
	if stream.Type != providers.EventRequestContext || stream.RequestContext == nil {
		t.Fatalf("request context not converted back: %+v", stream)
	}
	if !reflect.DeepEqual(stream.RequestContext, summary) {
		t.Fatalf("request context changed during stream conversion: got %+v want %+v", stream.RequestContext, summary)
	}
}

func TestAdaptStreamEventPreservesProviderState(t *testing.T) {
	summary := &providers.ProviderStateSummary{
		Provider:               "openai",
		Protocol:               "responses_websocket",
		ReplayMode:             "previous_response_id",
		PreviousResponseIDUsed: true,
		InputItems:             1,
		FullInputItems:         3,
		DeltaInputItems:        1,
	}

	event := AdaptStreamEvent(providers.StreamEvent{
		Type:          providers.EventProviderState,
		ProviderState: summary,
	})
	if event.Type != ProviderState || event.ProviderState == nil {
		t.Fatalf("provider state not adapted: %+v", event)
	}
	if !reflect.DeepEqual(event.ProviderState, summary) {
		t.Fatalf("provider state changed during adaptation: got %+v want %+v", event.ProviderState, summary)
	}

	stream := ToStreamEvent(event)
	if stream.Type != providers.EventProviderState || stream.ProviderState == nil {
		t.Fatalf("provider state not converted back: %+v", stream)
	}
	if !reflect.DeepEqual(stream.ProviderState, summary) {
		t.Fatalf("provider state changed during stream conversion: got %+v want %+v", stream.ProviderState, summary)
	}
}

func TestAdaptStreamEventPreservesTextPhase(t *testing.T) {
	event := AdaptStreamEvent(providers.StreamEvent{
		Type:    providers.EventContentDelta,
		Content: "answer",
		Phase:   providers.MessagePhaseFinalAnswer,
	})
	if event.Type != TextDelta || event.Content != "answer" || event.Phase != providers.MessagePhaseFinalAnswer {
		t.Fatalf("text phase not adapted: %+v", event)
	}

	stream := ToStreamEvent(event)
	if stream.Type != providers.EventContentDelta || stream.Content != "answer" || stream.Phase != providers.MessagePhaseFinalAnswer {
		t.Fatalf("text phase not converted back: %+v", stream)
	}
}

func TestAdaptStreamEventPreservesCompactPhase(t *testing.T) {
	stream := providers.StreamEvent{
		Type:          providers.EventCompact,
		CompactReason: "proactive",
		CompactPhase:  providers.CompactPhaseStarted,
	}

	event := AdaptStreamEvent(stream)
	if event.CompactReason != stream.CompactReason || event.CompactPhase != stream.CompactPhase {
		t.Fatalf("adapted compact event = %+v, want reason and phase preserved", event)
	}

	roundTrip := ToStreamEvent(event)
	if roundTrip.CompactReason != stream.CompactReason || roundTrip.CompactPhase != stream.CompactPhase {
		t.Fatalf("round-tripped compact event = %+v, want reason and phase preserved", roundTrip)
	}
}
