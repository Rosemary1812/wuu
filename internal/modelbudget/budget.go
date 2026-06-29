package modelbudget

import (
	"strings"

	"github.com/blueberrycongee/wuu/internal/config"
	"github.com/blueberrycongee/wuu/internal/modelcatalog"
)

type Source string

const (
	SourceUnknown               Source = "unknown"
	SourceProviderContextWindow Source = "provider_context_window"
	SourceProviderModelLimit    Source = "provider_model_limit"
	SourceProviderInputLimit    Source = "provider_input_limit"
	SourceAgentOverride         Source = "agent_max_context_tokens"
)

const CodexSubscriptionGPT5InputCap = 272_000

type Budget struct {
	Model                  string
	APIModel               string
	ContextWindowTokens    int
	InputLimitTokens       int
	OutputLimitTokens      int
	OutputReserveTokens    int
	UsableInputTokens      int
	CompactThresholdTokens int
	ContextWindowSource    Source
	ContextWindowKnown     bool
}

func Resolve(model string, provider config.ProviderConfig, agentOverride int) Budget {
	model = strings.TrimSpace(model)
	apiModel := modelcatalog.APIModel(provider, model)
	if apiModel == "" {
		apiModel = model
	}

	context, source := resolveContextWindow(model, apiModel, provider, agentOverride)
	input := resolveInputWindow(model, apiModel, provider)
	output := configuredModelOutputLimit(model, provider)
	outputReserve := output
	usable := 0
	if input > 0 {
		reserve := outputReserve
		if reserve > 20_000 {
			reserve = 20_000
		}
		usable = max(0, input-reserve)
	} else if context > 0 {
		usable = max(0, context-outputReserve)
	}

	return Budget{
		Model:                  model,
		APIModel:               apiModel,
		ContextWindowTokens:    context,
		InputLimitTokens:       input,
		OutputLimitTokens:      output,
		OutputReserveTokens:    outputReserve,
		UsableInputTokens:      usable,
		CompactThresholdTokens: usable,
		ContextWindowSource:    source,
		ContextWindowKnown:     source != SourceUnknown && context > 0,
	}
}

// EffectiveContextWindow resolves the user-visible context ceiling for the
// current provider channel. Some channels publish a prompt/input cap that is
// lower than, or available without, the model's full context window. The UI
// should show the smaller effective cap because that is the limit users can
// actually fill with retained context.
func (b Budget) EffectiveContextWindow() (int, Source) {
	if b.InputLimitTokens > 0 && (b.ContextWindowTokens <= 0 || b.InputLimitTokens < b.ContextWindowTokens) {
		return b.InputLimitTokens, SourceProviderInputLimit
	}
	if b.ContextWindowTokens > 0 {
		return b.ContextWindowTokens, b.ContextWindowSource
	}
	return 0, SourceUnknown
}

func resolveContextWindow(model, apiModel string, provider config.ProviderConfig, agentOverride int) (int, Source) {
	if provider.ContextWindow > 0 {
		return provider.ContextWindow, SourceProviderContextWindow
	}
	if limit := configuredModelContextLimit(model, provider); limit > 0 {
		return limit, SourceProviderModelLimit
	}
	if agentOverride > 0 {
		return agentOverride, SourceAgentOverride
	}
	return 0, SourceUnknown
}

func resolveInputWindow(model, apiModel string, provider config.ProviderConfig) int {
	if limit := configuredModelInputLimit(model, provider); limit > 0 {
		if cap := codexSubscriptionInputCapForModels(model, apiModel, provider.Type); cap > 0 && cap < limit {
			return cap
		}
		return limit
	}
	if cap := codexSubscriptionInputCapForModels(model, apiModel, provider.Type); cap > 0 {
		return cap
	}
	return 0
}

func configuredModelContextLimit(model string, provider config.ProviderConfig) int {
	for _, cfg := range configuredModelCandidates(model, provider) {
		if cfg.ContextWindow > 0 {
			return cfg.ContextWindow
		}
		if cfg.Limit != nil && cfg.Limit.Context > 0 {
			return cfg.Limit.Context
		}
	}
	return 0
}

func configuredModelInputLimit(model string, provider config.ProviderConfig) int {
	for _, cfg := range configuredModelCandidates(model, provider) {
		if cfg.Limit != nil && cfg.Limit.Input > 0 {
			return cfg.Limit.Input
		}
	}
	return 0
}

func configuredModelOutputLimit(model string, provider config.ProviderConfig) int {
	for _, cfg := range configuredModelCandidates(model, provider) {
		if cfg.Limit != nil && cfg.Limit.Output > 0 {
			return cfg.Limit.Output
		}
	}
	return 0
}

func configuredModelCandidates(model string, provider config.ProviderConfig) []config.ProviderModelConfig {
	model = strings.TrimSpace(model)
	if model == "" || len(provider.Models) == 0 {
		return nil
	}
	out := make([]config.ProviderModelConfig, 0, 2)
	if cfg, ok := provider.Models[model]; ok {
		out = append(out, cfg)
	}
	apiModel := modelcatalog.APIModel(provider, model)
	if apiModel != "" && apiModel != model {
		if cfg, ok := provider.Models[apiModel]; ok {
			out = append(out, cfg)
		}
	}
	return out
}

func CodexSubscriptionInputCap(model, providerType string) int {
	if !isCodexSubscriptionProviderType(providerType) {
		return 0
	}
	id := strings.ToLower(strings.TrimSpace(model))
	if idx := strings.LastIndex(id, "/"); idx >= 0 {
		id = id[idx+1:]
	}
	if strings.Contains(id, "gpt-5") {
		return CodexSubscriptionGPT5InputCap
	}
	return 0
}

func codexSubscriptionInputCapForModels(model, apiModel, providerType string) int {
	if cap := CodexSubscriptionInputCap(model, providerType); cap > 0 {
		return cap
	}
	if apiModel != "" && apiModel != model {
		return CodexSubscriptionInputCap(apiModel, providerType)
	}
	return 0
}

func isCodexSubscriptionProviderType(providerType string) bool {
	switch strings.ToLower(strings.TrimSpace(providerType)) {
	case "openai-codex", "codex-subscription", "chatgpt-codex":
		return true
	default:
		return false
	}
}
