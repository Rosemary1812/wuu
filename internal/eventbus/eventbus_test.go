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
