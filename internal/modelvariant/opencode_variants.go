package modelvariant

import "strings"

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
