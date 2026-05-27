package modelvariant

import (
	"sort"
	"strings"

	"github.com/blueberrycongee/wuu/internal/config"
)

type Variant struct {
	ID      string
	Options map[string]any
}

type Selection struct {
	Variant         string
	DisplayEffort   string
	LegacyEffort    string
	ProviderOptions map[string]any
}

func Resolve(provider config.ProviderConfig, model, variant, legacyEffort string) Selection {
	variant = strings.TrimSpace(variant)
	legacyEffort = strings.TrimSpace(legacyEffort)

	if variant == "" && legacyEffort != "" {
		if _, ok := Options(provider, model, legacyEffort); ok {
			variant = legacyEffort
		}
	}
	if variant == "" {
		if candidate := DefaultVariant(provider, model); candidate != "" {
			if _, ok := Options(provider, model, candidate); ok {
				variant = candidate
			}
		}
	}
	if variant != "" {
		if options, ok := Options(provider, model, variant); ok {
			return Selection{
				Variant:         variant,
				DisplayEffort:   variant,
				ProviderOptions: options,
			}
		}
	}
	return Selection{
		DisplayEffort: legacyEffort,
		LegacyEffort:  legacyEffort,
	}
}

func DefaultVariant(provider config.ProviderConfig, model string) string {
	modelCfg, ok := provider.Models[strings.TrimSpace(model)]
	if !ok {
		return ""
	}
	if value := strings.TrimSpace(modelCfg.DefaultVariant); value != "" {
		return value
	}
	return strings.TrimSpace(modelCfg.DefaultEffort)
}

func Options(provider config.ProviderConfig, model, variant string) (map[string]any, bool) {
	variant = strings.TrimSpace(variant)
	if variant == "" {
		return nil, false
	}
	for _, summary := range Summaries(provider, model) {
		if summary.ID == variant {
			return CloneOptions(summary.Options), true
		}
	}
	return nil, false
}

func Summaries(provider config.ProviderConfig, model string) []Variant {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil
	}
	if modelCfg, ok := provider.Models[model]; ok && len(modelCfg.Variants) > 0 {
		return variantsFromOptionMap(modelCfg.Variants)
	}
	return variantsFromOptionMap(inferredOptions(provider, model))
}

func SupportedEfforts(provider config.ProviderConfig, model string, modelCfg config.ProviderModelConfig) []string {
	if values := normalizedEffortList(modelCfg.SupportedEfforts); len(values) > 0 {
		return values
	}
	if values := EffortIDs(Summaries(provider, model)); len(values) > 0 {
		return values
	}
	return inferredEfforts(provider, model)
}

func EffortIDs(variants []Variant) []string {
	if len(variants) == 0 {
		return nil
	}
	out := make([]string, 0, len(variants))
	for _, variant := range variants {
		id := strings.TrimSpace(variant.ID)
		if id == "" || id == "default" {
			continue
		}
		out = append(out, id)
	}
	return out
}

func CloneOptions(options map[string]any) map[string]any {
	if len(options) == 0 {
		return nil
	}
	out := make(map[string]any, len(options))
	for key, value := range options {
		if key == "disabled" {
			continue
		}
		out[key] = cloneValue(value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return CloneOptions(typed)
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = cloneValue(item)
		}
		return out
	default:
		return value
	}
}

func variantsFromOptionMap(variants map[string]map[string]any) []Variant {
	if len(variants) == 0 {
		return nil
	}
	out := make([]Variant, 0, len(variants))
	for id, options := range variants {
		id = strings.TrimSpace(id)
		if id == "" || variantDisabled(options) {
			continue
		}
		out = append(out, Variant{
			ID:      id,
			Options: CloneOptions(options),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		leftRank := variantRank(out[i].ID)
		rightRank := variantRank(out[j].ID)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		return strings.ToLower(out[i].ID) < strings.ToLower(out[j].ID)
	})
	return out
}

func variantDisabled(options map[string]any) bool {
	if len(options) == 0 {
		return false
	}
	disabled, ok := options["disabled"].(bool)
	return ok && disabled
}

func variantRank(id string) int {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "none":
		return 0
	case "minimal":
		return 1
	case "low":
		return 2
	case "medium":
		return 3
	case "high":
		return 4
	case "xhigh":
		return 5
	case "max":
		return 6
	default:
		return 100
	}
}

func normalizedEffortList(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		effort := strings.TrimSpace(value)
		if effort == "" || seen[effort] {
			continue
		}
		seen[effort] = true
		out = append(out, effort)
	}
	return out
}

