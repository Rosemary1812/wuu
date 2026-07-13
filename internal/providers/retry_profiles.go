package providers

import "time"

// StreamRetryConfigForProfile is the single source of truth for stream replay
// budgets. Product call sites select a workload profile; they do not carry
// local retry counts or backoff constants.
//
// These initial values preserve Wuu's established behavior while moving
// ownership into one registry. Runtime evidence can tune them later without
// reopening every agent and auxiliary caller.
func StreamRetryConfigForProfile(profile InferenceWorkloadProfile) RetryConfig {
	switch profile {
	case InferenceProfileInteractive, InferenceProfileBackgroundAgent:
		return RetryConfig{
			MaxRetries:   5,
			InitialDelay: time.Second,
			MaxDelay:     time.Minute,
		}
	case InferenceProfileContinuationCritical, InferenceProfileBestEffort:
		return RetryConfig{
			MaxRetries:   3,
			InitialDelay: time.Second,
			MaxDelay:     time.Minute,
		}
	default:
		// Unknown workloads fail closed to one provider attempt. New product
		// paths must choose a profile explicitly before gaining replay budget.
		return DefaultRetryConfig()
	}
}

// ProviderRequestRetryConfigForProfile controls legacy unary provider calls
// while they are migrated into the shared attempt executor. Streaming clients
// must keep their SDK/request layer single-attempt because the stream retry
// engine already owns replay.
func ProviderRequestRetryConfigForProfile(profile InferenceWorkloadProfile) RetryConfig {
	if profile == InferenceProfileBackgroundAgent {
		return RetryConfig{
			MaxRetries:   6,
			InitialDelay: 2 * time.Second,
			MaxDelay:     time.Minute,
		}
	}
	return DefaultRetryConfig()
}
