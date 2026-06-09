package modelvariant

import (
	"regexp"
	"strings"

	"github.com/blueberrycongee/wuu/internal/config"
)

const (
	openCodeNPMOpenAI           = "@ai-sdk/openai"
	openCodeNPMOpenAICompatible = "@ai-sdk/openai-compatible"
	openCodeNPMOpenRouter       = "@openrouter/ai-sdk-provider"
	openCodeNPMLLMGateway       = "@llmgateway/ai-sdk-provider"
	openCodeNPMAnthropic        = "@ai-sdk/anthropic"
	openCodeNPMVertexAnthropic  = "@ai-sdk/google-vertex/anthropic"
	openCodeNPMGithubCopilot    = "@ai-sdk/github-copilot"
	openCodeNPMAzure            = "@ai-sdk/azure"
	openCodeNPMGateway          = "@ai-sdk/gateway"
	openCodeNPMAIGateway        = "ai-gateway-provider"
	openCodeNPMGoogle           = "@ai-sdk/google"
	openCodeNPMGoogleVertex     = "@ai-sdk/google-vertex"
	openCodeNPMAmazonBedrock    = "@ai-sdk/amazon-bedrock"
	openCodeNPMBedrockMantle    = "@ai-sdk/amazon-bedrock/mantle"
	openCodeNPMCerebras         = "@ai-sdk/cerebras"
	openCodeNPMTogetherAI       = "@ai-sdk/togetherai"
	openCodeNPMXAI              = "@ai-sdk/xai"
	openCodeNPMDeepInfra        = "@ai-sdk/deepinfra"
	openCodeNPMVenice           = "venice-ai-sdk-provider"
	openCodeNPMMistral          = "@ai-sdk/mistral"
	openCodeNPMCohere           = "@ai-sdk/cohere"
	openCodeNPMGroq             = "@ai-sdk/groq"
	openCodeNPMPerplexity       = "@ai-sdk/perplexity"
	openCodeNPMSAP              = "@jerome-benoit/sap-ai-provider-v2"
	openCodeNoneEffortRelease   = "2025-11-13"
	openCodeXHighEffortRelease  = "2025-12-04"
)

