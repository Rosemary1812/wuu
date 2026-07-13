package providers

import "time"

// RetryConfigForProfile is the single source of truth for inference recovery
// budgets. Unary and streaming executors use the same attempt ceiling.
//
// These initial values preserve Wuu's established behavior while moving
// ownership into one registry. Runtime evidence can tune them later without
// reopening every agent and auxiliary caller.
func RetryConfigForProfile(profile InferenceWorkloadProfile) RetryConfig {
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
