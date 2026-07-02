package appserver

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
)

func TestSanitizeStreamEventIncludesLifecycle(t *testing.T) {
	got := sanitizeStreamEvent(providers.StreamEvent{
		Type: providers.EventLifecycle,
		Lifecycle: &providers.StreamLifecycle{
			Phase:      providers.StreamPhaseReconnecting,
			Attempt:    2,
			RetryCount: 1,
			MaxRetries: 3,
			RetryIn:    1500 * time.Millisecond,
			Reason:     "connection reset",
		},
	})

	if got.Lifecycle == nil {
		t.Fatal("expected lifecycle payload")
	}
	if got.Lifecycle.Phase != "reconnecting" ||
		got.Lifecycle.Attempt != 2 ||
		got.Lifecycle.RetryCount != 1 ||
		got.Lifecycle.MaxRetries != 3 ||
		got.Lifecycle.RetryInMS != 1500 ||
		got.Lifecycle.Reason != "connection reset" {
		t.Fatalf("unexpected lifecycle payload: %+v", got.Lifecycle)
	}

	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal stream event: %v", err)
	}
	wire := string(raw)
	if !strings.Contains(wire, `"retry_in_ms":1500`) {
		t.Fatalf("expected snake_case lifecycle wire payload, got %s", wire)
	}
	if strings.Contains(wire, "RetryIn") {
		t.Fatalf("wire payload leaked Go field names: %s", wire)
	}
}