var (
	openCodeGPT5FamilyRE       = regexp.MustCompile(`(?:^|/)gpt-5(?:[.-]|$)`)
	openCodeGPT5VersionRE      = regexp.MustCompile(`(?:^|/)gpt-5[.-](\d+)(?:[.-]|$)`)
	openCodeGPT5ProRE          = regexp.MustCompile(`(?:^|/)gpt-5[.-]?pro(?:[.-]|$)`)
	openCodeGPT5VersionedProRE = regexp.MustCompile(`(?:^|/)gpt-5[.-]\d+[.-]pro(?:[.-]|$)`)
	openCodeAnthropicOpusRE    = regexp.MustCompile(`(?i)opus-(\d+)[.-](\d+)(?:[.@-]|$)|claude-(\d+)[.-](\d+)-opus(?:[.@-]|$)`)
	openCodeSAPReasoningRE     = regexp.MustCompile(`\bo[1-9]`)
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

func BaseOptionsForProvider(providerName string, provider config.ProviderConfig, model string) map[string]any {
	desc := openCodeDescriptor(providerName, provider, model)
	modelCfg := provider.Models[strings.TrimSpace(model)]
	result := CloneOptions(modelCfg.Options)
	if result == nil {
		result = map[string]any{}
	}

	if desc.APINPM == openCodeNPMVertexAnthropic || (desc.APINPM == openCodeNPMAnthropic && !strings.Contains(desc.APIID, "claude")) {
		result["toolStreaming"] = false
	}
	if desc.ProviderID == "openai" || desc.APINPM == openCodeNPMOpenAI || desc.APINPM == openCodeNPMGithubCopilot || desc.APINPM == openCodeNPMBedrockMantle {
		result["store"] = false
	}
	if desc.APINPM == openCodeNPMAzure {
		result["store"] = false
	}
	if desc.APINPM == openCodeNPMOpenRouter || desc.APINPM == openCodeNPMLLMGateway {
		result["usage"] = map[string]any{"include": true}
		if strings.Contains(desc.APIID, "gemini-3") {
			result["reasoning"] = map[string]any{"effort": "high"}
		}
	}
	if desc.ProviderID == "baseten" || (desc.ProviderID == "opencode" && (desc.APIID == "kimi-k2-thinking" || desc.APIID == "glm-4.6")) {
		result["chat_template_args"] = map[string]any{"enable_thinking": true}
	}
	if (strings.Contains(desc.ProviderID, "zai") || strings.Contains(desc.ProviderID, "zhipuai")) && desc.APINPM == openCodeNPMOpenAICompatible {
		result["thinking"] = map[string]any{
			"type":           "enabled",
			"clear_thinking": false,
		}
	}
	if desc.APINPM == openCodeNPMGoogle || desc.APINPM == openCodeNPMGoogleVertex {
		if desc.Reasoning {
			thinking := map[string]any{"includeThoughts": true}
			if strings.Contains(desc.APIID, "gemini-3") {
				thinking["thinkingLevel"] = "high"
			}
			result["thinkingConfig"] = thinking
		}
	}
	if strings.Contains(desc.APIID, "minimax-m3") && desc.APINPM == openCodeNPMAnthropic {
		result["thinking"] = map[string]any{"type": "adaptive"}
	}
	if (desc.APINPM == openCodeNPMAnthropic || desc.APINPM == openCodeNPMVertexAnthropic) &&
		(strings.Contains(desc.APIID, "k2p") || strings.Contains(desc.APIID, "kimi-k2.") || strings.Contains(desc.APIID, "kimi-k2p")) {
		result["thinking"] = map[string]any{
			"type":         "enabled",
			"budgetTokens": openCodeAnthropicHighBudget(desc.OutputLimit),
		}
	}
	if desc.ProviderID == "alibaba-cn" && desc.Reasoning && desc.APINPM == openCodeNPMOpenAICompatible && !strings.Contains(desc.APIID, "kimi-k2-thinking") {
		result["enable_thinking"] = true
	}
	if desc.APINPM == openCodeNPMAzure && strings.Contains(desc.APIID, "gpt-5.5") {
		result["reasoningSummary"] = "auto"
		return nilIfEmpty(result)
	}
	if strings.Contains(desc.APIID, "gpt-5") && !strings.Contains(desc.APIID, "gpt-5-chat") {
		if !strings.Contains(desc.APIID, "gpt-5-pro") {
			result["reasoningEffort"] = "medium"
			if desc.APINPM == openCodeNPMOpenAI || desc.APINPM == openCodeNPMAzure || desc.APINPM == openCodeNPMGithubCopilot || desc.APINPM == openCodeNPMBedrockMantle {
				result["reasoningSummary"] = "auto"
			}
			if desc.APINPM == openCodeNPMOpenAI || desc.APINPM == openCodeNPMBedrockMantle {
				result["include"] = []any{"reasoning.encrypted_content"}
			}
		}
		if strings.Contains(desc.APIID, "gpt-5.") &&
			!strings.Contains(desc.APIID, "codex") &&
			!strings.Contains(desc.APIID, "-chat") &&
			desc.ProviderID != "azure" {
			result["textVerbosity"] = "low"
		}
		if strings.HasPrefix(desc.ProviderID, "opencode") {
			result["include"] = []any{"reasoning.encrypted_content"}
			result["reasoningSummary"] = "auto"
		}
	}
	if desc.APINPM == openCodeNPMGateway {
		result["gateway"] = map[string]any{"caching": "auto"}
	}
	return nilIfEmpty(result)
}

func inferredOptionsForProvider(providerName string, provider config.ProviderConfig, model string) map[string]map[string]any {
	desc := openCodeDescriptor(providerName, provider, model)
	if !desc.Reasoning {
		return nil
	}
	id := desc.ModelID
	apiID := desc.APIID
	adaptiveEfforts := openCodeAnthropicAdaptiveEfforts(apiID)
	if strings.Contains(desc.APIID, "minimax-m3") &&
		(desc.APINPM == openCodeNPMAnthropic || desc.APINPM == openCodeNPMOpenAICompatible) {
		return map[string]map[string]any{
			"none":     {"thinking": map[string]any{"type": "disabled"}},
			"thinking": {"thinking": map[string]any{"type": "adaptive"}},
		}
	}
	if openCodeExcludedReasoningModel(id) {
		return nil
	}
	if strings.Contains(id, "grok") && strings.Contains(id, "grok-3-mini") {
		if desc.APINPM == openCodeNPMOpenRouter {
			return openCodeVariantsFromEfforts([]string{"low", "high"}, func(effort string) map[string]any {
				return map[string]any{"reasoning": map[string]any{"effort": effort}}
			})
		}
		return openCodeReasoningEffortVariants([]string{"low", "high"})
	}
	if strings.Contains(id, "grok") {
		return nil
	}

	switch desc.APINPM {
	case openCodeNPMOpenRouter:
		efforts := openCodeWidelySupportedEfforts()
		if strings.HasPrefix(apiID, "openai/") || strings.Contains(id, "gpt") {
			efforts = openCodeCompatibleReasoningEfforts(id)
		}
		return openCodeVariantsFromEfforts(efforts, func(effort string) map[string]any {
			return map[string]any{"reasoning": map[string]any{"effort": effort}}
		})
	case openCodeNPMAIGateway:
		if strings.HasPrefix(apiID, "openai/") {
			return openCodeReasoningEffortVariants(openCodeReasoningEfforts(apiID, desc.ReleaseDate))
		}
		return openCodeReasoningEffortVariants(openCodeWidelySupportedEfforts())
	case openCodeNPMGateway:
		if strings.Contains(id, "anthropic") {
			return openCodeAnthropicVariants(desc, adaptiveEfforts, false)
		}
		if strings.Contains(id, "google") {
			return openCodeGatewayGoogleVariants(id)
		}
		return openCodeReasoningEffortVariants(openCodeCompatibleReasoningEfforts(apiID))
	case openCodeNPMGithubCopilot:
		if strings.Contains(id, "gemini") {
			return nil
		}
		if strings.Contains(id, "claude") {
			return openCodeReasoningEffortVariants(openCodeWidelySupportedEfforts())
		}
		efforts := append([]string{}, openCodeWidelySupportedEfforts()...)
		if strings.Contains(id, "5.1-codex-max") || strings.Contains(id, "5.2") || strings.Contains(id, "5.3") {
			efforts = append(efforts, "xhigh")
		} else if strings.Contains(id, "gpt-5") && desc.ReleaseDate >= openCodeXHighEffortRelease {
			efforts = append(efforts, "xhigh")
		}
		return openCodeVariantsFromEfforts(efforts, openCodeOpenAIProviderVariantOptions)
	case openCodeNPMCerebras, openCodeNPMTogetherAI, openCodeNPMXAI, openCodeNPMDeepInfra, openCodeNPMVenice, openCodeNPMOpenAICompatible:
		efforts := append([]string{}, openCodeWidelySupportedEfforts()...)
		if strings.Contains(apiID, "deepseek-v4") {
			efforts = append(efforts, "max")
		}
		return openCodeReasoningEffortVariants(efforts)
	case openCodeNPMAzure:
		if id == "o1-mini" {
			return nil
		}
		efforts := openCodeWidelySupportedEfforts()
		if openCodeGPT5FamilyRE.MatchString(id) && openCodeGPT5Version(id) == 0 {
			efforts = append([]string{"minimal"}, efforts...)
		}
		return openCodeVariantsFromEfforts(efforts, openCodeOpenAIProviderVariantOptions)
	case openCodeNPMOpenAI, openCodeNPMBedrockMantle:
		return openCodeVariantsFromEfforts(openCodeReasoningEfforts(apiID, desc.ReleaseDate), openCodeOpenAIProviderVariantOptions)
	case openCodeNPMAnthropic, openCodeNPMVertexAnthropic:
		return openCodeAnthropicVariants(desc, adaptiveEfforts, true)
	case openCodeNPMAmazonBedrock:
		return openCodeBedrockVariants(apiID, adaptiveEfforts)
	case openCodeNPMGoogleVertex, openCodeNPMGoogle:
		return openCodeGoogleVariants(id)
	case openCodeNPMMistral:
		mistralID := strings.ToLower(apiID)
		if strings.Contains(mistralID, "mistral-small-2603") ||
			strings.Contains(mistralID, "mistral-small-latest") ||
			strings.Contains(mistralID, "mistral-medium-3.5") ||
			strings.Contains(mistralID, "mistral-medium-2604") {
			return map[string]map[string]any{"high": {"reasoningEffort": "high"}}
		}
		return nil
	case openCodeNPMCohere, openCodeNPMPerplexity:
		return nil
	case openCodeNPMGroq:
		return openCodeReasoningEffortVariants(append([]string{"none"}, openCodeWidelySupportedEfforts()...))
	case openCodeNPMSAP:
		return openCodeSAPVariants(desc, adaptiveEfforts)
	default:
		return nil
	}
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

func openCodeExcludedReasoningModel(id string) bool {
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
	for _, value := range excluded {
		if strings.Contains(id, value) {
			return true
		}
	}
	return false
}

func openCodeWidelySupportedEfforts() []string {
	return []string{"low", "medium", "high"}
}

func openCodeEfforts() []string {
	return []string{"none", "minimal", "low", "medium", "high", "xhigh"}
}

func openCodeReasoningEfforts(apiID, releaseDate string) []string {
	id := strings.ToLower(apiID)
	if strings.Contains(id, "deep-research") {
		return []string{"medium"}
	}
	if efforts, ok := openCodeGPT5ChatReasoningEfforts(id); ok {
		return efforts
	}
	if openCodeGPT5ProRE.MatchString(id) {
		return []string{"high"}
	}
	if efforts, ok := openCodeGPT5CodexReasoningEfforts(id); ok {
		return efforts
	}
	if efforts, ok := openCodeVersionedGPT5ReasoningEfforts(id); ok {
		return efforts
	}
	efforts := append([]string{}, openCodeWidelySupportedEfforts()...)
	if openCodeGPT5FamilyRE.MatchString(id) {
		efforts = append([]string{"minimal"}, efforts...)
	}
	if releaseDate >= openCodeNoneEffortRelease {
		efforts = append([]string{"none"}, efforts...)
	}
	if releaseDate >= openCodeXHighEffortRelease {
		efforts = append(efforts, "xhigh")
	}
	return efforts
}

func openCodeCompatibleReasoningEfforts(id string) []string {
	apiID := strings.ToLower(id)
	if efforts, ok := openCodeGPT5ChatReasoningEfforts(apiID); ok {
		return efforts
	}
	if openCodeGPT5ProRE.MatchString(apiID) {
		return []string{"high"}
	}
	if efforts, ok := openCodeGPT5CodexReasoningEfforts(apiID); ok {
		return efforts
	}
	if efforts, ok := openCodeVersionedGPT5ReasoningEfforts(apiID); ok {
		return efforts
	}
	return openCodeEfforts()
}

func openCodeGPT5Version(apiID string) int {
	match := openCodeGPT5VersionRE.FindStringSubmatch(apiID)
	if len(match) != 2 {
		return 0
	}
	switch match[1] {
	case "1":
		return 1
	case "2":
		return 2
	case "3":
		return 3
	case "4":
		return 4
	case "5":
		return 5
	default:
		return 99
	}
}

func openCodeVersionedGPT5ReasoningEfforts(apiID string) ([]string, bool) {
	if openCodeGPT5VersionedProRE.MatchString(apiID) {
		return []string{"medium", "high", "xhigh"}, true
	}
	version := openCodeGPT5Version(apiID)
	if version == 0 {
		return nil, false
	}
	if version == 1 {
		return []string{"none", "low", "medium", "high"}, true
	}
	return []string{"none", "low", "medium", "high", "xhigh"}, true
}

func openCodeGPT5CodexReasoningEfforts(apiID string) ([]string, bool) {
	if !openCodeGPT5FamilyRE.MatchString(apiID) || !strings.Contains(apiID, "codex") {
		return nil, false
	}
	version := openCodeGPT5Version(apiID)
	if version >= 3 {
		return []string{"none", "low", "medium", "high", "xhigh"}, true
	}
	if strings.Contains(apiID, "codex-max") || version >= 2 {
		return []string{"low", "medium", "high", "xhigh"}, true
	}
	return openCodeWidelySupportedEfforts(), true
}

func openCodeGPT5ChatReasoningEfforts(apiID string) ([]string, bool) {
	if !openCodeGPT5FamilyRE.MatchString(apiID) || !strings.Contains(apiID, "-chat") {
		return nil, false
	}
	if openCodeGPT5Version(apiID) == 0 {
		return nil, true
	}
	return []string{"medium"}, true
}

func openCodeAnthropicAdaptiveEfforts(apiID string) []string {
	if openCodeAnthropicOpus47OrLater(apiID) {
		return []string{"low", "medium", "high", "xhigh", "max"}
	}
	if strings.Contains(apiID, "opus-4-6") || strings.Contains(apiID, "opus-4.6") ||
		strings.Contains(apiID, "4-6-opus") || strings.Contains(apiID, "4.6-opus") ||
		strings.Contains(apiID, "sonnet-4-6") || strings.Contains(apiID, "sonnet-4.6") ||
		strings.Contains(apiID, "4-6-sonnet") || strings.Contains(apiID, "4.6-sonnet") {
		return []string{"low", "medium", "high", "max"}
	}
	return nil
}

func openCodeAnthropicOpus47OrLater(apiID string) bool {
	match := openCodeAnthropicOpusRE.FindStringSubmatch(apiID)
	if len(match) == 0 {
		return false
	}
	major, minor := 0, 0
	if match[1] != "" {
		major = parseSmallVersion(match[1])
		minor = parseSmallVersion(match[2])
	} else {
		major = parseSmallVersion(match[3])
		minor = parseSmallVersion(match[4])
	}
	return major > 4 || (major == 4 && minor >= 7)
}

func parseSmallVersion(value string) int {
	switch value {
	case "0":
		return 0
	case "1":
		return 1
	case "2":
		return 2
	case "3":
		return 3
	case "4":
		return 4
	case "5":
		return 5
	case "6":
		return 6
	case "7":
		return 7
	case "8":
		return 8
	case "9":
		return 9
	default:
		return 10
	}
}

func nilIfEmpty(options map[string]any) map[string]any {
	if len(options) == 0 {
		return nil
	}
	return options
}

func openCodeGoogleThinkingLevelEfforts(apiID string) []string {
	id := strings.ToLower(apiID)
	if !strings.Contains(id, "gemini-3") {
		return []string{"low", "high"}
	}
	if strings.Contains(id, "flash-image") {
		return []string{"minimal", "high"}
	}
	if strings.Contains(id, "pro-image") {
		return []string{"high"}
	}
	if strings.Contains(id, "flash") {
		return []string{"minimal", "low", "medium", "high"}
	}
	return []string{"low", "medium", "high"}
}

func openCodeGoogleThinkingBudgetMax(apiID string) int {
	id := strings.ToLower(apiID)
	if strings.Contains(id, "2.5") && strings.Contains(id, "pro") && !strings.Contains(id, "flash") {
		return 32768
	}
	return 24576
}

func openCodeVariantsFromEfforts(efforts []string, build func(string) map[string]any) map[string]map[string]any {
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
	if len(out) == 0 {
		return nil
	}
	return out
}

func openCodeReasoningEffortVariants(efforts []string) map[string]map[string]any {
	return openCodeVariantsFromEfforts(efforts, func(effort string) map[string]any {
		return map[string]any{"reasoningEffort": effort}
	})
}

func openCodeOpenAIProviderVariantOptions(effort string) map[string]any {
	return map[string]any{
		"reasoningEffort":  effort,
		"reasoningSummary": "auto",
		"include":          []any{"reasoning.encrypted_content"},
	}
}

func openCodeAnthropicVariants(desc openCodeModelDescriptor, adaptiveEfforts []string, githubCopilotFilter bool) map[string]map[string]any {
	if len(adaptiveEfforts) > 0 {
		efforts := append([]string{}, adaptiveEfforts...)
		if githubCopilotFilter && desc.ProviderID == "github-copilot" {
			if strings.Contains(desc.APIID, "opus-4.7") {
				efforts = []string{"medium"}
			}
			filtered := make([]string, 0, len(efforts))
			for _, effort := range efforts {
				if effort != "max" && effort != "xhigh" {
					filtered = append(filtered, effort)
				}
			}
			efforts = filtered
		}
		return openCodeVariantsFromEfforts(efforts, func(effort string) map[string]any {
			thinking := map[string]any{"type": "adaptive"}
			if openCodeAnthropicOpus47OrLater(desc.APIID) {
				thinking["display"] = "summarized"
			}
			return map[string]any{
				"thinking": thinking,
				"effort":   effort,
			}
		})
	}
	if strings.Contains(desc.APIID, "opus-4-5") || strings.Contains(desc.APIID, "opus-4.5") {
		return openCodeVariantsFromEfforts(openCodeWidelySupportedEfforts(), func(effort string) map[string]any {
			return map[string]any{"effort": effort}
		})
	}
	return map[string]map[string]any{
		"high": {"thinking": map[string]any{"type": "enabled", "budgetTokens": openCodeAnthropicHighBudget(desc.OutputLimit)}},
		"max":  {"thinking": map[string]any{"type": "enabled", "budgetTokens": openCodeAnthropicMaxBudget(desc.OutputLimit)}},
	}
}

func openCodeAnthropicHighBudget(outputLimit int) int {
	if outputLimit <= 0 {
		return 16000
	}
	return minInt(16000, outputLimit/2-1)
}

func openCodeAnthropicMaxBudget(outputLimit int) int {
	if outputLimit <= 0 {
		return 31999
	}
	return minInt(31999, outputLimit-1)
}

func openCodeBedrockVariants(apiID string, adaptiveEfforts []string) map[string]map[string]any {
	if len(adaptiveEfforts) > 0 {
		return openCodeVariantsFromEfforts(adaptiveEfforts, func(effort string) map[string]any {
			reasoning := map[string]any{
				"type":               "adaptive",
				"maxReasoningEffort": effort,
			}
			if openCodeAnthropicOpus47OrLater(apiID) {
				reasoning["display"] = "summarized"
			}
			return map[string]any{"reasoningConfig": reasoning}
		})
	}
	if strings.Contains(apiID, "anthropic") {
		return map[string]map[string]any{
			"high": {"reasoningConfig": map[string]any{"type": "enabled", "budgetTokens": 16000}},
			"max":  {"reasoningConfig": map[string]any{"type": "enabled", "budgetTokens": 31999}},
		}
	}
	return openCodeVariantsFromEfforts(openCodeWidelySupportedEfforts(), func(effort string) map[string]any {
		return map[string]any{"reasoningConfig": map[string]any{"type": "enabled", "maxReasoningEffort": effort}}
	})
}

func openCodeGatewayGoogleVariants(id string) map[string]map[string]any {
	if strings.Contains(id, "2.5") {
		return map[string]map[string]any{
			"high": {"thinkingConfig": map[string]any{"includeThoughts": true, "thinkingBudget": 16000}},
			"max":  {"thinkingConfig": map[string]any{"includeThoughts": true, "thinkingBudget": openCodeGoogleThinkingBudgetMax(id)}},
		}
	}
	return openCodeVariantsFromEfforts([]string{"low", "high"}, func(effort string) map[string]any {
		return map[string]any{"includeThoughts": true, "thinkingLevel": effort}
	})
}

func openCodeGoogleVariants(id string) map[string]map[string]any {
	if strings.Contains(id, "2.5") {
		return map[string]map[string]any{
			"high": {"thinkingConfig": map[string]any{"includeThoughts": true, "thinkingBudget": 16000}},
			"max":  {"thinkingConfig": map[string]any{"includeThoughts": true, "thinkingBudget": openCodeGoogleThinkingBudgetMax(id)}},
		}
	}
	return openCodeVariantsFromEfforts(openCodeGoogleThinkingLevelEfforts(id), func(effort string) map[string]any {
		return map[string]any{"thinkingConfig": map[string]any{"includeThoughts": true, "thinkingLevel": effort}}
	})
}

func openCodeSAPVariants(desc openCodeModelDescriptor, adaptiveEfforts []string) map[string]map[string]any {
	if strings.Contains(desc.APIID, "anthropic") {
		if len(adaptiveEfforts) > 0 {
			return openCodeWrapInSAPModelParams(openCodeVariantsFromEfforts(adaptiveEfforts, func(effort string) map[string]any {
				thinking := map[string]any{"type": "adaptive"}
				if openCodeAnthropicOpus47OrLater(desc.APIID) {
					thinking["display"] = "summarized"
				}
				return map[string]any{
					"thinking":      thinking,
					"output_config": map[string]any{"effort": effort},
				}
			}))
		}
		return openCodeWrapInSAPModelParams(map[string]map[string]any{
			"high": {"thinking": map[string]any{"type": "enabled", "budget_tokens": 16000}},
			"max":  {"thinking": map[string]any{"type": "enabled", "budget_tokens": 31999}},
		})
	}
	if strings.Contains(desc.APIID, "gemini") && strings.Contains(desc.APIID, "2.5") {
		return openCodeWrapInSAPModelParams(map[string]map[string]any{
			"high": {"thinkingConfig": map[string]any{"includeThoughts": true, "thinkingBudget": 16000}},
			"max":  {"thinkingConfig": map[string]any{"includeThoughts": true, "thinkingBudget": openCodeGoogleThinkingBudgetMax(desc.APIID)}},
		})
	}
	if strings.Contains(desc.APIID, "gpt") || openCodeSAPReasoningRE.MatchString(desc.APIID) {
		return openCodeWrapInSAPModelParams(openCodeVariantsFromEfforts(
			openCodeReasoningEfforts(desc.APIID, desc.ReleaseDate),
			func(effort string) map[string]any {
				return map[string]any{"reasoning_effort": effort}
			},
		))
	}
	return openCodeWrapInSAPModelParams(openCodeVariantsFromEfforts(openCodeWidelySupportedEfforts(), func(effort string) map[string]any {
		return map[string]any{"reasoning_effort": effort}
	}))
}

func openCodeWrapInSAPModelParams(variants map[string]map[string]any) map[string]map[string]any {
	if len(variants) == 0 {
		return nil
	}
	out := make(map[string]map[string]any, len(variants))
	for key, value := range variants {
		out[key] = map[string]any{"modelParams": value}
	}
	return out
}

func minInt(left, right int) int {
	if right < left {
		return right
	}
	return left
}
