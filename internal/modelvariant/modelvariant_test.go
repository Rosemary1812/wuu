package modelvariant

import (
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/config"
)

func TestSummariesInferXiaomiReasoningEfforts(t *testing.T) {
	provider := config.ProviderConfig{
		Type:    "openai-compatible",
		BaseURL: "https://token-plan-cn.xiaomimimo.com/v1",
		Model:   "mimo-v2.5-pro",
	}

	variants := Summaries(provider, provider.Model)
	if got := variantIDs(variants); strings.Join(got, ",") != "low,medium,high" {
		t.Fatalf("variants = %v", got)
	}
	if got := variants[0].Options["reasoningEffort"]; got != "low" {
		t.Fatalf("low options = %#v", variants[0].Options)
	}
}

func TestSummariesInferOpenRouterNestedReasoning(t *testing.T) {
	provider := config.ProviderConfig{
		Type:    "openai-compatible",
		BaseURL: "https://openrouter.ai/api/v1",
		Model:   "openai/gpt-5.5",
	}

	options, ok := Options(provider, provider.Model, "high")
	if !ok {
		t.Fatal("expected high variant")
	}
	reasoning, ok := options["reasoning"].(map[string]any)
	if !ok || reasoning["effort"] != "high" {
		t.Fatalf("expected nested reasoning effort, got %#v", options)
	}
}

func TestSummariesMatchOpenCodeForGenericOpenAICompatible(t *testing.T) {
	provider := config.ProviderConfig{
		Type:  "openai-compatible",
		Model: "gpt-5.5",
	}

	variants := Summaries(provider, provider.Model)
	if got := variantIDs(variants); strings.Join(got, ",") != "low,medium,high" {
		t.Fatalf("variants = %v", got)
	}
}

func TestSummariesMatchOpenCodeForOpenAIProvider(t *testing.T) {
	provider := config.ProviderConfig{
		Type:  "openai",
		Model: "gpt-5.5",
	}

	options, ok := Options(provider, provider.Model, "xhigh")
	if !ok {
		t.Fatal("expected xhigh variant")
	}
	if got := options["reasoningEffort"]; got != "xhigh" {
		t.Fatalf("reasoningEffort = %#v", got)
	}
	if got := options["reasoningSummary"]; got != "auto" {
		t.Fatalf("reasoningSummary = %#v", got)
	}
	include, ok := options["include"].([]any)
	if !ok || len(include) != 1 || include[0] != "reasoning.encrypted_content" {
		t.Fatalf("include = %#v", options["include"])
	}
}

func TestSummariesMatchOpenCodeForExcludedReasoningModels(t *testing.T) {
	for _, tc := range []struct {
		name     string
		provider config.ProviderConfig
	}{
		{
			name: "glm anthropic",
			provider: config.ProviderConfig{
				Type:  "anthropic",
				Model: "glm-5.1",
			},
		},
		{
			name: "unversioned gpt chat",
			provider: config.ProviderConfig{
				Type:    "openai-compatible",
				BaseURL: "https://openrouter.ai/api/v1",
				Model:   "openai/gpt-5-chat",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if variants := Summaries(tc.provider, tc.provider.Model); len(variants) != 0 {
				t.Fatalf("variants = %+v", variants)
			}
		})
	}
}

func TestSummariesUseOpenCodeModelMetadata(t *testing.T) {
	reasoning := true
	provider := config.ProviderConfig{
		Type:  "openai-compatible",
		NPM:   "@ai-sdk/google",
		Model: "gemini-3-flash",
		Models: map[string]config.ProviderModelConfig{
			"gemini-3-flash": {
				Reasoning: &reasoning,
				Provider:  &config.ProviderModelProviderConfig{NPM: "@ai-sdk/google"},
			},
		},
	}

	variants := Summaries(provider, provider.Model)
	if got := variantIDs(variants); strings.Join(got, ",") != "minimal,low,medium,high" {
		t.Fatalf("variants = %v", got)
	}
	options, ok := Options(provider, provider.Model, "minimal")
	if !ok {
		t.Fatal("expected minimal variant")
	}
	thinking, ok := options["thinkingConfig"].(map[string]any)
	if !ok || thinking["thinkingLevel"] != "minimal" || thinking["includeThoughts"] != true {
		t.Fatalf("thinkingConfig = %#v", options)
	}
}

func TestResolveUsesVariantOptionsInsteadOfLegacyEffort(t *testing.T) {
	provider := config.ProviderConfig{
		Type:    "openai-compatible",
		BaseURL: "https://token-plan-cn.xiaomimimo.com/v1",
		Model:   "mimo-v2.5-pro",
	}

	selection := Resolve(provider, provider.Model, "high", "low")
	if selection.Variant != "high" || selection.LegacyEffort != "" || selection.DisplayEffort != "high" {
		t.Fatalf("unexpected selection: %+v", selection)
	}
	if got := selection.ProviderOptions["reasoningEffort"]; got != "high" {
		t.Fatalf("provider options = %#v", selection.ProviderOptions)
	}
}

func variantIDs(variants []Variant) []string {
	out := make([]string, 0, len(variants))
	for _, variant := range variants {
		out = append(out, variant.ID)
	}
	return out
}
