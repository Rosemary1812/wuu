package modelvariant

import (
	"strings"

	"github.com/blueberrycongee/wuu/internal/config"
)

type openCodeModelDescriptor struct {
	ProviderID  string
	ModelID     string
	APIID       string
	APINPM      string
	ReleaseDate string
	Reasoning   bool
	OutputLimit int
	BaseURL     string
}

func openCodeDescriptor(providerName string, provider config.ProviderConfig, model string) openCodeModelDescriptor {
	modelID := strings.TrimSpace(model)
	modelCfg := provider.Models[modelID]
	apiID := strings.TrimSpace(modelCfg.ID)
	if apiID == "" {
		apiID = modelID
	}
	apiNPM := ""
	if modelCfg.Provider != nil {
		apiNPM = strings.TrimSpace(modelCfg.Provider.NPM)
	}
	if apiNPM == "" {
		apiNPM = strings.TrimSpace(provider.NPM)
	}
	if apiNPM == "" {
		apiNPM = openCodeInferNPM(providerName, provider)
	}
	outputLimit := 0
	if modelCfg.Limit != nil {
		outputLimit = modelCfg.Limit.Output
	}
	desc := openCodeModelDescriptor{
		ProviderID:  openCodeProviderID(providerName, provider),
		ModelID:     strings.ToLower(modelID),
		APIID:       strings.ToLower(apiID),
		APINPM:      apiNPM,
		ReleaseDate: strings.TrimSpace(modelCfg.ReleaseDate),
		OutputLimit: outputLimit,
		BaseURL:     strings.ToLower(strings.TrimSpace(provider.BaseURL)),
	}
	desc.Reasoning = openCodeReasoningEnabled(desc, modelCfg.Reasoning)
	return desc
}

func openCodeProviderID(providerName string, provider config.ProviderConfig) string {
	if value := strings.ToLower(strings.TrimSpace(providerName)); value != "" {
		return value
	}
	providerType := normalizedProviderType(provider.Type)
	baseURL := strings.ToLower(strings.TrimSpace(provider.BaseURL))
	switch {
	case strings.Contains(baseURL, "openrouter.ai"):
		return "openrouter"
	case isCodexProviderType(provider.Type):
		return "opencode"
	case providerType == "openai":
		return "openai"
	case providerType == "anthropic" || providerType == "claude" || providerType == "anthropic-official":
		return "anthropic"
	default:
		return providerType
	}
}

func openCodeInferNPM(providerName string, provider config.ProviderConfig) string {
	providerType := normalizedProviderType(provider.Type)
	baseURL := strings.ToLower(strings.TrimSpace(provider.BaseURL))
	if strings.Contains(baseURL, "openrouter.ai") || strings.ToLower(strings.TrimSpace(providerName)) == "openrouter" {
		return openCodeNPMOpenRouter
	}
	if isCodexProviderType(provider.Type) || providerType == "openai" {
		return openCodeNPMOpenAI
	}
	if providerType == "anthropic" || providerType == "claude" || providerType == "anthropic-official" {
		return openCodeNPMAnthropic
	}
	return openCodeNPMOpenAICompatible
}

func normalizedProviderType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.ReplaceAll(value, "_", "-")
}

func openCodeReasoningEnabled(desc openCodeModelDescriptor, configured *bool) bool {
	if configured != nil {
		return *configured
	}
	if openCodeExcludedReasoningModel(desc.ModelID) {
		return false
	}
	if strings.Contains(desc.BaseURL, "xiaomimimo.com") {
		return true
	}
	id := desc.ModelID
	apiID := desc.APIID
	if strings.Contains(id, "claude") || strings.Contains(apiID, "claude") || strings.Contains(apiID, "anthropic") {
		return true
	}
	if strings.Contains(id, "gemini") || strings.Contains(apiID, "gemini") {
		return true
	}
	if strings.Contains(id, "grok-3-mini") || strings.Contains(apiID, "grok-3-mini") {
		return true
	}
	if strings.Contains(apiID, "deepseek-v4") || strings.Contains(id, "deepseek-v4") {
		return true
	}
	if openCodeGPT5FamilyRE.MatchString(apiID) || openCodeGPT5FamilyRE.MatchString(id) ||
		strings.Contains(apiID, "o1") || strings.Contains(apiID, "o3") || strings.Contains(apiID, "o4") ||
		strings.Contains(id, "o1") || strings.Contains(id, "o3") || strings.Contains(id, "o4") {
		return true
	}
	if desc.APINPM == openCodeNPMAmazonBedrock && (strings.Contains(apiID, "nova") || strings.Contains(id, "nova")) {
		return true
	}
	if desc.APINPM == openCodeNPMMistral && strings.Contains(apiID, "mistral") {
		return true
	}
	return false
}
