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

func TestSummariesInferOpenRouterNonOpenAIReasoningEfforts(t *testing.T) {
	provider := config.ProviderConfig{
		Type:    "openai-compatible",
		BaseURL: "https://openrouter.ai/api/v1",
		Model:   "anthropic/claude-sonnet-4.6",
	}

	variants := Summaries(provider, provider.Model)
	if got := variantIDs(variants); strings.Join(got, ",") != "low,medium,high" {
		t.Fatalf("variants = %v", got)
	}
	options, ok := Options(provider, provider.Model, "medium")
	if !ok {
		t.Fatal("expected medium variant")
	}
	reasoning, ok := options["reasoning"].(map[string]any)
	if !ok || reasoning["effort"] != "medium" {
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

func TestSummariesMatchOpenCodeForBedrockMantle(t *testing.T) {
	reasoning := true
	provider := config.ProviderConfig{
		Type:  "openai-compatible",
		NPM:   "@ai-sdk/amazon-bedrock/mantle",
		Model: "openai.gpt-5.5",
		Models: map[string]config.ProviderModelConfig{
			"openai.gpt-5.5": {
				Reasoning: &reasoning,
			},
		},
	}

	selection := Resolve(provider, provider.Model, "high", "")
	if got := selection.ProviderOptions["store"]; got != false {
		t.Fatalf("store = %#v", got)
	}
	if got := selection.ProviderOptions["reasoningSummary"]; got != "auto" {
		t.Fatalf("reasoningSummary = %#v", got)
	}
	include, ok := selection.ProviderOptions["include"].([]any)
	if !ok || len(include) != 1 || include[0] != "reasoning.encrypted_content" {
		t.Fatalf("include = %#v", selection.ProviderOptions["include"])
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

func TestSummariesMatchOpenCodeForMiniMaxM3(t *testing.T) {
	reasoning := true
	provider := config.ProviderConfig{
		Type:  "anthropic",
		NPM:   "@ai-sdk/anthropic",
		Model: "minimax-m3",
		Models: map[string]config.ProviderModelConfig{
			"minimax-m3": {
				Reasoning: &reasoning,
			},
		},
	}

	variants := Summaries(provider, provider.Model)
	if got := variantIDs(variants); strings.Join(got, ",") != "none,thinking" {
		t.Fatalf("variants = %v", got)
	}
	options, ok := Options(provider, provider.Model, "thinking")
	if !ok {
		t.Fatal("expected thinking variant")
	}
	thinking, ok := options["thinking"].(map[string]any)
	if !ok || thinking["type"] != "adaptive" {
		t.Fatalf("thinking options = %#v", options)
	}

	selection := Resolve(provider, provider.Model, "", "")
	thinking, ok = selection.ProviderOptions["thinking"].(map[string]any)
	if !ok || thinking["type"] != "adaptive" {
		t.Fatalf("base thinking options = %#v", selection.ProviderOptions)
	}
}

func TestSummariesMatchOpenCodeForAnthropicOpus47Aliases(t *testing.T) {
	provider := config.ProviderConfig{
		Type:  "anthropic",
		Model: "claude-4.7-opus",
	}

	variants := Summaries(provider, provider.Model)
	if got := variantIDs(variants); strings.Join(got, ",") != "low,medium,high,xhigh,max" {
		t.Fatalf("variants = %v", got)
	}
	options, ok := Options(provider, provider.Model, "xhigh")
	if !ok {
		t.Fatal("expected xhigh variant")
	}
	thinking, ok := options["thinking"].(map[string]any)
	if !ok || thinking["type"] != "adaptive" || thinking["display"] != "summarized" {
		t.Fatalf("thinking options = %#v", options)
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

func TestSummariesWrapSAPModelParams(t *testing.T) {
	reasoning := true
	provider := config.ProviderConfig{
		Type:  "openai-compatible",
		NPM:   "@jerome-benoit/sap-ai-provider-v2",
		Model: "gpt-5.5",
		Models: map[string]config.ProviderModelConfig{
			"gpt-5.5": {
				Reasoning:   &reasoning,
				ReleaseDate: "2026-04-23",
			},
		},
	}

	options, ok := Options(provider, provider.Model, "xhigh")
	if !ok {
		t.Fatal("expected xhigh variant")
	}
	params, ok := options["modelParams"].(map[string]any)
	if !ok || params["reasoning_effort"] != "xhigh" {
		t.Fatalf("modelParams = %#v", options)
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

func TestResolveMergesOpenCodeBaseOptions(t *testing.T) {
	provider := config.ProviderConfig{
		Type:  "openai",
		Model: "gpt-5.5",
	}

	selection := Resolve(provider, provider.Model, "high", "")
	if selection.Variant != "high" {
		t.Fatalf("selection = %+v", selection)
	}
	if got := selection.ProviderOptions["reasoningEffort"]; got != "high" {
		t.Fatalf("reasoningEffort = %#v", got)
	}
	if got := selection.ProviderOptions["reasoningSummary"]; got != "auto" {
		t.Fatalf("reasoningSummary = %#v", got)
	}
	if got := selection.ProviderOptions["textVerbosity"]; got != "low" {
		t.Fatalf("textVerbosity = %#v", got)
	}
	if got := selection.ProviderOptions["store"]; got != false {
		t.Fatalf("store = %#v", got)
	}
}

func TestResolveKeepsOpenCodeDefaultOptionsWithoutVariant(t *testing.T) {
	provider := config.ProviderConfig{
		Type:  "openai-compatible",
		Model: "gpt-5.5",
	}

	selection := Resolve(provider, provider.Model, "", "")
	if selection.Variant != "" || selection.DisplayEffort != "" {
		t.Fatalf("selection = %+v", selection)
	}
	if got := selection.ProviderOptions["reasoningEffort"]; got != "medium" {
		t.Fatalf("reasoningEffort = %#v", got)
	}
	if _, ok := selection.ProviderOptions["reasoningSummary"]; ok {
		t.Fatalf("generic OpenAI-compatible should not set reasoningSummary: %#v", selection.ProviderOptions)
	}
}

func TestResolveMergesOpenRouterUsageOptions(t *testing.T) {
	provider := config.ProviderConfig{
		Type:    "openai-compatible",
		BaseURL: "https://openrouter.ai/api/v1",
		Model:   "openai/gpt-5.5",
	}

	selection := ResolveForProvider("openrouter", provider, provider.Model, "high", "")
	usage, ok := selection.ProviderOptions["usage"].(map[string]any)
	if !ok || usage["include"] != true {
		t.Fatalf("usage = %#v", selection.ProviderOptions)
	}
	reasoning, ok := selection.ProviderOptions["reasoning"].(map[string]any)
	if !ok || reasoning["effort"] != "high" {
		t.Fatalf("reasoning = %#v", selection.ProviderOptions)
	}
}

func variantIDs(variants []Variant) []string {
	out := make([]string, 0, len(variants))
	for _, variant := range variants {
		out = append(out, variant.ID)
	}
	return out
}
