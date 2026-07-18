package modelcatalog

const kimiForCodingProviderID = "kimi-for-coding"

func applyBuiltinCatalogOverrides(data *catalogData) {
	if data == nil {
		return
	}
	for i := range data.Providers {
		provider := &data.Providers[i]
		switch provider.ID {
		case kimiForCodingProviderID:
			provider.API = "https://api.kimi.com/coding/"
			provider.ModelOptions = mergeModelOptions(provider.ModelOptions, map[string]any{
				"force_adaptive_thinking": true,
				"anthropic_default_betas": false,
			})
			provider.Headers = map[string]string{"User-Agent": "KimiCLI/1.5"}
			found := false
			for j := range provider.Models {
				if provider.Models[j].ID == "k3" {
					provider.Models[j] = kimiK3Model(provider.Models[j])
					found = true
					break
				}
			}
			if !found {
				provider.Models = append(provider.Models, kimiK3Model(Model{}))
			}
		case "minimax", "minimax-cn", "minimax-coding-plan", "minimax-cn-coding-plan":
			provider.ModelOptions = mergeModelOptions(provider.ModelOptions, map[string]any{
				"anthropic_default_betas": false,
			})
		case "xai":
			for j := range provider.Models {
				if len(provider.Models[j].SupportedEfforts) != 0 {
					continue
				}
				switch provider.Models[j].ID {
				case "grok-4.3":
					provider.Models[j].SupportedEfforts = []string{"none", "low", "medium", "high"}
				case "grok-4.20-multi-agent-0309":
					provider.Models[j].SupportedEfforts = []string{"low", "medium", "high"}
				}
			}
		}
	}
}

func kimiK3Model(model Model) Model {
	attachment := false
	toolCall := true
	structuredOutput := true
	temperature := true
	model.ID = "k3"
	model.Name = "Kimi K3"
	model.Family = "kimi-k3"
	model.ReleaseDate = "2026-07-16"
	model.Reasoning = true
	model.ReasoningOptions = []map[string]any{
		{"type": "effort", "values": []any{"max"}},
	}
	model.Attachment = &attachment
	model.ToolCall = &toolCall
	model.StructuredOutput = &structuredOutput
	model.Temperature = &temperature
	model.Modalities = &Modalities{
		Input:  []string{"text", "image", "video"},
		Output: []string{"text"},
	}
	model.Limit = &Limit{Context: 1_048_576, Output: 131_072}
	model.Options = map[string]any{
		"allow_empty_signature": true,
		"thinking_replay":       "full",
	}
	model.SupportedEfforts = []string{"max"}
	model.DefaultVariant = "max"
	model.Variants = map[string]map[string]any{
		"max": {
			"thinking": map[string]any{
				"type":    "adaptive",
				"display": "summarized",
			},
			"effort": "max",
		},
	}
	return model
}
