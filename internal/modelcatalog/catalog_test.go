package modelcatalog

import (
	"testing"

	"github.com/blueberrycongee/wuu/internal/config"
)

func TestMatchProviderByBaseURL(t *testing.T) {
	provider, ok := MatchProvider("xiaomi", config.ProviderConfig{
		Type:    "openai-compatible",
		BaseURL: "https://token-plan-cn.xiaomimimo.com/v1/",
	})
	if !ok {
		t.Fatal("expected Xiaomi token plan provider match")
	}
	if provider.ID != "xiaomi-token-plan-cn" {
		t.Fatalf("provider ID = %q", provider.ID)
	}

	ruleName, enriched := EnrichProvider("xiaomi", config.ProviderConfig{
		Type:    "openai-compatible",
		BaseURL: "https://token-plan-cn.xiaomimimo.com/v1",
		Model:   "mimo-v2.5-pro",
	}, "mimo-v2.5-pro")
	if ruleName != "xiaomi-token-plan-cn" {
		t.Fatalf("rule provider name = %q", ruleName)
	}
	model := enriched.Models["mimo-v2.5-pro"]
	if model.Name != "MiMo-V2.5-Pro" || model.ReleaseDate != "2026-04-22" {
		t.Fatalf("unexpected model metadata: %+v", model)
	}
	if model.Reasoning == nil || !*model.Reasoning {
		t.Fatalf("expected reasoning model metadata: %+v", model)
	}
	if enriched.ContextWindow != 1048576 {
		t.Fatalf("ContextWindow = %d", enriched.ContextWindow)
	}
}

func TestCatalogMatchesOpenCodeDefaultVisibility(t *testing.T) {
	provider, ok := MatchProvider("openai", config.ProviderConfig{Type: "openai"})
	if !ok {
		t.Fatal("expected OpenAI provider match")
	}

	var hasHiddenChatAlias bool
	var fastMode Model
	for _, model := range provider.Models {
		if model.ID == "gpt-5-chat-latest" {
			hasHiddenChatAlias = true
		}
		if model.ID == "gpt-5.5-fast" {
			fastMode = model
		}
	}
	if hasHiddenChatAlias {
		t.Fatal("catalog should hide OpenCode's invalid OpenAI chat alias")
	}
	if fastMode.ID == "" || fastMode.APIID != "gpt-5.5" || fastMode.Options["serviceTier"] != "priority" {
		t.Fatalf("unexpected experimental mode metadata: %+v", fastMode)
	}
}

func TestCatalogSnapshotMatchesOpenCodeDefaultVisibleCounts(t *testing.T) {
	providers, err := Providers()
	if err != nil {
		t.Fatalf("Providers: %v", err)
	}
	if len(providers) != 145 {
		t.Fatalf("provider count = %d, want 145", len(providers))
	}

	modelCount := 0
	for _, provider := range providers {
		for _, model := range provider.Models {
			modelCount++
			switch model.Status {
			case "deprecated", "alpha":
				t.Fatalf("catalog should hide %s model %s/%s", model.Status, provider.ID, model.ID)
			}
			if model.ID == "gpt-5-chat-latest" && (provider.ID == "openai" || provider.ID == "github-copilot" || provider.ID == "openrouter") {
				t.Fatalf("catalog should hide invalid chat alias %s/%s", provider.ID, model.ID)
			}
			if provider.ID == "openrouter" && model.ID == "openai/gpt-5-chat" {
				t.Fatalf("catalog should hide invalid OpenRouter chat alias")
			}
		}
	}
	if modelCount != 5199 {
		t.Fatalf("model count = %d, want 5199", modelCount)
	}
}

func TestModelConfigCarriesCapabilityMetadata(t *testing.T) {
	provider, ok := MatchProvider("openai", config.ProviderConfig{Type: "openai"})
	if !ok {
		t.Fatal("expected OpenAI provider match")
	}
	enriched := MergeProvider(config.ProviderConfig{Type: "openai"}, provider, "text-embedding-3-large", "o3")

	embedding := enriched.Models["text-embedding-3-large"]
	if embedding.ToolCall == nil || *embedding.ToolCall {
		t.Fatalf("expected embedding model to carry tool_call=false: %+v", embedding)
	}
	if embedding.Modalities == nil || !stringSliceContains(embedding.Modalities.Input, "text") {
		t.Fatalf("expected embedding modalities: %+v", embedding.Modalities)
	}

	o3 := enriched.Models["o3"]
	if o3.ToolCall == nil || !*o3.ToolCall || o3.Modalities == nil || !stringSliceContains(o3.Modalities.Input, "image") || !stringSliceContains(o3.Modalities.Input, "pdf") {
		t.Fatalf("expected o3 tool and media capability metadata: %+v", o3)
	}
}