func inferredOptions(provider config.ProviderConfig, model string) map[string]map[string]any {
	modelID := strings.ToLower(strings.TrimSpace(model))
	if modelID == "" {
		return nil
	}
	if excludedOpenCodeReasoningVariantModel(modelID) {
		return nil
	}
	providerType := strings.ToLower(strings.TrimSpace(provider.Type))
	providerType = strings.ReplaceAll(providerType, "_", "-")
	baseURL := strings.ToLower(strings.TrimSpace(provider.BaseURL))

	if isCodexProviderType(provider.Type) {
		return variantsWithReasoningEffort(openAICodexEfforts(modelID))
	}
	if providerType == "anthropic" || providerType == "claude" || providerType == "anthropic-official" {
		return anthropicVariantOptions(modelID)
	}
	if strings.Contains(baseURL, "openrouter.ai") {
		if strings.Contains(modelID, "gpt") {
			return variantsWithNestedReasoningEffort(openAICompatibleEfforts(modelID))
		}
		if strings.Contains(modelID, "gemini-3") || strings.Contains(modelID, "claude") {
			return variantsWithNestedReasoningEffort(openAIEfforts())
		}
		return nil
	}
	if providerType == "openai" || providerType == "openai-compatible" || providerType == "codex" {
		if strings.Contains(modelID, "mimo-v2.5-pro") || strings.Contains(baseURL, "xiaomimimo.com") {
			return variantsWithReasoningEffort(widelySupportedEfforts())
		}
		if strings.Contains(modelID, "deepseek-v4") {
			return variantsWithReasoningEffort(append(widelySupportedEfforts(), "max"))
		}
		if strings.Contains(modelID, "grok-3-mini") {
			return variantsWithReasoningEffort([]string{"low", "high"})
		}
		if strings.Contains(modelID, "gpt") || openAIOSeriesModel(modelID) {
			return variantsWithReasoningEffort(openAICompatibleEfforts(modelID))
		}
	}
	return nil
}

func excludedOpenCodeReasoningVariantModel(modelID string) bool {
	excluded := []string{
		"deepseek-chat",
		"deepseek-reasoner",
		"deepseek-r1",
		"deepseek-v3",
		"minimax",
		"glm",
		"kimi",
		"k2p",
		"qwen",
		"big-pickle",
	}
	for _, item := range excluded {
		if strings.Contains(modelID, item) {
			return true
		}
	}
	return strings.Contains(modelID, "grok") && !strings.Contains(modelID, "grok-3-mini")
}

func widelySupportedEfforts() []string {
	return []string{"low", "medium", "high"}
}

func openAIEfforts() []string {
	return []string{"none", "minimal", "low", "medium", "high", "xhigh"}
}

func openAICompatibleEfforts(modelID string) []string {
	if strings.Contains(modelID, "gpt-5-pro") || strings.Contains(modelID, "gpt-5pro") {
		return []string{"high"}
	}
	if strings.Contains(modelID, "gpt-5.2-pro") || strings.Contains(modelID, "gpt-5.3-pro") ||
		strings.Contains(modelID, "gpt-5.4-pro") || strings.Contains(modelID, "gpt-5.5-pro") {
		return []string{"medium", "high", "xhigh"}
	}
	if strings.Contains(modelID, "gpt-5.1-chat") || strings.Contains(modelID, "gpt-5.2-chat") ||
		strings.Contains(modelID, "gpt-5.3-chat") || strings.Contains(modelID, "gpt-5-chat") {
		return []string{"medium"}
	}
	if strings.Contains(modelID, "gpt-5.1") {
		return []string{"none", "low", "medium", "high"}
	}
	if strings.Contains(modelID, "gpt-5.2") || strings.Contains(modelID, "gpt-5.3") ||
		strings.Contains(modelID, "gpt-5.4") || strings.Contains(modelID, "gpt-5.5") {
		return []string{"none", "low", "medium", "high", "xhigh"}
	}
	return openAIEfforts()
}

func openAICodexEfforts(modelID string) []string {
	if strings.Contains(modelID, "gpt-5.3-codex") || strings.Contains(modelID, "gpt-5.4-codex") ||
		strings.Contains(modelID, "gpt-5.5-codex") {
		return []string{"none", "low", "medium", "high", "xhigh"}
	}
	if strings.Contains(modelID, "gpt-5.2-codex") || strings.Contains(modelID, "codex-max") {
		return []string{"low", "medium", "high", "xhigh"}
	}
	if strings.Contains(modelID, "gpt-5") || strings.Contains(modelID, "codex") {
		return widelySupportedEfforts()
	}
	return inferredEfforts(config.ProviderConfig{Type: "openai-codex"}, modelID)
}

