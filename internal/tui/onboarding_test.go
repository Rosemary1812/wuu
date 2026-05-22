package tui

import "testing"

func TestOnboardingResult(t *testing.T) {
	m := OnboardingModel{
		providerType: "openai",
		baseURL:      "https://api.openai.com/v1",
		apiKey:       "sk-test",
		model:        "gpt-4.1",
		theme:        "dark",
		step:         stepDone,
	}

	result := m.Result()
	if result.ProviderType != "openai" {
		t.Fatalf("provider type: %q", result.ProviderType)
	}
	if result.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("base url: %q", result.BaseURL)
	}
	if result.APIKey != "sk-test" {
		t.Fatalf("api key: %q", result.APIKey)
	}
	if result.Model != "gpt-4.1" {
		t.Fatalf("model: %q", result.Model)
	}
	if result.Theme != "dark" {
		t.Fatalf("theme: %q", result.Theme)
	}
	if !result.Completed {
		t.Fatal("expected completed")
	}
}

func TestOnboardingStepTransitions(t *testing.T) {
	m := NewOnboardingModel()
	if m.step != stepProviderType {
		t.Fatalf("initial step: %d", m.step)
	}

	// Select "openai".
	m.cursor = 1
	(&m).selectCurrentOption()
	if m.step != stepBaseURL {
		t.Fatalf("after provider select, step: %d", m.step)
	}
	if m.providerType != "openai" {
		t.Fatalf("provider type: %q", m.providerType)
	}
	if m.baseURL != "https://api.openai.com/v1" {
		t.Fatalf("base url not pre-filled: %q", m.baseURL)
	}
}

func TestOnboardingCodexOAuthSkipsAPIKey(t *testing.T) {
	m := NewOnboardingModel()
	m.cursor = 0
	(&m).selectCurrentOption()
	if m.providerType != "openai-codex" {
		t.Fatalf("provider type: %q", m.providerType)
	}
	if m.step != stepModel {
		t.Fatalf("expected Codex OAuth to skip base URL/API key steps, got step %d", m.step)
	}
	if m.baseURL != "https://chatgpt.com/backend-api/codex" {
		t.Fatalf("base url: %q", m.baseURL)
	}
	if m.apiKey != "" {
		t.Fatalf("api key should be empty, got %q", m.apiKey)
	}
}

func TestOnboardingResult_NotCompleted(t *testing.T) {
	m := NewOnboardingModel()
	result := m.Result()
	if result.Completed {
		t.Fatal("expected not completed on initial state")
	}
}
