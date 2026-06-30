package providerfactory

import (
	"os"
	"testing"

	"github.com/blueberrycongee/wuu/internal/config"
)

func TestBuildClient_OpenAICompatible(t *testing.T) {
	t.Setenv("TEST_WUU_KEY", "abc")

	client, err := BuildClient(config.ProviderConfig{
		Type:      "openai-compatible",
		BaseURL:   "https://example.com/v1",
		APIKeyEnv: "TEST_WUU_KEY",
		Model:     "gpt-test",
	}, "test")
	if err != nil {
		t.Fatalf("BuildClient returned error: %v", err)
	}
	if client == nil {
		t.Fatal("expected client")
	}
}

func TestBuildClient_OpenAICodexDoesNotRequireAPIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")

	client, err := BuildClient(config.ProviderConfig{
		Type:    "openai-codex",
		Model:   "gpt-5-codex",
		WireAPI: "responses",
	}, "openai-codex")
	if err != nil {
		t.Fatalf("BuildClient returned error: %v", err)
	}
	if client == nil {
		t.Fatal("expected client")
	}
}

func TestBuildClient_Anthropic(t *testing.T) {
	t.Setenv("TEST_ANTHROPIC_KEY", "abc")

	client, err := BuildClient(config.ProviderConfig{
		Type:      "anthropic",
		BaseURL:   "https://api.anthropic.com",
		APIKeyEnv: "TEST_ANTHROPIC_KEY",
		Model:     "claude-test",
	}, "test")
	if err != nil {
		t.Fatalf("BuildClient returned error: %v", err)
	}
	if client == nil {
		t.Fatal("expected client")
	}
}

func TestResolveProviderProfile_OpenAICodex(t *testing.T) {
	profile, err := resolveProviderProfile(config.ProviderConfig{Type: "openai-codex"})
	if err != nil {
		t.Fatalf("resolveProviderProfile returned error: %v", err)
	}
	if profile.Wire != wireOpenAIResponses {
		t.Fatalf("Wire = %q, want %q", profile.Wire, wireOpenAIResponses)
	}
	if profile.Auth != authCodexOAuth {
		t.Fatalf("Auth = %q, want %q", profile.Auth, authCodexOAuth)
	}
}

func TestResolveProviderProfile_OpenAIResponses(t *testing.T) {
	profile, err := resolveProviderProfile(config.ProviderConfig{Type: "openai-compatible", WireAPI: "responses"})
	if err != nil {
		t.Fatalf("resolveProviderProfile returned error: %v", err)
	}
	if profile.Wire != wireOpenAIResponses {
		t.Fatalf("Wire = %q, want %q", profile.Wire, wireOpenAIResponses)
	}
	if profile.Auth != authAPIKey {
		t.Fatalf("Auth = %q, want %q", profile.Auth, authAPIKey)
	}
}

func TestResolveProviderProfile_Anthropic(t *testing.T) {
	profile, err := resolveProviderProfile(config.ProviderConfig{Type: "anthropic"})
	if err != nil {
		t.Fatalf("resolveProviderProfile returned error: %v", err)
	}
	if profile.Wire != wireAnthropicMessages {
		t.Fatalf("Wire = %q, want %q", profile.Wire, wireAnthropicMessages)
	}
	if profile.Auth != authAnthropicToken {
		t.Fatalf("Auth = %q, want %q", profile.Auth, authAnthropicToken)
	}
}

