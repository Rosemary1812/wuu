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

func TestMatchProviderDoesNotUseWireTypeForCustomEndpoint(t *testing.T) {
	if provider, ok := MatchProvider("zhipu2", config.ProviderConfig{
		Type:    "anthropic",
		BaseURL: "https://open.bigmodel.cn/api/anthropic",
		Model:   "glm-5.1",
	}); ok {
		t.Fatalf("custom Anthropic-compatible endpoint matched %q", provider.ID)
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
