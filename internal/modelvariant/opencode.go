package modelvariant

import (
	"strings"

	"github.com/blueberrycongee/wuu/internal/config"
)

// OpenCode compatibility rules are aligned with
// sst/opencode@671d1937867fc85c0d300a47faba371b5eab5e54
// packages/opencode/src/provider/transform.ts.
//
// Keep runtime support separate from catalog/UI parity: some option bundles
// here are exposed so copied OpenCode/models.dev configs keep the same shape,
// even when wuu's current Go clients do not consume that provider package.
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
