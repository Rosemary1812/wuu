package modelbudget

import (
	"testing"

	"github.com/blueberrycongee/wuu/internal/config"
)

func TestResolveMiniMaxM3WithoutProviderLimitStaysUnknown(t *testing.T) {
	budget := Resolve("MiniMax-M3", config.ProviderConfig{Type: "anthropic"}, 0)
	if budget.ContextWindowTokens != 0 {
		t.Fatalf("ContextWindowTokens = %d, want 0", budget.ContextWindowTokens)
	}
	if budget.ContextWindowSource != SourceUnknown || budget.ContextWindowKnown {
		t.Fatalf("unexpected context source: source=%q known=%v", budget.ContextWindowSource, budget.ContextWindowKnown)
	}
	if budget.OutputReserveTokens != 0 {
		t.Fatalf("OutputReserveTokens = %d, want 0 without provider metadata", budget.OutputReserveTokens)
	}
	if budget.CompactThresholdTokens != 0 {
		t.Fatalf("CompactThresholdTokens = %d, want 0", budget.CompactThresholdTokens)
	}
}

func TestResolveUnknownModelStaysUnknown(t *testing.T) {
	budget := Resolve("private-unknown-model", config.ProviderConfig{Type: "anthropic"}, 0)
	if budget.ContextWindowTokens != 0 || budget.ContextWindowKnown {
		t.Fatalf("unknown model should not get a synthetic context window: %+v", budget)
	}
	if budget.ContextWindowSource != SourceUnknown {
		t.Fatalf("ContextWindowSource = %q, want %q", budget.ContextWindowSource, SourceUnknown)
	}
	if budget.UsableInputTokens != 0 || budget.CompactThresholdTokens != 0 {
		t.Fatalf("unknown model should not synthesize compact thresholds: %+v", budget)
	}
}

func TestResolveProviderModelLimitAndOutputReserve(t *testing.T) {
	provider := config.ProviderConfig{
		Type:  "anthropic",
		Model: "alias",
		Models: map[string]config.ProviderModelConfig{
			"alias": {
				ID: "base-model",
			},
			"base-model": {
				Limit: &config.ProviderModelLimitConfig{
					Context: 1_000_000,
					Input:   900_000,
					Output:  128_000,
				},
			},
		},
	}
	budget := Resolve("alias", provider, 0)
	if budget.ContextWindowTokens != 1_000_000 || budget.ContextWindowSource != SourceProviderModelLimit {
		t.Fatalf("unexpected context resolution: %+v", budget)
	}
	if budget.InputLimitTokens != 900_000 {
		t.Fatalf("InputLimitTokens = %d, want 900000", budget.InputLimitTokens)
	}
	if budget.OutputLimitTokens != 128_000 || budget.OutputReserveTokens != 128_000 {
		t.Fatalf("unexpected output budget: %+v", budget)
	}
	if budget.UsableInputTokens != 880_000 {
		t.Fatalf("UsableInputTokens = %d, want input limit minus capped 20k reserve", budget.UsableInputTokens)
	}
}

func TestResolveProviderOverrideWins(t *testing.T) {
	budget := Resolve("MiniMax-M3", config.ProviderConfig{
		Type:          "anthropic",
		ContextWindow: 512_000,
	}, 1_000_000)
	if budget.ContextWindowTokens != 512_000 || budget.ContextWindowSource != SourceProviderContextWindow {
		t.Fatalf("provider override should win: %+v", budget)
	}
}

func TestResolveAliasDoesNotUseAPIModelRegistry(t *testing.T) {
	provider := config.ProviderConfig{
		Type: "anthropic",
		Models: map[string]config.ProviderModelConfig{
			"fast": {ID: "MiniMax-M3"},
		},
	}
	budget := Resolve("fast", provider, 0)
	if budget.APIModel != "MiniMax-M3" {
		t.Fatalf("APIModel = %q, want MiniMax-M3", budget.APIModel)
	}
	if budget.ContextWindowTokens != 0 || budget.ContextWindowSource != SourceUnknown {
		t.Fatalf("alias should not infer API model context registry: %+v", budget)
	}
	if budget.OutputReserveTokens != 0 {
		t.Fatalf("alias should not infer API model output reserve, got %+v", budget)
	}
}

func TestResolveAliasUsesAPIModelCodexCap(t *testing.T) {
	provider := config.ProviderConfig{
		Type: "openai-codex",
		Models: map[string]config.ProviderModelConfig{
			"fast": {
				ID: "gpt-5.5",
				Limit: &config.ProviderModelLimitConfig{
					Context: 400_000,
					Input:   390_000,
					Output:  32_000,
				},
			},
		},
	}
	budget := Resolve("fast", provider, 0)
	if budget.InputLimitTokens != CodexSubscriptionGPT5InputCap {
		t.Fatalf("InputLimitTokens = %d, want Codex subscription cap %d", budget.InputLimitTokens, CodexSubscriptionGPT5InputCap)
	}
}

func TestEffectiveContextWindowUsesCodexInputCapWhenModelWindowUnknown(t *testing.T) {
	budget := Resolve("gpt-5.5", config.ProviderConfig{
		Type:  "openai-codex",
		Model: "gpt-5.5",
	}, 0)
	if budget.ContextWindowTokens != 0 || budget.ContextWindowSource != SourceUnknown {
		t.Fatalf("model context window should stay unknown: %+v", budget)
	}
	got, source := budget.EffectiveContextWindow()
	if got != CodexSubscriptionGPT5InputCap || source != SourceProviderInputLimit {
		t.Fatalf("EffectiveContextWindow = %d, %q; want %d, %q", got, source, CodexSubscriptionGPT5InputCap, SourceProviderInputLimit)
	}
}

func TestEffectiveContextWindowPrefersLowerInputLimit(t *testing.T) {
	budget := Resolve("gpt-5.5", config.ProviderConfig{
		Type:  "openai-codex",
		Model: "gpt-5.5",
		Models: map[string]config.ProviderModelConfig{
			"gpt-5.5": {
				Limit: &config.ProviderModelLimitConfig{
					Context: 400_000,
					Input:   390_000,
					Output:  128_000,
				},
			},
		},
	}, 0)
	if budget.ContextWindowTokens != 400_000 || budget.InputLimitTokens != CodexSubscriptionGPT5InputCap {
		t.Fatalf("unexpected raw budget: %+v", budget)
	}
	got, source := budget.EffectiveContextWindow()
	if got != CodexSubscriptionGPT5InputCap || source != SourceProviderInputLimit {
		t.Fatalf("EffectiveContextWindow = %d, %q; want %d, %q", got, source, CodexSubscriptionGPT5InputCap, SourceProviderInputLimit)
	}
}
