package modelcatalog

import "github.com/blueberrycongee/wuu/internal/config"

const kimiForCodingProviderID = "kimi-for-coding"

// applyProviderCompatibilityDefaults adds request-transport defaults that
// models.dev does not describe. These are deliberately kept separate from
// catalog facts such as modalities, limits, and reasoning efforts. Explicit
// user headers and model options always win over these defaults.
func applyProviderCompatibilityDefaults(providerID string, provider config.ProviderConfig) config.ProviderConfig {
	switch providerID {
	case kimiForCodingProviderID:
		provider.Headers = mergeHeaders(
			map[string]string{"User-Agent": "KimiCLI/1.5"},
			provider.Headers,
		)
		provider = applyModelCompatibilityDefaults(provider, map[string]any{
			"force_adaptive_thinking": true,
			"anthropic_default_betas": false,
		})
		if model, ok := provider.Models["k3"]; ok {
			model.Options = mergeModelOptions(map[string]any{
				"allow_empty_signature": true,
				"thinking_replay":       "full",
			}, model.Options)
			provider.Models["k3"] = model
		}
	case "minimax", "minimax-cn", "minimax-coding-plan", "minimax-cn-coding-plan":
		provider = applyModelCompatibilityDefaults(provider, map[string]any{
			"anthropic_default_betas": false,
		})
	}
	return provider
}

func applyModelCompatibilityDefaults(provider config.ProviderConfig, defaults map[string]any) config.ProviderConfig {
	for id, model := range provider.Models {
		model.Options = mergeModelOptions(defaults, model.Options)
		provider.Models[id] = model
	}
	return provider
}