func TestSupportsNativeToolDiscovery(t *testing.T) {
	for _, tc := range []struct {
		name     string
		provider config.ProviderConfig
		model    string
		options  map[string]any
		want     bool
	}{
		{
			name:     "openai responses",
			provider: config.ProviderConfig{Type: "openai-compatible", WireAPI: "responses"},
			model:    "gpt-test",
			want:     true,
		},
		{
			name:     "openai chat fallback",
			provider: config.ProviderConfig{Type: "openai-compatible"},
			model:    "gpt-test",
			want:     false,
		},
		{
			name:     "first party anthropic",
			provider: config.ProviderConfig{Type: "anthropic", BaseURL: "https://api.anthropic.com"},
			model:    "claude-sonnet-4-5",
			want:     true,
		},
		{
			name:     "anthropic compatible fallback",
			provider: config.ProviderConfig{Type: "anthropic", BaseURL: "https://anthropic-proxy.example.com"},
			model:    "claude-sonnet-4-5",
			want:     false,
		},
		{
			name:     "minimax official anthropic endpoint",
			provider: config.ProviderConfig{Type: "anthropic", BaseURL: "https://api.minimaxi.com/anthropic"},
			model:    "MiniMax-M3",
			want:     true,
		},
		{
			name:     "zai official anthropic endpoint",
			provider: config.ProviderConfig{Type: "anthropic", BaseURL: "https://api.z.ai/api/anthropic"},
			model:    "glm-4.7",
			want:     true,
		},
		{
			name:     "minimax m3 proxy fallback",
			provider: config.ProviderConfig{Type: "anthropic", BaseURL: "https://anthropic-proxy.example.com"},
			model:    "MiniMax-M3",
			want:     false,
		},
		{
			name:     "kimi official fallback",
			provider: config.ProviderConfig{Type: "anthropic", BaseURL: "https://api.moonshot.cn/anthropic"},
			model:    "kimi-k2",
			want:     false,
		},
		{
			name:     "anthropic compatible opt in",
			provider: config.ProviderConfig{Type: "anthropic", BaseURL: "https://anthropic-proxy.example.com"},
			model:    "MiniMax-M3",
			options:  map[string]any{"anthropicToolSearch": true},
			want:     true,
		},
		{
			name:     "explicitly disabled",
			provider: config.ProviderConfig{Type: "anthropic", BaseURL: "https://api.anthropic.com"},
			model:    "claude-sonnet-4-5",
			options:  map[string]any{"anthropicToolSearch": false},
			want:     false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := SupportsNativeToolDiscovery(tc.provider, tc.model, tc.options); got != tc.want {
				t.Fatalf("SupportsNativeToolDiscovery = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestResolveProviderProfile_InfersKnownOpenCodeNPMProviders(t *testing.T) {
	for _, tc := range []struct {
		name string
		npm  string
		wire wireProtocol
		auth authMode
	}{
		{name: "openai compatible", npm: "@ai-sdk/openai-compatible", wire: wireOpenAIChat, auth: authAPIKey},
		{name: "openrouter", npm: "@openrouter/ai-sdk-provider", wire: wireOpenAIChat, auth: authAPIKey},
		{name: "anthropic", npm: "@ai-sdk/anthropic", wire: wireAnthropicMessages, auth: authAnthropicToken},
	} {
		t.Run(tc.name, func(t *testing.T) {
			profile, err := resolveProviderProfile(config.ProviderConfig{Type: "opencode-provider-id", NPM: tc.npm})
			if err != nil {
				t.Fatalf("resolveProviderProfile returned error: %v", err)
			}
			if profile.Wire != tc.wire {
				t.Fatalf("Wire = %q, want %q", profile.Wire, tc.wire)
			}
			if profile.Auth != tc.auth {
				t.Fatalf("Auth = %q, want %q", profile.Auth, tc.auth)
			}
		})
	}
}

func TestResolveProviderProfile_RejectsUnsupportedNPMProviders(t *testing.T) {
	_, err := resolveProviderProfile(config.ProviderConfig{Type: "google", NPM: "@ai-sdk/google"})
	if err == nil {
		t.Fatal("expected unsupported provider error")
	}
}

func TestResolveProviderProfile_OpenAICodexRejectsChatWire(t *testing.T) {
	_, err := resolveProviderProfile(config.ProviderConfig{Type: "openai-codex", WireAPI: "chat"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveAPIKey_AuthStoreFallback(t *testing.T) {
	// Clear default env var so fallback to auth store is exercised.
	t.Setenv("OPENAI_API_KEY", "")

	home := t.TempDir()
	// Save key to auth store.
	if err := config.SaveAuthKey(home, "myapi", "sk-from-auth-store"); err != nil {
		t.Fatalf("save auth key: %v", err)
	}

	provider := config.ProviderConfig{
		Type:    "openai-compatible",
		BaseURL: "https://example.com/v1",
		Model:   "test",
		// No APIKey, no APIKeyEnv set.
	}

	key, err := ResolveAPIKeyWithHome(provider, "myapi", home)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if key != "sk-from-auth-store" {
		t.Fatalf("expected sk-from-auth-store, got %q", key)
	}
}

func TestBuildClientWithRetry_AppliesCustomConfig(t *testing.T) {
	t.Setenv("TEST_WUU_KEY", "abc")

	rc := SubAgentRetryConfig()
	if rc.MaxRetries != 6 {
		t.Fatalf("SubAgentRetryConfig MaxRetries = %d, want 6", rc.MaxRetries)
	}
	if rc.MaxDelay < rc.InitialDelay {
		t.Fatalf("SubAgentRetryConfig MaxDelay (%v) < InitialDelay (%v)", rc.MaxDelay, rc.InitialDelay)
	}

	// We can't introspect the underlying client's RetryConfig from the
	// public providers.Client interface, but we can at least verify
	// the constructor accepts the override and returns successfully.
	// Smoke test for both supported provider families.
	for _, ptype := range []string{"openai-compatible", "anthropic"} {
		client, err := BuildClientWithRetry(config.ProviderConfig{
			Type:      ptype,
			BaseURL:   "https://example.com/v1",
			APIKeyEnv: "TEST_WUU_KEY",
			Model:     "test",
		}, "test", &rc)
		if err != nil {
			t.Fatalf("BuildClientWithRetry(%s) returned error: %v", ptype, err)
		}
		if client == nil {
			t.Fatalf("BuildClientWithRetry(%s) returned nil client", ptype)
		}
	}
}

func TestBuildClient_MissingAPIKey(t *testing.T) {
	_ = os.Unsetenv("MISSING_WUU_KEY")

	_, err := BuildClient(config.ProviderConfig{
		Type:      "openai-compatible",
		BaseURL:   "https://example.com/v1",
		APIKeyEnv: "MISSING_WUU_KEY",
		Model:     "gpt-test",
	}, "test")
	if err == nil {
		t.Fatal("expected error")
	}
}
