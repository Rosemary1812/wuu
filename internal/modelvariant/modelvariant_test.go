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
