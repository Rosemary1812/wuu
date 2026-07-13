package providers

import (
	"testing"
	"time"
)

func TestRetryConfigForProfile(t *testing.T) {
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
			got := RetryConfigForProfile(test.profile)
			if got.MaxRetries != test.maxRetries || got.InitialDelay != time.Second || got.MaxDelay != time.Minute {
				t.Fatalf("config = %+v, want max retries %d and 1s..1m backoff", got, test.maxRetries)
			}
		})
	}
}
