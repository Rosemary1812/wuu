package providerfactory

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/config"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/providers/anthropic"
	"github.com/blueberrycongee/wuu/internal/providers/codex"
	"github.com/blueberrycongee/wuu/internal/providers/openai"
)

// BuildClient constructs a provider client from config using the provider
// default retry policy. providerName is the key under which this provider lives
// in the config map; it's needed so resolveAPIKey can fall back to the global
// auth store.
func BuildClient(provider config.ProviderConfig, providerName string) (providers.Client, error) {
	return BuildClientWithRetry(provider, providerName, nil)
}

// BuildClientWithRetry is like BuildClient but lets the caller pin a
// specific HTTP retry policy. Use this for long-running consumers
// (e.g. sub-agents that may run for many minutes and should be more
// tolerant of transient 429 / 5xx than the interactive main agent).
// Pass nil to use the provider client's built-in default.
func BuildClientWithRetry(provider config.ProviderConfig, providerName string, retry *providers.RetryConfig) (providers.Client, error) {
	return buildClientWithRetry(provider, providerName, retry)
}

// SubAgentRetryConfig returns the more aggressive HTTP retry policy
// recommended for long-running sub-agent runs. Compared to the default single
// attempt, this gives the worker substantially more headroom to ride out
// transient rate limits and upstream blips: 6 retries, 2s to 60s backoff.
func SubAgentRetryConfig() providers.RetryConfig {
	return providers.RetryConfig{
		MaxRetries:   6,
		InitialDelay: 2 * time.Second,
		MaxDelay:     60 * time.Second,
	}
}

// BuildStreamClient constructs a streaming-capable provider client using the
// provider default retry policy. providerName is the config map key; see
// BuildClient for why this matters for the global auth-store fallback.
func BuildStreamClient(provider config.ProviderConfig, providerName string) (providers.StreamClient, error) {
	return BuildStreamClientWithRetry(provider, providerName, nil)
}

// BuildStreamClientWithRetry is like BuildStreamClient but lets the
// caller pin a specific HTTP retry policy. Use this for long-running
// stream consumers (e.g. sub-agents that may run for many minutes and
// should be more tolerant of transient 429 / 5xx than the interactive
// main agent). Pass nil to use the provider client's built-in default.
func BuildStreamClientWithRetry(provider config.ProviderConfig, providerName string, retry *providers.RetryConfig) (providers.StreamClient, error) {
	client, err := buildClientWithRetry(provider, providerName, retry)
	if err != nil {
		return nil, err
	}
	return providers.AdaptStreamClient(client), nil
}

// SupportsNativeToolDiscoveryByDefault reports whether auto mode should use
// provider-native deferred tool loading. Only first-party provider paths are
// enabled by default; compatible endpoints must opt in explicitly.
func SupportsNativeToolDiscoveryByDefault(provider config.ProviderConfig, model string, providerOptions map[string]any) bool {
	profile, err := resolveProviderProfile(provider)
	if err != nil {
		return false
	}
	switch profile.Wire {
	case wireOpenAIResponses:
		return openAIModelSupportsNativeToolSearch(model) &&
			(isFirstPartyOpenAIResponsesBaseURL(provider.BaseURL) || profile.Auth == authCodexOAuth)
	case wireAnthropicMessages:
		return anthropic.SupportsNativeToolSearchByDefault(provider.BaseURL, model, providerOptions)
	default:
		return false
	}
}

// SupportsNativeToolDiscovery reports whether this provider path can receive
// provider-native deferred tool declarations when the user explicitly opts in.
func SupportsNativeToolDiscovery(provider config.ProviderConfig, model string, providerOptions map[string]any) bool {
	profile, err := resolveProviderProfile(provider)
	if err != nil {
		return false
	}
	switch profile.Wire {
	case wireOpenAIResponses:
		return openAIModelSupportsNativeToolSearch(model)
	case wireAnthropicMessages:
		return anthropic.SupportsNativeToolSearchWhenExplicitlyEnabled(model, providerOptions)
	default:
		return false
	}
}