func openAIOSeriesModel(modelID string) bool {
	return strings.Contains(modelID, "o1") || strings.Contains(modelID, "o3") || strings.Contains(modelID, "o4")
}

func variantsWithReasoningEffort(efforts []string) map[string]map[string]any {
	return variantsFromEfforts(efforts, func(effort string) map[string]any {
		return map[string]any{"reasoningEffort": effort}
	})
}

func variantsWithNestedReasoningEffort(efforts []string) map[string]map[string]any {
	return variantsFromEfforts(efforts, func(effort string) map[string]any {
		return map[string]any{"reasoning": map[string]any{"effort": effort}}
	})
}

func variantsFromEfforts(efforts []string, build func(string) map[string]any) map[string]map[string]any {
	if len(efforts) == 0 {
		return nil
	}
	out := make(map[string]map[string]any, len(efforts))
	for _, effort := range efforts {
		effort = strings.TrimSpace(effort)
		if effort == "" {
			continue
		}
		out[effort] = build(effort)
	}
	return out
}

func anthropicVariantOptions(modelID string) map[string]map[string]any {
	if strings.Contains(modelID, "opus-4-7") || strings.Contains(modelID, "opus-4.7") {
		return variantsFromEfforts([]string{"low", "medium", "high", "xhigh", "max"}, func(effort string) map[string]any {
			return map[string]any{
				"thinking": map[string]any{"type": "adaptive", "display": "summarized"},
				"effort":   effort,
			}
		})
	}
	if strings.Contains(modelID, "opus-4-6") || strings.Contains(modelID, "opus-4.6") ||
		strings.Contains(modelID, "sonnet-4-6") || strings.Contains(modelID, "sonnet-4.6") {
		return variantsFromEfforts([]string{"low", "medium", "high", "max"}, func(effort string) map[string]any {
			return map[string]any{
				"thinking": map[string]any{"type": "adaptive"},
				"effort":   effort,
			}
		})
	}
	if strings.Contains(modelID, "opus-4-5") || strings.Contains(modelID, "opus-4.5") {
		return variantsFromEfforts(widelySupportedEfforts(), func(effort string) map[string]any {
			return map[string]any{"effort": effort}
		})
	}
	return map[string]map[string]any{
		"high": {"thinking": map[string]any{"type": "enabled", "budgetTokens": 16000}},
		"max":  {"thinking": map[string]any{"type": "enabled", "budgetTokens": 31999}},
	}
}

func inferredEfforts(provider config.ProviderConfig, model string) []string {
	modelID := strings.ToLower(strings.TrimSpace(model))
	if modelID == "" {
		return nil
	}
	providerType := strings.ToLower(strings.TrimSpace(provider.Type))
	providerType = strings.ReplaceAll(providerType, "_", "-")
	baseURL := strings.ToLower(strings.TrimSpace(provider.BaseURL))
	if isCodexProviderType(provider.Type) {
		return []string{"low", "medium", "high", "xhigh"}
	}
	if providerType == "anthropic" || providerType == "claude" || providerType == "anthropic-official" {
		if strings.Contains(modelID, "claude") && (strings.Contains(modelID, "sonnet-4") || strings.Contains(modelID, "opus-4")) {
			return []string{"low", "medium", "high", "max"}
		}
		return nil
	}
	if strings.Contains(baseURL, "openrouter.ai") {
		if strings.Contains(modelID, "gpt") || strings.Contains(modelID, "claude") || strings.Contains(modelID, "gemini-3") {
			return []string{"low", "medium", "high"}
		}
		return nil
	}
	if providerType == "openai" || providerType == "openai-compatible" || providerType == "codex" {
		if strings.Contains(modelID, "gpt-5") || strings.Contains(modelID, "o1") || strings.Contains(modelID, "o3") || strings.Contains(modelID, "o4") {
			return []string{"low", "medium", "high"}
		}
	}
	return nil
}

func isCodexProviderType(providerType string) bool {
	providerType = strings.ToLower(strings.TrimSpace(providerType))
	providerType = strings.ReplaceAll(providerType, "_", "-")
	return providerType == "openai-codex" ||
		providerType == "codex-subscription" ||
		providerType == "chatgpt-codex" ||
		providerType == "codex-cli" ||
		providerType == "codex"
}
