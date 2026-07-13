package providers

import (
	"testing"
	"time"
)

func TestStreamRetryConfigForProfile(t *testing.T) {
	tests := []struct {
		name       string
		profile    InferenceWorkloadProfile
		maxRetries int
	}{
		{name: "interactive", profile: InferenceProfileInteractive, maxRetries: 5},
		{name: "background agent", profile: InferenceProfileBackgroundAgent, maxRetries: 5},
		{name: "continuation critical", profile: InferenceProfileContinuationCritical, maxRetries: 3},
		{name: "best effort", profile: InferenceProfileBestEffort, maxRetries: 3},
		{name: "unknown fails closed", profile: "new-unregistered-workload", maxRetries: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := StreamRetryConfigForProfile(test.profile)
			if got.MaxRetries != test.maxRetries || got.InitialDelay != time.Second || got.MaxDelay != time.Minute {
				t.Fatalf("config = %+v, want max retries %d and 1s..1m backoff", got, test.maxRetries)
			}
		})
	}
}

func TestProviderRequestRetryConfigForProfile(t *testing.T) {
	background := ProviderRequestRetryConfigForProfile(InferenceProfileBackgroundAgent)
	if background.MaxRetries != 6 || background.InitialDelay != 2*time.Second || background.MaxDelay != time.Minute {
		t.Fatalf("background request config = %+v", background)
	}
	interactive := ProviderRequestRetryConfigForProfile(InferenceProfileInteractive)
	if interactive.MaxRetries != 0 {
		t.Fatalf("interactive request layer must remain single-attempt: %+v", interactive)
	}
}