func ShouldFallbackToWuuToolSearchByDefault(provider config.ProviderConfig, model string, _ map[string]any) bool {
	profile, err := resolveProviderProfile(provider)
	if err != nil {
		return false
	}
	switch profile.Wire {
	case wireOpenAIResponses:
		return !openAIModelSupportsNativeToolSearch(model) &&
			(isFirstPartyOpenAIResponsesBaseURL(provider.BaseURL) || profile.Auth == authCodexOAuth)
	default:
		return false
	}
}

func openAIModelSupportsNativeToolSearch(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	if !strings.HasPrefix(model, "gpt-") {
		return false
	}
	version := strings.TrimPrefix(model, "gpt-")
	version = strings.TrimLeft(version, "-_")
	versionToken := ""
	for _, r := range version {
		if (r >= '0' && r <= '9') || r == '.' {
			versionToken += string(r)
			continue
		}
		break
	}
	if versionToken == "" {
		return false
	}
	majorText, minorText, hasMinor := strings.Cut(versionToken, ".")
	major, err := strconv.Atoi(majorText)
	if err != nil {
		return false
	}
	minor := 0
	if hasMinor {
		minorValue, err := strconv.Atoi(minorText)
		if err != nil {
			return false
		}
		minor = minorValue
	}
	return major > 5 || (major == 5 && minor >= 4)
}

func isFirstPartyOpenAIResponsesBaseURL(raw string) bool {
	u := strings.TrimRight(strings.ToLower(strings.TrimSpace(raw)), "/")
	return u == "https://api.openai.com" ||
		u == "https://api.openai.com/v1" ||
		u == "https://chatgpt.com/backend-api/codex"
}

func buildClientWithRetry(provider config.ProviderConfig, providerName string, retry *providers.RetryConfig) (providers.Client, error) {
	profile, err := resolveProviderProfile(provider)
	if err != nil {
		return nil, err
	}

	// Resolve auth token for anthropic providers (Bearer auth, aligned with
	// the Anthropic SDK's ANTHROPIC_AUTH_TOKEN support).
	authToken := resolveAuthToken(provider, providerName)

	switch profile.Wire {
	case wireOpenAIChat, wireOpenAIResponses:
		if profile.Auth == authCodexOAuth {
			client, newErr := codex.New(codex.ClientConfig{
				BaseURL:               provider.BaseURL,
				APIKey:                resolveExplicitAPIKey(provider),
				Headers:               provider.Headers,
				RetryConfig:           retry,
				StreamConfig:          providerStreamTransportConfig(provider),
				StreamTransport:       providerStreamTransportMode(provider),
				ReuseCodexCredentials: provider.ReuseCodexCredentials,
			})
			if newErr != nil {
				return nil, newErr
			}
			return client, nil
		}
		apiKey, apiKeyErr := resolveAPIKey(provider, providerName)
		if apiKeyErr != nil {
			return nil, apiKeyErr
		}
		client, newErr := openai.New(openai.ClientConfig{
			BaseURL:            provider.BaseURL,
			WireAPI:            openAIWireConfig(profile.Wire),
			APIKey:             apiKey,
			Headers:            provider.Headers,
			RetryConfig:        retry,
			StreamConfig:       providerStreamTransportConfig(provider),
			ResponsesTransport: providerStreamTransportMode(provider),
		})
		if newErr != nil {
			return nil, newErr
		}
		return client, nil
	case wireAnthropicMessages:
		// API key is optional when auth token is available.
		apiKey, apiKeyErr := resolveAPIKey(provider, providerName)
		if apiKeyErr != nil && authToken == "" {
			return nil, apiKeyErr
		}
		client, newErr := anthropic.New(anthropic.ClientConfig{
			BaseURL:                         provider.BaseURL,
			APIKey:                          apiKey,
			AuthToken:                       authToken,
			Headers:                         provider.Headers,
			RetryConfig:                     retry,
			StreamConfig:                    providerStreamTransportConfig(provider),
			CacheCreationInputTokensOmitted: provider.CacheCreationInputTokensOmitted,
		})
		if newErr != nil {
			return nil, newErr
		}
		return client, nil
	default:
		return nil, fmt.Errorf("unsupported provider wire protocol %q", profile.Wire)
	}
}