func stringSliceContains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func TestMatchProviderDoesNotUseWireTypeForCustomEndpoint(t *testing.T) {
	if provider, ok := MatchProvider("zhipu2", config.ProviderConfig{
		Type:    "anthropic",
		BaseURL: "https://open.bigmodel.cn/api/anthropic",
		Model:   "glm-5.1",
	}); ok {
		t.Fatalf("custom Anthropic-compatible endpoint matched %q", provider.ID)
	}
}

func TestMatchProviderDoesNotUseProviderNameForCustomEndpoint(t *testing.T) {
	for _, tc := range []struct {
		name     string
		provider config.ProviderConfig
	}{
		{
			name: "openai",
			provider: config.ProviderConfig{
				Type:    "openai-compatible",
				BaseURL: "https://proxy.example/v1",
			},
		},
		{
			name: "claude",
			provider: config.ProviderConfig{
				Type:    "anthropic",
				BaseURL: "https://proxy.example/anthropic",
			},
		},
		{
			name: "gemini",
			provider: config.ProviderConfig{
				Type:    "openai-compatible",
				BaseURL: "https://proxy.example/v1",
			},
		},
	} {
		if provider, ok := MatchProvider(tc.name, tc.provider); ok {
			t.Fatalf("custom endpoint %q matched %q", tc.name, provider.ID)
		}
	}
}

func TestMatchProviderDoesNotTreatCodexSubscriptionAsOpenCodeZen(t *testing.T) {
	if provider, ok := MatchProvider("openai-codex", config.ProviderConfig{
		Type:    "openai-codex",
		BaseURL: "https://chatgpt.com/backend-api/codex",
		Model:   "gpt-5.5",
	}); ok {
		t.Fatalf("Codex subscription provider matched %q", provider.ID)
	}

	provider, ok := MatchProvider("opencode", config.ProviderConfig{
		Type:    "openai-compatible",
		BaseURL: "https://opencode.ai/zen/v1",
	})
	if !ok || provider.ID != "opencode" {
		t.Fatalf("OpenCode Zen provider match = %q, %v", provider.ID, ok)
	}
}

func TestMatchProviderDisambiguatesDuplicateEndpointByName(t *testing.T) {
	for _, tc := range []struct {
		name string
		want string
	}{
		{name: "firepass", want: "firepass"},
		{name: "fireworks-ai", want: "fireworks-ai"},
	} {
		provider, ok := MatchProvider(tc.name, config.ProviderConfig{
			Type:    "openai-compatible",
			BaseURL: "https://api.fireworks.ai/inference/v1",
		})
		if !ok || provider.ID != tc.want {
			t.Fatalf("provider %q matched %q, %v; want %q", tc.name, provider.ID, ok, tc.want)
		}
	}

	provider, ok := MatchProvider("minimax-coding-plan", config.ProviderConfig{
		Type:    "anthropic",
		BaseURL: "https://api.minimax.io/anthropic/v1",
	})
	if !ok || provider.ID != "minimax-coding-plan" {
		t.Fatalf("MiniMax coding plan matched %q, %v", provider.ID, ok)
	}
}

func TestMergeProviderCarriesModelOptionsAndHeaders(t *testing.T) {
	provider, ok := MatchProvider("anthropic", config.ProviderConfig{Type: "anthropic"})
	if !ok {
		t.Fatal("expected Anthropic provider match")
	}
	enriched := MergeProvider(config.ProviderConfig{Type: "anthropic"}, provider, "claude-opus-4-7-fast")
	model := enriched.Models["claude-opus-4-7-fast"]
	if model.ID != "claude-opus-4-7" || model.Options["speed"] != "fast" {
		t.Fatalf("unexpected fast model metadata: %+v", model)
	}
	if enriched.Headers["anthropic-beta"] != "fast-mode-2026-02-01" {
		t.Fatalf("unexpected merged headers: %+v", enriched.Headers)
	}
}
