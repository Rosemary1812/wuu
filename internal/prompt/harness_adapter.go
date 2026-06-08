package prompt

import (
	"fmt"
	"strings"
)

type HarnessFamily string

const (
	HarnessFamilyPortable HarnessFamily = "portable"
	HarnessFamilyClaude   HarnessFamily = "claude"
	HarnessFamilyCodex    HarnessFamily = "codex"
	HarnessFamilyGPT      HarnessFamily = "gpt"
	HarnessFamilyGemini   HarnessFamily = "gemini"
	HarnessFamilyKimi     HarnessFamily = "kimi"
)

func HarnessFamilyForModel(providerName, model string) HarnessFamily {
	id := strings.ToLower(strings.TrimSpace(providerName + "/" + model))
	switch {
	case strings.Contains(id, "claude") || strings.Contains(id, "anthropic"):
		return HarnessFamilyClaude
	case strings.Contains(id, "codex"):
		return HarnessFamilyCodex
	case strings.Contains(id, "gpt") || strings.Contains(id, "openai"):
		return HarnessFamilyGPT
	case strings.Contains(id, "gemini"):
		return HarnessFamilyGemini
	case strings.Contains(id, "kimi") || strings.Contains(id, "moonshot"):
		return HarnessFamilyKimi
	default:
		return HarnessFamilyPortable
	}
}

func (b *Builder) AddHarnessAdapter(providerName, model string) {
	text := HarnessAdapterText(providerName, model)
	if strings.TrimSpace(text) == "" {
		return
	}
	b.AddSection("harness_adapter", text, true)
}

func HarnessAdapterText(providerName, model string) string {
	providerName = strings.TrimSpace(providerName)
	model = strings.TrimSpace(model)
	if providerName == "" && model == "" {
		return ""
	}
	family := HarnessFamilyForModel(providerName, model)
	header := "# Harness Adapter\n\n" +
		fmt.Sprintf("Provider/model: %s/%s. Treat model behavior, prompts, tools, and subagents as one harness. Follow the model-family guidance below when choosing workflow, subagent, and tool style.\n\n", providerName, model)
	switch family {
	case HarnessFamilyClaude:
		return header + strings.Join([]string{
			"- Prefer the script driver for executable workflow definitions: load the saved script, call run_workflow, and let the script manage phase/spawn/await/synthesize state under the same Workflow Run and Workflow Team model.",
			"- Use subagents for parallel research, verification, and isolated implementation when the worker brief is explicit and self-contained.",
			"- Keep workflow and worker state in Workflow Run state, Agent Reports, and artifacts instead of carrying long-running state only in chat.",
		}, "\n")
	case HarnessFamilyCodex, HarnessFamilyGPT:
		return header + strings.Join([]string{
			"- Prefer compact, tool-contract-driven control flow: inspect, plan only when useful, execute, verify, and report.",
			"- Use workflow drivers only when durable state, scheduling, repeatability, or multi-agent coordination matters; do not turn ordinary implementation into workflow ceremony.",
			"- Use specialized modes such as review or goal tracking as separate task modes when available instead of folding every task into workflow state.",
		}, "\n")
	case HarnessFamilyGemini, HarnessFamilyKimi:
		return header + strings.Join([]string{
			"- Treat tool schemas and tool descriptions as the source of truth; avoid relying on model-family-specific aliases or hidden conventions.",
			"- Prefer portable primitives: spawn_agent for delegated workers, run_workflow for the script driver, create_workflow for agent-managed workflow state.",
			"- Keep prompts explicit about required inputs, expected outputs, and tool result handling.",
		}, "\n")
	default:
		return header + strings.Join([]string{
			"- Use the portable harness path: follow tool descriptions exactly and avoid model-family-specific assumptions.",
			"- Prefer generic primitives: spawn_agent for delegated workers, run_workflow for the script driver, create_workflow for agent-managed workflow state.",
			"- Keep task briefs explicit and self-contained when delegation or workflow execution depends on another agent.",
		}, "\n")
	}
}