func providerStreamTransportConfig(provider config.ProviderConfig) *providers.StreamTransportConfig {
	cfg := providers.StreamTransportConfig{}
	if provider.StreamConnectTimeoutMS > 0 {
		cfg.ConnectTimeout = time.Duration(provider.StreamConnectTimeoutMS) * time.Millisecond
	}
	if provider.StreamIdleTimeoutMS > 0 {
		cfg.IdleTimeout = time.Duration(provider.StreamIdleTimeoutMS) * time.Millisecond
	}
	if cfg == (providers.StreamTransportConfig{}) {
		return nil
	}
	return &cfg
}

func providerStreamTransportMode(provider config.ProviderConfig) providers.StreamTransportMode {
	mode, _ := providers.NormalizeStreamTransportMode(provider.StreamTransport)
	return mode
}

func normalizeType(value string) string {
	s := strings.ToLower(strings.TrimSpace(value))
	s = strings.ReplaceAll(s, "_", "-")
	return s
}

// ResolveAPIKeyWithHome resolves API key with explicit home directory.
func ResolveAPIKeyWithHome(provider config.ProviderConfig, providerName, home string) (string, error) {
	// 1. Explicit api_key in config.
	if strings.TrimSpace(provider.APIKey) != "" {
		return strings.TrimSpace(provider.APIKey), nil
	}

	// 2. Environment variable.
	envKey := strings.TrimSpace(provider.APIKeyEnv)
	if envKey == "" {
		envKey = defaultAPIKeyEnv(normalizeType(provider.Type))
	}
	if envKey != "" {
		value := strings.TrimSpace(os.Getenv(envKey))
		if value != "" {
			return value, nil
		}
	}

	// 3. Global auth store.
	if home != "" && providerName != "" {
		key, err := config.LoadAuthKey(home, providerName)
		if err == nil && key != "" {
			return key, nil
		}
	}

	hint := "set api_key or run wuu init"
	if envKey != "" {
		hint = fmt.Sprintf("set api_key, %s env var, or run wuu init", envKey)
	}
	return "", fmt.Errorf("no API key found for provider %q (%s)", provider.Type, hint)
}

func resolveAPIKey(provider config.ProviderConfig, providerName string) (string, error) {
	return ResolveAPIKeyWithHome(provider, providerName, os.Getenv("HOME"))
}

func resolveExplicitAPIKey(provider config.ProviderConfig) string {
	if strings.TrimSpace(provider.APIKey) != "" {
		return strings.TrimSpace(provider.APIKey)
	}
	if envKey := strings.TrimSpace(provider.APIKeyEnv); envKey != "" {
		return strings.TrimSpace(os.Getenv(envKey))
	}
	return ""
}

// resolveAuthToken resolves a Bearer auth token from config or environment.
// This mirrors the Anthropic SDK's ANTHROPIC_AUTH_TOKEN support.
func resolveAuthToken(provider config.ProviderConfig, providerName string) string {
	if t := strings.TrimSpace(provider.AuthToken); t != "" {
		return t
	}
	envKey := strings.TrimSpace(provider.AuthTokenEnv)
	if envKey == "" {
		envKey = "ANTHROPIC_AUTH_TOKEN"
	}
	if token := strings.TrimSpace(os.Getenv(envKey)); token != "" {
		return token
	}
	if providerName != "" {
		token, err := config.LoadAuthToken(os.Getenv("HOME"), providerName)
		if err == nil && strings.TrimSpace(token) != "" {
			return strings.TrimSpace(token)
		}
	}
	return ""
}

func defaultAPIKeyEnv(providerType string) string {
	switch providerType {
	case "openai", "openai-compatible", "codex":
		return "OPENAI_API_KEY"
	case "anthropic", "claude", "anthropic-official":
		return "ANTHROPIC_API_KEY"
	default:
		return ""
	}
}
